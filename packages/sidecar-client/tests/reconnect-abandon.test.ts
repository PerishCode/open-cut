import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { normalize } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { type WebSocket, WebSocketServer } from "ws";

import { SidecarConnection } from "../src/index.js";

type FakeBroker = { port: number; destroy: () => Promise<void> };

async function startFakeBroker(): Promise<FakeBroker> {
  const server = createServer((_, response) => {
    response.statusCode = 404;
    response.end();
  });
  const registrations = new WebSocketServer({ server, path: "/v1/sessions/register" });
  const sockets = new Set<WebSocket>();
  registrations.on("connection", (socket) => {
    sockets.add(socket);
    socket.on("close", () => sockets.delete(socket));
    socket.on("message", (raw) => {
      const event = JSON.parse(raw.toString()) as { type?: string };
      if (event.type === "register") socket.send(JSON.stringify({ type: "registered" }));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  return {
    port: (server.address() as AddressInfo).port,
    destroy: async () => {
      for (const socket of sockets) socket.terminate();
      registrations.close();
      server.closeAllConnections();
      await new Promise<void>((resolve) => server.close(() => resolve()));
    },
  };
}

function setLaunchEnvironment(port: number, mode: string): void {
  process.env.OC_SIDECAR_APP = "payload";
  process.env.OC_SIDECAR_CHANNEL = "test";
  process.env.OC_SIDECAR_CONTROL = JSON.stringify({
    schema: 1,
    protocol: "sidecar.v1",
    address: `127.0.0.1:${port}`,
    pid: 4242,
    sessionId: "fake-session",
    generation: 1,
    startedAt: new Date().toISOString(),
  });
  process.env.OC_SIDECAR_DATA_DIR = normalize(tmpdir());
  process.env.OC_SIDECAR_INSTALLATION = JSON.stringify({
    schema: 1,
    installationId: "fake-installation",
    generation: 1,
    keys: [{ role: "payload", algorithm: "ed25519", publicKey: "AAAA" }],
  });
  process.env.OC_SIDECAR_MODE = mode;
  process.env.OC_SIDECAR_NAMESPACE = "default";
  process.env.OC_SIDECAR_PRESENTATION = "headless";
  process.env.OC_SIDECAR_SOURCE = "test";
  process.env.OC_SIDECAR_TOKEN = "fake-token";
}

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

const cleanups: Array<() => void | Promise<void>> = [];

afterEach(async () => {
  while (cleanups.length > 0) await cleanups.pop()?.();
  for (const key of Object.keys(process.env)) {
    if (key.startsWith("OC_SIDECAR_")) delete process.env[key];
  }
});

describe("bounded reconnect fail-closed policy", () => {
  it("abandons a dev session once the broker stays unreachable beyond the window", async () => {
    const broker = await startFakeBroker();
    setLaunchEnvironment(broker.port, "dev");
    let resolveAbandoned!: () => void;
    const abandoned = new Promise<string>((resolve) => {
      resolveAbandoned = () => resolve("abandoned");
    });
    const connection = await SidecarConnection.connect({
      abandonWindowMs: 250,
      onAbandoned: () => resolveAbandoned(),
    });
    cleanups.push(() => connection.close());
    await broker.destroy();
    await expect(Promise.race([abandoned, delay(8_000).then(() => "timeout")])).resolves.toBe("abandoned");
  }, 15_000);

  it("keeps a packaged session reconnecting without abandonment", async () => {
    const broker = await startFakeBroker();
    setLaunchEnvironment(broker.port, "packaged");
    let calls = 0;
    const connection = await SidecarConnection.connect({
      abandonWindowMs: 100,
      onAbandoned: () => {
        calls += 1;
      },
    });
    cleanups.push(() => connection.close());
    await broker.destroy();
    await delay(800);
    expect(calls).toBe(0);
  }, 10_000);
});
