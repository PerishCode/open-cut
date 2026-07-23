import type { MediaPlayerActuator } from "@open-cut/components";
import {
  type DurableID,
  int64String,
  type RationalTime,
  type RevisionString,
  type SequencePreviewContinuation,
  type SequencePreviewPreparation,
  type ViewerMediaPort,
} from "@open-cut/contracts";

export type SequenceViewerStatus = "idle" | "preparing" | "empty" | "ready" | "failed" | "unavailable";

export type SequenceViewerSnapshot = Readonly<{
  status: SequenceViewerStatus;
  projectId?: DurableID;
  sequenceId?: DurableID;
  pinnedRevision?: RevisionString;
  availableRevision?: RevisionString;
  playhead: RationalTime;
  playback: "paused" | "playing";
  preparation?: SequencePreviewPreparation;
  error?: Error;
}>;

export type SequenceViewerRuntime = Readonly<{
  now(): number;
  schedule(callback: () => void, delayMilliseconds: number): () => void;
}>;

const zeroPlayhead: RationalTime = { value: int64String("0"), scale: 1 };
const browserFrameBoundaryTolerance = 0.001;

const browserRuntime: SequenceViewerRuntime = {
  now: () => Date.now(),
  schedule: (callback, delay) => {
    const timer = window.setTimeout(callback, delay);
    return () => window.clearTimeout(timer);
  },
};

export class SequenceViewerController {
  readonly #viewer: ViewerMediaPort;
  readonly #runtime: SequenceViewerRuntime;
  readonly #listeners = new Set<() => void>();
  #snapshot: SequenceViewerSnapshot = { status: "idle", playhead: zeroPlayhead, playback: "paused" };
  #generation = 0;
  #abort?: AbortController;
  #cancelTimer: () => void = () => undefined;
  #retryDelay = 1_000;
  #actuator?: MediaPlayerActuator;

  constructor(viewer: ViewerMediaPort, runtime: SequenceViewerRuntime = browserRuntime) {
    this.#viewer = viewer;
    this.#runtime = runtime;
  }

  getSnapshot = (): SequenceViewerSnapshot => this.#snapshot;

  subscribe = (listener: () => void): (() => void) => {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  };

  open(projectId: DurableID, sequenceId: DurableID, revision: RevisionString): void {
    if (this.#snapshot.projectId === projectId && this.#snapshot.sequenceId === sequenceId) {
      this.setAvailableRevision(revision);
      return;
    }
    this.#actuator?.pause();
    this.#snapshot = {
      status: "preparing",
      projectId,
      sequenceId,
      pinnedRevision: revision,
      availableRevision: revision,
      playhead: zeroPlayhead,
      playback: "paused",
    };
    this.#emit();
    this.#begin("prepare");
  }

  setAvailableRevision(revision: RevisionString): void {
    const current = this.#snapshot.availableRevision;
    if (current !== undefined && BigInt(revision) <= BigInt(current)) return;
    this.#snapshot = { ...this.#snapshot, availableRevision: revision };
    this.#emit();
  }

  adoptRevision(revision: RevisionString): void {
    const snapshot = this.#snapshot;
    if (!snapshot.projectId || !snapshot.sequenceId || !snapshot.pinnedRevision) return;
    if (revision === snapshot.pinnedRevision) return;
    if (snapshot.availableRevision !== undefined && BigInt(revision) > BigInt(snapshot.availableRevision)) {
      throw new Error("cannot adopt an unobserved Sequence revision");
    }
    this.pause();
    this.#snapshot = {
      status: "preparing",
      projectId: snapshot.projectId,
      sequenceId: snapshot.sequenceId,
      pinnedRevision: revision,
      availableRevision: snapshot.availableRevision,
      playhead: snapshot.playhead,
      playback: "paused",
    };
    this.#emit();
    this.#begin("prepare");
  }

  wake(): void {
    const preparation = this.#snapshot.preparation;
    if (!preparation?.continuation || preparation.status === "failed") return;
    this.#begin("continue");
  }

  restart(): void {
    const preparation = this.#snapshot.preparation;
    if (preparation?.status === "failed") return;
    this.pause();
    this.#snapshot = { ...this.#snapshot, status: "preparing", playback: "paused", error: undefined };
    this.#emit();
    this.#begin(preparation?.continuation ? "continue" : "prepare");
  }

  retry(): void {
    const preparation = this.#snapshot.preparation;
    if (
      preparation?.status !== "failed" ||
      !preparation.continuation ||
      !preparation.diagnostics.some((diagnostic) => diagnostic.recovery === "retry-job")
    ) {
      throw new Error("Sequence preview is not retryable");
    }
    this.pause();
    this.#snapshot = { ...this.#snapshot, status: "preparing", playback: "paused", error: undefined };
    this.#emit();
    this.#begin("retry");
  }

  setPlayhead(playhead: RationalTime): void {
    const duration = this.#snapshot.preparation?.lease?.facts.semanticDuration;
    const settled = duration ? clampTime(playhead, duration) : playhead;
    this.#snapshot = { ...this.#snapshot, playhead: settled };
    this.#emit();
    this.#seekActuator(settled);
  }

  setPlaying(playing: boolean): void {
    if (playing && this.#snapshot.status !== "ready") return;
    this.#snapshot = { ...this.#snapshot, playback: playing ? "playing" : "paused" };
    this.#emit();
  }

  attachActuator(actuator: MediaPlayerActuator | undefined): void {
    this.#actuator = actuator;
  }

  syncActuator(): void {
    this.#seekActuator(this.#snapshot.playhead);
  }

  observePlaybackPosition(seconds: number): void {
    const facts = this.#snapshot.preparation?.lease?.facts;
    if (this.#snapshot.status !== "ready" || !facts || !Number.isFinite(seconds) || seconds < 0) return;
    try {
      const playhead = clampTime(frameTimeAtOrBefore(seconds, facts.frameRate), facts.semanticDuration);
      if (sameTime(playhead, this.#snapshot.playhead)) return;
      this.#snapshot = { ...this.#snapshot, playhead };
      this.#emit();
    } catch {
      // A browser clock observation can be rounded or temporarily invalid; it
      // never replaces the last exact logical playhead unless it maps cleanly.
    }
  }

  async play(): Promise<void> {
    const actuator = this.#actuator;
    const duration = this.#snapshot.preparation?.lease?.facts.semanticDuration;
    if (this.#snapshot.status !== "ready" || !actuator || !duration) {
      throw new Error("Sequence playback is unavailable");
    }
    if (compareTime(this.#snapshot.playhead, duration) >= 0) this.setPlayhead(zeroPlayhead);
    else this.#seekActuator(this.#snapshot.playhead);
    await actuator.play();
  }

  async togglePlayback(): Promise<void> {
    if (this.#snapshot.playback === "playing") {
      this.pause();
      return;
    }
    await this.play();
  }

  seekToStart(): void {
    this.pause();
    this.setPlayhead(zeroPlayhead);
  }

  stepFrame(direction: -1 | 1): void {
    const facts = this.#snapshot.preparation?.lease?.facts;
    if (this.#snapshot.status !== "ready" || !facts) throw new Error("Sequence frame-step is unavailable");
    const coordinate = sequenceFrameCoordinate(this.#snapshot.playhead, facts.frameRate);
    const frame =
      direction < 0
        ? coordinate.remainder === 0n
          ? maximumBigInt(0n, coordinate.index - 1n)
          : coordinate.index
        : coordinate.index + 1n;
    this.pause();
    this.setPlayhead(clampTime(frameTime(frame, facts.frameRate), facts.semanticDuration));
  }

  pause(): void {
    this.#actuator?.pause();
    if (this.#snapshot.playback === "paused") return;
    this.#snapshot = { ...this.#snapshot, playback: "paused" };
    this.#emit();
  }

  close(): void {
    this.#generation += 1;
    this.#abort?.abort();
    this.#abort = undefined;
    this.#cancelTimer();
    this.#cancelTimer = () => undefined;
    this.#actuator?.pause();
    this.#actuator = undefined;
    this.#snapshot = { status: "idle", playhead: zeroPlayhead, playback: "paused" };
    this.#emit();
  }

  #begin(operation: "prepare" | "continue" | "retry"): void {
    const snapshot = this.#snapshot;
    if (!snapshot.projectId || !snapshot.sequenceId || !snapshot.pinnedRevision) return;
    const continuation = snapshot.preparation?.continuation;
    if (operation !== "prepare" && !continuation) return;
    const generation = ++this.#generation;
    this.#abort?.abort();
    this.#cancelTimer();
    this.#cancelTimer = () => undefined;
    const abort = new AbortController();
    this.#abort = abort;
    const input = { expectedSequenceRevision: snapshot.pinnedRevision };
    const request =
      operation === "prepare"
        ? this.#viewer.prepareSequencePreview(snapshot.projectId, snapshot.sequenceId, input, abort.signal)
        : operation === "continue"
          ? this.#viewer.continueSequencePreview(
              snapshot.projectId,
              snapshot.sequenceId,
              { ...input, continuation: continuation as SequencePreviewContinuation },
              abort.signal,
            )
          : this.#viewer.retrySequencePreview(
              snapshot.projectId,
              snapshot.sequenceId,
              { ...input, continuation: continuation as SequencePreviewContinuation },
              abort.signal,
            );
    void request.then(
      (preparation) => this.#accept(generation, preparation),
      (value) => this.#reject(generation, value),
    );
  }

  #accept(generation: number, preparation: SequencePreviewPreparation): void {
    if (generation !== this.#generation) return;
    const snapshot = this.#snapshot;
    if (
      preparation.projectId !== snapshot.projectId ||
      preparation.sequenceId !== snapshot.sequenceId ||
      preparation.sequenceRevision !== snapshot.pinnedRevision
    ) {
      this.#reject(generation, new Error("Sequence preview escaped its pinned selection"));
      return;
    }
    this.#retryDelay = 1_000;
    const playhead = preparation.lease
      ? clampTime(snapshot.playhead, preparation.lease.facts.semanticDuration)
      : snapshot.playhead;
    this.#snapshot = {
      ...snapshot,
      status: preparation.status,
      preparation,
      playhead,
      playback: preparation.status === "ready" ? snapshot.playback : "paused",
      error: undefined,
    };
    this.#emit();
    if (preparation.status === "preparing") {
      this.#schedule(preparation.stage === "integrity" ? 100 : 1_000, "continue");
    } else if (preparation.status === "ready" && preparation.lease) {
      const renewIn = Date.parse(preparation.lease.expiresAt) - this.#runtime.now() - 30_000;
      this.#schedule(Math.max(1_000, renewIn), "continue");
    }
  }

  #reject(generation: number, value: unknown): void {
    if (generation !== this.#generation) return;
    const error = value instanceof Error ? value : new Error(String(value));
    const snapshot = this.#snapshot;
    const lease = snapshot.preparation?.status === "ready" ? snapshot.preparation.lease : undefined;
    if (lease && this.#runtime.now() < Date.parse(lease.expiresAt)) {
      this.#snapshot = { ...snapshot, error };
      this.#emit();
      const remaining = Date.parse(lease.expiresAt) - this.#runtime.now();
      this.#schedule(Math.min(remaining, this.#retryDelay), "continue");
      this.#retryDelay = Math.min(30_000, this.#retryDelay * 2);
      return;
    }
    this.#snapshot = { ...snapshot, status: "unavailable", playback: "paused", error };
    this.#emit();
  }

  #schedule(delay: number, operation: "continue"): void {
    this.#cancelTimer();
    this.#cancelTimer = this.#runtime.schedule(() => this.#begin(operation), Math.max(0, delay));
  }

  #emit(): void {
    for (const listener of this.#listeners) listener();
  }

  #seekActuator(playhead: RationalTime): void {
    if (!this.#actuator || this.#snapshot.status !== "ready") return;
    const seconds = Number(playhead.value) / playhead.scale;
    if (!Number.isFinite(seconds) || seconds < 0) return;
    try {
      this.#actuator.seekToSeconds(seconds);
    } catch {
      // Metadata can be between leases. onReady calls syncActuator once the
      // replacement media timeline is available.
    }
  }
}

function clampTime(value: RationalTime, maximum: RationalTime): RationalTime {
  if (BigInt(value.value) < 0n) return zeroPlayhead;
  const left = BigInt(value.value) * BigInt(maximum.scale);
  const right = BigInt(maximum.value) * BigInt(value.scale);
  return left > right ? maximum : value;
}

function sameTime(left: RationalTime, right: RationalTime): boolean {
  return BigInt(left.value) * BigInt(right.scale) === BigInt(right.value) * BigInt(left.scale);
}

function compareTime(left: RationalTime, right: RationalTime): number {
  const difference = BigInt(left.value) * BigInt(right.scale) - BigInt(right.value) * BigInt(left.scale);
  return difference < 0n ? -1 : difference > 0n ? 1 : 0;
}

function frameTimeAtOrBefore(seconds: number, frameRate: RationalTime): RationalTime {
  const rate = Number(frameRate.value) / frameRate.scale;
  const position = seconds * rate;
  if (!Number.isFinite(position) || position < 0 || !Number.isSafeInteger(Math.floor(position))) {
    throw new Error("Sequence playback clock exceeds its exact range");
  }
  return frameTime(BigInt(Math.floor(position + browserFrameBoundaryTolerance)), frameRate);
}

function sequenceFrameCoordinate(
  playhead: RationalTime,
  frameRate: RationalTime,
): Readonly<{ index: bigint; remainder: bigint }> {
  const numerator = BigInt(playhead.value) * BigInt(frameRate.value);
  const denominator = BigInt(playhead.scale) * BigInt(frameRate.scale);
  return { index: numerator / denominator, remainder: numerator % denominator };
}

function frameTime(frame: bigint, frameRate: RationalTime): RationalTime {
  return rational(frame * BigInt(frameRate.scale), BigInt(frameRate.value));
}

function rational(numerator: bigint, denominator: bigint): RationalTime {
  if (denominator <= 0n) throw new Error("Sequence playback rational is invalid");
  const divisor = greatestCommonDivisor(numerator, denominator);
  const value = numerator / divisor;
  const scale = denominator / divisor;
  if (scale > 2_147_483_647n) throw new Error("Sequence playback rational scale exceeds its bound");
  return { value: int64String(value.toString()), scale: Number(scale) };
}

function greatestCommonDivisor(left: bigint, right: bigint): bigint {
  let a = left < 0n ? -left : left;
  let b = right;
  while (b !== 0n) [a, b] = [b, a % b];
  return a;
}

function maximumBigInt(left: bigint, right: bigint): bigint {
  return left > right ? left : right;
}
