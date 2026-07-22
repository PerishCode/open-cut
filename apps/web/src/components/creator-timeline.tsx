import { Button, Stack, Status, Text, TimelineSurface } from "@open-cut/components";
import {
  type Asset,
  type Caption,
  type Clip,
  type CreatorEditCommit,
  type DurableID,
  int64String,
  type RationalTime,
  type TimeRange,
  type Track,
  useContracts,
} from "@open-cut/contracts";
import { useCallback, useEffect, useMemo, useState, useSyncExternalStore } from "react";

import { CreatorTimelineController } from "../lib/creator-timeline-controller.js";
import type { SequenceViewerController } from "../lib/sequence-viewer-controller.js";
import { formatTime, formatTimeEnd } from "./creator-workspace-presentation.js";

type AsyncResult = unknown;

export function CreatorTimeline({
  assets,
  captions,
  clips,
  frameRate,
  onCommitted,
  onContextClip,
  onReload,
  projectId,
  range,
  sequenceId,
  tracks,
  viewer,
}: Readonly<{
  assets: readonly Asset[];
  captions: readonly Caption[];
  clips: readonly Clip[];
  frameRate: RationalTime;
  onCommitted(receipt: CreatorEditCommit): Promise<AsyncResult>;
  onContextClip(clip: Clip): void;
  onReload(): Promise<AsyncResult>;
  projectId: DurableID;
  range: TimeRange;
  sequenceId: DurableID;
  tracks: readonly Track[];
  viewer: SequenceViewerController;
}>) {
  const contracts = useContracts();
  const controller = useMemo(
    () => new CreatorTimelineController(contracts.editing.timeline, viewer),
    [contracts.editing.timeline, viewer],
  );
  const snapshot = useSyncExternalStore(controller.subscribe, controller.getSnapshot, controller.getSnapshot);
  const [validationError, setValidationError] = useState<Error>();

  useEffect(() => {
    controller.setProjection({ projectId, sequenceId, clips, tracks });
  }, [clips, controller, projectId, sequenceId, tracks]);

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

  const selected = snapshot.selectedClip;
  const selectedTrack = selected ? tracks.find((track) => track.id === selected.trackId) : undefined;
  const busy = snapshot.phase === "planning" || snapshot.phase === "applying";
  const ready =
    Boolean(selected && snapshot.scope && snapshot.alignmentHandling) && !busy && snapshot.phase !== "conflict";
  const rangeStart = seconds(range.start);
  const rangeDuration = seconds(range.duration);
  const timelineItems = [
    ...clips.map((clip) => ({
      id: clip.id,
      trackId: clip.trackId,
      label: assets.find((asset) => asset.id === clip.assetId)?.displayName ?? "Source clip",
      startSeconds: seconds(clip.timelineRange.start),
      durationSeconds: seconds(clip.timelineRange.duration),
      selected: clip.id === selected?.id,
      linked: clip.linkGroupId !== undefined,
    })),
    ...captions.map((caption) => ({
      id: caption.id,
      trackId: caption.trackId,
      label: caption.text.length > 42 ? `${caption.text.slice(0, 41)}…` : caption.text,
      startSeconds: seconds(caption.range.start),
      durationSeconds: seconds(caption.range.duration),
      selectable: false,
    })),
  ];

  return (
    <Stack spacing="compact">
      <TimelineSurface
        durationSeconds={rangeDuration}
        items={timelineItems}
        onItemSelect={(id) => controller.selectClip(id as DurableID)}
        onSeek={(value) => controller.setPlayhead(snapToFrame(value, frameRate))}
        playheadSeconds={seconds(viewer.getSnapshot().playhead)}
        startSeconds={rangeStart}
        tracks={tracks.map((track) => ({ id: track.id, label: track.label, kind: track.type }))}
      />
      {clips.length === 0 ? <Text>Drop a source range or apply a rough cut to add the first Clip.</Text> : null}
      {selected ? (
        <Stack spacing="compact">
          <Text tone="eyebrow">SELECTED CLIP · {selectedTrack?.label ?? selected.trackId}</Text>
          <Text>
            Source {formatTime(selected.sourceRange.start)} → {formatTimeEnd(selected.sourceRange)} · Timeline{" "}
            {formatTime(selected.timelineRange.start)} → {formatTimeEnd(selected.timelineRange)}
          </Text>
          <Button onPress={() => onContextClip(selected)}>Add selected Clip to Agent context</Button>
          {selected.linkGroupId ? (
            <Stack spacing="compact">
              <Text>Choose whether this gesture affects the complete LinkGroup or only the selected Clip.</Text>
              <Button disabled={busy} onPress={() => controller.chooseScope("linked")}>
                {snapshot.scope === "linked" ? "✓ " : ""}Edit linked A/V
              </Button>
              <Button disabled={busy} onPress={() => controller.chooseScope("single")}>
                {snapshot.scope === "single" ? "✓ " : ""}Edit selected Clip only
              </Button>
            </Stack>
          ) : (
            <Text>Scope · selected Clip only</Text>
          )}
          <Text tone="eyebrow">ALIGNMENT CONSEQUENCE</Text>
          {snapshot.alignmentHandling === undefined ? <Text>Choose an explicit Alignment consequence.</Text> : null}
          <Button disabled={busy} onPress={() => controller.chooseAlignmentHandling("preserve-if-provable")}>
            {snapshot.alignmentHandling === "preserve-if-provable" ? "✓ " : ""}Preserve if provable
          </Button>
          <Button disabled={busy} onPress={() => controller.chooseAlignmentHandling("mark-stale")}>
            {snapshot.alignmentHandling === "mark-stale" ? "✓ " : ""}Mark dependent Alignments stale
          </Button>
          <Button disabled={busy} onPress={() => controller.chooseAlignmentHandling("unbind")}>
            {snapshot.alignmentHandling === "unbind" ? "✓ " : ""}Unbind dependent Alignments
          </Button>
          <Text>Gesture target · exact Sequence Viewer playhead {formatTime(viewer.getSnapshot().playhead)}</Text>
          <Button disabled={!ready} onPress={() => void run(() => controller.moveToPlayhead())}>
            Move selected scope to playhead
          </Button>
          <Button disabled={!ready} onPress={() => void run(() => controller.trimStartToPlayhead())}>
            Trim in to playhead
          </Button>
          <Button disabled={!ready} onPress={() => void run(() => controller.trimEndToPlayhead())}>
            Trim out to playhead
          </Button>
          <Button disabled={!ready} onPress={() => void run(() => controller.splitAtPlayhead())}>
            Split at playhead
          </Button>
          <Button
            disabled={!ready || snapshot.alignmentHandling === "preserve-if-provable"}
            onPress={() => void run(() => controller.remove())}
          >
            Remove selected scope
          </Button>
          {snapshot.alignmentHandling === "preserve-if-provable" || snapshot.alignmentHandling === undefined ? (
            <Text>Remove requires an explicit mark-stale or unbind choice.</Text>
          ) : null}
        </Stack>
      ) : null}
      {snapshot.phase === "planning" ? (
        <Status state="pending">Planning complete linked/Alignment closure…</Status>
      ) : null}
      {snapshot.phase === "applying" && snapshot.review ? (
        <Stack spacing="compact">
          <Status state="pending">
            APPLYING · {snapshot.review.kind.toUpperCase()} · {snapshot.review.affectedClipIds.length} affected Clips ·{" "}
            {snapshot.review.alignmentEffects.length} Alignment effects
          </Status>
          <Text>
            Semantic review · {snapshot.review.createdClipCount} created Clips · {snapshot.review.preconditionCount}{" "}
            exact preconditions · {snapshot.review.clipEffects.length} normalized Clip effects
          </Text>
        </Stack>
      ) : null}
      {snapshot.phase === "blocked" && snapshot.blocked ? (
        <Stack spacing="compact">
          <Status state="unavailable">Timeline gesture blocked · {snapshot.blocked.reason}</Status>
          <Text>
            {snapshot.blocked.subjectClipIds.length} Clip subjects · {snapshot.blocked.subjectAlignmentIds.length}{" "}
            Alignment subjects · no mutation was submitted
          </Text>
          {snapshot.blocked.recoveries.includes("mark-stale") ? (
            <Button onPress={() => void run(() => controller.recoverBlocked("mark-stale"))}>
              Mark Alignments stale and continue
            </Button>
          ) : null}
          {snapshot.blocked.recoveries.includes("unbind") ? (
            <Button onPress={() => void run(() => controller.recoverBlocked("unbind"))}>
              Unbind Alignments and continue
            </Button>
          ) : null}
          {snapshot.blocked.recoveries.includes("choose-single") ? (
            <Button onPress={() => void run(() => controller.recoverBlocked("choose-single"))}>
              Apply to selected Clip only
            </Button>
          ) : null}
          {snapshot.blocked.recoveries.includes("change-target") ||
          snapshot.blocked.recoveries.includes("choose-compatible-track") ? (
            <Text>Choose another exact playhead or compatible Track, then start a new gesture.</Text>
          ) : null}
          {snapshot.blocked.recoveries.includes("reduce-scope") ? (
            <Text>The complete gesture exceeds its atomic closure budget; reduce the explicit scope.</Text>
          ) : null}
        </Stack>
      ) : null}
      {snapshot.phase === "committed" ? <Status state="ready">Timeline transaction committed</Status> : null}
      {snapshot.phase === "conflict" ? (
        <Stack spacing="compact">
          <Status state="unavailable">Timeline conflict · reload and reselect the Clip before retrying</Status>
          <Button
            onPress={() => {
              controller.clearSelection();
              void onReload();
            }}
          >
            Refresh committed Timeline
          </Button>
        </Stack>
      ) : null}
      {snapshot.phase === "error" && snapshot.error ? (
        <Stack spacing="compact">
          <Status state="unavailable">Timeline operation failed · {snapshot.error.message}</Status>
          {snapshot.canRetryIdenticalApply ? (
            <Button onPress={() => void run(() => controller.retryIdenticalApply())}>
              Retry identical Timeline apply
            </Button>
          ) : null}
        </Stack>
      ) : null}
      {validationError ? <Status state="unavailable">Gesture unavailable · {validationError.message}</Status> : null}
    </Stack>
  );
}

function seconds(value: RationalTime): number {
  return Number(value.value) / value.scale;
}

function snapToFrame(value: number, frameRate: RationalTime): RationalTime {
  const rateNumerator = BigInt(frameRate.value);
  const rateDenominator = BigInt(frameRate.scale);
  const frame = BigInt(Math.max(0, Math.round((value * Number(rateNumerator)) / frameRate.scale)));
  const numerator = frame * rateDenominator;
  const divisor = greatestCommonDivisor(numerator, rateNumerator);
  return {
    value: int64String((numerator / divisor).toString()),
    scale: Number(rateNumerator / divisor),
  };
}

function greatestCommonDivisor(left: bigint, right: bigint): bigint {
  let a = left < 0n ? -left : left;
  let b = right < 0n ? -right : right;
  while (b !== 0n) [a, b] = [b, a % b];
  return a;
}
