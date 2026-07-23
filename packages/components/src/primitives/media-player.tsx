import { type ReactNode, useCallback, useEffect, useRef } from "react";

import styles from "./media-player.module.css";

export type MediaPlayerProps = {
  label: string;
  source: string;
  mimeType: "video/webm" | "audio/webm";
  controls?: boolean;
  onActuator?: (actuator: MediaPlayerActuator | undefined) => void;
  onPlaybackError?: () => void;
  onPlaybackPause?: () => void;
  onPlaybackPosition?: (seconds: number) => void;
  onPlaybackStart?: () => void;
  onReady?: () => void;
  transport?: ReactNode;
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
  controls = true,
  onActuator,
  onPlaybackError,
  onPlaybackPause,
  onPlaybackPosition,
  onPlaybackStart,
  onReady,
  transport,
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
    if (!element) return;
    if (snapshot) {
      element.currentTime = Math.min(
        snapshot.currentTime,
        Number.isFinite(element.duration) ? element.duration : snapshot.currentTime,
      );
      element.playbackRate = snapshot.playbackRate;
      element.volume = snapshot.volume;
      resume.current = undefined;
      if (!snapshot.paused) void element.play().catch(() => undefined);
    }
    onReady?.();
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
        controls={controls}
        onLoadedMetadata={restorePlayback}
        onError={onPlaybackError}
        onPause={(event) => {
          onPlaybackPosition?.(event.currentTarget.currentTime);
          onPlaybackPause?.();
        }}
        onPlay={(event) => {
          onPlaybackPosition?.(event.currentTarget.currentTime);
          onPlaybackStart?.();
        }}
        onSeeked={(event) => onPlaybackPosition?.(event.currentTarget.currentTime)}
        onTimeUpdate={(event) => onPlaybackPosition?.(event.currentTarget.currentTime)}
        preload="metadata"
        ref={attachPlayer}
      />
    );
  }
  return (
    <div className={styles.mediaPlayerFrame}>
      {/* biome-ignore lint/a11y/useMediaCaption: Product captions are separate revision-pinned Viewer tracks. */}
      <video
        aria-label={label}
        className={styles.mediaPlayer}
        controls={controls}
        onLoadedMetadata={restorePlayback}
        onError={onPlaybackError}
        onPause={(event) => {
          onPlaybackPosition?.(event.currentTarget.currentTime);
          onPlaybackPause?.();
        }}
        onPlay={(event) => {
          onPlaybackPosition?.(event.currentTarget.currentTime);
          onPlaybackStart?.();
        }}
        onSeeked={(event) => onPlaybackPosition?.(event.currentTarget.currentTime)}
        onTimeUpdate={(event) => onPlaybackPosition?.(event.currentTarget.currentTime)}
        playsInline
        preload="metadata"
        ref={attachPlayer}
      />
      {transport ? <div className={styles.mediaPlayerTransport}>{transport}</div> : null}
    </div>
  );
}
