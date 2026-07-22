import type { MediaPlayerActuator } from "@open-cut/components";
import {
  type DurableID,
  int64String,
  type RationalTime,
  type SourcePositionResult,
  type SourcePreviewPreparation,
  type SourcePreviewSelectionInput,
  type SourcePreviewTrackTiming,
  type ViewerMediaPort,
} from "@open-cut/contracts";

export type SourceViewerStatus = "idle" | "preparing" | "ready" | "failed" | "unavailable";

export type SourceViewerSelection = SourcePreviewSelectionInput &
  Readonly<{
    projectId: DurableID;
    assetId: DurableID;
  }>;

export type SourceViewerMarks = Readonly<{
  in?: RationalTime;
  out?: RationalTime;
}>;

export type SourceViewerSnapshot = Readonly<{
  status: SourceViewerStatus;
  selection?: SourceViewerSelection;
  preparation?: SourcePreviewPreparation;
  playhead?: RationalTime;
  proxyPlayhead?: RationalTime;
  marks: SourceViewerMarks;
  playback: "paused" | "playing";
  error?: Error;
}>;

export type SourceViewerRuntime = Readonly<{
  now(): number;
  schedule(callback: () => void, delayMilliseconds: number): () => void;
}>;

const browserRuntime: SourceViewerRuntime = {
  now: () => Date.now(),
  schedule: (callback, delay) => {
    const timer = window.setTimeout(callback, delay);
    return () => window.clearTimeout(timer);
  },
};

export class SourceViewerController {
  readonly #viewer: ViewerMediaPort;
  readonly #runtime: SourceViewerRuntime;
  readonly #listeners = new Set<() => void>();
  #snapshot: SourceViewerSnapshot = { status: "idle", marks: {}, playback: "paused" };
  #generation = 0;
  #abort?: AbortController;
  #cancelTimer: () => void = () => undefined;
  #retryDelay = 1_000;
  #actuator?: MediaPlayerActuator;

  constructor(viewer: ViewerMediaPort, runtime: SourceViewerRuntime = browserRuntime) {
    this.#viewer = viewer;
    this.#runtime = runtime;
  }

  getSnapshot = (): SourceViewerSnapshot => this.#snapshot;

  subscribe = (listener: () => void): (() => void) => {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  };

  open(selection: SourceViewerSelection): void {
    if (sameSelection(this.#snapshot.selection, selection)) return;
    this.#snapshot = {
      status: "preparing",
      selection,
      marks: {},
      playback: "paused",
    };
    this.#emit();
    this.#begin();
  }

  attachActuator(actuator: MediaPlayerActuator | undefined): void {
    this.#actuator = actuator;
    const proxyPlayhead = this.#snapshot.proxyPlayhead;
    if (actuator && proxyPlayhead) actuator.seekToSeconds(rationalSeconds(proxyPlayhead));
  }

  setPlaying(playing: boolean): void {
    if (playing && this.#snapshot.status !== "ready") return;
    this.#snapshot = { ...this.#snapshot, playback: playing ? "playing" : "paused" };
    this.#emit();
  }

  pause(): void {
    this.#actuator?.pause();
    if (this.#snapshot.playback === "paused") return;
    this.#snapshot = { ...this.#snapshot, playback: "paused" };
    this.#emit();
  }

  async settleActuator(): Promise<SourcePositionResult> {
    const actuator = this.#actuator;
    const lease = this.#snapshot.preparation?.lease;
    if (!actuator || !lease) throw new Error("Source Viewer actuator is unavailable");
    const proxySeconds = actuator.readCurrentTimeSeconds();
    if (!Number.isFinite(proxySeconds) || proxySeconds < 0) throw new Error("Source Viewer media time is invalid");
    const approximateSource = addRational(lease.sourceEpoch, rationalFromBrowserSeconds(proxySeconds));
    const result = await this.#resolve("settle", approximateSource);
    this.#applyPosition(result);
    return result;
  }

  async seek(target: RationalTime): Promise<SourcePositionResult> {
    const result = await this.#resolve("settle", target);
    this.#applyPosition(result);
    return result;
  }

  async step(operation: "previous" | "next"): Promise<SourcePositionResult> {
    const playhead = this.#snapshot.playhead;
    if (!playhead) throw new Error("Source Viewer playhead is unavailable");
    const result = await this.#resolve(operation, playhead);
    this.#applyPosition(result);
    return result;
  }

  async captureIn(): Promise<RationalTime> {
    const result = await this.settleActuator();
    this.#snapshot = { ...this.#snapshot, marks: { ...this.#snapshot.marks, in: result.sourceTime } };
    this.#emit();
    return result.sourceTime;
  }

  async captureOut(): Promise<RationalTime> {
    const settled = await this.settleActuator();
    const boundary = await this.#resolve("next", settled.sourceTime);
    this.#snapshot = { ...this.#snapshot, marks: { ...this.#snapshot.marks, out: boundary.sourceTime } };
    this.#emit();
    return boundary.sourceTime;
  }

  useFullSelectedSource(): Readonly<{ start: RationalTime; duration: RationalTime }> {
    const lease = this.#snapshot.preparation?.lease;
    if (!lease) throw new Error("Source Viewer lease is unavailable");
    const timings = [lease.video, lease.audio].filter(
      (timing): timing is SourcePreviewTrackTiming => timing !== undefined,
    );
    if (!this.hasFiniteSelectedCoverage()) {
      throw new Error("Selected source does not declare finite coverage; capture In and Out explicitly");
    }
    let start = timings[0]?.coverageStart;
    let end = start && timings[0]?.coverageDuration ? addRational(start, timings[0].coverageDuration) : undefined;
    for (const timing of timings.slice(1)) {
      if (compareRational(timing.coverageStart, start as RationalTime) > 0) start = timing.coverageStart;
      const candidateEnd = addRational(timing.coverageStart, timing.coverageDuration as RationalTime);
      if (compareRational(candidateEnd, end as RationalTime) < 0) end = candidateEnd;
    }
    if (!start || !end || compareRational(end, start) <= 0) {
      throw new Error("Selected source streams have no positive finite coverage intersection");
    }
    const duration = subtractRational(end, start);
    this.#snapshot = { ...this.#snapshot, marks: { in: start, out: end } };
    this.#emit();
    return { start, duration };
  }

  hasFiniteSelectedCoverage(): boolean {
    const lease = this.#snapshot.preparation?.lease;
    const timings = [lease?.video, lease?.audio].filter(
      (timing): timing is SourcePreviewTrackTiming => timing !== undefined,
    );
    return timings.length > 0 && timings.every((timing) => timing.coverageDuration !== undefined);
  }

  selectedRangeFitsCoverage(lanes: Readonly<{ video: boolean; audio: boolean }>): boolean {
    const range = this.selectedRange();
    const lease = this.#snapshot.preparation?.lease;
    if (!range || (!lanes.video && !lanes.audio)) return false;
    const timings: SourcePreviewTrackTiming[] = [];
    if (lanes.video) {
      if (!lease?.video) return false;
      timings.push(lease.video);
    }
    if (lanes.audio) {
      if (!lease?.audio) return false;
      timings.push(lease.audio);
    }
    return timings.every((timing) => rangeFitsCoverage(range, timing));
  }

  selectedRange(): Readonly<{ start: RationalTime; duration: RationalTime }> | undefined {
    const start = this.#snapshot.marks.in;
    const end = this.#snapshot.marks.out;
    if (!start || !end || compareRational(end, start) <= 0) return undefined;
    return { start, duration: subtractRational(end, start) };
  }

  clearMarks(): void {
    if (!this.#snapshot.marks.in && !this.#snapshot.marks.out) return;
    this.#snapshot = { ...this.#snapshot, marks: {} };
    this.#emit();
  }

  wake(): void {
    if (!this.#snapshot.selection || this.#snapshot.status === "failed") return;
    this.#begin();
  }

  close(): void {
    this.#generation += 1;
    this.#abort?.abort();
    this.#abort = undefined;
    this.#cancelTimer();
    this.#cancelTimer = () => undefined;
    this.#actuator = undefined;
    this.#snapshot = { status: "idle", marks: {}, playback: "paused" };
    this.#emit();
  }

  #begin(): void {
    const selection = this.#snapshot.selection;
    if (!selection) return;
    const generation = ++this.#generation;
    this.#abort?.abort();
    this.#cancelTimer();
    this.#cancelTimer = () => undefined;
    const abort = new AbortController();
    this.#abort = abort;
    const request = this.#viewer.prepareSourcePreview(selection.projectId, selection.assetId, selection, abort.signal);
    void request.then(
      (preparation) => this.#accept(generation, preparation),
      (value) => this.#reject(generation, value),
    );
  }

  #accept(generation: number, preparation: SourcePreviewPreparation): void {
    if (generation !== this.#generation) return;
    const snapshot = this.#snapshot;
    const selection = snapshot.selection;
    if (!selection || !preparationMatchesSelection(preparation, selection)) {
      this.#reject(generation, new Error("Source preview escaped its pinned selection"));
      return;
    }
    this.#retryDelay = 1_000;
    let playhead = snapshot.playhead;
    let proxyPlayhead = snapshot.proxyPlayhead;
    if (preparation.status === "ready" && preparation.lease && (!playhead || !proxyPlayhead)) {
      const timing = preparation.lease.video ?? preparation.lease.audio;
      playhead = timing?.sourceStartTime;
      proxyPlayhead = timing?.proxyStartTime;
    }
    this.#snapshot = {
      ...snapshot,
      status: preparation.status,
      preparation,
      playhead,
      proxyPlayhead,
      playback: preparation.status === "ready" ? snapshot.playback : "paused",
      error: undefined,
    };
    this.#emit();
    if (preparation.status === "preparing") {
      this.#schedule(preparation.stage === "integrity" ? 100 : 1_000);
    } else if (preparation.status === "ready" && preparation.lease) {
      if (proxyPlayhead) this.#actuator?.seekToSeconds(rationalSeconds(proxyPlayhead));
      const renewIn = Date.parse(preparation.lease.expiresAt) - this.#runtime.now() - 30_000;
      this.#schedule(Math.max(1_000, renewIn));
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
      this.#schedule(Math.min(remaining, this.#retryDelay));
      this.#retryDelay = Math.min(30_000, this.#retryDelay * 2);
      return;
    }
    this.#snapshot = { ...snapshot, status: "unavailable", playback: "paused", error };
    this.#emit();
  }

  async #resolve(operation: "settle" | "previous" | "next", target: RationalTime): Promise<SourcePositionResult> {
    const snapshot = this.#snapshot;
    const selection = snapshot.selection;
    const lease = snapshot.preparation?.lease;
    if (!selection || !lease) throw new Error("Source Viewer lease is unavailable");
    const resourceId = lease.resourceId;
    const result = await this.#viewer.resolveSourcePosition(selection.projectId, selection.assetId, selection, {
      resourceId,
      operation,
      target,
    });
    if (
      this.#snapshot.preparation?.lease?.resourceId !== resourceId ||
      !sameSelection(this.#snapshot.selection, selection)
    ) {
      throw new Error("Source Viewer lease changed during position resolution");
    }
    return result;
  }

  #applyPosition(result: SourcePositionResult): void {
    this.#snapshot = {
      ...this.#snapshot,
      playhead: result.sourceTime,
      proxyPlayhead: result.proxyTime,
      error: undefined,
    };
    this.#actuator?.seekToSeconds(rationalSeconds(result.proxyTime));
    this.#emit();
  }

  #schedule(delay: number): void {
    this.#cancelTimer();
    this.#cancelTimer = this.#runtime.schedule(() => this.#begin(), Math.max(0, delay));
  }

  #emit(): void {
    for (const listener of this.#listeners) listener();
  }
}

function sameSelection(left: SourceViewerSelection | undefined, right: SourceViewerSelection): boolean {
  return (
    left?.projectId === right.projectId &&
    left.assetId === right.assetId &&
    left.assetRevision === right.assetRevision &&
    left.fingerprint === right.fingerprint &&
    left.videoStreamId === right.videoStreamId &&
    left.audioStreamId === right.audioStreamId
  );
}

function preparationMatchesSelection(preparation: SourcePreviewPreparation, selection: SourceViewerSelection): boolean {
  return (
    preparation.projectId === selection.projectId &&
    preparation.assetId === selection.assetId &&
    preparation.assetRevision === selection.assetRevision &&
    preparation.fingerprint === selection.fingerprint &&
    preparation.videoStreamId === selection.videoStreamId &&
    preparation.audioStreamId === selection.audioStreamId
  );
}

function rationalFromBrowserSeconds(value: number): RationalTime {
  const microseconds = Math.round(value * 1_000_000);
  if (!Number.isSafeInteger(microseconds)) throw new Error("Source Viewer media time exceeds the exact range");
  return makeRational(BigInt(microseconds), 1_000_000n);
}

function rationalSeconds(value: RationalTime): number {
  return Number(BigInt(value.value)) / value.scale;
}

function compareRational(left: RationalTime, right: RationalTime): number {
  const difference = BigInt(left.value) * BigInt(right.scale) - BigInt(right.value) * BigInt(left.scale);
  return difference < 0n ? -1 : difference > 0n ? 1 : 0;
}

function rangeFitsCoverage(
  range: Readonly<{ start: RationalTime; duration: RationalTime }>,
  timing: SourcePreviewTrackTiming,
): boolean {
  if (compareRational(range.start, timing.coverageStart) < 0) return false;
  if (!timing.coverageDuration) return true;
  return (
    compareRational(
      addRational(range.start, range.duration),
      addRational(timing.coverageStart, timing.coverageDuration),
    ) <= 0
  );
}

function addRational(left: RationalTime, right: RationalTime): RationalTime {
  return makeRational(
    BigInt(left.value) * BigInt(right.scale) + BigInt(right.value) * BigInt(left.scale),
    BigInt(left.scale) * BigInt(right.scale),
  );
}

function subtractRational(left: RationalTime, right: RationalTime): RationalTime {
  return makeRational(
    BigInt(left.value) * BigInt(right.scale) - BigInt(right.value) * BigInt(left.scale),
    BigInt(left.scale) * BigInt(right.scale),
  );
}

function makeRational(numerator: bigint, denominator: bigint): RationalTime {
  if (denominator <= 0n) throw new Error("exact time denominator is invalid");
  if (numerator === 0n) return { value: int64String("0"), scale: 1 };
  const divisor = gcd(numerator < 0n ? -numerator : numerator, denominator);
  const value = numerator / divisor;
  const scale = denominator / divisor;
  if (value < -9_223_372_036_854_775_808n || value > 9_223_372_036_854_775_807n || scale > 2_147_483_647n) {
    throw new Error("exact time exceeds the supported range");
  }
  return { value: int64String(value.toString()), scale: Number(scale) };
}

function gcd(left: bigint, right: bigint): bigint {
  let a = left;
  let b = right;
  while (b !== 0n) {
    const next = a % b;
    a = b;
    b = next;
  }
  return a;
}
