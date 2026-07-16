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
    this.#snapshot = { ...this.#snapshot, status: "preparing", playback: "paused", error: undefined };
    this.#emit();
    this.#begin("retry");
  }

  setPlayhead(playhead: RationalTime): void {
    const duration = this.#snapshot.preparation?.lease?.facts.semanticDuration;
    this.#snapshot = { ...this.#snapshot, playhead: duration ? clampTime(playhead, duration) : playhead };
    this.#emit();
  }

  setPlaying(playing: boolean): void {
    if (playing && this.#snapshot.status !== "ready") return;
    this.#snapshot = { ...this.#snapshot, playback: playing ? "playing" : "paused" };
    this.#emit();
  }

  attachActuator(actuator: MediaPlayerActuator | undefined): void {
    this.#actuator = actuator;
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
}

function clampTime(value: RationalTime, maximum: RationalTime): RationalTime {
  if (BigInt(value.value) < 0n) return zeroPlayhead;
  const left = BigInt(value.value) * BigInt(maximum.scale);
  const right = BigInt(maximum.value) * BigInt(value.scale);
  return left > right ? maximum : value;
}
