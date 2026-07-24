import { Button, ControlStrip, Stack, Status, Text } from "@open-cut/components";
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
import { formatClock, formatTime } from "./creator-workspace-presentation.js";

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
  const [error, setError] = useState(undefined as string | undefined);
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
      setError("Sequence preview is not ready. Try Capture playhead again.");
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
      setError("Set Source In/Out and capture a Sequence playhead before placing.");
      return;
    }
    if (destination.sequenceRevision !== sequenceSnapshot.pinnedRevision) {
      setError("The Sequence changed. Capture the playhead again.");
      return;
    }
    const videoTrack = videoTrackId ? videoTracks.find((track) => track.id === videoTrackId) : undefined;
    const audioTrack = audioTrackId ? audioTracks.find((track) => track.id === audioTrackId) : undefined;
    if ((!videoTrack || !selection.videoStreamId) && (!audioTrack || !selection.audioStreamId)) {
      setError("Select at least one compatible Video or Audio lane.");
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
      setError(placementFailureMessage(value, false));
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
      setError(placementFailureMessage(value, true));
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
      setError(placementFailureMessage(value, true));
      setBusy(false);
      return;
    }
    await finishCommit(receipt, pendingApply.review);
  };

  const finishCommit = async (receipt: CreatorEditCommit, committedReview: CreatorClipPlacementReview) => {
    acceptCommit(receipt, committedReview);
    try {
      await onCommitted(receipt);
    } catch {
      setError("Source was placed, but the workspace could not refresh. Choose Sync now to reload it.");
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
  const hasVideoLane = Boolean(selection?.videoStreamId && videoTrackId);
  const hasAudioLane = Boolean(selection?.audioStreamId && audioTrackId);
  const rangeFitsCoverage = sourceViewer.selectedRangeFitsCoverage({ video: hasVideoLane, audio: hasAudioLane });
  const coverageConflict = Boolean(sourceRange && (hasVideoLane || hasAudioLane) && !rangeFitsCoverage);
  const canPlace = Boolean(
    selection && sourceRange && destination && !destinationStale && (hasVideoLane || hasAudioLane) && rangeFitsCoverage,
  );
  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">PLACE SOURCE</Text>
      <ControlStrip
        hint={
          destination
            ? `AT ${formatClock(destination.playhead)} · r${destination.sequenceRevision}${
                destinationStale ? " · STALE" : ""
              }`
            : "AT · NOT SET"
        }
        label="Source placement target"
        summary={
          sourceRange
            ? `IN ${formatClock(sourceRange.start)} · ${formatTime(sourceRange.duration)}s`
            : "IN / OUT · NOT SET"
        }
      >
        <Button onPress={captureDestination}>Capture playhead</Button>
      </ControlStrip>
      {selection?.videoStreamId ? (
        <TrackSelection label="VIDEO" onChange={setVideoTrackId} selected={videoTrackId} tracks={videoTracks} />
      ) : null}
      {selection?.audioStreamId ? (
        <TrackSelection label="AUDIO" onChange={setAudioTrackId} selected={audioTrackId} tracks={audioTracks} />
      ) : null}
      {coverageConflict ? (
        <Text>Marked range falls outside selected A/V coverage. Adjust In/Out or clear the incompatible lane.</Text>
      ) : null}
      <Button disabled={busy || !canPlace} variant="primary" onPress={() => void place()}>
        {busy ? "Placing…" : destination ? `Place at ${formatClock(destination.playhead)}` : "Place source"}
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
      {error ? <Status state="unavailable">{error}</Status> : null}
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
  const selectedTrack = tracks.find((track) => track.id === selected);
  return (
    <ControlStrip
      hint={selectedTrack ? `${selectedTrack.label} · r${selectedTrack.revision}` : "NOT SELECTED"}
      label={`${label} placement lane`}
      summary={label}
    >
      <Button disabled={!selected} onPress={() => onChange(undefined)}>
        Clear
      </Button>
      {tracks.map((track) => (
        <Button key={track.id} pressed={selected === track.id} onPress={() => onChange(track.id)}>
          {selected === track.id ? "Selected · " : ""}
          {track.label} · r{track.revision}
        </Button>
      ))}
      {tracks.length === 0 ? <Text>Create a compatible Track separately before placement.</Text> : null}
    </ControlStrip>
  );
}

function asError(value: unknown): Error {
  return value instanceof Error ? value : new Error(String(value));
}

function placementFailureMessage(value: unknown, applying: boolean): string {
  const error = asError(value);
  if (error instanceof CreatorEditError && error.code === "conflict") {
    return "The Source or Sequence changed. Choose Sync now, then capture the playhead again.";
  }
  return applying
    ? "Could not confirm the source placement."
    : "Could not prepare this source placement. Review the marked range and lanes, then try again.";
}
