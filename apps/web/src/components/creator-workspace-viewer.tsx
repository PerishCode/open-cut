import { Button, ControlStrip, EditorSplit, MediaPlayer, Stack, Text } from "@open-cut/components";
import type { Asset, DurableID, SequencePreviewPreparation, SourceStream } from "@open-cut/contracts";
import { type ReactNode, useCallback, useState } from "react";

import type { SequenceViewerController, SequenceViewerSnapshot } from "../lib/sequence-viewer-controller.js";
import type { SourceViewerController, SourceViewerSnapshot } from "../lib/source-viewer-controller.js";
import { formatClock } from "./creator-workspace-presentation.js";

export function SourceViewerLayout({
  asset,
  onBack,
  placement,
  preview,
  videoStreamId,
}: {
  asset: Asset | undefined;
  onBack: () => void;
  placement: ReactNode;
  preview: ReactNode;
  videoStreamId: DurableID | undefined;
}) {
  const video = asset?.facts?.streams.find((stream) => stream.id === videoStreamId)?.descriptor.video;
  const dimensions = video ? `${video.width} × ${video.height}` : asset?.facts ? "Audio source" : "Preparing source";
  return (
    <EditorSplit
      primary={
        <Stack spacing="compact">
          <ControlStrip label="Source Viewer controls" summary="SOURCE · VIEWER" hint={dimensions}>
            <Button onPress={onBack}>Back to Sequence</Button>
          </ControlStrip>
          {preview}
        </Stack>
      }
      primaryLabel="Source preview and range"
      secondary={placement}
      secondaryLabel="Source placement"
    />
  );
}

export function SequencePreviewSurface({
  controller,
  snapshot,
}: {
  controller: SequenceViewerController;
  snapshot: SequenceViewerSnapshot;
}) {
  const newerRevision =
    snapshot.availableRevision !== undefined &&
    snapshot.pinnedRevision !== undefined &&
    BigInt(snapshot.availableRevision) > BigInt(snapshot.pinnedRevision)
      ? snapshot.availableRevision
      : undefined;
  return (
    <Stack spacing="compact">
      {snapshot.status !== "ready" ? (
        <Text>
          {snapshot.pinnedRevision ? `Main Sequence · pinned r${snapshot.pinnedRevision}` : "Opening Main Sequence…"}
        </Text>
      ) : null}
      {newerRevision ? (
        <Button onPress={() => controller.adoptRevision(newerRevision)}>Adopt available r{newerRevision}</Button>
      ) : null}
      <SequencePreparationSurface controller={controller} preparation={snapshot.preparation} snapshot={snapshot} />
    </Stack>
  );
}

function SequencePreparationSurface({
  controller,
  preparation,
  snapshot,
}: {
  controller: SequenceViewerController;
  preparation: SequencePreviewPreparation | undefined;
  snapshot: SequenceViewerSnapshot;
}) {
  const attachActuator = useCallback(
    (actuator: Parameters<SequenceViewerController["attachActuator"]>[0]) => controller.attachActuator(actuator),
    [controller],
  );
  const [transportError, setTransportError] = useState<Error>();
  const runTransport = (action: () => unknown) => {
    setTransportError(undefined);
    void Promise.resolve()
      .then(action)
      .catch((value) => setTransportError(value instanceof Error ? value : new Error(String(value))));
  };
  if (snapshot.status === "idle" || snapshot.status === "preparing") {
    const progress = preparation?.job ? ` · ${preparation.job.progressBasisPoints / 100}%` : "";
    return <Text>Preparing immutable Sequence preview{progress}</Text>;
  }
  if (snapshot.status === "empty") return <Text>The pinned Sequence is empty.</Text>;
  if (snapshot.status === "unavailable") {
    return (
      <Stack spacing="compact">
        <Text>{snapshot.error?.message ?? "Sequence preview is unavailable."}</Text>
        <Button onPress={() => controller.restart()}>Retry preparation</Button>
      </Stack>
    );
  }
  if (snapshot.status === "failed" || preparation?.status === "failed") {
    const diagnostic = preparation?.diagnostics[0];
    return (
      <Stack spacing="compact">
        <Text>Sequence preview failed{diagnostic ? ` · ${diagnostic.code} · ${diagnostic.recovery}` : ""}</Text>
        {diagnostic?.recovery === "retry-job" ? <Button onPress={() => controller.retry()}>Retry job</Button> : null}
      </Stack>
    );
  }
  if (preparation?.status !== "ready" || !preparation.lease) return <Text>Reconciling Sequence preview…</Text>;
  const facts = preparation.lease.facts;
  const transport = (
    <ControlStrip
      hint={`${formatFrameRate(facts.frameRate)} FPS · PLAN ${preparation.lease.renderPlanDigest.slice(7, 15)}…`}
      label="Sequence transport"
      summary={`SEQUENCE r${preparation.sequenceRevision} · ${formatClock(snapshot.playhead)} / ${formatClock(facts.semanticDuration)}`}
    >
      <Button onPress={() => runTransport(() => controller.seekToStart())}>Go to start</Button>
      <Button onPress={() => runTransport(() => controller.stepFrame(-1))}>Previous frame</Button>
      <Button onPress={() => runTransport(() => controller.togglePlayback())}>
        {snapshot.playback === "playing" ? "Pause" : "Play"}
      </Button>
      <Button onPress={() => runTransport(() => controller.stepFrame(1))}>Next frame</Button>
    </ControlStrip>
  );
  return (
    <Stack spacing="compact">
      <MediaPlayer
        controls={false}
        label={`Main Sequence revision ${preparation.sequenceRevision}`}
        mimeType={preparation.lease.mimeType}
        onActuator={attachActuator}
        onPlaybackError={() => controller.wake()}
        onPlaybackPause={() => controller.setPlaying(false)}
        onPlaybackPosition={(seconds) => controller.observePlaybackPosition(seconds)}
        onPlaybackStart={() => controller.setPlaying(true)}
        onReady={() => controller.syncActuator()}
        source={preparation.lease.sameOriginUrl}
        transport={transport}
      />
      {transportError ? <Text>{transportError.message}</Text> : null}
    </Stack>
  );
}

export function SourcePreviewSurface({
  asset,
  audioStreamId,
  controller,
  onAudioStreamChange,
  onVideoStreamChange,
  snapshot,
  videoStreamId,
}: {
  asset: Asset | undefined;
  audioStreamId: DurableID | undefined;
  controller: SourceViewerController;
  onAudioStreamChange: (streamId: DurableID | undefined) => void;
  onVideoStreamChange: (streamId: DurableID | undefined) => void;
  snapshot: SourceViewerSnapshot;
  videoStreamId: DurableID | undefined;
}) {
  const [actionError, setActionError] = useState<Error>();
  const attachActuator = useCallback(
    (actuator: Parameters<SourceViewerController["attachActuator"]>[0]) => controller.attachActuator(actuator),
    [controller],
  );
  if (!asset) return <Text>Open an Asset explicitly to start a Source Viewer session.</Text>;
  const streams = asset.facts?.streams ?? [];
  const videoStreams = streams.filter((stream) => stream.descriptor.mediaType === "video");
  const audioStreams = streams.filter((stream) => stream.descriptor.mediaType === "audio");
  const run = (action: () => void) => {
    setActionError(undefined);
    void Promise.resolve()
      .then(action)
      .catch((value) => setActionError(value instanceof Error ? value : new Error(String(value))));
  };
  const preparation = snapshot.preparation;
  const lease = preparation?.lease;
  const range = controller.selectedRange();
  const canUseFullSource = controller.hasFiniteSelectedCoverage();
  return (
    <Stack spacing="compact">
      <Text>{asset.displayName}</Text>
      {!videoStreamId && !audioStreamId ? <Text>Select at least one explicit SourceStream.</Text> : null}
      {snapshot.status === "preparing" ? (
        <Text>
          {preparation?.stage === "integrity" ? "Verifying source proxy integrity" : "Preparing explicit source proxy"}
          {preparation?.job ? ` · ${preparation.job.progressBasisPoints / 100}%` : ""}
        </Text>
      ) : null}
      {snapshot.status === "unavailable" ? (
        <Stack spacing="compact">
          <Text>{snapshot.error?.message ?? "Source preview is unavailable."}</Text>
          <Button onPress={() => controller.wake()}>Retry preview</Button>
        </Stack>
      ) : null}
      {snapshot.status === "failed" ? <Text>Source preview proxy failed. Relink or retry the source.</Text> : null}
      {snapshot.status === "ready" && lease ? (
        <>
          <MediaPlayer
            label={`${asset.displayName} source preview`}
            mimeType={lease.mimeType}
            onActuator={attachActuator}
            onPlaybackError={() => controller.wake()}
            onPlaybackPause={() => {
              controller.setPlaying(false);
              run(() => controller.settleActuator());
            }}
            onPlaybackStart={() => controller.setPlaying(true)}
            source={lease.sameOriginUrl}
          />
          <ControlStrip
            hint={`IN ${formatExact(snapshot.marks.in)} · OUT ${formatExact(snapshot.marks.out)}${
              range ? ` · ${formatExact(range.duration)}` : ""
            }`}
            label="Source range controls"
            summary={`SOURCE ${formatExact(snapshot.playhead)} · PROXY ${formatExact(snapshot.proxyPlayhead)}`}
          >
            <Button onPress={() => run(() => controller.step("previous"))}>Previous boundary</Button>
            <Button onPress={() => run(() => controller.step("next"))}>Next boundary</Button>
            <Button onPress={() => run(() => controller.captureIn())}>Mark In</Button>
            <Button onPress={() => run(() => controller.captureOut())}>Mark Out</Button>
            <Button disabled={!canUseFullSource} onPress={() => run(() => controller.useFullSelectedSource())}>
              Use full range
            </Button>
            <Button
              disabled={snapshot.marks.in === undefined && snapshot.marks.out === undefined}
              onPress={() => controller.clearMarks()}
            >
              Clear marks
            </Button>
          </ControlStrip>
          {!canUseFullSource ? <Text>Full range unavailable; mark In and Out explicitly.</Text> : null}
          <Text tone="eyebrow">NORMALIZED SOURCE PROXY · {lease.byteLength} BYTES</Text>
        </>
      ) : null}
      <Text tone="eyebrow">SOURCE TRACKS</Text>
      <StreamSelection label="VIDEO" onChange={onVideoStreamChange} selected={videoStreamId} streams={videoStreams} />
      <StreamSelection label="AUDIO" onChange={onAudioStreamChange} selected={audioStreamId} streams={audioStreams} />
      {actionError ? <Text>{actionError.message}</Text> : null}
    </Stack>
  );
}

function StreamSelection({
  label,
  onChange,
  selected,
  streams,
}: {
  label: string;
  onChange: (streamId: DurableID | undefined) => void;
  selected: DurableID | undefined;
  streams: readonly SourceStream[];
}) {
  const selectedStream = streams.find((stream) => stream.id === selected);
  return (
    <ControlStrip
      hint={
        selectedStream ? `#${selectedStream.descriptor.index} · ${selectedStream.descriptor.codec}` : "None selected"
      }
      label={`${label} source stream`}
      summary={label}
    >
      <Button disabled={!selected} onPress={() => onChange(undefined)}>
        Clear
      </Button>
      {streams.map((stream) => (
        <Button key={stream.id} onPress={() => onChange(stream.id)}>
          {selected === stream.id ? "Selected · " : ""}#{stream.descriptor.index} {stream.descriptor.codec}
          {stream.descriptor.language ? ` · ${stream.descriptor.language}` : ""}
          {stream.descriptor.dispositions.includes("default") ? " · default disposition" : ""}
        </Button>
      ))}
      {streams.length === 0 ? <Text>No compatible stream declared.</Text> : null}
    </ControlStrip>
  );
}

function formatExact(value: { value: string; scale: number } | undefined): string {
  return value ? `${value.value}/${value.scale}s` : "—";
}

function formatFrameRate(value: { value: string; scale: number }): string {
  const scale = BigInt(value.scale);
  const thousandths = (BigInt(value.value) * 1_000n + scale / 2n) / scale;
  const whole = thousandths / 1_000n;
  const fraction = (thousandths % 1_000n).toString().padStart(3, "0").replace(/0+$/, "");
  return fraction ? `${whole}.${fraction}` : whole.toString();
}
