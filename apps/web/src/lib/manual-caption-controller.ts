import {
  type Caption,
  type CreatorCaptionAlignmentHandling,
  type CreatorEditCommit,
  CreatorEditError,
  type CreatorManualCaptionGestureInput,
  type CreatorManualCaptionPort,
  type CreatorManualCaptionReview,
  type DurableID,
  int64String,
  type RationalTime,
  type Track,
} from "@open-cut/contracts";

import type { TimelinePlayheadAuthority } from "./creator-timeline-controller.js";

export type ManualCaptionPhase = "idle" | "drafting" | "planning" | "applying" | "committed" | "conflict" | "error";

export type ManualCaptionDraft = Readonly<{
  kind: "create" | "update";
  captionId?: DurableID;
  trackId?: DurableID;
  inPoint?: RationalTime;
  outPoint?: RationalTime;
  inCaptured: boolean;
  outCaptured: boolean;
  language: string;
  text: string;
  alignmentHandling?: CreatorCaptionAlignmentHandling;
  dirty: boolean;
}>;

export type ManualCaptionSnapshot = Readonly<{
  phase: ManualCaptionPhase;
  captions: readonly Caption[];
  tracks: readonly Track[];
  draft?: ManualCaptionDraft;
  review?: CreatorManualCaptionReview;
  error?: Error;
  canRetryIdenticalApply: boolean;
}>;

export type ManualCaptionProjection = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  captions: readonly Caption[];
  tracks: readonly Track[];
}>;

type ApplyAttempt = Readonly<{
  gesture: CreatorManualCaptionGestureInput;
  review: CreatorManualCaptionReview;
  input: Readonly<{ requestId: string; intent: string }>;
  draftVersion: number;
}>;

const initialSnapshot: ManualCaptionSnapshot = {
  phase: "idle",
  captions: [],
  tracks: [],
  canRetryIdenticalApply: false,
};

export class ManualCaptionController {
  readonly #captions: CreatorManualCaptionPort;
  readonly #playhead: TimelinePlayheadAuthority;
  readonly #listeners = new Set<() => void>();
  #snapshot: ManualCaptionSnapshot = initialSnapshot;
  #projection?: ManualCaptionProjection;
  #baseCaption?: Caption;
  #baseTrack?: Track;
  #attempt?: ApplyAttempt;
  #generation = 0;
  #draftVersion = 0;
  #inFlight = false;
  #refreshRequested = false;

  constructor(captions: CreatorManualCaptionPort, playhead: TimelinePlayheadAuthority) {
    this.#captions = captions;
    this.#playhead = playhead;
  }

  getSnapshot = (): ManualCaptionSnapshot => this.#snapshot;

  subscribe = (listener: () => void): (() => void) => {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  };

  setProjection(projection: ManualCaptionProjection): void {
    this.#projection = projection;
    const captions = projection.captions
      .filter((caption) => !caption.tombstoned)
      .sort((left, right) => compareTime(left.range.start, right.range.start) || left.id.localeCompare(right.id));
    const tracks = projection.tracks
      .filter((track) => track.type === "caption")
      .sort((left, right) => left.label.localeCompare(right.label) || left.id.localeCompare(right.id));
    const draft = this.#snapshot.draft;
    if (!draft) {
      this.#snapshot = { ...this.#snapshot, captions, tracks };
      this.#emit();
      return;
    }
    if (draft.kind === "create") {
      const track = draft.trackId ? tracks.find((candidate) => candidate.id === draft.trackId) : undefined;
      if (track && !this.#attempt) this.#baseTrack = track;
      this.#snapshot = { ...this.#snapshot, captions, tracks };
      this.#emit();
      return;
    }
    const current = captions.find((caption) => caption.id === draft.captionId);
    const track = current ? tracks.find((candidate) => candidate.id === current.trackId) : undefined;
    if (!current || !track) {
      this.#conflict(new Error("The selected Caption is no longer in the current projection"));
      this.#snapshot = { ...this.#snapshot, captions, tracks };
      this.#emit();
      return;
    }
    if (this.#refreshRequested) {
      this.#refreshRequested = false;
      this.#baseCaption = current;
      this.#baseTrack = track;
      this.#attempt = undefined;
      this.#snapshot = {
        ...this.#snapshot,
        phase: "drafting",
        captions,
        tracks,
        draft: { ...draft, dirty: draftDiffersFromCaption(draft, current) },
        review: undefined,
        error: undefined,
        canRetryIdenticalApply: false,
      };
      this.#emit();
      return;
    }
    if (this.#baseCaption && current.revision !== this.#baseCaption.revision) {
      if (draft.dirty || this.#attempt) {
        this.#conflict(new Error("The selected Caption revision changed while the local draft was open"));
        this.#snapshot = { ...this.#snapshot, captions, tracks };
        this.#emit();
        return;
      }
      this.#baseCaption = current;
      this.#baseTrack = track;
      this.#snapshot = { ...this.#snapshot, phase: "drafting", captions, tracks, draft: existingDraft(current) };
      this.#emit();
      return;
    }
    this.#baseCaption = current;
    this.#baseTrack = track;
    this.#snapshot = { ...this.#snapshot, captions, tracks };
    this.#emit();
  }

  beginCreate(): void {
    const tracks = this.#snapshot.tracks;
    this.#resetAttempt();
    this.#baseCaption = undefined;
    this.#baseTrack = tracks.length === 1 ? tracks[0] : undefined;
    this.#snapshot = {
      ...this.#snapshot,
      phase: "drafting",
      draft: {
        kind: "create",
        ...(this.#baseTrack ? { trackId: this.#baseTrack.id } : {}),
        inCaptured: false,
        outCaptured: false,
        language: "und",
        text: "",
        dirty: true,
      },
      review: undefined,
      error: undefined,
      canRetryIdenticalApply: false,
    };
    this.#emit();
  }

  selectCaption(captionId: DurableID): void {
    const caption = this.#snapshot.captions.find((candidate) => candidate.id === captionId);
    const track = caption && this.#snapshot.tracks.find((candidate) => candidate.id === caption.trackId);
    if (!caption || !track) throw new Error("Caption is not in the current editable projection");
    this.#resetAttempt();
    this.#baseCaption = caption;
    this.#baseTrack = track;
    this.#snapshot = {
      ...this.#snapshot,
      phase: "drafting",
      draft: existingDraft(caption),
      review: undefined,
      error: undefined,
      canRetryIdenticalApply: false,
    };
    this.#emit();
  }

  selectTrack(trackId: DurableID): void {
    const draft = this.#requireDraft("create");
    const track = this.#snapshot.tracks.find((candidate) => candidate.id === trackId);
    if (!track) throw new Error("Caption Track is not in the current projection");
    this.#baseTrack = track;
    this.#changeDraft({ ...draft, trackId, dirty: true });
  }

  captureIn(): void {
    const draft = this.#requireDraft();
    this.#changeDraft({ ...draft, inPoint: this.#playhead.getSnapshot().playhead, inCaptured: true });
  }

  captureOut(): void {
    const draft = this.#requireDraft();
    this.#changeDraft({ ...draft, outPoint: this.#playhead.getSnapshot().playhead, outCaptured: true });
  }

  setLanguage(language: string): void {
    const draft = this.#requireDraft();
    this.#changeDraft(this.#withContentChange({ ...draft, language }));
  }

  setText(text: string): void {
    const draft = this.#requireDraft();
    this.#changeDraft(this.#withContentChange({ ...draft, text }));
  }

  chooseAlignmentHandling(value: CreatorCaptionAlignmentHandling): void {
    const draft = this.#requireDraft("update");
    if (value === "preserve-if-provable" && this.#contentChanged(draft)) {
      throw new Error("Text or language changes require mark-stale or unbind");
    }
    this.#changeDraft({ ...draft, alignmentHandling: value });
  }

  checkpoint(): Promise<CreatorEditCommit | undefined> {
    if (this.#attempt) throw new Error("Resolve the ambiguous Caption apply before creating another checkpoint");
    const gesture = this.#checkpointGesture();
    if (!gesture) return Promise.resolve(undefined);
    return this.#commit(gesture, gesture.kind === "create" ? "Create manual Caption" : "Update manual Caption");
  }

  remove(): Promise<CreatorEditCommit | undefined> {
    if (this.#attempt) throw new Error("Resolve the ambiguous Caption apply before removing the Caption");
    const draft = this.#requireDraft("update");
    if (draft.dirty) throw new Error("Checkpoint or reload the local Caption draft before removing it");
    const caption = this.#requireBaseCaption();
    const track = this.#requireBaseTrack();
    if (draft.alignmentHandling !== "mark-stale" && draft.alignmentHandling !== "unbind") {
      throw new Error("Removing a Caption requires mark-stale or unbind");
    }
    return this.#commit(
      {
        projectId: this.#requireProjection().projectId,
        sequenceId: this.#requireProjection().sequenceId,
        kind: "remove",
        captionId: caption.id,
        captionRevision: caption.revision,
        trackId: track.id,
        trackRevision: track.revision,
        alignmentHandling: draft.alignmentHandling,
      },
      "Remove manual Caption",
    );
  }

  retryIdenticalApply(): Promise<CreatorEditCommit | undefined> {
    const attempt = this.#attempt;
    if (!attempt || this.#inFlight) return Promise.resolve(undefined);
    return this.#apply(attempt, this.#generation);
  }

  prepareRefreshForRetry(): void {
    if (this.#snapshot.phase !== "conflict") return;
    this.#refreshRequested = true;
  }

  reloadCommitted(): void {
    const draft = this.#snapshot.draft;
    if (!draft) return;
    if (draft.kind === "create") {
      this.beginCreate();
      return;
    }
    const caption = this.#projection?.captions.find(
      (candidate) => candidate.id === draft.captionId && !candidate.tombstoned,
    );
    if (!caption) {
      this.clear();
      return;
    }
    this.selectCaption(caption.id);
  }

  clear(): void {
    this.#resetAttempt();
    this.#baseCaption = undefined;
    this.#baseTrack = undefined;
    this.#refreshRequested = false;
    this.#snapshot = { ...initialSnapshot, captions: this.#snapshot.captions, tracks: this.#snapshot.tracks };
    this.#emit();
  }

  close(): void {
    this.clear();
    this.#projection = undefined;
    this.#listeners.clear();
  }

  #checkpointGesture(): CreatorManualCaptionGestureInput | undefined {
    if (this.#inFlight) return undefined;
    const draft = this.#requireDraft();
    const projection = this.#requireProjection();
    const track = this.#requireBaseTrack();
    if (!draft.trackId || draft.trackId !== track.id) throw new Error("Choose one current Caption Track");
    if (draft.text.length === 0) throw new Error("Caption text cannot be empty");
    if (draft.language.trim().length === 0) throw new Error("Caption language cannot be empty");
    if (draft.kind === "create") {
      if (!draft.inCaptured || !draft.outCaptured) throw new Error("Capture explicit In and Out marks before creating");
      const range = this.#draftRange(draft);
      return {
        projectId: projection.projectId,
        sequenceId: projection.sequenceId,
        kind: "create",
        trackId: track.id,
        trackRevision: track.revision,
        range,
        language: draft.language,
        text: draft.text,
      };
    }
    if (!draft.dirty) return undefined;
    const range = this.#draftRange(draft);
    if (!draft.alignmentHandling) throw new Error("Choose how dependent Alignments should change");
    if (draft.alignmentHandling === "preserve-if-provable" && this.#contentChanged(draft)) {
      throw new Error("Text or language changes require mark-stale or unbind");
    }
    const caption = this.#requireBaseCaption();
    return {
      projectId: projection.projectId,
      sequenceId: projection.sequenceId,
      kind: "update",
      captionId: caption.id,
      captionRevision: caption.revision,
      trackId: track.id,
      trackRevision: track.revision,
      range,
      language: draft.language,
      text: draft.text,
      alignmentHandling: draft.alignmentHandling,
    };
  }

  async #commit(input: CreatorManualCaptionGestureInput, intent: string): Promise<CreatorEditCommit | undefined> {
    if (this.#inFlight) return undefined;
    const generation = this.#generation;
    this.#inFlight = true;
    this.#attempt = undefined;
    this.#snapshot = {
      ...this.#snapshot,
      phase: "planning",
      review: undefined,
      error: undefined,
      canRetryIdenticalApply: false,
    };
    this.#emit();
    try {
      const review = await this.#captions.preview(input);
      if (generation !== this.#generation) return undefined;
      const attempt: ApplyAttempt = {
        gesture: input,
        review,
        input: { requestId: `ui:creator-caption:${input.kind}:${crypto.randomUUID()}`, intent },
        draftVersion: this.#draftVersion,
      };
      this.#attempt = attempt;
      this.#inFlight = false;
      return this.#apply(attempt, generation);
    } catch (value) {
      if (generation === this.#generation) this.#fail(value, false);
      return undefined;
    } finally {
      if (generation === this.#generation && this.#snapshot.phase === "planning") this.#inFlight = false;
    }
  }

  async #apply(attempt: ApplyAttempt, generation: number): Promise<CreatorEditCommit | undefined> {
    this.#inFlight = true;
    this.#snapshot = {
      ...this.#snapshot,
      phase: "applying",
      review: attempt.review,
      error: undefined,
      canRetryIdenticalApply: false,
    };
    this.#emit();
    try {
      const receipt = await this.#captions.apply(attempt.review, attempt.input);
      if (generation !== this.#generation) return undefined;
      this.#acceptCommit(attempt, receipt);
      return receipt;
    } catch (value) {
      if (generation === this.#generation) this.#fail(value, true);
      return undefined;
    } finally {
      if (generation === this.#generation) this.#inFlight = false;
    }
  }

  #acceptCommit(attempt: ApplyAttempt, receipt: CreatorEditCommit): void {
    this.#attempt = undefined;
    if (attempt.gesture.kind === "create" || attempt.gesture.kind === "remove") {
      this.#baseCaption = undefined;
      this.#baseTrack = undefined;
      this.#snapshot = {
        ...this.#snapshot,
        phase: "committed",
        draft: undefined,
        review: attempt.review,
        error: undefined,
        canRetryIdenticalApply: false,
      };
      this.#emit();
      return;
    }
    const caption = this.#requireBaseCaption();
    const revision = receipt.changes.find((change) => change.kind === "caption" && change.id === caption.id)?.revision;
    const trackRevision = receipt.changes.find(
      (change) => change.kind === "track" && change.id === caption.trackId,
    )?.revision;
    this.#baseCaption = {
      ...caption,
      revision: revision ?? caption.revision,
      range: attempt.gesture.range,
      language: attempt.gesture.language,
      text: attempt.gesture.text,
    };
    if (trackRevision && this.#baseTrack) this.#baseTrack = { ...this.#baseTrack, revision: trackRevision };
    const draft = this.#snapshot.draft;
    const newerDraft = draft && this.#draftVersion !== attempt.draftVersion;
    this.#snapshot = {
      ...this.#snapshot,
      phase: newerDraft ? "drafting" : "committed",
      ...(draft
        ? {
            draft: newerDraft
              ? { ...draft, dirty: draftDiffersFromCaption(draft, this.#baseCaption) }
              : existingDraft(this.#baseCaption),
          }
        : {}),
      review: attempt.review,
      error: undefined,
      canRetryIdenticalApply: false,
    };
    this.#emit();
  }

  #fail(value: unknown, applying: boolean): void {
    const error = value instanceof Error ? value : new Error(String(value));
    if (error instanceof CreatorEditError && error.code === "conflict") {
      this.#conflict(error);
      this.#emit();
      return;
    }
    const ambiguous =
      applying && Boolean(this.#attempt) && (!(error instanceof CreatorEditError) || error.status >= 500);
    this.#snapshot = {
      ...this.#snapshot,
      phase: "error",
      error,
      canRetryIdenticalApply: ambiguous,
    };
    this.#emit();
  }

  #conflict(error: Error): void {
    this.#attempt = undefined;
    this.#snapshot = {
      ...this.#snapshot,
      phase: "conflict",
      review: undefined,
      error,
      canRetryIdenticalApply: false,
    };
  }

  #changeDraft(draft: ManualCaptionDraft): void {
    this.#draftVersion += 1;
    const dirty = draft.kind === "create" || !this.#baseCaption || draftDiffersFromCaption(draft, this.#baseCaption);
    this.#snapshot = {
      ...this.#snapshot,
      phase: this.#inFlight ? this.#snapshot.phase : "drafting",
      draft: { ...draft, dirty },
      review: this.#inFlight ? this.#snapshot.review : undefined,
      error: undefined,
      canRetryIdenticalApply: Boolean(this.#attempt),
    };
    this.#emit();
  }

  #withContentChange(draft: ManualCaptionDraft): ManualCaptionDraft {
    if (draft.kind !== "update") return draft;
    const contentChanged = this.#contentChanged(draft);
    if (contentChanged && draft.alignmentHandling === "preserve-if-provable") {
      return { ...draft, alignmentHandling: undefined };
    }
    if (!contentChanged && draft.alignmentHandling === undefined) {
      return { ...draft, alignmentHandling: "preserve-if-provable" };
    }
    return draft;
  }

  #contentChanged(draft: ManualCaptionDraft): boolean {
    return Boolean(
      this.#baseCaption && (draft.text !== this.#baseCaption.text || draft.language !== this.#baseCaption.language),
    );
  }

  #draftRange(draft: ManualCaptionDraft) {
    if (draft.kind === "update" && !draft.inCaptured && !draft.outCaptured) {
      return this.#requireBaseCaption().range;
    }
    if (!draft.inPoint || !draft.outPoint || compareTime(draft.outPoint, draft.inPoint) <= 0) {
      throw new Error("Caption Out must be later than In");
    }
    const start = draft.inCaptured ? draft.inPoint : this.#requireBaseCaption().range.start;
    const end = draft.outCaptured
      ? draft.outPoint
      : addTime(this.#requireBaseCaption().range.start, this.#requireBaseCaption().range.duration);
    if (compareTime(end, start) <= 0) throw new Error("Caption Out must be later than In");
    return { start, duration: subtractTime(end, start) };
  }

  #requireDraft(kind?: ManualCaptionDraft["kind"]): ManualCaptionDraft {
    const draft = this.#snapshot.draft;
    if (!draft || (kind && draft.kind !== kind)) throw new Error("Open the required Caption draft first");
    return draft;
  }

  #requireProjection(): ManualCaptionProjection {
    if (!this.#projection) throw new Error("Caption projection is unavailable");
    return this.#projection;
  }

  #requireBaseCaption(): Caption {
    if (!this.#baseCaption) throw new Error("Committed Caption base is unavailable");
    return this.#baseCaption;
  }

  #requireBaseTrack(): Track {
    if (!this.#baseTrack) throw new Error("Choose one current Caption Track");
    return this.#baseTrack;
  }

  #resetAttempt(): void {
    this.#generation += 1;
    this.#inFlight = false;
    this.#attempt = undefined;
    this.#draftVersion += 1;
  }

  #emit(): void {
    for (const listener of this.#listeners) listener();
  }
}

function existingDraft(caption: Caption): ManualCaptionDraft {
  return {
    kind: "update",
    captionId: caption.id,
    trackId: caption.trackId,
    inPoint: caption.range.start,
    outPoint: addTime(caption.range.start, caption.range.duration),
    inCaptured: false,
    outCaptured: false,
    language: caption.language,
    text: caption.text,
    alignmentHandling: "preserve-if-provable",
    dirty: false,
  };
}

function draftDiffersFromCaption(draft: ManualCaptionDraft, caption: Caption): boolean {
  const captionEnd = addTime(caption.range.start, caption.range.duration);
  return (
    draft.kind !== "update" ||
    draft.text !== caption.text ||
    draft.language !== caption.language ||
    (draft.inCaptured && draft.inPoint !== undefined && compareTime(draft.inPoint, caption.range.start) !== 0) ||
    (draft.outCaptured && draft.outPoint !== undefined && compareTime(draft.outPoint, captionEnd) !== 0)
  );
}

function compareTime(left: RationalTime, right: RationalTime): number {
  const difference = BigInt(left.value) * BigInt(right.scale) - BigInt(right.value) * BigInt(left.scale);
  return difference < 0n ? -1 : difference > 0n ? 1 : 0;
}

function addTime(left: RationalTime, right: RationalTime): RationalTime {
  return rational(
    BigInt(left.value) * BigInt(right.scale) + BigInt(right.value) * BigInt(left.scale),
    BigInt(left.scale) * BigInt(right.scale),
  );
}

function subtractTime(left: RationalTime, right: RationalTime): RationalTime {
  return rational(
    BigInt(left.value) * BigInt(right.scale) - BigInt(right.value) * BigInt(left.scale),
    BigInt(left.scale) * BigInt(right.scale),
  );
}

function rational(value: bigint, scale: bigint): RationalTime {
  if (scale <= 0n) throw new Error("Rational time scale must be positive");
  const divisor = gcd(value < 0n ? -value : value, scale);
  const reducedScale = scale / divisor;
  if (reducedScale > 2_147_483_647n) throw new Error("Rational time scale exceeds int32");
  return { value: int64String(String(value / divisor)), scale: Number(reducedScale) };
}

function gcd(left: bigint, right: bigint): bigint {
  let a = left;
  let b = right;
  while (b !== 0n) {
    const next = a % b;
    a = b;
    b = next;
  }
  return a === 0n ? 1n : a;
}
