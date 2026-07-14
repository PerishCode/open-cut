import { describe, expect, it } from "vitest";

import { controlCommand, lifecycleMode, presentation, resolveSidecarDataDir } from "../src/index.js";

describe("sidecar client public contract", () => {
  it("re-exports generated lifecycle values without restating them", () => {
    expect(controlCommand.shutdown).toBe("shutdown");
    expect(lifecycleMode.dev).toBe("dev");
    expect(presentation.headless).toBe("headless");
  });

  it("derives an app-owned directory from the injected cell data directory", () => {
    expect(resolveSidecarDataDir({ app: "api", dataDir: "/tmp/open-cut/dev/default" })).toBe(
      "/tmp/open-cut/dev/default/api",
    );
    expect(() => resolveSidecarDataDir({ app: "../api", dataDir: "/tmp/open-cut/dev/default" })).toThrow(
      /safe path segment/,
    );
  });
});
