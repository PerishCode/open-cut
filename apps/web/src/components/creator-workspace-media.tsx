import { Button, Stack, Text } from "@open-cut/components";
import type {
  Asset,
  DurableID,
  TranscriptCorrection,
  TranscriptReadPage,
  TranscriptSegment,
} from "@open-cut/contracts";
import { type CreatorExcerptTarget, CreatorTranscriptExcerpt } from "./creator-transcript-excerpt.js";
import { formatMediaFacts, formatTime, formatTimeEnd } from "./creator-workspace-presentation.js";

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
  const blocked = asset.jobs.filter((job) => job.state === "blocked").length;
  return (
    <Stack spacing="compact">
      <Text>{asset.displayName}</Text>
      <Text tone="eyebrow">
        {asset.availability} · r{asset.revision} · {pending} active · {blocked} blocked
      </Text>
      {asset.facts ? <Text>{formatMediaFacts(asset.facts)}</Text> : <Text>Awaiting identity and media facts.</Text>}
      <Button onPress={onContext}>Use this Asset as @ context</Button>
      <Button disabled={!previewAvailable} onPress={onPreview}>
        {selected ? "Viewing source" : "Open in Source Viewer"}
      </Button>
      {asset.artifacts.some((artifact) => artifact.kind === "transcript" && artifact.state === "ready") ? (
        <Button onPress={onTranscript}>Open transcript</Button>
      ) : (
        <Text>{transcriptJobStatus(asset)}</Text>
      )}
    </Stack>
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
  if (!asset) return <Text>Select an Asset to inspect its original transcript.</Text>;
  if (state.status === "idle" || state.assetId !== asset.id) {
    if (!asset.artifacts.some((artifact) => artifact.kind === "transcript" && artifact.state === "ready")) {
      return <Text>{transcriptJobStatus(asset)}</Text>;
    }
    return <Button onPress={() => onLoad?.()}>Load original transcript</Button>;
  }
  if (state.status === "loading") return <Text>Loading bounded transcript recognition…</Text>;
  if (state.status === "unavailable") {
    return (
      <Stack spacing="compact">
        <Text>{state.error.message}</Text>
        <Button onPress={() => onLoad?.()}>Retry transcript read</Button>
      </Stack>
    );
  }
  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">
        {state.page.artifact.detectedLanguage} · {state.page.artifact.modelVersion}
        {state.page.artifact.isDefault ? " · DEFAULT" : ""}
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
            {formatTime(correction.sourceRange.start)} → {formatTimeEnd(correction.sourceRange)} · r
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
  if (job.state === "blocked") return "Transcript is waiting for its exact local model or executor.";
  if (job.state === "queued" || job.state === "running") {
    return `Transcript ${job.state} · ${job.progressBasisPoints / 100}%`;
  }
  if (job.state === "succeeded") return "No transcript artifact was produced for this source.";
  return `Transcript ${job.state}.`;
}
