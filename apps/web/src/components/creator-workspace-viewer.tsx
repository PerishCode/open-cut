import { Button, MediaPlayer, Stack, Text } from "@open-cut/components";
import type { Asset, DurableID, SequencePreviewPreparation, SourceStream } from "@open-cut/contracts";
import { useCallback, useState } from "react";

import type { SequenceViewerController, SequenceViewerSnapshot } from "../lib/sequence-viewer-controller.js";
import type { SourceViewerController, SourceViewerSnapshot } from "../lib/source-viewer-controller.js";

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
      <Text>
        {snapshot.pinnedRevision ? `Main Sequence · pinned r${snapshot.pinnedRevision}` : "Opening Main Sequence…"}
      </Text>
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
  return (
    <Stack spacing="compact">
      <MediaPlayer
        label={`Main Sequence revision ${preparation.sequenceRevision}`}
        mimeType={preparation.lease.mimeType}
        onActuator={attachActuator}
        onPlaybackError={() => controller.wake()}
        onPlaybackPause={() => controller.setPlaying(false)}
        onPlaybackStart={() => controller.setPlaying(true)}
        source={preparation.lease.sameOriginUrl}
      />
      <Text tone="eyebrow">
        PLAN {preparation.lease.renderPlanDigest.slice(0, 19)}… · {preparation.lease.facts.canvasWidth} ×{" "}
        {preparation.lease.facts.canvasHeight}
      </Text>
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
  return (
    <Stack spacing="compact">
      <Text>
        {asset.displayName} · Asset r{asset.revision} · {asset.acceptedFingerprint?.slice(0, 19) ?? "no fingerprint"}…
      </Text>
      <StreamSelection
        label="VIDEO STREAM"
        onChange={onVideoStreamChange}
        selected={videoStreamId}
        streams={videoStreams}
      />
      <StreamSelection
        label="AUDIO STREAM"
        onChange={onAudioStreamChange}
        selected={audioStreamId}
        streams={audioStreams}
      />
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
          <Text tone="eyebrow">
            SOURCE {formatExact(snapshot.playhead)} · PROXY {formatExact(snapshot.proxyPlayhead)}
          </Text>
          <Button onPress={() => run(() => controller.step("previous"))}>Previous source boundary</Button>
          <Button onPress={() => run(() => controller.step("next"))}>Next source boundary</Button>
          <Button onPress={() => run(() => controller.captureIn())}>Mark In at settled position</Button>
          <Button onPress={() => run(() => controller.captureOut())}>Mark Out after displayed boundary</Button>
          <Button onPress={() => run(() => controller.useFullSelectedSource())}>Use full selected source</Button>
          <Button onPress={() => controller.clearMarks()}>Clear source marks</Button>
          <Text>
            In {formatExact(snapshot.marks.in)} · Out {formatExact(snapshot.marks.out)}
            {range ? ` · duration ${formatExact(range.duration)}` : " · select a positive range"}
          </Text>
          <Text tone="eyebrow">NORMALIZED SOURCE PROXY · {lease.byteLength} BYTES</Text>
        </>
      ) : null}
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
  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">{label}</Text>
      <Button onPress={() => onChange(undefined)}>{selected ? "Clear selection" : "None selected"}</Button>
      {streams.map((stream) => (
        <Button key={stream.id} onPress={() => onChange(stream.id)}>
          {selected === stream.id ? "Selected · " : ""}#{stream.descriptor.index} {stream.descriptor.codec}
          {stream.descriptor.language ? ` · ${stream.descriptor.language}` : ""}
          {stream.descriptor.dispositions.includes("default") ? " · default disposition" : ""}
        </Button>
      ))}
      {streams.length === 0 ? <Text>No compatible stream declared.</Text> : null}
    </Stack>
  );
}

function formatExact(value: { value: string; scale: number } | undefined): string {
  return value ? `${value.value}/${value.scale}s` : "—";
}
