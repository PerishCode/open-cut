import {
  type Alignment,
  type Clip,
  type CreatorCaptionPort,
  type CreatorCaptionReview,
  type CreatorEditCommit,
  CreatorEditError,
  type DurableID,
  type SourceExcerpt,
  type Track,
} from "@open-cut/contracts";

export type CreatorCaptionPhase =
  | "idle"
  | "ready"
  | "previewing"
  | "review"
  | "applying"
  | "success"
  | "conflict"
  | "error";

export type CreatorCaptionSource = Readonly<{
  sourceExcerpt: SourceExcerpt;
  evidenceStatus: "exact" | "stale";
}>;

export type CreatorCaptionClipCandidate = Readonly<{
  clip: Clip;
  recommendation: "exact-alignment" | "source-stream" | "compatible-range";
}>;

export type CreatorCaptionSnapshot = Readonly<{
  phase: CreatorCaptionPhase;
  source?: CreatorCaptionSource;
  clipCandidates: readonly CreatorCaptionClipCandidate[];
  trackCandidates: readonly Track[];
  selectedClip?: Clip;
  selectedTrack?: Track;
  review?: CreatorCaptionReview;
  error?: Error;
  canRetryIdenticalApply: boolean;
}>;

export type CreatorCaptionProjection = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  source?: CreatorCaptionSource;
  clips: readonly Clip[];
  alignments: readonly Alignment[];
  tracks: readonly Track[];
}>;

type ApplyAttempt = Readonly<{
  review: CreatorCaptionReview;
  input: Readonly<{ requestId: string; intent: string }>;
}>;

const initialSnapshot: CreatorCaptionSnapshot = {
  phase: "idle",
  clipCandidates: [],
  trackCandidates: [],
  canRetryIdenticalApply: false,
};

export class CreatorCaptionController {
  readonly #captions: CreatorCaptionPort;
  readonly #listeners = new Set<() => void>();
  #snapshot: CreatorCaptionSnapshot = initialSnapshot;
  #projection?: CreatorCaptionProjection;
  #attempt?: ApplyAttempt;
  #clipExplicit = false;
  #trackExplicit = false;
  #generation = 0;
  #inFlight = false;

  constructor(captions: CreatorCaptionPort) {
    this.#captions = captions;
  }

  getSnapshot = (): CreatorCaptionSnapshot => this.#snapshot;

  subscribe = (listener: () => void): (() => void) => {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  };

  setProjection(projection: CreatorCaptionProjection): void {
    const previous = this.#projection;
    this.#projection = projection;
    const sourceChanged = sourceKey(previous?.source) !== sourceKey(projection.source);
    const scopeChanged =
      previous !== undefined &&
      (previous.projectId !== projection.projectId || previous.sequenceId !== projection.sequenceId);
    const clipCandidates = captionClipCandidates(projection.source, projection.clips, projection.alignments);
    const trackCandidates = projection.tracks
      .filter((track) => track.type === "caption")
      .sort((left, right) => left.label.localeCompare(right.label) || left.id.localeCompare(right.id));
    if (!projection.source) {
      this.#resetSelection();
      this.#snapshot = initialSnapshot;
      this.#emit();
      return;
    }

    const currentClip = this.#snapshot.selectedClip;
    const currentTrack = this.#snapshot.selectedTrack;
    const selectedClip = sourceChanged
      ? uniqueClip(clipCandidates)
      : retainClip(currentClip, clipCandidates, this.#clipExplicit);
    const selectedTrack = sourceChanged
      ? uniqueTrack(trackCandidates)
      : retainTrack(currentTrack, trackCandidates, this.#trackExplicit);
    const revisionChanged =
      (currentClip !== undefined && selectedClip?.revision !== currentClip.revision) ||
      (currentTrack !== undefined && selectedTrack?.revision !== currentTrack.revision);
    const invalidated =
      sourceChanged ||
      scopeChanged ||
      revisionChanged ||
      selectedClip?.id !== currentClip?.id ||
      selectedTrack?.id !== currentTrack?.id;
    if (invalidated) {
      this.#invalidateReview();
    }
    if (sourceChanged) {
      this.#clipExplicit = false;
      this.#trackExplicit = false;
    }
    this.#snapshot = {
      ...(invalidated ? { phase: "ready" as const, canRetryIdenticalApply: false } : this.#snapshot),
      source: projection.source,
      clipCandidates,
      trackCandidates,
      ...(selectedClip ? { selectedClip } : {}),
      ...(selectedTrack ? { selectedTrack } : {}),
    };
    this.#emit();
  }

  selectClip(clipId: DurableID): void {
    const candidate = this.#snapshot.clipCandidates.find((value) => value.clip.id === clipId);
    if (!candidate) throw new Error("Caption Clip is not a compatible visible candidate");
    this.#clipExplicit = true;
    this.#invalidateReview();
    this.#snapshot = {
      ...this.#snapshot,
      phase: "ready",
      selectedClip: candidate.clip,
      review: undefined,
      error: undefined,
      canRetryIdenticalApply: false,
    };
    this.#emit();
  }

  selectTrack(trackId: DurableID): void {
    const track = this.#snapshot.trackCandidates.find((candidate) => candidate.id === trackId);
    if (!track) throw new Error("Caption Track is not a current candidate");
    this.#trackExplicit = true;
    this.#invalidateReview();
    this.#snapshot = {
      ...this.#snapshot,
      phase: "ready",
      selectedTrack: track,
      review: undefined,
      error: undefined,
      canRetryIdenticalApply: false,
    };
    this.#emit();
  }

  async preview(): Promise<CreatorCaptionReview | undefined> {
    const projection = this.#projection;
    const source = this.#snapshot.source;
    const clip = this.#snapshot.selectedClip;
    const track = this.#snapshot.selectedTrack;
    if (!projection || !source || !clip || !track || this.#inFlight) {
      throw new Error("Caption preview requires one SourceExcerpt, Clip, and Caption Track");
    }
    if (source.evidenceStatus !== "exact") throw new Error("Caption preview requires exact SourceExcerpt evidence");
    const generation = ++this.#generation;
    this.#inFlight = true;
    this.#attempt = undefined;
    this.#snapshot = {
      ...this.#snapshot,
      phase: "previewing",
      review: undefined,
      error: undefined,
      canRetryIdenticalApply: false,
    };
    this.#emit();
    try {
      const review = await this.#captions.preview({
        projectId: projection.projectId,
        sequenceId: projection.sequenceId,
        sourceExcerptId: source.sourceExcerpt.id,
        sourceExcerptRevision: source.sourceExcerpt.revision,
        clipId: clip.id,
        clipRevision: clip.revision,
        trackId: track.id,
        trackRevision: track.revision,
      });
      if (generation !== this.#generation) return undefined;
      this.#snapshot = {
        ...this.#snapshot,
        phase: "review",
        review,
        error: undefined,
        canRetryIdenticalApply: false,
      };
      this.#emit();
      return review;
    } catch (value) {
      if (generation === this.#generation) this.#fail(value, false);
      return undefined;
    } finally {
      if (generation === this.#generation) this.#inFlight = false;
    }
  }

  apply(): Promise<CreatorEditCommit | undefined> {
    const review = this.#snapshot.review;
    if (!review || this.#snapshot.phase !== "review" || this.#inFlight) {
      throw new Error("Caption Apply requires the current immutable review");
    }
    const attempt = {
      review,
      input: {
        requestId: `ui:creator-caption:${crypto.randomUUID()}`,
        intent: "Create reviewed readable captions",
      },
    } as const;
    this.#attempt = attempt;
    return this.#apply(attempt, ++this.#generation);
  }

  retryIdenticalApply(): Promise<CreatorEditCommit | undefined> {
    const attempt = this.#attempt;
    if (!attempt || this.#inFlight) return Promise.resolve(undefined);
    return this.#apply(attempt, ++this.#generation);
  }

  clear(): void {
    this.#resetSelection();
    this.#projection = undefined;
    this.#snapshot = initialSnapshot;
    this.#emit();
  }

  close(): void {
    this.clear();
    this.#listeners.clear();
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
      this.#attempt = undefined;
      this.#snapshot = {
        ...this.#snapshot,
        phase: "success",
        error: undefined,
        canRetryIdenticalApply: false,
      };
      this.#emit();
      return receipt;
    } catch (value) {
      if (generation === this.#generation) this.#fail(value, true);
      return undefined;
    } finally {
      if (generation === this.#generation) this.#inFlight = false;
    }
  }

  #fail(value: unknown, applying: boolean): void {
    const error = value instanceof Error ? value : new Error(String(value));
    if (error instanceof CreatorEditError && error.code === "conflict") {
      this.#attempt = undefined;
      this.#clipExplicit = false;
      this.#trackExplicit = false;
      this.#snapshot = {
        ...this.#snapshot,
        phase: "conflict",
        selectedClip: undefined,
        selectedTrack: undefined,
        review: undefined,
        error,
        canRetryIdenticalApply: false,
      };
    } else {
      this.#snapshot = {
        ...this.#snapshot,
        phase: "error",
        error,
        canRetryIdenticalApply: applying && Boolean(this.#attempt),
      };
    }
    this.#emit();
  }

  #invalidateReview(): void {
    this.#generation += 1;
    this.#inFlight = false;
    this.#attempt = undefined;
  }

  #resetSelection(): void {
    this.#invalidateReview();
    this.#clipExplicit = false;
    this.#trackExplicit = false;
  }

  #emit(): void {
    for (const listener of this.#listeners) listener();
  }
}

export function captionClipCandidates(
  source: CreatorCaptionSource | undefined,
  clips: readonly Clip[],
  alignments: readonly Alignment[],
): readonly CreatorCaptionClipCandidate[] {
  if (!source) return [];
  const excerpt = source.sourceExcerpt;
  return clips
    .filter(
      (clip) =>
        clip.enabled &&
        !clip.tombstoned &&
        clip.assetId === excerpt.assetId &&
        containsRange(clip.sourceRange, excerpt.sourceRange),
    )
    .map((clip) => ({
      clip,
      recommendation: isExactlyAligned(excerpt, clip, alignments)
        ? ("exact-alignment" as const)
        : clip.sourceStreamId === excerpt.evidence.sourceStreamId
          ? ("source-stream" as const)
          : ("compatible-range" as const),
    }))
    .sort(
      (left, right) =>
        recommendationRank(left.recommendation) - recommendationRank(right.recommendation) ||
        compareTime(left.clip.timelineRange.start, right.clip.timelineRange.start) ||
        left.clip.id.localeCompare(right.clip.id),
    );
}

function isExactlyAligned(excerpt: SourceExcerpt, clip: Clip, alignments: readonly Alignment[]): boolean {
  return alignments.some(
    (alignment) =>
      alignment.status === "exact" &&
      alignment.narrativeNodeId === excerpt.id &&
      alignment.narrativeNodeRevision === excerpt.revision &&
      alignment.targets.some(
        (target) =>
          target.type === "clip" && target.clip.clipId === clip.id && target.clip.clipRevision === clip.revision,
      ),
  );
}

function containsRange(container: Clip["sourceRange"], value: SourceExcerpt["sourceRange"]): boolean {
  return compareTime(container.start, value.start) <= 0 && compareRangeEnd(container, value) >= 0;
}

function compareRangeEnd(left: Clip["sourceRange"], right: SourceExcerpt["sourceRange"]): number {
  const leftEnd = rangeEnd(left);
  const rightEnd = rangeEnd(right);
  const difference = leftEnd.numerator * rightEnd.denominator - rightEnd.numerator * leftEnd.denominator;
  return difference < 0n ? -1 : difference > 0n ? 1 : 0;
}

function rangeEnd(range: Clip["sourceRange"]) {
  return {
    numerator:
      BigInt(range.start.value) * BigInt(range.duration.scale) +
      BigInt(range.duration.value) * BigInt(range.start.scale),
    denominator: BigInt(range.start.scale) * BigInt(range.duration.scale),
  };
}

function compareTime(
  left: Readonly<{ value: string; scale: number }>,
  right: Readonly<{ value: string; scale: number }>,
): number {
  const difference = BigInt(left.value) * BigInt(right.scale) - BigInt(right.value) * BigInt(left.scale);
  return difference < 0n ? -1 : difference > 0n ? 1 : 0;
}

function recommendationRank(value: CreatorCaptionClipCandidate["recommendation"]): number {
  return value === "exact-alignment" ? 0 : value === "source-stream" ? 1 : 2;
}

function sourceKey(source: CreatorCaptionSource | undefined): string {
  return source ? `${source.sourceExcerpt.id}\u0000${source.sourceExcerpt.revision}\u0000${source.evidenceStatus}` : "";
}

function uniqueClip(values: readonly CreatorCaptionClipCandidate[]): Clip | undefined {
  return values.length === 1 ? values[0]?.clip : undefined;
}

function uniqueTrack(values: readonly Track[]): Track | undefined {
  return values.length === 1 ? values[0] : undefined;
}

function retainClip(
  current: Clip | undefined,
  candidates: readonly CreatorCaptionClipCandidate[],
  explicit: boolean,
): Clip | undefined {
  const retained = current && candidates.find((candidate) => candidate.clip.id === current.id)?.clip;
  if (retained?.revision !== current?.revision) return uniqueClip(candidates);
  if (!explicit && candidates.length !== 1) return undefined;
  return retained ?? uniqueClip(candidates);
}

function retainTrack(current: Track | undefined, candidates: readonly Track[], explicit: boolean): Track | undefined {
  const retained = current && candidates.find((candidate) => candidate.id === current.id);
  if (retained?.revision !== current?.revision) return uniqueTrack(candidates);
  if (!explicit && candidates.length !== 1) return undefined;
  return retained ?? uniqueTrack(candidates);
}
