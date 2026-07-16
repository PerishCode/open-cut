import { useCallback, useEffect, useRef } from "react";

import styles from "./theme.module.css";

export type MediaPlayerProps = {
  label: string;
  source: string;
  mimeType: "video/webm" | "audio/webm";
  onActuator?: (actuator: MediaPlayerActuator | undefined) => void;
  onPlaybackError?: () => void;
  onPlaybackPause?: () => void;
  onPlaybackStart?: () => void;
};

export type MediaPlayerActuator = Readonly<{
  readCurrentTimeSeconds(): number;
  seekToSeconds(value: number): void;
  play(): Promise<void>;
  pause(): void;
}>;

export function MediaPlayer({
  label,
  source,
  mimeType,
  onActuator,
  onPlaybackError,
  onPlaybackPause,
  onPlaybackStart,
}: MediaPlayerProps) {
  const player = useRef<HTMLMediaElement | null>(null);
  const resume = useRef<
    Readonly<{ currentTime: number; paused: boolean; playbackRate: number; volume: number }> | undefined
  >(undefined);

  useEffect(() => {
    const element = player.current;
    if (!element) return;
    if (element.getAttribute("src")) {
      resume.current = {
        currentTime: element.currentTime,
        paused: element.paused,
        playbackRate: element.playbackRate,
        volume: element.volume,
      };
    }
    element.setAttribute("src", source);
  }, [source]);

  const restorePlayback = () => {
    const element = player.current;
    const snapshot = resume.current;
    if (!element || !snapshot) return;
    element.currentTime = Math.min(
      snapshot.currentTime,
      Number.isFinite(element.duration) ? element.duration : snapshot.currentTime,
    );
    element.playbackRate = snapshot.playbackRate;
    element.volume = snapshot.volume;
    resume.current = undefined;
    if (!snapshot.paused) void element.play().catch(() => undefined);
  };

  const attachPlayer = useCallback(
    (element: HTMLMediaElement | null) => {
      player.current = element;
      onActuator?.(
        element
          ? {
              readCurrentTimeSeconds: () => element.currentTime,
              seekToSeconds: (value) => {
                if (!Number.isFinite(value) || value < 0) throw new Error("media seek position is invalid");
                element.currentTime = value;
              },
              play: () => element.play(),
              pause: () => element.pause(),
            }
          : undefined,
      );
    },
    [onActuator],
  );

  if (mimeType === "audio/webm") {
    return (
      // biome-ignore lint/a11y/useMediaCaption: Product captions are separate revision-pinned Viewer tracks.
      <audio
        aria-label={label}
        className={styles.audioPlayer}
        controls
        onLoadedMetadata={restorePlayback}
        onError={onPlaybackError}
        onPause={onPlaybackPause}
        onPlay={onPlaybackStart}
        preload="metadata"
        ref={attachPlayer}
      />
    );
  }
  return (
    // biome-ignore lint/a11y/useMediaCaption: Product captions are separate revision-pinned Viewer tracks.
    <video
      aria-label={label}
      className={styles.mediaPlayer}
      controls
      onLoadedMetadata={restorePlayback}
      onError={onPlaybackError}
      onPause={onPlaybackPause}
      onPlay={onPlaybackStart}
      playsInline
      preload="metadata"
      ref={attachPlayer}
    />
  );
}
