import type { Asset, Caption, DurableID, NarrativeNode, SourceStream, TranscriptCorrection } from "@open-cut/contracts";

export type SourceStreamSelection = Readonly<{
  assetId: DurableID;
  videoStreamId?: DurableID;
  audioStreamId?: DurableID;
}>;

export function uniqueSourceStream(
  streams: readonly SourceStream[],
  mediaType: "video" | "audio",
): SourceStream | undefined {
  const candidates = streams.filter((stream) => stream.descriptor.mediaType === mediaType);
  return candidates.length === 1 ? candidates[0] : undefined;
}

export function updateSourceStreamSelection(
  current: SourceStreamSelection | undefined,
  mediaType: "video" | "audio",
  streamId: DurableID | undefined,
): SourceStreamSelection | undefined {
  if (!current) return undefined;
  if (mediaType === "video") {
    return {
      assetId: current.assetId,
      ...(streamId === undefined ? {} : { videoStreamId: streamId }),
      ...(current.audioStreamId === undefined ? {} : { audioStreamId: current.audioStreamId }),
    };
  }
  return {
    assetId: current.assetId,
    ...(current.videoStreamId === undefined ? {} : { videoStreamId: current.videoStreamId }),
    ...(streamId === undefined ? {} : { audioStreamId: streamId }),
  };
}

export function captionProvenanceLabel(caption: Caption): string {
  if (caption.provenance.kind === "manual") return "MANUAL";
  const status = caption.provenanceStatus;
  if (!status) return "DERIVED";
  return `DERIVED · ${status.content.toUpperCase()} · EVIDENCE ${status.evidence.toUpperCase()}`;
}

export function mergeTranscriptCorrections(
  current: readonly TranscriptCorrection[],
  incoming: readonly TranscriptCorrection[],
): readonly TranscriptCorrection[] {
  const corrections = new Map(current.map((correction) => [correction.id, correction]));
  for (const correction of incoming) corrections.set(correction.id, correction);
  return [...corrections.values()].sort((left, right) => {
    const start = Number(left.sourceRange.start.value) / left.sourceRange.start.scale;
    const otherStart = Number(right.sourceRange.start.value) / right.sourceRange.start.scale;
    return start - otherStart || left.id.localeCompare(right.id);
  });
}

export function narrativeNodeID(node: NarrativeNode): DurableID {
  switch (node.kind) {
    case "section":
      return node.section.id;
    case "authored-text":
      return node.authoredText.id;
    case "source-excerpt":
      return node.sourceExcerpt.id;
    case "visual-intent":
      return node.visualIntent.id;
    case "note":
      return node.note.id;
  }
}

export function narrativeNodeText(node: NarrativeNode): string {
  switch (node.kind) {
    case "section":
      return node.section.title;
    case "authored-text":
      return node.authoredText.text;
    case "source-excerpt":
      return node.sourceExcerpt.effectiveText;
    case "visual-intent":
      return node.visualIntent.description;
    case "note":
      return node.note.text;
  }
}

export function narrativeNodeLabel(node: NarrativeNode): string {
  switch (node.kind) {
    case "section":
      return `SECTION · ${node.section.language} · r${node.section.revision}`;
    case "authored-text":
      return `${node.authoredText.purpose.toUpperCase()} · ${node.authoredText.language} · r${node.authoredText.revision}`;
    case "source-excerpt": {
      const range = node.sourceExcerpt.sourceRange;
      return `SOURCE EXCERPT · ${node.evidenceStatus.toUpperCase()} · ${formatClock(range.start)} → ${formatClockEnd(
        range,
      )} · r${node.sourceExcerpt.revision}`;
    }
    case "visual-intent":
      return `VISUAL ${node.visualIntent.purpose.toUpperCase()} · ${node.visualIntent.language} · r${node.visualIntent.revision}`;
    case "note":
      return `NOTE · ${node.note.language} · r${node.note.revision}`;
  }
}

export function formatTime(value: { value: string; scale: number }): string {
  return (Number(value.value) / value.scale).toFixed(2);
}

export function formatTimeEnd(range: {
  start: { value: string; scale: number };
  duration: { value: string; scale: number };
}): string {
  return (Number(range.start.value) / range.start.scale + Number(range.duration.value) / range.duration.scale).toFixed(
    2,
  );
}

export function formatClock(value: { value: string; scale: number }): string {
  const scale = BigInt(value.scale);
  const numerator = BigInt(value.value);
  return formatClockParts(numerator, scale);
}

export function formatClockEnd(range: {
  start: { value: string; scale: number };
  duration: { value: string; scale: number };
}): string {
  const startScale = BigInt(range.start.scale);
  const durationScale = BigInt(range.duration.scale);
  return formatClockParts(
    BigInt(range.start.value) * durationScale + BigInt(range.duration.value) * startScale,
    startScale * durationScale,
  );
}

function formatClockParts(numerator: bigint, scale: bigint): string {
  const negative = numerator < 0n;
  const absolute = negative ? -numerator : numerator;
  const hundredths = (absolute * 100n + scale / 2n) / scale;
  const hours = hundredths / 360_000n;
  const minutes = (hundredths % 360_000n) / 6_000n;
  const seconds = (hundredths % 6_000n) / 100n;
  const fraction = hundredths % 100n;
  const clock = `${minutes.toString().padStart(2, "0")}:${seconds.toString().padStart(2, "0")}.${fraction
    .toString()
    .padStart(2, "0")}`;
  return `${negative ? "−" : ""}${hours > 0 ? `${hours.toString().padStart(2, "0")}:` : ""}${clock}`;
}

export function formatMediaFacts(facts: NonNullable<Asset["facts"]>): string {
  const video = facts.streams.find((stream) => stream.descriptor.video)?.descriptor.video;
  const audio = facts.streams.find((stream) => stream.descriptor.audio)?.descriptor.audio;
  return [
    facts.container,
    facts.duration ? `${formatTime(facts.duration)}s` : "duration unknown",
    video ? `${video.width} × ${video.height}` : undefined,
    audio ? `${audio.sampleRate} Hz · ${audio.channels} ch` : undefined,
    `${facts.streams.length} streams`,
  ]
    .filter((value): value is string => value !== undefined)
    .join(" · ");
}

export function scheduleTimer(callback: () => void, delay: number): () => void {
  const timer = setTimeout(callback, delay);
  return () => clearTimeout(timer);
}
