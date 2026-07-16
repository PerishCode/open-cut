export function derivedCaptionFixture(input: {
  captionId: string;
  sequenceId: string;
  trackId: string;
  sourceExcerptId: string;
  assetId: string;
  transcriptArtifactId: string;
  sourceStreamId: string;
  segmentId: string;
  correctionId: string;
  clipId: string;
}) {
  const range = { start: { value: "0", scale: 1 }, duration: { value: "2", scale: 1 } };
  const text = "Open on the product promise.";
  return {
    id: input.captionId,
    revision: "1",
    sequenceId: input.sequenceId,
    trackId: input.trackId,
    range,
    language: "en",
    text,
    provenance: {
      kind: "transcript-derivation",
      derivation: {
        sourceExcerptId: input.sourceExcerptId,
        sourceExcerptRevision: "1",
        assetId: input.assetId,
        acceptedFingerprint: `sha256:${"a".repeat(64)}`,
        transcriptArtifactId: input.transcriptArtifactId,
        sourceStreamId: input.sourceStreamId,
        segmentIds: [input.segmentId],
        correctionRevisions: [{ id: input.correctionId, revision: "1" }],
        clipId: input.clipId,
        clipRevision: "1",
        clipSourceRange: range,
        clipTimelineRange: range,
        evidenceSourceRange: range,
        policy: {
          id: "readable-captions-v1",
          maximumLines: 2,
          maximumLineGraphemes: 42,
          minimumDuration: { value: "1", scale: 1 },
          maximumDuration: { value: "6", scale: 1 },
          maximumGap: { value: "3", scale: 4 },
          maximumReadingRate: 20,
          boundaryPolicy: "terminal-punctuation-v1",
          timingPolicy: "forward-pad-no-overlap-v1",
          unicodeSegmentationId: "unicode-egc-15.0.0-uniseg-v0.4.7",
        },
        derivedRange: range,
        derivedLanguage: "en",
        derivedText: text,
      },
    },
    provenanceStatus: { content: "exact", evidence: "exact" },
    tombstoned: false,
  };
}
