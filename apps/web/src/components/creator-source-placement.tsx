import { Button, Stack, Text } from "@open-cut/components";
import {
  type ApplyCreatorClipPlacementInput,
  type CreatorClipPlacementReview,
  type CreatorEditCommit,
  CreatorEditError,
  type DurableID,
  type RationalTime,
  type Track,
  useContracts,
} from "@open-cut/contracts";
import { useEffect, useMemo, useState } from "react";

import type { SequenceViewerController, SequenceViewerSnapshot } from "../lib/sequence-viewer-controller.js";
import type { SourceViewerController, SourceViewerSnapshot } from "../lib/source-viewer-controller.js";

type SequenceDestination = Readonly<{
  sequenceRevision: string;
  playhead: RationalTime;
}>;

type AsyncResult = unknown;

type PendingApply = Readonly<{
  review: CreatorClipPlacementReview;
  input: ApplyCreatorClipPlacementInput;
}>;

export function CreatorSourcePlacement({
  onCommitted,
  onShowSequence,
  sequenceId,
  sequenceSnapshot,
  sequenceViewer,
  sourceSnapshot,
  sourceViewer,
  tracks,
}: {
  onCommitted: (receipt: CreatorEditCommit) => Promise<AsyncResult>;
  onShowSequence: () => void;
  sequenceId: DurableID;
  sequenceSnapshot: SequenceViewerSnapshot;
  sequenceViewer: SequenceViewerController;
  sourceSnapshot: SourceViewerSnapshot;
  sourceViewer: SourceViewerController;
  tracks: readonly Track[];
}) {
  const contracts = useContracts();
  const [videoTrackId, setVideoTrackId] = useState<DurableID>();
  const [audioTrackId, setAudioTrackId] = useState<DurableID>();
  const [destination, setDestination] = useState<SequenceDestination>();
  const [review, setReview] = useState<CreatorClipPlacementReview>();
  const [pendingApply, setPendingApply] = useState<PendingApply>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<Error>();
  const selection = sourceSnapshot.selection;
  const sourceRange = sourceViewer.selectedRange();
  const videoTracks = useMemo(() => tracks.filter((track) => track.type === "video"), [tracks]);
  const audioTracks = useMemo(() => tracks.filter((track) => track.type === "audio"), [tracks]);

  useEffect(() => {
    setVideoTrackId(selection?.videoStreamId && videoTracks.length === 1 ? videoTracks[0]?.id : undefined);
    setAudioTrackId(selection?.audioStreamId && audioTracks.length === 1 ? audioTracks[0]?.id : undefined);
    setReview(undefined);
    setPendingApply(undefined);
  }, [audioTracks, selection?.audioStreamId, selection?.videoStreamId, videoTracks]);

  const captureDestination = () => {
    const snapshot = sequenceViewer.getSnapshot();
    if (!snapshot.pinnedRevision) {
      setError(new Error("Sequence Viewer has no pinned revision to capture"));
      return;
    }
    sourceViewer.pause();
    sequenceViewer.pause();
    setDestination({ sequenceRevision: snapshot.pinnedRevision, playhead: snapshot.playhead });
    setReview(undefined);
    setPendingApply(undefined);
    setError(undefined);
  };

  const place = async () => {
    if (!selection || !sourceRange || !destination) {
      setError(new Error("Source marks and an explicit Sequence destination are required"));
      return;
    }
    if (destination.sequenceRevision !== sequenceSnapshot.pinnedRevision) {
      setError(new Error("Captured Sequence destination is stale; capture the playhead again"));
      return;
    }
    const videoTrack = videoTrackId ? videoTracks.find((track) => track.id === videoTrackId) : undefined;
    const audioTrack = audioTrackId ? audioTracks.find((track) => track.id === audioTrackId) : undefined;
    if ((!videoTrack || !selection.videoStreamId) && (!audioTrack || !selection.audioStreamId)) {
      setError(new Error("Select at least one explicit compatible Track/SourceStream lane"));
      return;
    }
    sourceViewer.pause();
    sequenceViewer.pause();
    setBusy(true);
    setError(undefined);
    setReview(undefined);
    setPendingApply(undefined);
    let nextReview: CreatorClipPlacementReview;
    try {
      nextReview = await contracts.editing.clipPlacement.preview({
        projectId: selection.projectId,
        sequenceId,
        assetId: selection.assetId,
        assetRevision: selection.assetRevision,
        acceptedFingerprint: selection.fingerprint,
        sourceRange,
        timelineStart: destination.playhead,
        ...(videoTrack && selection.videoStreamId
          ? {
              video: {
                trackId: videoTrack.id,
                trackRevision: videoTrack.revision,
                sourceStreamId: selection.videoStreamId,
              },
            }
          : {}),
        ...(audioTrack && selection.audioStreamId
          ? {
              audio: {
                trackId: audioTrack.id,
                trackRevision: audioTrack.revision,
                sourceStreamId: selection.audioStreamId,
              },
            }
          : {}),
      });
    } catch (value) {
      setBusy(false);
      setError(asError(value));
      return;
    }
    setReview(nextReview);
    const input = {
      requestId: `ui:source-placement:${crypto.randomUUID()}`,
      intent: "Place selected source at captured Sequence playhead",
    } as const;
    let receipt: CreatorEditCommit;
    try {
      receipt = await contracts.editing.clipPlacement.apply(nextReview, input);
    } catch (value) {
      if (value instanceof CreatorEditError && value.code === "failed") {
        setPendingApply({ review: nextReview, input });
      }
      setError(asError(value));
      setBusy(false);
      return;
    }
    await finishCommit(receipt, nextReview);
  };

  const retryApply = async () => {
    if (!pendingApply) return;
    setBusy(true);
    setError(undefined);
    let receipt: CreatorEditCommit;
    try {
      receipt = await contracts.editing.clipPlacement.apply(pendingApply.review, pendingApply.input);
    } catch (value) {
      if (value instanceof CreatorEditError && value.code !== "failed") setPendingApply(undefined);
      setError(asError(value));
      setBusy(false);
      return;
    }
    await finishCommit(receipt, pendingApply.review);
  };

  const finishCommit = async (receipt: CreatorEditCommit, committedReview: CreatorClipPlacementReview) => {
    acceptCommit(receipt, committedReview);
    try {
      await onCommitted(receipt);
    } catch (value) {
      setError(new Error(`Placement committed, but workspace refresh failed: ${asError(value).message}`));
    } finally {
      setBusy(false);
    }
  };

  const acceptCommit = (receipt: CreatorEditCommit, committedReview: CreatorClipPlacementReview) => {
    const sequenceChange = receipt.changes.find((change) => change.kind === "sequence" && change.id === sequenceId);
    if (sequenceChange) {
      sequenceViewer.setAvailableRevision(sequenceChange.revision);
      sequenceViewer.adoptRevision(sequenceChange.revision);
    }
    sequenceViewer.setPlayhead(committedReview.timelineRange.start);
    sourceViewer.clearMarks();
    setVideoTrackId(undefined);
    setAudioTrackId(undefined);
    setDestination(undefined);
    setReview(undefined);
    setPendingApply(undefined);
    onShowSequence();
  };

  const destinationStale =
    destination !== undefined && destination.sequenceRevision !== sequenceSnapshot.pinnedRevision;
  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">PLACE SOURCE</Text>
      <Text>
        Source {sourceRange ? `${exact(sourceRange.start)} + ${exact(sourceRange.duration)}` : "requires In and Out"}
      </Text>
      <Button onPress={captureDestination}>Capture current Sequence playhead</Button>
      <Text>
        Destination{" "}
        {destination ? `${exact(destination.playhead)} · Sequence r${destination.sequenceRevision}` : "not captured"}
        {destinationStale ? " · stale" : ""}
      </Text>
      {selection?.videoStreamId ? (
        <TrackSelection label="VIDEO TRACK" onChange={setVideoTrackId} selected={videoTrackId} tracks={videoTracks} />
      ) : null}
      {selection?.audioStreamId ? (
        <TrackSelection label="AUDIO TRACK" onChange={setAudioTrackId} selected={audioTrackId} tracks={audioTracks} />
      ) : null}
      <Button disabled={busy || destinationStale} onPress={() => void place()}>
        {busy ? "Placing…" : "Place selected source at captured playhead"}
      </Button>
      {pendingApply ? (
        <Button disabled={busy} onPress={() => void retryApply()}>
          Retry identical apply
        </Button>
      ) : null}
      {review ? (
        <Text>
          {review.linked ? "Linked A/V" : "Single lane"} · {review.lanes.length} lane
          {review.lanes.length === 1 ? "" : "s"} · {review.preconditionCount} exact preconditions
        </Text>
      ) : null}
      {error ? <Text>{error.message}</Text> : null}
    </Stack>
  );
}

function TrackSelection({
  label,
  onChange,
  selected,
  tracks,
}: {
  label: string;
  onChange: (trackId: DurableID | undefined) => void;
  selected: DurableID | undefined;
  tracks: readonly Track[];
}) {
  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">{label}</Text>
      <Button onPress={() => onChange(undefined)}>{selected ? "Clear lane" : "No lane selected"}</Button>
      {tracks.map((track) => (
        <Button key={track.id} onPress={() => onChange(track.id)}>
          {selected === track.id ? "Selected · " : ""}
          {track.label} · r{track.revision}
        </Button>
      ))}
      {tracks.length === 0 ? <Text>Create a compatible Track separately before placement.</Text> : null}
    </Stack>
  );
}

function exact(value: RationalTime): string {
  return `${value.value}/${value.scale}s`;
}

function asError(value: unknown): Error {
  return value instanceof Error ? value : new Error(String(value));
}
