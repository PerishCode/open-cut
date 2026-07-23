import assert from "node:assert/strict";

import { lifecycleMode, presentation, sidecarEnvironment } from "@open-cut/sidecar-client";
import { describe, it, vi } from "vitest";

import { configureHarnessCDP, harnessCDPPortEnvironment } from "../sidecar/harness-cdp.js";

describe("delivery harness CDP", () => {
  it("enables only a canonical loopback port for interactive packaged and dev checks", () => {
    for (const mode of [lifecycleMode.packaged, lifecycleMode.harness, lifecycleMode.dev]) {
      const appendSwitch = vi.fn();
      const port = configureHarnessCDP(
        { appendSwitch },
        {
          [harnessCDPPortEnvironment]: "43123",
          [sidecarEnvironment.mode]: mode,
          [sidecarEnvironment.presentation]: presentation.interactive,
        },
      );
      assert.equal(port, 43123);
      assert.deepEqual(appendSwitch.mock.calls, [
        ["remote-debugging-address", "127.0.0.1"],
        ["remote-debugging-port", "43123"],
        ["disable-backgrounding-occluded-windows"],
      ]);
    }
  });

  it("does nothing unless explicitly requested and rejects escaped surfaces", () => {
    const appendSwitch = vi.fn();
    assert.equal(configureHarnessCDP({ appendSwitch }, {}), undefined);
    assert.equal(appendSwitch.mock.calls.length, 0);
    for (const environment of [
      {
        [harnessCDPPortEnvironment]: "43123",
        [sidecarEnvironment.mode]: lifecycleMode.production,
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
