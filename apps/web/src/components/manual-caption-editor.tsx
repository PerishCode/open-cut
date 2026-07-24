import { Button, ControlStrip, Stack, Status, Text, TextAreaField, TextField } from "@open-cut/components";
import {
  type Caption,
  type CreatorCaptionAlignmentHandling,
  type CreatorEditCommit,
  type DurableID,
  type Track,
  useContracts,
} from "@open-cut/contracts";
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
      {snapshot.captions.map((caption, index) => {
        const identity = `Caption ${index + 1} at ${formatClock(caption.range.start)} → ${formatClockEnd(caption.range)}`;
        return (
          <ControlStrip
            hint={caption.text}
            key={caption.id}
            label={`${identity} actions`}
            summary={`${formatClock(caption.range.start)} → ${formatClockEnd(caption.range)} · r${caption.revision} · ${captionProvenanceLabel(caption)}`}
          >
            <Button disabled={busy} label={`Edit ${identity}`} onPress={() => controller.selectCaption(caption.id)}>
              Edit
            </Button>
            <Button label={`Use ${identity} as @ context`} onPress={() => onContextCaption(caption)}>
              @ Agent
            </Button>
          </ControlStrip>
        );
      })}
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
            ) : (
              <Button disabled={busy || draft.dirty} label="Close Caption inspector" onPress={() => controller.clear()}>
                Close
              </Button>
            )}
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
            <ControlStrip
              hint="CONTENT CHANGES REQUIRE STALE OR UNBIND"
              label="Dependent Alignment handling"
              summary={`ALIGNMENTS · ${alignmentHandlingLabel(draft.alignmentHandling)}`}
            >
              <Button
                disabled={busy || draft.alignmentHandling === undefined}
                label="Preserve dependent Alignments if provable"
                pressed={draft.alignmentHandling === "preserve-if-provable"}
                onPress={() => controller.chooseAlignmentHandling("preserve-if-provable")}
              >
                Preserve
              </Button>
              <Button
                disabled={busy}
                label="Mark dependent Alignments stale"
                pressed={draft.alignmentHandling === "mark-stale"}
                onPress={() => controller.chooseAlignmentHandling("mark-stale")}
              >
                Mark stale
              </Button>
              <Button
                disabled={busy}
                label="Unbind dependent Alignments"
                pressed={draft.alignmentHandling === "unbind"}
                onPress={() => controller.chooseAlignmentHandling("unbind")}
              >
                Unbind
              </Button>
            </ControlStrip>
          ) : null}

          <ControlStrip
            hint={
              draft.kind === "update" && draft.alignmentHandling === undefined
                ? "CHOOSE STALE OR UNBIND"
                : draft.dirty
                  ? "LOCAL CHANGES"
                  : "COMMITTED"
            }
            label="Caption checkpoint actions"
            summary={draft.kind === "create" ? "CREATE CAPTION" : "CAPTION CHECKPOINT"}
          >
            <Button disabled={!canCheckpoint} variant="primary" onPress={() => void checkpoint()}>
              {draft.kind === "create" ? "Create manual Caption" : "Save Caption checkpoint"}
            </Button>
            {draft.kind === "update" ? (
              <Button disabled={!canRemove} variant="danger" onPress={() => void run(() => controller.remove())}>
                Remove Caption
              </Button>
            ) : null}
          </ControlStrip>
          {draft.kind === "update" && draft.alignmentHandling === undefined ? (
            <Status state="unavailable">Choose stale or unbind before checkpointing changed content</Status>
          ) : null}
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

function alignmentHandlingLabel(value: CreatorCaptionAlignmentHandling | undefined): string {
  switch (value) {
    case "preserve-if-provable":
      return "PRESERVE";
    case "mark-stale":
      return "MARK STALE";
    case "unbind":
      return "UNBIND";
    default:
      return "REQUIRED";
  }
}

function trackLabel(trackId: DurableID | undefined, tracks: readonly Track[]): string {
  if (!trackId) return "No Caption Track";
  return tracks.find((track) => track.id === trackId)?.label ?? "Unavailable Caption Track";
}
