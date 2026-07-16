import { describe, expect, it } from "vitest";
import {
  durableID,
  int64String,
  revisionString,
  selectSourceExcerptEvidence,
  type TranscriptArtifact,
  type TranscriptCorrection,
  type TranscriptSegment,
} from "../src/index.js";

const ids = {
  asset: "018f0a60-7b80-7a01-8000-000000000601",
  artifact: "018f0a60-7b80-7a01-8000-000000000602",
  stream: "018f0a60-7b80-7a01-8000-000000000603",
  segment1: "018f0a60-7b80-7a01-8000-000000000604",
  segment2: "018f0a60-7b80-7a01-8000-000000000605",
  token1: "018f0a60-7b80-7a01-8000-000000000606",
  token2: "018f0a60-7b80-7a01-8000-000000000607",
  token3: "018f0a60-7b80-7a01-8000-000000000608",
  token4: "018f0a60-7b80-7a01-8000-000000000609",
  correction: "018f0a60-7b80-7a01-8000-00000000060a",
} as const;

describe("Creator SourceExcerpt evidence selection", () => {
  it("closes a reversed contiguous token selection over exact segments and corrections", () => {
    const evidence = selectSourceExcerptEvidence({
      artifact: transcriptArtifact(),
      segments: transcriptSegments(),
      corrections: [correction(range(1, 1, 1, 2))],
      anchorTokenId: durableID(ids.token3),
      focusTokenId: durableID(ids.token2),
    });

    expect(evidence).toEqual({
      artifactId: ids.artifact,
      assetId: ids.asset,
      sourceStreamId: ids.stream,
      language: "en",
      segmentIds: [ids.segment1, ids.segment2],
      selectedTokenIds: [ids.token2, ids.token3],
      sourceRange: range(1, 2, 1, 1),
      correctionRevisions: [{ id: ids.correction, revision: "3" }],
    });
  });

  it("rejects a token boundary that cuts through a current correction", () => {
    expect(() =>
      selectSourceExcerptEvidence({
        artifact: transcriptArtifact(),
        segments: transcriptSegments(),
        corrections: [correction(range(1, 4, 1, 2))],
        anchorTokenId: durableID(ids.token2),
        focusTokenId: durableID(ids.token3),
      }),
    ).toThrow("cuts through a TranscriptCorrection");
  });
});

function transcriptArtifact(): TranscriptArtifact {
  return {
    id: durableID(ids.artifact),
    assetId: durableID(ids.asset),
    sourceStreamId: durableID(ids.stream),
    recognitionProfile: "whisper-small-multilingual-v1",
    engineVersion: "1.0.0",
    engineTarget: "test",
    modelName: "whisper-small",
    modelVersion: "1",
    detectedLanguage: "en",
    sourceStartTime: time(0, 1),
    normalizedSampleCount: "96000" as TranscriptArtifact["normalizedSampleCount"],
    isDefault: true,
    createdAt: "2026-07-16T00:00:00Z",
  };
}

function transcriptSegments(): TranscriptSegment[] {
  return [
    {
      id: durableID(ids.segment1),
      ordinal: 0,
      sourceRange: range(0, 1, 1, 1),
      text: "Hello ",
      tokens: [
        { id: durableID(ids.token1), sourceRange: range(0, 1, 1, 2), text: "Hello" },
        { id: durableID(ids.token2), sourceRange: range(1, 2, 1, 2), text: " " },
      ],
    },
    {
      id: durableID(ids.segment2),
      ordinal: 1,
      sourceRange: range(1, 1, 1, 1),
      text: "world!",
      tokens: [
        { id: durableID(ids.token3), sourceRange: range(1, 1, 1, 2), text: "world" },
        { id: durableID(ids.token4), sourceRange: range(3, 2, 1, 2), text: "!" },
      ],
    },
  ];
}

function correction(sourceRange: TranscriptCorrection["sourceRange"]): TranscriptCorrection {
  return {
    id: durableID(ids.correction),
    revision: revisionString("3"),
    segmentIds: [durableID(ids.segment1), durableID(ids.segment2)],
    sourceRange,
    originalText: "world",
    effectiveText: "people",
    language: "en",
  };
}

function range(startValue: number, startScale: number, durationValue: number, durationScale: number) {
  return { start: time(startValue, startScale), duration: time(durationValue, durationScale) };
}

function time(value: number, scale: number) {
  return { value: int64String(String(value)), scale };
}
