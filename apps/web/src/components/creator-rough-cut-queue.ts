import type { Asset, DurableID, SourceExcerpt, Track } from "@open-cut/contracts";

export type CreatorRoughCutLaneCandidate = Readonly<{
  id: string;
  kind: "video" | "audio";
  trackId: DurableID;
  trackRevision: Track["revision"];
  trackLabel: string;
  sourceStreamId: DurableID;
  streamLabel: string;
}>;

export type CreatorRoughCutLaneSelection =
  | Readonly<{ state: "unresolved" }>
  | Readonly<{ state: "omitted" }>
  | Readonly<{ state: "selected"; candidate: CreatorRoughCutLaneCandidate }>;

export type CreatorRoughCutOccurrence = Readonly<{
  key: string;
  sourceExcerpt: SourceExcerpt;
  evidenceStatus: "exact" | "stale";
  assetLabel: string;
  videoCandidates: readonly CreatorRoughCutLaneCandidate[];
  audioCandidates: readonly CreatorRoughCutLaneCandidate[];
  video: CreatorRoughCutLaneSelection;
  audio: CreatorRoughCutLaneSelection;
}>;

export function createCreatorRoughCutOccurrence(
  sourceExcerpt: SourceExcerpt,
  evidenceStatus: "exact" | "stale",
  assets: readonly Asset[],
  tracks: readonly Track[],
): CreatorRoughCutOccurrence {
  const asset = assets.find((candidate) => candidate.id === sourceExcerpt.assetId);
  const videoCandidates = roughCutLaneCandidates("video", asset, tracks);
  const audioCandidates = roughCutLaneCandidates("audio", asset, tracks);
  return {
    key: crypto.randomUUID(),
    sourceExcerpt,
    evidenceStatus,
    assetLabel: asset?.displayName ?? `Asset ${sourceExcerpt.assetId}`,
    videoCandidates,
    audioCandidates,
    video: initialLaneSelection(videoCandidates),
    audio: initialLaneSelection(audioCandidates),
  };
}

export function roughCutLaneCandidates(
  kind: "video" | "audio",
  asset: Asset | undefined,
  tracks: readonly Track[],
): readonly CreatorRoughCutLaneCandidate[] {
  if (!asset?.facts) return [];
  const compatibleTracks = tracks.filter((track) => track.type === kind);
  const compatibleStreams = asset.facts.streams.filter((stream) => stream.descriptor.mediaType === kind);
  return compatibleTracks.flatMap((track) =>
    compatibleStreams.map((stream) => ({
      id: `${track.id}:${stream.id}`,
      kind,
      trackId: track.id,
      trackRevision: track.revision,
      trackLabel: track.label,
      sourceStreamId: stream.id,
      streamLabel: `${kind === "video" ? "Video" : "Audio"} ${stream.descriptor.index + 1} · ${stream.descriptor.codec.toUpperCase()}`,
    })),
  );
}

function initialLaneSelection(candidates: readonly CreatorRoughCutLaneCandidate[]): CreatorRoughCutLaneSelection {
  if (candidates.length === 0) return { state: "omitted" };
  const candidate = candidates[0];
  return candidates.length === 1 && candidate ? { state: "selected", candidate } : { state: "unresolved" };
}
