import { type Caption, digestString, durableID, int64String, revisionString } from "@open-cut/contracts";
import { describe, expect, it } from "vitest";
import {
  captionProvenanceLabel,
  formatClock,
  formatClockEnd,
  formatTime,
  formatTimeEnd,
  narrativeNodeLabel,
} from "../../src/components/creator-workspace-presentation.js";

const base = {
  id: "018f0000-0000-7000-8000-000000000001",
  revision: "1",
  sequenceId: "018f0000-0000-7000-8000-000000000002",
  trackId: "018f0000-0000-7000-8000-000000000003",
  range: { start: { value: "0", scale: 1 }, duration: { value: "1", scale: 1 } },
  language: "en",
  text: "Hello",
  tombstoned: false,
} as const;

describe("caption presentation", () => {
  it("formats exact rational ranges for Creator reads", () => {
    expect(formatTime({ value: "3", scale: 2 })).toBe("1.50");
    expect(formatTimeEnd({ start: { value: "3", scale: 2 }, duration: { value: "1", scale: 4 } })).toBe("1.75");
    expect(formatClock({ value: "3", scale: 2 })).toBe("00:01.50");
    expect(formatClockEnd({ start: { value: "3", scale: 2 }, duration: { value: "1", scale: 4 } })).toBe("00:01.75");
  });

  it("keeps manual and derived evidence state creator-visible", () => {
    expect(captionProvenanceLabel({ ...base, provenance: { kind: "manual" } } as Caption)).toBe("MANUAL");
    expect(
      captionProvenanceLabel({
        ...base,
        provenance: { kind: "transcript-derivation", derivation: {} },
        provenanceStatus: { content: "modified", evidence: "stale" },
      } as unknown as Caption),
    ).toBe("DERIVED · MODIFIED · EVIDENCE STALE");
  });

  it("presents SourceExcerpt evidence with exact editor clocks", () => {
    const id = durableID(base.id);
    expect(
      narrativeNodeLabel({
        kind: "source-excerpt",
        evidenceStatus: "exact",
        sourceExcerpt: {
          id,
          revision: revisionString("4"),
          documentId: id,
          parentId: id,
          assetId: id,
          acceptedFingerprint: digestString(`sha256:${"a".repeat(64)}`),
          sourceRange: {
            start: { value: int64String("1"), scale: 100 },
            duration: { value: int64String("73"), scale: 20 },
          },
          language: "en",
          effectiveText: "Exact evidence",
          evidence: {
            artifactId: id,
            sourceStreamId: id,
            segmentIds: [id],
            correctionRevisions: [],
          },
          tombstoned: false,
        },
      }),
    ).toBe("SOURCE EXCERPT · EXACT · 00:00.01 → 00:03.66 · r4");
  });
});
