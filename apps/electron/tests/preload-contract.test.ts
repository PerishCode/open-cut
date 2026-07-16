import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

import { describe, it } from "vitest";

import { droppedSourceIPCChannel } from "../src/platform-channel.js";

describe("sandboxed preload contract", () => {
  it("stays self-contained CommonJS and shares the exact narrow IPC channel", () => {
    const source = readFileSync(new URL("../src/preload.cts", import.meta.url), "utf8");
    assert.equal(source.includes(JSON.stringify(droppedSourceIPCChannel)), true);
    assert.doesNotMatch(source, /from\s+["']\.\.?\//);
  });
});
