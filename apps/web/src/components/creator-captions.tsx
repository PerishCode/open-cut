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
import { formatTime, formatTimeEnd } from "./creator-workspace-presentation.js";

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
  const [validationError, setValidationError] = useState<Error>();

  useEffect(() => {
    controller.setProjection({ projectId, sequenceId, source, clips, alignments, tracks });
  }, [alignments, clips, controller, projectId, sequenceId, source, tracks]);
  useEffect(() => () => controller.close(), [controller]);

  const run = useCallback(
    async (operation: () => Promise<CreatorEditCommit | undefined>) => {
      setValidationError(undefined);
      try {
        const receipt = await operation();
        if (receipt) await onCommitted(receipt);
      } catch (value) {
        setValidationError(value instanceof Error ? value : new Error(String(value)));
      }
    },
    [onCommitted],
  );
  const preview = useCallback(async () => {
    setValidationError(undefined);
    try {
      await controller.preview();
    } catch (value) {
      setValidationError(value instanceof Error ? value : new Error(String(value)));
    }
  }, [controller]);

  const busy = snapshot.phase === "previewing" || snapshot.phase === "applying";
  const canPreview =
    snapshot.source?.evidenceStatus === "exact" &&
    snapshot.selectedClip !== undefined &&
    snapshot.selectedTrack !== undefined &&
    !busy;

  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">CAPTION DRAFT · STORY EXCERPT</Text>
      <Text>Choose one exact Story excerpt, one committed Clip instance, and one Caption Track.</Text>
      {!snapshot.source ? <Text>Select “Create captions” on an excerpt in Story to begin.</Text> : null}
      {snapshot.source ? (
        <Stack spacing="compact">
          <Text tone="eyebrow">
            SOURCE · {snapshot.source.evidenceStatus.toUpperCase()} · r{snapshot.source.sourceExcerpt.revision}
          </Text>
          <Text>{snapshot.source.sourceExcerpt.effectiveText}</Text>
          {snapshot.source.evidenceStatus === "stale" ? (
            <Status state="unavailable">Excerpt evidence is stale · repair it before creating captions</Status>
          ) : null}
        </Stack>
      ) : null}

      {snapshot.source && snapshot.clipCandidates.length > 1 ? (
        <Text>Multiple compatible Clips · recommendation is informational; choose one explicitly.</Text>
      ) : null}
      {snapshot.clipCandidates.map((candidate) => (
        <Button disabled={busy} key={candidate.clip.id} onPress={() => controller.selectClip(candidate.clip.id)}>
          {candidate.clip.id === snapshot.selectedClip?.id ? "✓ " : ""}Choose Clip {candidate.clip.id} ·{" "}
          {candidateLabel(candidate.recommendation)} · {formatTime(candidate.clip.timelineRange.start)} →{" "}
          {formatTimeEnd(candidate.clip.timelineRange)}
        </Button>
      ))}
      {snapshot.source && snapshot.clipCandidates.length === 0 ? (
        <Text>No enabled committed Clip contains this excerpt range.</Text>
      ) : null}

      {snapshot.source && snapshot.trackCandidates.length > 1 ? (
        <Text>Multiple Caption Tracks · choose the destination explicitly.</Text>
      ) : null}
      {snapshot.trackCandidates.map((track) => (
        <Button disabled={busy} key={track.id} onPress={() => controller.selectTrack(track.id)}>
          {track.id === snapshot.selectedTrack?.id ? "✓ " : ""}Choose Caption Track · {track.label} · r{track.revision}
        </Button>
      ))}
      {snapshot.source && snapshot.trackCandidates.length === 0 ? <Text>No Caption Track is available.</Text> : null}

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
                CUE {String(cue.ordinal).padStart(2, "0")} · {formatTime(cue.timelineRange.start)} →{" "}
                {formatTimeEnd(cue.timelineRange)}
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
          <Status state="unavailable">Caption operation failed · {snapshot.error.message}</Status>
          {snapshot.canRetryIdenticalApply ? (
            <Button onPress={() => void run(() => controller.retryIdenticalApply())}>
              Retry identical Caption apply
            </Button>
          ) : null}
        </Stack>
      ) : null}
      {validationError ? (
        <Status state="unavailable">Caption selection invalid · {validationError.message}</Status>
      ) : null}
    </Stack>
  );
}

function candidateLabel(value: "exact-alignment" | "source-stream" | "compatible-range"): string {
  if (value === "exact-alignment") return "recommended by exact Alignment";
  if (value === "source-stream") return "recommended by SourceStream";
  return "compatible source range";
}
