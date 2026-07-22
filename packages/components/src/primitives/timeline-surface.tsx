import { type CSSProperties, type MouseEvent, useMemo, useState } from "react";

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
  durationSeconds: number;
  items: readonly TimelineSurfaceItem[];
  onItemSelect(id: string): void;
  onSeek(seconds: number): void;
  playheadSeconds: number;
  startSeconds?: number;
  tracks: readonly TimelineSurfaceTrack[];
}>;

const zoomLevels = [1, 2, 4] as const;

export function TimelineSurface({
  durationSeconds,
  items,
  onItemSelect,
  onSeek,
  playheadSeconds,
  startSeconds = 0,
  tracks,
}: TimelineSurfaceProps) {
  const [zoomIndex, setZoomIndex] = useState(0);
  const zoom = zoomLevels[zoomIndex] ?? 1;
  const safeDuration = Number.isFinite(durationSeconds) && durationSeconds > 0 ? durationSeconds : 1;
  const playheadPercent = percent(playheadSeconds - startSeconds, safeDuration);
  const ticks = useMemo(() => createTicks(startSeconds, safeDuration), [safeDuration, startSeconds]);
  const stageStyle = { width: `${zoom * 100}%` } as CSSProperties;

  return (
    <section aria-label="Timeline canvas" className={styles.timelineSurface}>
      <header className={styles.timelineToolbar}>
        <span className={styles.timelineTimecode}>{formatClock(playheadSeconds)}</span>
        <span className={styles.timelineToolbarMeta}>
          {formatClock(startSeconds)} — {formatClock(startSeconds + safeDuration)}
        </span>
        <span className={styles.timelineToolbarSpacer} />
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
              <div className={styles.timelineLane}>
                <button
                  aria-label={`Seek ${track.label}`}
                  className={styles.timelineLaneHit}
                  type="button"
                  onClick={(event) => onSeek(positionFromEvent(event, startSeconds, safeDuration))}
                />
                {items
                  .filter((item) => item.trackId === track.id)
                  .map((item) => (
                    <button
                      aria-label={`Select ${item.label}`}
                      aria-pressed={item.selected}
                      className={`${styles.timelineItem} ${toneClass(track.kind)}${item.selected ? ` ${styles.timelineItemSelected}` : ""}`}
                      key={item.id}
                      title={`${item.label} · ${formatClock(item.startSeconds)} · ${formatClock(item.durationSeconds)}`}
                      type="button"
                      onClick={() => item.selectable !== false && onItemSelect(item.id)}
                      style={{
                        left: `${percent(item.startSeconds - startSeconds, safeDuration)}%`,
                        width: `${Math.max(0.8, percent(item.durationSeconds, safeDuration))}%`,
                      }}
                    >
                      <span>{item.label}</span>
                      {item.linked ? <span className={styles.timelineItemBadge}>LINK</span> : null}
                    </button>
                  ))}
                <span className={styles.timelinePlayhead} style={{ left: `${playheadPercent}%` }} />
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function positionFromEvent(event: MouseEvent<HTMLElement>, start: number, duration: number): number {
  const rect = event.currentTarget.getBoundingClientRect();
  const ratio = rect.width > 0 ? clamp((event.clientX - rect.left) / rect.width, 0, 1) : 0;
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

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(Math.max(value, minimum), maximum);
}
