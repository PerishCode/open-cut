import { Button, EmptyState, ResourceCard, Stack, Status, Text } from "@open-cut/components";
import type {
  Asset,
  DurableID,
  TranscriptCorrection,
  TranscriptReadPage,
  TranscriptSegment,
} from "@open-cut/contracts";
import { type CreatorExcerptTarget, CreatorTranscriptExcerpt } from "./creator-transcript-excerpt.js";
import { formatClock, formatClockEnd, formatMediaFacts } from "./creator-workspace-presentation.js";

export type TranscriptState =
  | Readonly<{ status: "idle" }>
  | Readonly<{ status: "loading"; assetId: DurableID }>
  | Readonly<{
      status: "resolved";
      assetId: DurableID;
      page: TranscriptReadPage;
      segments: readonly TranscriptSegment[];
      corrections: readonly TranscriptCorrection[];
      defaultArtifactId: DurableID;
      loadingMore: boolean;
      selectingDefault: boolean;
      selectionError?: Error;
    }>
  | Readonly<{ status: "unavailable"; assetId: DurableID; error: Error }>;

export function AssetSummary({
  asset,
  onContext,
  onPreview,
  onTranscript,
  previewAvailable,
  selected,
}: {
  asset: Asset;
  onContext: () => void;
  onPreview: () => void;
  onTranscript: () => void;
  previewAvailable: boolean;
  selected: boolean;
}) {
  const pending = asset.jobs.filter((job) => job.state === "queued" || job.state === "running").length;
  const failed = asset.jobs.filter((job) => job.state === "failed").length;
  const previewable = Boolean(
    previewAvailable &&
      asset.acceptedFingerprint &&
      asset.facts?.streams.some(
        (stream) => stream.descriptor.mediaType === "video" || stream.descriptor.mediaType === "audio",
      ),
  );
  const readiness =
    asset.availability !== "online"
      ? asset.availability
      : !asset.facts
        ? "Checking media"
        : pending > 0
          ? `Preparing local media · ${pending} in progress`
          : failed > 0
            ? `Media needs attention · ${failed} failed`
            : "Ready";
  const readinessState =
    asset.availability !== "online" || failed > 0 ? "unavailable" : !asset.facts || pending > 0 ? "pending" : "ready";
  const transcriptReady = asset.artifacts.some(
    (artifact) => artifact.kind === "transcript" && artifact.state === "ready",
  );
  return (
    <ResourceCard
      actions={
        <>
          <Button onPress={onContext}>Add @ context</Button>
          <Button disabled={!previewable} onPress={onPreview}>
            {selected ? "In Viewer" : "Open source"}
          </Button>
          {transcriptReady ? <Button onPress={onTranscript}>Open transcript</Button> : null}
        </>
      }
      eyebrow={asset.facts?.container ?? "Media"}
      selected={selected}
      status={<Status state={readinessState}>{readiness}</Status>}
      title={asset.displayName}
      details={[
        asset.facts ? formatMediaFacts(asset.facts) : "Awaiting identity and media facts.",
        ...(!transcriptReady ? [transcriptJobStatus(asset)] : []),
      ]}
    />
  );
}

export function TranscriptSurface({
  asset,
  excerptTarget,
  onContext,
  onInspect,
  onLoad,
  onLoadMore,
  onSelectDefault,
  state,
}: {
  asset: Asset | undefined;
  excerptTarget?: CreatorExcerptTarget;
  onContext: (transcript: TranscriptReadPage, segment: TranscriptSegment) => void;
  onInspect: (artifactId: DurableID) => void;
  onLoad: (() => void) | undefined;
  onLoadMore: () => void;
  onSelectDefault: () => void;
  state: TranscriptState;
}) {
  if (!asset) return null;
  if (state.status === "idle" || state.assetId !== asset.id) {
    if (!asset.artifacts.some((artifact) => artifact.kind === "transcript" && artifact.state === "ready")) {
      return <EmptyState hint={transcriptJobStatus(asset)} title="Transcript not ready" />;
    }
    return (
      <EmptyState
        action={<Button onPress={() => onLoad?.()}>Open transcript</Button>}
        hint={`Review recognized words from ${asset.displayName}, select an exact range, then use it in your edit or Agent context.`}
        title="Transcript ready"
      />
    );
  }
  if (state.status === "loading") {
    return (
      <EmptyState
        hint={`Reading the bounded recognition result for ${asset.displayName}.`}
        title="Loading transcript"
      />
    );
  }
  if (state.status === "unavailable") {
    return (
      <EmptyState
        action={<Button onPress={() => onLoad?.()}>Retry transcript read</Button>}
        hint={state.error.message}
        title="Transcript unavailable"
      />
    );
  }
  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">
        {asset.displayName} · {state.page.artifact.detectedLanguage.toUpperCase()} ·{" "}
        {state.page.artifact.isDefault ? "CREATOR DEFAULT" : "ALTERNATE TRANSCRIPT"}
      </Text>
      {asset.artifacts
        .filter(
          (artifact) =>
            artifact.kind === "transcript" && artifact.state === "ready" && artifact.id !== state.page.artifact.id,
        )
        .map((artifact, index) => (
          <Button key={artifact.id} onPress={() => onInspect(artifact.id)}>
            Inspect transcript {index + 1}
          </Button>
        ))}
      {!state.page.artifact.isDefault ? (
        <Button disabled={state.selectingDefault} onPress={onSelectDefault}>
          {state.selectingDefault ? "Selecting…" : "Make this the Creator default"}
        </Button>
      ) : null}
      {state.selectionError ? <Text>{state.selectionError.message}</Text> : null}
      {state.corrections.length > 0 ? <Text tone="eyebrow">CREATOR CORRECTIONS</Text> : null}
      {state.corrections.map((correction) => (
        <Stack key={correction.id} spacing="compact">
          <Text tone="eyebrow">
            {formatClock(correction.sourceRange.start)} → {formatClockEnd(correction.sourceRange)} · r
            {correction.revision}
          </Text>
          <Text>
            {correction.originalText} → {correction.effectiveText}
          </Text>
        </Stack>
      ))}
      <CreatorTranscriptExcerpt
        asset={asset}
        corrections={state.corrections}
        onContext={onContext}
        page={state.page}
        segments={state.segments}
        target={excerptTarget}
      />
      {state.page.nextAfter ? (
        <Button disabled={state.loadingMore} onPress={onLoadMore}>
          {state.loadingMore ? "Loading…" : "Load more transcript"}
        </Button>
      ) : null}
    </Stack>
  );
}

function transcriptJobStatus(asset: Asset): string {
  const job = asset.jobs.find((candidate) => candidate.kind === "transcript");
  if (!job) return "Transcript has not been scheduled.";
  if (job.state === "blocked") return "Transcript is waiting for local transcription support. Check System.";
  if (job.state === "queued" || job.state === "running") {
    return `Preparing transcript · ${job.progressBasisPoints / 100}%`;
  }
  if (job.state === "succeeded") return "No transcript artifact was produced for this source.";
  return `Transcript ${job.state}${job.terminalErrorCode ? ` · ${job.terminalErrorCode}` : ""}.`;
}
