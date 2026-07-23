import {
  type CSSProperties,
  type MouseEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  type PointerEvent as ReactPointerEvent,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import styles from "./timeline.module.css";

export type TimelineSurfaceTrack = Readonly<{
  id: string;
  label: string;
  kind: "video" | "audio" | "caption";
}>;

export type TimelineSurfaceItem = Readonly<{
  id: string;
  trackId: string;
  label: string;
  startSeconds: number;
  durationSeconds: number;
  selected?: boolean;
  linked?: boolean;
  selectable?: boolean;
}>;

export type TimelineSurfaceProps = Readonly<{
  /** Primary policy/control chrome kept inside the same non-scrolling editor unit as the tracks. */
  accessory?: ReactNode;
  durationSeconds: number;
  itemGesturesEnabled?: boolean;
  items: readonly TimelineSurfaceItem[];
  onItemMove?(id: string, startSeconds: number): void;
  onItemSelect(id: string): void;
  onItemTrimEnd?(id: string, endSeconds: number): void;
  onItemTrimStart?(id: string, startSeconds: number): void;
  onSeek(seconds: number): void;
  playheadSeconds: number;
  startSeconds?: number;
  tracks: readonly TimelineSurfaceTrack[];
}>;

type ActiveGesture =
  | Readonly<{
      kind: "move";
      id: string;
      label: string;
      trackId: string;
      linked: boolean;
      durationSeconds: number;
      originStart: number;
      grabOffsetSeconds: number;
      currentStart: number;
      laneElement: HTMLElement;
    }>
  | Readonly<{
      kind: "trim-start";
      id: string;
      label: string;
      trackId: string;
      linked: boolean;
      originStart: number;
      originEnd: number;
      currentStart: number;
      laneElement: HTMLElement;
    }>
  | Readonly<{
      kind: "trim-end";
      id: string;
      label: string;
      trackId: string;
      linked: boolean;
      originStart: number;
      originEnd: number;
      currentEnd: number;
      laneElement: HTMLElement;
    }>;

const zoomLevels = [1, 2, 4, 8, 16] as const;
const dragActivationPixels = 3;
const minimumGestureDurationSeconds = 0.001;

export function TimelineSurface({
  accessory,
  durationSeconds,
  itemGesturesEnabled = false,
  items,
  onItemMove,
  onItemSelect,
  onItemTrimEnd,
  onItemTrimStart,
  onSeek,
  playheadSeconds,
  startSeconds = 0,
  tracks,
}: TimelineSurfaceProps) {
  const [zoomIndex, setZoomIndex] = useState(() => defaultZoomIndex(durationSeconds));
  const [gesture, setGesture] = useState<ActiveGesture>();
  const gestureRef = useRef<ActiveGesture | undefined>(undefined);
  const pointerOriginRef = useRef<{ x: number; y: number } | undefined>(undefined);
  const activatedRef = useRef(false);
  const cancelledRef = useRef(false);
  /** One-shot: consume the native click that follows an activated drag or Escape cancel. */
  const suppressClickRef = useRef(false);

  const zoom = zoomLevels[zoomIndex] ?? 1;
  const safeDuration = Number.isFinite(durationSeconds) && durationSeconds > 0 ? durationSeconds : 1;
  const playheadPercent = percent(playheadSeconds - startSeconds, safeDuration);
  const ticks = useMemo(() => createTicks(startSeconds, safeDuration), [safeDuration, startSeconds]);
  const stageStyle = { width: `${zoom * 100}%` } as CSSProperties;

  useEffect(() => {
    gestureRef.current = gesture;
  }, [gesture]);

  useEffect(() => {
    if (!gesture) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      if (activatedRef.current) suppressClickRef.current = true;
      cancelledRef.current = true;
      activatedRef.current = false;
      pointerOriginRef.current = undefined;
      gestureRef.current = undefined;
      setGesture(undefined);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [gesture]);

  const beginGesture = (next: ActiveGesture, clientX: number, clientY: number) => {
    cancelledRef.current = false;
    activatedRef.current = false;
    pointerOriginRef.current = { x: clientX, y: clientY };
    gestureRef.current = next;
    setGesture(next);
  };

  const updateGestureFromPointer = (clientX: number) => {
    const active = gestureRef.current;
    if (!active || cancelledRef.current) return;
    const laneRect = active.laneElement.getBoundingClientRect();
    const pointerSeconds = secondsFromClientX(clientX, laneRect, startSeconds, safeDuration);
    if (active.kind === "move") {
      const unbounded = pointerSeconds - active.grabOffsetSeconds;
      const maxStart = startSeconds + safeDuration - active.durationSeconds;
      const currentStart = clamp(unbounded, startSeconds, Math.max(startSeconds, maxStart));
      const next = { ...active, currentStart };
      gestureRef.current = next;
      setGesture(next);
      return;
    }
    if (active.kind === "trim-start") {
      const maxStart = active.originEnd - minimumGestureDurationSeconds;
      const currentStart = clamp(pointerSeconds, startSeconds, Math.max(startSeconds, maxStart));
      const next = { ...active, currentStart };
      gestureRef.current = next;
      setGesture(next);
      return;
    }
    const minEnd = active.originStart + minimumGestureDurationSeconds;
    const currentEnd = clamp(pointerSeconds, minEnd, startSeconds + safeDuration);
    const next = { ...active, currentEnd };
    gestureRef.current = next;
    setGesture(next);
  };

  const finishGesture = () => {
    const active = gestureRef.current;
    const cancelled = cancelledRef.current;
    const activated = activatedRef.current;
    // Activated pointer work always produces a trailing browser click; consume it once.
    if (activated) suppressClickRef.current = true;
    gestureRef.current = undefined;
    pointerOriginRef.current = undefined;
    activatedRef.current = false;
    cancelledRef.current = false;
    setGesture(undefined);
    if (!active || cancelled || !activated) return;
    if (active.kind === "move") {
      if (nearlyEqual(active.currentStart, active.originStart)) return;
      onItemMove?.(active.id, active.currentStart);
      return;
    }
    if (active.kind === "trim-start") {
      if (nearlyEqual(active.currentStart, active.originStart)) return;
      onItemTrimStart?.(active.id, active.currentStart);
      return;
    }
    if (nearlyEqual(active.currentEnd, active.originEnd)) return;
    onItemTrimEnd?.(active.id, active.currentEnd);
  };

  const consumeSuppressedClick = (): boolean => {
    if (!suppressClickRef.current) return false;
    suppressClickRef.current = false;
    return true;
  };

  const onItemPointerMove = (event: ReactPointerEvent<HTMLElement>) => {
    if (!gestureRef.current || cancelledRef.current) return;
    const origin = pointerOriginRef.current;
    if (!activatedRef.current && origin) {
      const distance = Math.hypot(event.clientX - origin.x, event.clientY - origin.y);
      if (distance < dragActivationPixels) return;
      activatedRef.current = true;
    }
    if (!activatedRef.current) return;
    event.preventDefault();
    updateGestureFromPointer(event.clientX);
  };

  const onItemPointerUp = (event: ReactPointerEvent<HTMLElement>) => {
    releasePointer(event.currentTarget, event.pointerId);
    finishGesture();
  };

  const onItemPointerCancel = (event: ReactPointerEvent<HTMLElement>) => {
    cancelledRef.current = true;
    releasePointer(event.currentTarget, event.pointerId);
    finishGesture();
  };

  const visibleGhost = gesture ? visibleGhostGeometry(gesture, items) : undefined;
  const onToolbarKeyDown = (event: ReactKeyboardEvent<HTMLElement>) => {
    if (
      event.target !== event.currentTarget ||
      event.repeat ||
      event.altKey ||
      event.ctrlKey ||
      event.metaKey ||
      event.nativeEvent.isComposing
    ) {
      return;
    }
    if (event.key === "Home" && !event.shiftKey) {
      event.preventDefault();
      onSeek(startSeconds);
      return;
    }
    if (event.key === "0" && !event.shiftKey) {
      event.preventDefault();
      setZoomIndex(0);
      return;
    }
    if (event.key === "-" && !event.shiftKey) {
      event.preventDefault();
      setZoomIndex((current) => Math.max(0, current - 1));
      return;
    }
    if ((event.key === "=" && !event.shiftKey) || (event.key === "+" && event.shiftKey)) {
      event.preventDefault();
      setZoomIndex((current) => Math.min(zoomLevels.length - 1, current + 1));
    }
  };

  return (
    <section aria-label="Timeline editor" className={styles.timelineSurface}>
      <section aria-label="Timeline canvas" className={styles.timelineCanvas} data-timeline-canvas>
        <header
          aria-keyshortcuts="Home 0 - ="
          aria-label="Timeline view controls"
          className={styles.timelineToolbar}
          role="toolbar"
          tabIndex={0}
          onKeyDown={onToolbarKeyDown}
        >
          <span className={styles.timelineTimecode}>{formatClock(playheadSeconds)}</span>
          <span className={styles.timelineToolbarMeta}>
            {formatClock(startSeconds)} — {formatClock(startSeconds + safeDuration)}
          </span>
          <span className={styles.timelineToolbarSpacer} />
          <fieldset aria-label="Timeline zoom" className={styles.timelineToolGroup}>
            <button
              aria-label="Fit timeline"
              className={`${styles.timelineToolButton} ${styles.timelineFitButton}`}
              disabled={zoomIndex === 0}
              type="button"
              onClick={() => setZoomIndex(0)}
            >
              FIT
            </button>
            <button
              aria-label="Zoom timeline out"
              className={styles.timelineToolButton}
              disabled={zoomIndex === 0}
              type="button"
              onClick={() => setZoomIndex((current) => Math.max(0, current - 1))}
            >
              −
            </button>
            <span className={styles.timelineZoomLabel}>{zoom}×</span>
            <button
              aria-label="Zoom timeline in"
              className={styles.timelineToolButton}
              disabled={zoomIndex === zoomLevels.length - 1}
              type="button"
              onClick={() => setZoomIndex((current) => Math.min(zoomLevels.length - 1, current + 1))}
            >
              +
            </button>
          </fieldset>
        </header>
        <div className={styles.timelineViewport}>
          <div className={styles.timelineStage} style={stageStyle}>
            <div className={styles.timelineRulerRow}>
              <span className={styles.timelineCorner}>TRACK</span>
              <button
                aria-label="Seek timeline ruler"
                className={styles.timelineRulerLane}
                type="button"
                onClick={(event) => onSeek(positionFromEvent(event, startSeconds, safeDuration))}
              >
                {ticks.map((tick) => (
                  <span className={styles.timelineTick} key={tick.seconds} style={{ left: `${tick.percent}%` }}>
                    {formatRuler(tick.seconds)}
                  </span>
                ))}
                <span className={styles.timelinePlayhead} style={{ left: `${playheadPercent}%` }} />
              </button>
            </div>
            {tracks.map((track) => (
              <div className={styles.timelineTrackRow} key={track.id}>
                <span className={styles.timelineTrackLabel}>
                  <span className={styles.timelineTrackKind}>{track.kind.slice(0, 1).toUpperCase()}</span>
                  {track.label}
                </span>
                <div className={styles.timelineLane} data-timeline-lane={track.id}>
                  <button
                    aria-label={`Seek ${track.label}`}
                    className={styles.timelineLaneHit}
                    type="button"
                    onClick={(event) => onSeek(positionFromEvent(event, startSeconds, safeDuration))}
                  />
                  {items
                    .filter((item) => item.trackId === track.id)
                    .map((item) => {
                      const selected = Boolean(item.selected);
                      const gesturesActive = selected && itemGesturesEnabled && item.selectable !== false;
                      const durableLeft = percent(item.startSeconds - startSeconds, safeDuration);
                      const durableWidth = Math.max(0.8, percent(item.durationSeconds, safeDuration));
                      const draggingThis = gesture?.id === item.id && visibleGhost !== undefined;
                      return (
                        <div
                          className={`${styles.timelineItem} ${toneClass(track.kind)}${selected ? ` ${styles.timelineItemSelected}` : ""}${draggingThis ? ` ${styles.timelineItemDragging}` : ""}${gesturesActive ? ` ${styles.timelineItemInteractive}` : ""}`}
                          data-timeline-item={item.id}
                          key={item.id}
                          style={{
                            left: `${durableLeft}%`,
                            width: `${durableWidth}%`,
                          }}
                        >
                          <button
                            aria-label={gesturesActive ? `Move ${item.label}` : `Select ${item.label}`}
                            aria-pressed={selected}
                            className={styles.timelineItemBody}
                            title={`${item.label} · ${formatClock(item.startSeconds)} · ${formatClock(item.durationSeconds)}`}
                            type="button"
                            onClick={(event) => {
                              if (consumeSuppressedClick()) {
                                event.preventDefault();
                                return;
                              }
                              // Move body is not a reselection control while gestures are armed.
                              if (gesturesActive || gestureRef.current || activatedRef.current) return;
                              if (item.selectable !== false) onItemSelect(item.id);
                            }}
                            onKeyDown={(event: ReactKeyboardEvent<HTMLButtonElement>) => {
                              if (event.key === "Enter" || event.key === " ") {
                                event.preventDefault();
                                if (gesturesActive) return;
                                if (item.selectable !== false) onItemSelect(item.id);
                              }
                            }}
                            onPointerCancel={onItemPointerCancel}
                            onPointerDown={(event) => {
                              if (!gesturesActive || !onItemMove || event.button !== 0) return;
                              const lane = event.currentTarget.closest("[data-timeline-lane]");
                              if (!(lane instanceof HTMLElement)) return;
                              event.preventDefault();
                              capturePointer(event.currentTarget, event.pointerId);
                              const laneRect = lane.getBoundingClientRect();
                              const pointerSeconds = secondsFromClientX(
                                event.clientX,
                                laneRect,
                                startSeconds,
                                safeDuration,
                              );
                              beginGesture(
                                {
                                  kind: "move",
                                  id: item.id,
                                  label: item.label,
                                  trackId: item.trackId,
                                  linked: Boolean(item.linked),
                                  durationSeconds: item.durationSeconds,
                                  originStart: item.startSeconds,
                                  grabOffsetSeconds: pointerSeconds - item.startSeconds,
                                  currentStart: item.startSeconds,
                                  laneElement: lane,
                                },
                                event.clientX,
                                event.clientY,
                              );
                            }}
                            onPointerMove={onItemPointerMove}
                            onPointerUp={onItemPointerUp}
                          >
                            <span className={styles.timelineItemLabel}>{item.label}</span>
                            {item.linked ? <span className={styles.timelineItemBadge}>LINK</span> : null}
                          </button>
                          {gesturesActive ? (
                            <>
                              <button
                                aria-label={`Trim in ${item.label}`}
                                className={`${styles.timelineItemHandle} ${styles.timelineItemHandleStart}`}
                                type="button"
                                onPointerCancel={onItemPointerCancel}
                                onPointerDown={(event) => {
                                  if (!onItemTrimStart || event.button !== 0) return;
                                  const lane = event.currentTarget.closest("[data-timeline-lane]");
                                  if (!(lane instanceof HTMLElement)) return;
                                  event.preventDefault();
                                  event.stopPropagation();
                                  capturePointer(event.currentTarget, event.pointerId);
                                  beginGesture(
                                    {
                                      kind: "trim-start",
                                      id: item.id,
                                      label: item.label,
                                      trackId: item.trackId,
                                      linked: Boolean(item.linked),
                                      originStart: item.startSeconds,
                                      originEnd: item.startSeconds + item.durationSeconds,
                                      currentStart: item.startSeconds,
                                      laneElement: lane,
                                    },
                                    event.clientX,
                                    event.clientY,
                                  );
                                }}
                                onPointerMove={onItemPointerMove}
                                onPointerUp={onItemPointerUp}
                              />
                              <button
                                aria-label={`Trim out ${item.label}`}
                                className={`${styles.timelineItemHandle} ${styles.timelineItemHandleEnd}`}
                                type="button"
                                onPointerCancel={onItemPointerCancel}
                                onPointerDown={(event) => {
                                  if (!onItemTrimEnd || event.button !== 0) return;
                                  const lane = event.currentTarget.closest("[data-timeline-lane]");
                                  if (!(lane instanceof HTMLElement)) return;
                                  event.preventDefault();
                                  event.stopPropagation();
                                  capturePointer(event.currentTarget, event.pointerId);
                                  beginGesture(
                                    {
                                      kind: "trim-end",
                                      id: item.id,
                                      label: item.label,
                                      trackId: item.trackId,
                                      linked: Boolean(item.linked),
                                      originStart: item.startSeconds,
                                      originEnd: item.startSeconds + item.durationSeconds,
                                      currentEnd: item.startSeconds + item.durationSeconds,
                                      laneElement: lane,
                                    },
                                    event.clientX,
                                    event.clientY,
                                  );
                                }}
                                onPointerMove={onItemPointerMove}
                                onPointerUp={onItemPointerUp}
                              />
                            </>
                          ) : null}
                        </div>
                      );
                    })}
                  {visibleGhost && visibleGhost.trackId === track.id ? (
                    <div
                      aria-hidden="true"
                      className={`${styles.timelineItemGhost} ${toneClass(track.kind)}`}
                      style={{
                        left: `${percent(visibleGhost.startSeconds - startSeconds, safeDuration)}%`,
                        width: `${Math.max(0.8, percent(visibleGhost.durationSeconds, safeDuration))}%`,
                      }}
                    >
                      <span className={styles.timelineItemLabel}>{visibleGhost.label}</span>
                      {visibleGhost.linked ? <span className={styles.timelineItemBadge}>LINK</span> : null}
                    </div>
                  ) : null}
                  <span className={styles.timelinePlayhead} style={{ left: `${playheadPercent}%` }} />
                </div>
              </div>
            ))}
          </div>
        </div>
      </section>
      {accessory ? (
        <div className={styles.timelineAccessory} data-timeline-accessory>
          {accessory}
        </div>
      ) : null}
    </section>
  );
}

function visibleGhostGeometry(
  gesture: ActiveGesture,
  items: readonly TimelineSurfaceItem[],
):
  | Readonly<{ trackId: string; label: string; linked: boolean; startSeconds: number; durationSeconds: number }>
  | undefined {
  const geometry = gestureGeometry(gesture);
  const source = items.find((item) => item.id === gesture.id);
  if (!source) return geometry;
  if (gesture.kind === "move" && nearlyEqual(geometry.startSeconds, source.startSeconds)) return undefined;
  if (
    gesture.kind === "trim-start" &&
    nearlyEqual(geometry.startSeconds, source.startSeconds) &&
    nearlyEqual(geometry.durationSeconds, source.durationSeconds)
  ) {
    return undefined;
  }
  if (
    gesture.kind === "trim-end" &&
    nearlyEqual(geometry.startSeconds, source.startSeconds) &&
    nearlyEqual(geometry.durationSeconds, source.durationSeconds)
  ) {
    return undefined;
  }
  return geometry;
}

function gestureGeometry(gesture: ActiveGesture): Readonly<{
  trackId: string;
  label: string;
  linked: boolean;
  startSeconds: number;
  durationSeconds: number;
}> {
  if (gesture.kind === "move") {
    return {
      trackId: gesture.trackId,
      label: gesture.label,
      linked: gesture.linked,
      startSeconds: gesture.currentStart,
      durationSeconds: gesture.durationSeconds,
    };
  }
  if (gesture.kind === "trim-start") {
    return {
      trackId: gesture.trackId,
      label: gesture.label,
      linked: gesture.linked,
      startSeconds: gesture.currentStart,
      durationSeconds: Math.max(minimumGestureDurationSeconds, gesture.originEnd - gesture.currentStart),
    };
  }
  return {
    trackId: gesture.trackId,
    label: gesture.label,
    linked: gesture.linked,
    startSeconds: gesture.originStart,
    durationSeconds: Math.max(minimumGestureDurationSeconds, gesture.currentEnd - gesture.originStart),
  };
}

function defaultZoomIndex(durationSeconds: number): number {
  const safe = Number.isFinite(durationSeconds) && durationSeconds > 0 ? durationSeconds : 60;
  if (safe >= 45) return 2;
  if (safe >= 20) return 1;
  return 0;
}

function positionFromEvent(event: MouseEvent<HTMLElement>, start: number, duration: number): number {
  const rect = event.currentTarget.getBoundingClientRect();
  return secondsFromClientX(event.clientX, rect, start, duration);
}

function secondsFromClientX(clientX: number, rect: DOMRect, start: number, duration: number): number {
  const ratio = rect.width > 0 ? clamp((clientX - rect.left) / rect.width, 0, 1) : 0;
  return start + ratio * duration;
}

function createTicks(start: number, duration: number): readonly { seconds: number; percent: number }[] {
  const count = 6;
  return Array.from({ length: count + 1 }, (_, index) => ({
    seconds: start + (duration * index) / count,
    percent: (100 * index) / count,
  }));
}

function percent(value: number, duration: number): number {
  return clamp((value / duration) * 100, 0, 100);
}

function formatClock(seconds: number): string {
  const safe = Math.max(0, Number.isFinite(seconds) ? seconds : 0);
  const minutes = Math.floor(safe / 60);
  const remainder = safe - minutes * 60;
  return `${String(minutes).padStart(2, "0")}:${remainder.toFixed(2).padStart(5, "0")}`;
}

function formatRuler(seconds: number): string {
  if (seconds >= 60) return `${Math.floor(seconds / 60)}:${String(Math.round(seconds % 60)).padStart(2, "0")}`;
  return `${Math.round(seconds)}s`;
}

function toneClass(kind: TimelineSurfaceTrack["kind"]): string {
  if (kind === "audio") return styles.timelineItemAudio ?? "";
  if (kind === "caption") return styles.timelineItemCaption ?? "";
  return styles.timelineItemVideo ?? "";
}

function nearlyEqual(left: number, right: number): boolean {
  return Math.abs(left - right) < 1e-6;
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(Math.max(value, minimum), maximum);
}

function capturePointer(target: HTMLElement, pointerId: number): void {
  if (typeof target.setPointerCapture === "function") target.setPointerCapture(pointerId);
}

function releasePointer(target: HTMLElement, pointerId: number): void {
  if (typeof target.hasPointerCapture === "function") {
    if (target.hasPointerCapture(pointerId) && typeof target.releasePointerCapture === "function") {
      target.releasePointerCapture(pointerId);
    }
    return;
  }
  if (typeof target.releasePointerCapture === "function") {
    try {
      target.releasePointerCapture(pointerId);
    } catch {
      // jsdom and older targets may not implement pointer capture.
    }
  }
}
