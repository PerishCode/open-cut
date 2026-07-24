import { Button, ControlStrip, Stack, Status, Text, TextAreaField, TextField } from "@open-cut/components";
import { type Caption, type CreatorEditCommit, type DurableID, type Track, useContracts } from "@open-cut/contracts";
import { useCallback, useEffect, useMemo, useState, useSyncExternalStore } from "react";

import { ManualCaptionController } from "../lib/manual-caption-controller.js";
import type { SequenceViewerController } from "../lib/sequence-viewer-controller.js";
import {
  captionProvenanceLabel,
  formatClock,
  formatClockEnd,
  scheduleTimer,
} from "./creator-workspace-presentation.js";

type AsyncResult = unknown;

export function ManualCaptionEditor({
  captions,
  onCommitted,
  onContextCaption,
  onReload,
  projectId,
  sequenceId,
  tracks,
  viewer,
}: Readonly<{
  captions: readonly Caption[];
  onCommitted(receipt: CreatorEditCommit): Promise<AsyncResult>;
  onContextCaption(caption: Caption): void;
  onReload(): Promise<AsyncResult>;
  projectId: DurableID;
  sequenceId: DurableID;
  tracks: readonly Track[];
  viewer: SequenceViewerController;
}>) {
  const contracts = useContracts();
  const controller = useMemo(
    () => new ManualCaptionController(contracts.editing.manualCaptions, viewer),
    [contracts.editing.manualCaptions, viewer],
  );
  const snapshot = useSyncExternalStore(controller.subscribe, controller.getSnapshot, controller.getSnapshot);
  const [actionError, setActionError] = useState(undefined as string | undefined);

  useEffect(() => {
    controller.setProjection({ projectId, sequenceId, captions, tracks });
  }, [captions, controller, projectId, sequenceId, tracks]);
  useEffect(() => () => controller.close(), [controller]);

  const run = useCallback(
    async (operation: () => Promise<CreatorEditCommit | undefined>) => {
      setActionError(undefined);
      let receipt: CreatorEditCommit | undefined;
      try {
        receipt = await operation();
      } catch {
        setActionError("This Caption action is no longer available. Review the draft and try again.");
        return;
      }
      if (!receipt) return;
      try {
        await onCommitted(receipt);
      } catch {
        setActionError("The Caption was committed, but the workspace could not refresh. Choose Sync now to reload it.");
      }
    },
    [onCommitted],
  );
  const checkpoint = useCallback(() => run(() => controller.checkpoint()), [controller, run]);

  const draft = snapshot.draft;
  const busy = snapshot.phase === "planning" || snapshot.phase === "applying";
  const canCheckpoint = Boolean(
    draft &&
      !busy &&
      draft.text.length > 0 &&
      draft.language.trim().length > 0 &&
      draft.trackId &&
      (draft.kind === "create"
        ? draft.inCaptured && draft.outCaptured
        : draft.dirty && draft.alignmentHandling !== undefined),
  );
  const canRemove = Boolean(
    draft?.kind === "update" &&
      !draft.dirty &&
      !busy &&
      (draft.alignmentHandling === "mark-stale" || draft.alignmentHandling === "unbind"),
  );

  useEffect(() => {
    if (
      draft?.kind !== "update" ||
      draft.inCaptured ||
      draft.outCaptured ||
      !canCheckpoint ||
      snapshot.phase !== "drafting" ||
      snapshot.canRetryIdenticalApply
    ) {
      return;
    }
    return scheduleTimer(() => void checkpoint(), 750);
  }, [canCheckpoint, checkpoint, draft, snapshot.canRetryIdenticalApply, snapshot.phase]);

  return (
    <Stack spacing="compact">
      <ControlStrip
        hint={`${snapshot.captions.length} in first 60 seconds`}
        label="Caption editor actions"
        summary="COMMITTED CUES"
      >
        <Button disabled={busy} label="New manual Caption" onPress={() => controller.beginCreate()}>
          New caption
        </Button>
      </ControlStrip>
      {snapshot.captions.map((caption) => (
        <ControlStrip
          hint={caption.text}
          key={caption.id}
          label={`Caption ${caption.id} actions`}
          summary={`${formatClock(caption.range.start)} → ${formatClockEnd(caption.range)} · r${caption.revision} · ${captionProvenanceLabel(caption)}`}
        >
          <Button
            disabled={busy}
            label={`Edit Caption ${caption.id}`}
            onPress={() => controller.selectCaption(caption.id)}
          >
            Edit
          </Button>
          <Button label="Use this Caption as @ context" onPress={() => onContextCaption(caption)}>
            @ Agent
          </Button>
        </ControlStrip>
      ))}
      {snapshot.captions.length === 0 ? <Text>No Caption cues in the first 60 seconds.</Text> : null}

      {draft ? (
        <Stack spacing="compact">
          <ControlStrip
            hint={`PLAYHEAD ${formatClock(viewer.getSnapshot().playhead)}`}
            label={draft.kind === "create" ? "New manual Caption controls" : "Caption inspector controls"}
            summary={`${draft.kind === "create" ? "NEW CAPTION" : "CAPTION"} · ${
              draft.inPoint ? formatClock(draft.inPoint) : "IN —"
            } → ${draft.outPoint ? formatClock(draft.outPoint) : "OUT —"}`}
          >
            {draft.kind === "create"
              ? snapshot.tracks.map((track) => (
                  <Button
                    disabled={busy}
                    key={track.id}
                    label={`Use Caption Track ${track.label} at r${track.revision}`}
                    pressed={draft.trackId === track.id}
                    onPress={() => controller.selectTrack(track.id)}
                  >
                    {track.label} · r{track.revision}
                  </Button>
                ))
              : null}
            {draft.kind === "update" ? (
              <Status state="ready">{trackLabel(draft.trackId, snapshot.tracks)}</Status>
            ) : null}
            <Button
              disabled={busy}
              label="Capture In at Viewer playhead"
              pressed={draft.inCaptured}
              onPress={() => controller.captureIn()}
            >
              Set In
            </Button>
            <Button
              disabled={busy}
              label="Capture Out at Viewer playhead"
              pressed={draft.outCaptured}
              onPress={() => controller.captureOut()}
            >
              Set Out
            </Button>
            {draft.kind === "create" ? (
              <Button label="Cancel manual Caption draft" onPress={() => controller.clear()}>
                Cancel
              </Button>
            ) : null}
          </ControlStrip>
          {draft.kind === "create" && snapshot.tracks.length === 0 ? (
            <Status state="unavailable">No Caption Track is available.</Status>
          ) : null}
          <TextField
            density="compact"
            disabled={draft.kind === "create" && busy}
            label="Caption language"
            maxLength={64}
            onBlur={() => draft.kind === "update" && void checkpoint()}
            onChange={(value) => controller.setLanguage(value.trim() ? value : "und")}
            placeholder="Language · AUTO"
            value={draft.language === "und" ? "" : draft.language}
          />
          <TextAreaField
            density="compact"
            disabled={draft.kind === "create" && busy}
            label="Caption text"
            maxLength={262_144}
            onBlur={() => draft.kind === "update" && void checkpoint()}
            onChange={(value) => controller.setText(value)}
            placeholder="Caption text"
            rows={3}
            value={draft.text}
          />

          {draft.kind === "update" ? (
            <Stack spacing="compact">
              <Text tone="eyebrow">DEPENDENT ALIGNMENT HANDLING</Text>
              <Text>Text or language changes require mark-stale or unbind. Timing-only edits may request proof.</Text>
              <Button
                disabled={busy || draft.alignmentHandling === undefined}
                onPress={() => controller.chooseAlignmentHandling("preserve-if-provable")}
              >
                {draft.alignmentHandling === "preserve-if-provable" ? "✓ " : ""}Preserve if provable
              </Button>
              <Button disabled={busy} onPress={() => controller.chooseAlignmentHandling("mark-stale")}>
                {draft.alignmentHandling === "mark-stale" ? "✓ " : ""}Mark dependent Alignments stale
              </Button>
              <Button disabled={busy} onPress={() => controller.chooseAlignmentHandling("unbind")}>
                {draft.alignmentHandling === "unbind" ? "✓ " : ""}Unbind dependent Alignments
              </Button>
              {draft.alignmentHandling === undefined ? (
                <Status state="unavailable">Choose stale or unbind before checkpointing changed content</Status>
              ) : null}
            </Stack>
          ) : null}

          <Button disabled={!canCheckpoint} variant="primary" onPress={() => void checkpoint()}>
            {draft.kind === "create" ? "Create manual Caption" : "Save Caption checkpoint"}
          </Button>
          {draft.kind === "update" ? (
            <Button disabled={!canRemove} onPress={() => void run(() => controller.remove())}>
              Remove Caption
            </Button>
          ) : null}
          {draft.kind === "update" && draft.dirty ? <Text>Local Caption changes are not yet committed.</Text> : null}
        </Stack>
      ) : null}

      {snapshot.phase === "planning" ? (
        <Status state="pending">Planning complete collision and dependent Alignment closure…</Status>
      ) : null}
      {snapshot.phase === "applying" && snapshot.review ? (
        <Status state="pending">
          Applying one atomic {snapshot.review.kind} · {snapshot.review.alignmentEffects.length} Alignment effects ·{" "}
          {snapshot.review.preconditionCount} exact preconditions
        </Status>
      ) : null}
      {snapshot.phase === "committed" ? (
        <Status state="ready">Caption transaction committed to Workspace history</Status>
      ) : null}
      {snapshot.phase === "conflict" ? (
        <Stack spacing="compact">
          <Status state="unavailable">Caption conflict · local values remain unsaved and visible</Status>
          <Button
            onPress={() => {
              controller.prepareRefreshForRetry();
              void onReload();
            }}
          >
            Refresh exact revisions and keep local values
          </Button>
          <Button onPress={() => controller.reloadCommitted()}>Reload committed Caption</Button>
        </Stack>
      ) : null}
      {snapshot.phase === "error" && snapshot.error ? (
        <Stack spacing="compact">
          <Status state="unavailable">
            {snapshot.canRetryIdenticalApply
              ? "Could not confirm the Caption update."
              : "Could not prepare this Caption checkpoint. Review the draft and try again."}
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

function trackLabel(trackId: DurableID | undefined, tracks: readonly Track[]): string {
  if (!trackId) return "unselected";
  return tracks.find((track) => track.id === trackId)?.label ?? trackId;
}
