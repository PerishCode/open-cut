import { describe, expect, it } from "vitest";

import { controlCommand, lifecycleMode, presentation } from "../src/index.js";

describe("sidecar client public contract", () => {
  it("re-exports generated lifecycle values without restating them", () => {
    expect(controlCommand.shutdown).toBe("shutdown");
    expect(lifecycleMode.dev).toBe("dev");
    expect(presentation.headless).toBe("headless");
  });
});
