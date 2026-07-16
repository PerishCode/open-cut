import type { Caption } from "@open-cut/contracts";
import { describe, expect, it } from "vitest";
import {
  captionProvenanceLabel,
  formatTime,
  formatTimeEnd,
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
});
