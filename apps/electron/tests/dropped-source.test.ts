import assert from "node:assert/strict";
import path from "node:path";

import { describe, it } from "vitest";

import { DroppedSourceStager, isDroppedSourceToken } from "../src/main/dropped-source.js";

describe("DroppedSourceStager", () => {
  it("issues an owner-bound single-use opaque token", () => {
    const sourcePath = path.resolve("fixture.mov");
    const token = `drop.${"A".repeat(32)}`;
    const stager = new DroppedSourceStager({ token: () => token });

    assert.equal(stager.stage(7, sourcePath), token);
    assert.equal(isDroppedSourceToken(token), true);
    assert.equal(stager.consume(8, token), undefined);
    assert.equal(stager.consume(7, token), sourcePath);
    assert.equal(stager.consume(7, token), undefined);
  });

  it("expires staged authority without releasing the path", () => {
    let now = 1_000;
    const token = `drop.${"B".repeat(32)}`;
    const stager = new DroppedSourceStager({ clock: () => now, lifetimeMs: 50, token: () => token });
    stager.stage(7, path.resolve("fixture.wav"));
    now = 1_050;
    assert.equal(stager.consume(7, token), undefined);
  });

  it("rejects paths and tokens outside the narrow staging contract", () => {
    const malformedToken = new DroppedSourceStager({ token: () => "/private/fixture.mov" });
    assert.throws(() => malformedToken.stage(7, path.resolve("fixture.mov")), /token is invalid/);
    const stager = new DroppedSourceStager();
    assert.throws(() => stager.stage(7, "relative.mov"), /source is invalid/);
    assert.throws(() => stager.stage(0, path.resolve("fixture.mov")), /source is invalid/);
  });
});
