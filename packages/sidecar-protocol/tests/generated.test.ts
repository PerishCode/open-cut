import { describe, expect, it } from "vitest";

import {
  ProtocolDecodeError,
  decodeServerEvent,
  decodeSidecarLaunchEnvironment,
  decodeStatus,
  lifecycleMode,
  operations,
  presentation,
  sidecarEnvironment,
} from "../src/index.js";

const validEnvironment = {
  [sidecarEnvironment.app]: "web",
  [sidecarEnvironment.channel]: "beta",
  [sidecarEnvironment.control]: JSON.stringify({
    address: "127.0.0.1:4321",
    generation: 2,
    pid: 7,
    protocol: "sidecar.v1",
    schema: 1,
    sessionId: "session",
    startedAt: "2026-07-14T00:00:00Z",
  }),
  [sidecarEnvironment.mode]: lifecycleMode.harness,
  [sidecarEnvironment.namespace]: "tests",
  [sidecarEnvironment.presentation]: presentation.headless,
  [sidecarEnvironment.source]: "vitest",
  [sidecarEnvironment.token]: "token",
};

describe("generated sidecar protocol", () => {
  it("decodes the complete launch envelope from generated environment bindings", () => {
    expect(decodeSidecarLaunchEnvironment(validEnvironment)).toMatchObject({
      app: "web",
      mode: lifecycleMode.harness,
      presentation: presentation.headless,
    });
  });

  it("rejects missing and unexpected launch fields", () => {
    expect(() => decodeSidecarLaunchEnvironment({ ...validEnvironment, [sidecarEnvironment.app]: undefined }))
      .toThrow(ProtocolDecodeError);
    const control = JSON.parse(validEnvironment[sidecarEnvironment.control]) as Record<string, unknown>;
    expect(() => decodeSidecarLaunchEnvironment({
      ...validEnvironment,
      [sidecarEnvironment.control]: JSON.stringify({ ...control, escaped: true }),
    })).toThrow(/escaped/);
  });

  it("derives HTTP and WebSocket schemes from TypeSpec operation metadata", () => {
    expect(operations.status.scheme).toBe("http");
    expect(operations.registerSession.scheme).toBe("ws");
  });

  it("validates nested server events and status documents", () => {
    expect(decodeServerEvent({ type: "registered" })).toEqual({ type: "registered" });
    expect(() => decodeStatus({ schema: 1, revision: -1 })).toThrow(ProtocolDecodeError);
    expect(() => decodeServerEvent({ type: "registered", hidden: true })).toThrow(ProtocolDecodeError);
  });
});
