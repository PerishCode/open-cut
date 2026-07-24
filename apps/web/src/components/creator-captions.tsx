import { Button, Stack, Status, Text } from "@open-cut/components";
import {
  type Alignment,
  type Clip,
  type CreatorEditCommit,
  type DurableID,
  type Track,
  useContracts,
} from "@open-cut/contracts";
import { useCallback, useEffect, useMemo, useState, useSyncExternalStore } from "react";

import { CreatorCaptionController, type CreatorCaptionSource } from "../lib/creator-caption-controller.js";
import { formatClock, formatClockEnd } from "./creator-workspace-presentation.js";

type AsyncResult = unknown;

export function CreatorCaptions({
  alignments,
  clips,
  onCommitted,
  onReload,
  projectId,
  sequenceId,
  source,
  tracks,
}: Readonly<{
  alignments: readonly Alignment[];
  clips: readonly Clip[];
  onCommitted(receipt: CreatorEditCommit): Promise<AsyncResult>;
  onReload(): Promise<AsyncResult>;
  projectId: DurableID;
  sequenceId: DurableID;
  source?: CreatorCaptionSource;
  tracks: readonly Track[];
}>) {
  const contracts = useContracts();
  const controller = useMemo(
    () => new CreatorCaptionController(contracts.editing.captions),
    [contracts.editing.captions],
  );
  const snapshot = useSyncExternalStore(controller.subscribe, controller.getSnapshot, controller.getSnapshot);
  const [actionError, setActionError] = useState(undefined as string | undefined);

  useEffect(() => {
    controller.setProjection({ projectId, sequenceId, source, clips, alignments, tracks });
  }, [alignments, clips, controller, projectId, sequenceId, source, tracks]);
  useEffect(() => () => controller.close(), [controller]);

  const run = useCallback(
    async (operation: () => Promise<CreatorEditCommit | undefined>) => {
      setActionError(undefined);
      let receipt: CreatorEditCommit | undefined;
      try {
        receipt = await operation();
      } catch {
        setActionError("This Caption action is no longer available. Reselect the inputs and try again.");
        return;
      }
      if (!receipt) return;
      try {
        await onCommitted(receipt);
      } catch {
        setActionError("Captions were committed, but the workspace could not refresh. Choose Sync now to reload it.");
      }
    },
    [onCommitted],
  );
  const preview = useCallback(async () => {
    setActionError(undefined);
    try {
      await controller.preview();
    } catch {
      setActionError("Caption inputs changed. Reselect the Clip and Caption Track, then try again.");
    }
  }, [controller]);

  const busy = snapshot.phase === "previewing" || snapshot.phase === "applying";
  const canPreview =
    snapshot.source?.evidenceStatus === "exact" &&
    snapshot.selectedClip !== undefined &&
    snapshot.selectedTrack !== undefined &&
    !busy;

  if (!snapshot.source) {
    return <Text tone="eyebrow">STORY CAPTION DRAFT · CHOOSE CAPTIONS IN STORY</Text>;
  }

  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">CAPTION DRAFT · STORY EXCERPT</Text>
      <Text>Choose one exact Story excerpt, one committed Clip instance, and one Caption Track.</Text>
      <Stack spacing="compact">
        <Text tone="eyebrow">
          SOURCE · {snapshot.source.evidenceStatus.toUpperCase()} · r{snapshot.source.sourceExcerpt.revision}
        </Text>
        <Text>{snapshot.source.sourceExcerpt.effectiveText}</Text>
        {snapshot.source.evidenceStatus === "stale" ? (
          <Status state="unavailable">Excerpt evidence is stale · repair it before creating captions</Status>
        ) : null}
      </Stack>

      {snapshot.clipCandidates.length > 1 ? (
        <Text>Multiple compatible Clips · recommendation is informational; choose one explicitly.</Text>
      ) : null}
      {snapshot.clipCandidates.map((candidate) => (
        <Button disabled={busy} key={candidate.clip.id} onPress={() => controller.selectClip(candidate.clip.id)}>
          {candidate.clip.id === snapshot.selectedClip?.id ? "✓ " : ""}Choose Clip {candidate.clip.id} ·{" "}
          {candidateLabel(candidate.recommendation)} · {formatClock(candidate.clip.timelineRange.start)} →{" "}
          {formatClockEnd(candidate.clip.timelineRange)}
        </Button>
      ))}
      {snapshot.clipCandidates.length === 0 ? (
        <Text>No enabled committed Clip contains this excerpt range.</Text>
      ) : null}

      {snapshot.trackCandidates.length > 1 ? (
        <Text>Multiple Caption Tracks · choose the destination explicitly.</Text>
      ) : null}
      {snapshot.trackCandidates.map((track) => (
        <Button disabled={busy} key={track.id} onPress={() => controller.selectTrack(track.id)}>
          {track.id === snapshot.selectedTrack?.id ? "✓ " : ""}Choose Caption Track · {track.label} · r{track.revision}
        </Button>
      ))}
      {snapshot.trackCandidates.length === 0 ? <Text>No Caption Track is available.</Text> : null}

      <Button disabled={!canPreview} onPress={() => void preview()}>
        Preview readable captions
      </Button>
      {snapshot.phase === "previewing" ? <Status state="pending">Deriving immutable Caption review…</Status> : null}
      {snapshot.review ? (
        <Stack spacing="compact">
          <Text tone="eyebrow">
            REVIEW · {snapshot.review.cues.length} CUES · {snapshot.review.language.toUpperCase()} ·{" "}
            {snapshot.review.policy.id}
          </Text>
          <Text>
            Insert-only · {snapshot.review.preconditionCount} exact preconditions · no media work before Apply
          </Text>
          {snapshot.review.cues.map((cue) => (
            <Stack key={cue.ordinal} spacing="compact">
              <Text tone="eyebrow">
                CUE {String(cue.ordinal).padStart(2, "0")} · {formatClock(cue.timelineRange.start)} →{" "}
                {formatClockEnd(cue.timelineRange)}
              </Text>
              <Text>{cue.text}</Text>
            </Stack>
          ))}
          {snapshot.phase === "review" ? (
            <Button variant="primary" onPress={() => void run(() => controller.apply())}>
              Apply reviewed captions
            </Button>
          ) : null}
        </Stack>
      ) : null}
      {snapshot.phase === "applying" ? <Status state="pending">Applying one atomic Caption transaction…</Status> : null}
      {snapshot.phase === "success" ? <Status state="ready">Creator Caption transaction committed</Status> : null}
      {snapshot.phase === "conflict" ? (
        <Stack spacing="compact">
          <Status state="unavailable">Caption conflict · refresh and reselect Clip and Caption Track</Status>
          <Button
            onPress={() => {
              controller.clear();
              void onReload();
            }}
          >
            Refresh committed Caption inputs
          </Button>
        </Stack>
      ) : null}
      {snapshot.phase === "error" && snapshot.error ? (
        <Stack spacing="compact">
          <Status state="unavailable">
            {snapshot.canRetryIdenticalApply
              ? "Could not confirm the Caption update."
              : "Could not prepare a Caption review. Check the selected inputs and try again."}
          </Status>
          {snapshot.canRetryIdenticalApply ? (
            <Button onPress={() => void run(() => controller.retryIdenticalApply())}>
              Retry identical Caption apply
            </Button>
          ) : null}
        </Stack>
      ) : null}
      {actionError ? <Status state="unavailable">{actionError}</Status> : null}
    </Stack>
  );
}

function candidateLabel(value: "exact-alignment" | "source-stream" | "compatible-range"): string {
  if (value === "exact-alignment") return "recommended by exact Alignment";
  if (value === "source-stream") return "recommended by SourceStream";
  return "compatible source range";
}
