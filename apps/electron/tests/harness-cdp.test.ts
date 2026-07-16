import assert from "node:assert/strict";

import { lifecycleMode, presentation, sidecarEnvironment } from "@open-cut/sidecar-client";
import { describe, it, vi } from "vitest";

import { configureHarnessCDP, harnessCDPPortEnvironment } from "../sidecar/harness-cdp.js";

describe("delivery harness CDP", () => {
  it("enables only a canonical loopback port for interactive packaged checks", () => {
    for (const mode of [lifecycleMode.packaged, lifecycleMode.harness]) {
      const appendSwitch = vi.fn();
      configureHarnessCDP(
        { appendSwitch },
        {
          [harnessCDPPortEnvironment]: "43123",
          [sidecarEnvironment.mode]: mode,
          [sidecarEnvironment.presentation]: presentation.interactive,
        },
      );
      assert.deepEqual(appendSwitch.mock.calls, [
        ["remote-debugging-address", "127.0.0.1"],
        ["remote-debugging-port", "43123"],
      ]);
    }
  });

  it("does nothing unless explicitly requested and rejects escaped surfaces", () => {
    const appendSwitch = vi.fn();
    configureHarnessCDP({ appendSwitch }, {});
    assert.equal(appendSwitch.mock.calls.length, 0);
    for (const environment of [
      {
        [harnessCDPPortEnvironment]: "43123",
        [sidecarEnvironment.mode]: lifecycleMode.dev,
        [sidecarEnvironment.presentation]: presentation.interactive,
      },
      {
        [harnessCDPPortEnvironment]: "43123",
        [sidecarEnvironment.mode]: lifecycleMode.harness,
        [sidecarEnvironment.presentation]: presentation.headless,
      },
      {
        [harnessCDPPortEnvironment]: "04312",
        [sidecarEnvironment.mode]: lifecycleMode.harness,
        [sidecarEnvironment.presentation]: presentation.interactive,
      },
    ]) {
      assert.throws(() => configureHarnessCDP({ appendSwitch }, environment));
    }
  });
});
