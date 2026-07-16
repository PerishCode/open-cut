import {
  type Clip,
  type CreatorEditCommit,
  CreatorEditError,
  type CreatorTimelineAlignmentHandling,
  type CreatorTimelineBlockedRecovery,
  type CreatorTimelineGestureBlocked,
  type CreatorTimelineGestureInput,
  type CreatorTimelineGestureReview,
  type CreatorTimelinePort,
  type CreatorTimelineScope,
  type CreatorTimelineSelectionHint,
  type DurableID,
  int64String,
  type RationalTime,
  type Track,
} from "@open-cut/contracts";

export type CreatorTimelinePhase =
  | "idle"
  | "selected"
  | "planning"
  | "blocked"
  | "applying"
  | "committed"
  | "conflict"
  | "error";

export type CreatorTimelineSnapshot = Readonly<{
  phase: CreatorTimelinePhase;
  selectedClip?: Clip;
  scope?: CreatorTimelineScope;
  alignmentHandling?: CreatorTimelineAlignmentHandling;
  review?: CreatorTimelineGestureReview;
  blocked?: CreatorTimelineGestureBlocked;
  selectionHint?: CreatorTimelineSelectionHint;
  error?: Error;
  canRetryIdenticalApply: boolean;
}>;

export interface TimelinePlayheadAuthority {
  getSnapshot(): Readonly<{ playhead: RationalTime }>;
  setPlayhead(value: RationalTime): void;
}

type TimelineProjection = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  clips: readonly Clip[];
  tracks: readonly Track[];
}>;

type ApplyAttempt = Readonly<{
  review: CreatorTimelineGestureReview;
  input: Readonly<{ requestId: string; intent: string }>;
}>;

const initialSnapshot: CreatorTimelineSnapshot = {
  phase: "idle",
  canRetryIdenticalApply: false,
};

export class CreatorTimelineController {
  readonly #timeline: CreatorTimelinePort;
  readonly #playhead: TimelinePlayheadAuthority;
  readonly #listeners = new Set<() => void>();
  #snapshot: CreatorTimelineSnapshot = initialSnapshot;
  #projection?: TimelineProjection;
  #attempt?: ApplyAttempt;
  #blockedInput?: CreatorTimelineGestureInput;
  #pendingSelection?: Readonly<{
    hint: CreatorTimelineSelectionHint;
    kind: CreatorTimelineGestureInput["kind"];
  }>;
  #generation = 0;
  #inFlight = false;

  constructor(timeline: CreatorTimelinePort, playhead: TimelinePlayheadAuthority) {
    this.#timeline = timeline;
    this.#playhead = playhead;
  }

  getSnapshot = (): CreatorTimelineSnapshot => this.#snapshot;

  subscribe = (listener: () => void): (() => void) => {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  };

  setProjection(projection: TimelineProjection): void {
    this.#projection = projection;
    if (this.#pendingSelection) {
      const pending = this.#pendingSelection;
      this.#pendingSelection = undefined;
      const current = projection.clips.find(
        (clip) => clip.id === pending.hint.clipId && clip.revision === pending.hint.revision && !clip.tombstoned,
      );
      if (!current) {
        this.clearSelection();
        return;
      }
      this.#snapshot = {
        ...this.#snapshot,
        selectedClip: current,
        scope: pending.kind === "split" ? (current.linkGroupId ? undefined : "single") : this.#snapshot.scope,
        alignmentHandling: pending.kind === "split" ? undefined : this.#snapshot.alignmentHandling,
        selectionHint: undefined,
      };
      this.#emit();
      return;
    }
    const selected = this.#snapshot.selectedClip;
    if (!selected) return;
    const current = projection.clips.find((clip) => clip.id === selected.id && !clip.tombstoned);
    if (!current || current.revision !== selected.revision) {
      this.clearSelection();
      return;
    }
    this.#snapshot = { ...this.#snapshot, selectedClip: current };
    this.#emit();
  }

  selectClip(clipId: DurableID): void {
    const clip = this.#projection?.clips.find((candidate) => candidate.id === clipId && !candidate.tombstoned);
    if (!clip) throw new Error("Timeline Clip is not in the current projection");
    this.#generation += 1;
    this.#attempt = undefined;
    this.#blockedInput = undefined;
    this.#pendingSelection = undefined;
    this.#snapshot = {
      phase: "selected",
      selectedClip: clip,
      scope: clip.linkGroupId ? undefined : "single",
      canRetryIdenticalApply: false,
    };
    this.#emit();
  }

  clearSelection(): void {
    this.#generation += 1;
    this.#attempt = undefined;
    this.#blockedInput = undefined;
    this.#pendingSelection = undefined;
    this.#snapshot = initialSnapshot;
    this.#emit();
  }

  chooseScope(scope: CreatorTimelineScope): void {
    const clip = this.#snapshot.selectedClip;
    if (!clip) throw new Error("Select a Timeline Clip before choosing scope");
    if (scope === "linked" && !clip.linkGroupId) throw new Error("The selected Clip has no LinkGroup");
    this.#snapshot = {
      ...this.#snapshot,
      phase: "selected",
      scope,
      review: undefined,
      blocked: undefined,
      error: undefined,
      canRetryIdenticalApply: false,
    };
    this.#attempt = undefined;
    this.#blockedInput = undefined;
    this.#emit();
  }

  chooseAlignmentHandling(value: CreatorTimelineAlignmentHandling): void {
    if (!this.#snapshot.selectedClip) throw new Error("Select a Timeline Clip before choosing Alignment handling");
    this.#snapshot = {
      ...this.#snapshot,
      phase: "selected",
      alignmentHandling: value,
      review: undefined,
      blocked: undefined,
      error: undefined,
      canRetryIdenticalApply: false,
    };
    this.#attempt = undefined;
    this.#blockedInput = undefined;
    this.#emit();
  }

  setPlayhead(value: RationalTime): void {
    this.#playhead.setPlayhead(value);
  }

  moveToPlayhead(): Promise<CreatorEditCommit | undefined> {
    const { projection, clip, common } = this.#gestureContext();
    const track = projection.tracks.find((candidate) => candidate.id === clip.trackId);
    if (!track) throw new Error("The selected Clip Track is outside the current projection");
    return this.#commit({
      ...common,
      kind: "move",
      trackId: track.id,
      trackRevision: track.revision,
      timelineStart: this.#playhead.getSnapshot().playhead,
    });
  }

  trimStartToPlayhead(): Promise<CreatorEditCommit | undefined> {
    const { clip, common } = this.#gestureContext();
    const playhead = this.#playhead.getSnapshot().playhead;
    const timelineEnd = addTime(clip.timelineRange.start, clip.timelineRange.duration);
    requireInteriorPoint(playhead, clip.timelineRange.start, timelineEnd, "trim start");
    const leftTrim = subtractTime(playhead, clip.timelineRange.start);
    const duration = subtractTime(timelineEnd, playhead);
    return this.#commit({
      ...common,
      kind: "trim",
      sourceRange: { start: addTime(clip.sourceRange.start, leftTrim), duration },
      timelineRange: { start: playhead, duration },
    });
  }

  trimEndToPlayhead(): Promise<CreatorEditCommit | undefined> {
    const { clip, common } = this.#gestureContext();
    const playhead = this.#playhead.getSnapshot().playhead;
    const timelineEnd = addTime(clip.timelineRange.start, clip.timelineRange.duration);
    requireInteriorPoint(playhead, clip.timelineRange.start, timelineEnd, "trim end");
    const duration = subtractTime(playhead, clip.timelineRange.start);
    return this.#commit({
      ...common,
      kind: "trim",
      sourceRange: { start: clip.sourceRange.start, duration },
      timelineRange: { start: clip.timelineRange.start, duration },
    });
  }

  splitAtPlayhead(): Promise<CreatorEditCommit | undefined> {
    const { clip, common } = this.#gestureContext();
    const playhead = this.#playhead.getSnapshot().playhead;
    requireInteriorPoint(
      playhead,
      clip.timelineRange.start,
      addTime(clip.timelineRange.start, clip.timelineRange.duration),
      "split",
    );
    return this.#commit({ ...common, kind: "split", splitAt: playhead });
  }

  remove(): Promise<CreatorEditCommit | undefined> {
    if (
      this.#snapshot.selectedClip &&
      (this.#snapshot.alignmentHandling === undefined || this.#snapshot.alignmentHandling === "preserve-if-provable")
    ) {
      throw new Error("Choose mark-stale or unbind before removing a Clip");
    }
    const { common } = this.#gestureContext();
    return this.#commit({ ...common, kind: "remove" });
  }

  recoverBlocked(recovery: CreatorTimelineBlockedRecovery): Promise<CreatorEditCommit | undefined> {
    const blocked = this.#snapshot.blocked;
    const input = this.#blockedInput;
    if (!blocked || !input || !blocked.recoveries.includes(recovery) || this.#inFlight) {
      return Promise.resolve(undefined);
    }
    if (recovery === "mark-stale" || recovery === "unbind") {
      const alignmentHandling = recovery === "mark-stale" ? "mark-stale" : "unbind";
      this.#snapshot = { ...this.#snapshot, alignmentHandling };
      return this.#commit({ ...input, alignmentHandling });
    }
    if (recovery === "choose-single") {
      this.#snapshot = { ...this.#snapshot, scope: "single" };
      return this.#commit({ ...input, scope: "single" });
    }
    return Promise.resolve(undefined);
  }

  retryIdenticalApply(): Promise<CreatorEditCommit | undefined> {
    const attempt = this.#attempt;
    if (!attempt || this.#inFlight) return Promise.resolve(undefined);
    return this.#apply(attempt, ++this.#generation);
  }

  close(): void {
    this.#generation += 1;
    this.#inFlight = false;
    this.#attempt = undefined;
    this.#blockedInput = undefined;
    this.#pendingSelection = undefined;
    this.#projection = undefined;
    this.#snapshot = initialSnapshot;
    this.#emit();
  }

  #gestureContext() {
    const projection = this.#projection;
    const clip = this.#snapshot.selectedClip;
    const scope = this.#snapshot.scope;
    const alignmentHandling = this.#snapshot.alignmentHandling;
    if (!projection || !clip || !scope || !alignmentHandling || this.#inFlight) {
      throw new Error("Timeline gesture is not ready");
    }
    return {
      projection,
      clip,
      common: {
        projectId: projection.projectId,
        sequenceId: projection.sequenceId,
        clipId: clip.id,
        clipRevision: clip.revision,
        scope,
        alignmentHandling,
      } as const,
    };
  }

  async #commit(input: CreatorTimelineGestureInput): Promise<CreatorEditCommit | undefined> {
    const generation = ++this.#generation;
    this.#inFlight = true;
    this.#attempt = undefined;
    this.#blockedInput = undefined;
    this.#snapshot = {
      ...this.#snapshot,
      phase: "planning",
      review: undefined,
      blocked: undefined,
      error: undefined,
      canRetryIdenticalApply: false,
    };
    this.#emit();
    try {
      const plan = await this.#timeline.preview(input);
      if (generation !== this.#generation) return undefined;
      if (plan.status === "blocked") {
        this.#inFlight = false;
        this.#blockedInput = input;
        this.#snapshot = {
          ...this.#snapshot,
          phase: "blocked",
          blocked: plan.blocked,
          review: undefined,
          error: undefined,
          canRetryIdenticalApply: false,
        };
        this.#emit();
        return undefined;
      }
      const review = plan.review;
      const attempt = {
        review,
        input: {
          requestId: `ui:creator-timeline:${input.kind}:${crypto.randomUUID()}`,
          intent: timelineIntent(input.kind),
        },
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
      const applied = await this.#timeline.apply(attempt.review, attempt.input);
      if (generation !== this.#generation) return undefined;
      this.#attempt = undefined;
      this.#blockedInput = undefined;
      if (applied.selectionHint) {
        this.#pendingSelection = { hint: applied.selectionHint, kind: attempt.review.kind };
      }
      this.#snapshot = {
        ...this.#snapshot,
        phase: "committed",
        review: attempt.review,
        blocked: undefined,
        ...(applied.selectionHint === undefined
          ? { selectedClip: undefined, scope: undefined, alignmentHandling: undefined, selectionHint: undefined }
          : { selectionHint: applied.selectionHint }),
        error: undefined,
        canRetryIdenticalApply: false,
      };
      this.#emit();
      return applied.commit;
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
      this.#snapshot = {
        ...this.#snapshot,
        phase: "conflict",
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

  #emit(): void {
    for (const listener of this.#listeners) listener();
  }
}

function timelineIntent(kind: CreatorTimelineGestureInput["kind"]): string {
  switch (kind) {
    case "move":
      return "Move selected Timeline Clip";
    case "trim":
      return "Trim selected Timeline Clip";
    case "split":
      return "Split selected Timeline Clip";
    case "remove":
      return "Remove selected Timeline Clip";
  }
}

function requireInteriorPoint(value: RationalTime, start: RationalTime, end: RationalTime, label: string): void {
  if (compareTime(value, start) <= 0 || compareTime(value, end) >= 0) {
    throw new Error(`Sequence playhead must be inside the Clip to ${label}`);
  }
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

function rational(numerator: bigint, denominator: bigint): RationalTime {
  const divisor = greatestCommonDivisor(numerator, denominator);
  const value = numerator / divisor;
  const scale = denominator / divisor;
  if (scale < 1n || scale > 2_147_483_647n) throw new Error("Timeline rational scale exceeds its bound");
  return { value: int64String(value.toString()), scale: Number(scale) };
}

function greatestCommonDivisor(left: bigint, right: bigint): bigint {
  let a = left < 0n ? -left : left;
  let b = right;
  while (b !== 0n) [a, b] = [b, a % b];
  return a;
}
