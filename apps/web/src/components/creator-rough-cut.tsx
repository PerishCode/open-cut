import { Button, Stack, Status, Text } from "@open-cut/components";
import {
  type Asset,
  type CreatorEditCommit,
  CreatorEditError,
  type CreatorRoughCutLaneInput,
  type CreatorRoughCutReview,
  type DurableID,
  type RationalTime,
  type RevisionString,
  type Track,
  useContracts,
} from "@open-cut/contracts";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  type CreatorRoughCutLaneCandidate,
  type CreatorRoughCutLaneSelection,
  type CreatorRoughCutOccurrence,
  roughCutLaneCandidates,
} from "./creator-rough-cut-queue.js";
import { formatClock, formatClockEnd } from "./creator-workspace-presentation.js";

type AsyncResult = unknown;
type RoughCutPhase = "idle" | "previewing" | "review" | "applying" | "error" | "conflict" | "success";

type ApplyAttempt = Readonly<{
  review: CreatorRoughCutReview;
  input: Readonly<{ requestId: string; intent: string }>;
}>;

export function CreatorRoughCutPanel({
  assets,
  occurrences,
  onChange,
  onCommitted,
  onReload,
  onTimelineStartChange,
  projectId,
  projectRevision,
  sequenceId,
  sequenceRevision,
  timelineStart,
  tracks,
  currentPlayhead,
}: Readonly<{
  assets: readonly Asset[];
  occurrences: readonly CreatorRoughCutOccurrence[];
  onChange(value: readonly CreatorRoughCutOccurrence[]): void;
  onCommitted(receipt: CreatorEditCommit): Promise<AsyncResult>;
  onReload(): Promise<AsyncResult>;
  onTimelineStartChange(value: RationalTime): void;
  projectId: DurableID;
  projectRevision: RevisionString;
  sequenceId: DurableID;
  sequenceRevision: RevisionString;
  timelineStart: RationalTime;
  tracks: readonly Track[];
  currentPlayhead: RationalTime;
}>) {
  const contracts = useContracts();
  const [phase, setPhase] = useState<RoughCutPhase>("idle");
  const [review, setReview] = useState<CreatorRoughCutReview>();
  const [error, setError] = useState<Error>();
  const [projectionWarning, setProjectionWarning] = useState(false);
  const applyAttemptRef = useRef<ApplyAttempt | undefined>(undefined);
  const inFlightRef = useRef(false);
  const successfulDraftRef = useRef(false);
  const draftKey = useMemo(
    () => occurrenceDraftKey(occurrences, timelineStart, projectRevision, sequenceRevision, assets, tracks),
    [assets, occurrences, projectRevision, sequenceRevision, timelineStart, tracks],
  );
  const previousDraftKeyRef = useRef(draftKey);

  useEffect(() => {
    if (previousDraftKeyRef.current === draftKey) return;
    previousDraftKeyRef.current = draftKey;
    setReview(undefined);
    applyAttemptRef.current = undefined;
    if (successfulDraftRef.current) {
      successfulDraftRef.current = false;
      return;
    }
    setPhase("idle");
    setError(undefined);
  }, [draftKey]);

  const blocker = firstOccurrenceBlocker(occurrences, assets, tracks);

  const previewDraft = useCallback(async () => {
    if (inFlightRef.current || blocker || occurrences.length === 0) return;
    inFlightRef.current = true;
    setPhase("previewing");
    setError(undefined);
    setProjectionWarning(false);
    setReview(undefined);
    applyAttemptRef.current = undefined;
    try {
      const value = await contracts.editing.roughCut.preview({
        projectId,
        sequenceId,
        timelineStart,
        items: occurrences.map((occurrence) => ({
          sourceExcerptId: occurrence.sourceExcerpt.id,
          sourceExcerptRevision: occurrence.sourceExcerpt.revision,
          ...selectedLanes(occurrence),
        })),
      });
      setReview(value);
      setPhase("review");
    } catch (value) {
      const nextError = asError(value);
      setError(nextError);
      setPhase(isConflict(nextError) ? "conflict" : "error");
    } finally {
      inFlightRef.current = false;
    }
  }, [blocker, contracts.editing.roughCut, occurrences, projectId, sequenceId, timelineStart]);

  const applyReview = useCallback(
    async (retry?: ApplyAttempt) => {
      if (inFlightRef.current) return;
      const attempt =
        retry ??
        (review
          ? {
              review,
              input: {
                requestId: `ui:creator-rough-cut-apply:${crypto.randomUUID()}`,
                intent: "Apply reviewed Creator rough cut",
              },
            }
          : undefined);
      if (!attempt) return;
      applyAttemptRef.current = attempt;
      inFlightRef.current = true;
      setPhase("applying");
      setError(undefined);
      try {
        const receipt = await contracts.editing.roughCut.apply(attempt.review, attempt.input);
        applyAttemptRef.current = undefined;
        setReview(undefined);
        successfulDraftRef.current = true;
        setPhase("success");
        try {
          await onCommitted(receipt);
        } catch {
          setProjectionWarning(true);
        }
      } catch (value) {
        const nextError = asError(value);
        setError(nextError);
        if (isConflict(nextError)) {
          applyAttemptRef.current = undefined;
          setReview(undefined);
          setPhase("conflict");
        } else {
          setPhase("error");
        }
      } finally {
        inFlightRef.current = false;
      }
    },
    [contracts.editing.roughCut, onCommitted, review],
  );

  const updateOccurrence = useCallback(
    (key: string, update: (value: CreatorRoughCutOccurrence) => CreatorRoughCutOccurrence) => {
      onChange(occurrences.map((occurrence) => (occurrence.key === key ? update(occurrence) : occurrence)));
    },
    [occurrences, onChange],
  );

  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">ROUGH CUT · EXCERPT QUEUE</Text>
      <Text>
        Starts at {formatClock(timelineStart)} · {occurrences.length}{" "}
        {occurrences.length === 1 ? "excerpt" : "excerpts"}
      </Text>
      <Button
        disabled={phase === "applying" || phase === "previewing"}
        onPress={() => onTimelineStartChange(currentPlayhead)}
      >
        Start at current playhead · {formatClock(currentPlayhead)}
      </Button>
      {occurrences.map((occurrence, index) => (
        <Stack key={occurrence.key} spacing="compact">
          <Text tone="eyebrow">
            {String(index + 1).padStart(2, "0")} · {occurrence.assetLabel}
          </Text>
          <Text>{occurrence.sourceExcerpt.effectiveText}</Text>
          <LaneChoices
            candidates={currentLaneCandidates("video", occurrence, assets, tracks)}
            kind="video"
            onChange={(selection) => updateOccurrence(occurrence.key, (value) => ({ ...value, video: selection }))}
            selection={occurrence.video}
          />
          <LaneChoices
            candidates={currentLaneCandidates("audio", occurrence, assets, tracks)}
            kind="audio"
            onChange={(selection) => updateOccurrence(occurrence.key, (value) => ({ ...value, audio: selection }))}
            selection={occurrence.audio}
          />
          <Button disabled={index === 0} onPress={() => onChange(moveOccurrence(occurrences, index, index - 1))}>
            Move excerpt up
          </Button>
          <Button
            disabled={index === occurrences.length - 1}
            onPress={() => onChange(moveOccurrence(occurrences, index, index + 1))}
          >
            Move excerpt down
          </Button>
          <Button onPress={() => onChange(occurrences.filter((candidate) => candidate.key !== occurrence.key))}>
            Remove excerpt
          </Button>
        </Stack>
      ))}
      {occurrences.length === 0 ? <Text>Add an excerpt from Story to begin.</Text> : null}
      {blocker ? <Status state="unavailable">{blocker}</Status> : null}
      <Button
        disabled={Boolean(blocker) || occurrences.length === 0 || phase === "previewing" || phase === "applying"}
        onPress={() => void previewDraft()}
      >
        {phase === "previewing" ? "Preparing review…" : "Review rough cut"}
      </Button>
      {review ? <RoughCutReview occurrences={occurrences} review={review} /> : null}
      {review ? (
        <Button disabled={phase === "applying"} onPress={() => void applyReview()}>
          {phase === "applying" ? "Adding rough cut…" : "Add rough cut to Timeline"}
        </Button>
      ) : null}
      {phase === "error" && error ? (
        <>
          <Status state="unavailable">Rough cut failed · {error.message}</Status>
          {applyAttemptRef.current ? (
            <Button onPress={() => void applyReview(applyAttemptRef.current)}>Retry same rough cut</Button>
          ) : (
            <Button onPress={() => void previewDraft()}>Try review again</Button>
          )}
        </>
      ) : null}
      {phase === "conflict" ? (
        <>
          <Status state="unavailable">Rough cut needs a fresh review · excerpt queue preserved</Status>
          <Button onPress={() => void onReload()}>Refresh and review again</Button>
        </>
      ) : null}
      {phase === "success" ? (
        <Status state="ready">
          {projectionWarning ? "Rough cut added · use Sync now to reveal it" : "Rough cut added to Timeline"}
        </Status>
      ) : null}
    </Stack>
  );
}

function LaneChoices({
  candidates,
  kind,
  onChange,
  selection,
}: Readonly<{
  candidates: readonly CreatorRoughCutLaneCandidate[];
  kind: "video" | "audio";
  onChange(value: CreatorRoughCutLaneSelection): void;
  selection: CreatorRoughCutLaneSelection;
}>) {
  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">
        {kind.toUpperCase()} · {laneSelectionLabel(selection)}
      </Text>
      {candidates.map((candidate) => (
        <Button key={candidate.id} onPress={() => onChange({ state: "selected", candidate })}>
          Use {candidate.trackLabel} · {candidate.streamLabel}
        </Button>
      ))}
      <Button onPress={() => onChange({ state: "omitted" })}>Omit {kind}</Button>
    </Stack>
  );
}

function RoughCutReview({
  occurrences,
  review,
}: Readonly<{ occurrences: readonly CreatorRoughCutOccurrence[]; review: CreatorRoughCutReview }>) {
  return (
    <Stack spacing="compact">
      <Status state="ready">
        Ready to add · {review.items.length} {review.items.length === 1 ? "excerpt" : "excerpts"}
      </Status>
      {review.items.map((item, index) => (
        <Stack key={item.alignmentLocal} spacing="compact">
          <Text tone="eyebrow">
            {String(item.ordinal).padStart(2, "0")} · {formatClock(item.timelineRange.start)} →{" "}
            {formatClockEnd(item.timelineRange)} · {item.video ? "V" : ""}
            {item.video && item.audio ? "+" : ""}
            {item.audio ? "A" : ""}
          </Text>
          <Text>{occurrences[index]?.sourceExcerpt.effectiveText ?? `Excerpt ${item.sourceExcerptId}`}</Text>
        </Stack>
      ))}
      <Text>Nothing changes until you add this rough cut to the Timeline.</Text>
    </Stack>
  );
}

function firstOccurrenceBlocker(
  occurrences: readonly CreatorRoughCutOccurrence[],
  assets: readonly Asset[],
  tracks: readonly Track[],
): string | undefined {
  for (let index = 0; index !== occurrences.length; index += 1) {
    const occurrence = occurrences[index];
    if (!occurrence) continue;
    const label = `Excerpt ${index + 1}`;
    if (occurrence.evidenceStatus !== "exact") return `${label} evidence is stale`;
    const videoCandidates = currentLaneCandidates("video", occurrence, assets, tracks);
    const audioCandidates = currentLaneCandidates("audio", occurrence, assets, tracks);
    if (occurrence.video.state === "unresolved") return `${label} requires an explicit video lane choice`;
    if (occurrence.audio.state === "unresolved") return `${label} requires an explicit audio lane choice`;
    if (occurrence.video.state === "selected" && !hasCurrentCandidate(occurrence.video.candidate, videoCandidates)) {
      return `${label} video lane changed and requires a new choice`;
    }
    if (occurrence.audio.state === "selected" && !hasCurrentCandidate(occurrence.audio.candidate, audioCandidates)) {
      return `${label} audio lane changed and requires a new choice`;
    }
    if (occurrence.video.state !== "selected" && occurrence.audio.state !== "selected") {
      return `${label} has no compatible selected lane`;
    }
  }
  return undefined;
}

function currentLaneCandidates(
  kind: "video" | "audio",
  occurrence: CreatorRoughCutOccurrence,
  assets: readonly Asset[],
  tracks: readonly Track[],
): readonly CreatorRoughCutLaneCandidate[] {
  return roughCutLaneCandidates(
    kind,
    assets.find((candidate) => candidate.id === occurrence.sourceExcerpt.assetId),
    tracks,
  );
}

function hasCurrentCandidate(
  selected: CreatorRoughCutLaneCandidate,
  candidates: readonly CreatorRoughCutLaneCandidate[],
): boolean {
  return candidates.some(
    (candidate) => candidate.id === selected.id && candidate.trackRevision === selected.trackRevision,
  );
}

function selectedLanes(occurrence: CreatorRoughCutOccurrence): {
  video?: CreatorRoughCutLaneInput;
  audio?: CreatorRoughCutLaneInput;
} {
  return {
    ...(occurrence.video.state === "selected" ? { video: laneInput(occurrence.video.candidate) } : {}),
    ...(occurrence.audio.state === "selected" ? { audio: laneInput(occurrence.audio.candidate) } : {}),
  };
}

function laneInput(candidate: CreatorRoughCutLaneCandidate): CreatorRoughCutLaneInput {
  return {
    trackId: candidate.trackId,
    trackRevision: candidate.trackRevision,
    sourceStreamId: candidate.sourceStreamId,
  };
}

function laneSelectionLabel(selection: CreatorRoughCutLaneSelection): string {
  if (selection.state === "unresolved") return "CHOOSE ONE OR OMIT";
  if (selection.state === "omitted") return "NOT INCLUDED";
  return `${selection.candidate.trackLabel} · ${selection.candidate.streamLabel}`;
}

function moveOccurrence(
  occurrences: readonly CreatorRoughCutOccurrence[],
  from: number,
  to: number,
): readonly CreatorRoughCutOccurrence[] {
  const next = [...occurrences];
  const [moved] = next.splice(from, 1);
  if (moved) next.splice(to, 0, moved);
  return next;
}

function occurrenceDraftKey(
  occurrences: readonly CreatorRoughCutOccurrence[],
  timelineStart: RationalTime,
  projectRevision: RevisionString,
  sequenceRevision: RevisionString,
  assets: readonly Asset[],
  tracks: readonly Track[],
): string {
  return JSON.stringify({
    projectRevision,
    sequenceRevision,
    timelineStart,
    assets: assets.map((asset) => ({
      id: asset.id,
      revision: asset.revision,
      streams: asset.facts?.streams.map((stream) => stream.id) ?? [],
    })),
    tracks: tracks.map((track) => ({ id: track.id, revision: track.revision, type: track.type })),
    occurrences: occurrences.map((occurrence) => ({
      key: occurrence.key,
      id: occurrence.sourceExcerpt.id,
      revision: occurrence.sourceExcerpt.revision,
      video: laneSelectionKey(occurrence.video),
      audio: laneSelectionKey(occurrence.audio),
    })),
  });
}

function laneSelectionKey(selection: CreatorRoughCutLaneSelection): string {
  return selection.state === "selected" ? selection.candidate.id : selection.state;
}

function isConflict(value: Error): boolean {
  return value instanceof CreatorEditError && value.code === "conflict";
}

function asError(value: unknown): Error {
  return value instanceof Error ? value : new Error(String(value));
}
