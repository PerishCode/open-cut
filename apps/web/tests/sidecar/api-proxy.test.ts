import { createServer, type RequestListener, type Server } from "node:http";
import { afterEach, describe, expect, it } from "vitest";

import { ApiProxy, normalizeApiRuntimeUrl } from "../../sidecar/api-proxy.js";

const servers: Server[] = [];

afterEach(async () => {
  await Promise.all(servers.splice(0).map((server) => new Promise<void>((resolve) => server.close(() => resolve()))));
});

describe("Web sidecar API proxy", () => {
  it("rejects non-loopback runtime endpoints", () => {
    expect(() => normalizeApiRuntimeUrl("https://example.com:443")).toThrow(/loopback/);
    expect(() => normalizeApiRuntimeUrl("http://127.0.0.1:4000/path")).toThrow(/origin/);
  });

  it("returns 503 until an API lease is available", async () => {
    const proxy = new ApiProxy();
    const endpoint = await listen((request, response) => {
      if (!proxy.handle(request, response)) response.writeHead(404).end();
    });
    const response = await fetch(`${endpoint}/api/v1/health`);
    expect(response.status).toBe(503);
    await expect(response.json()).resolves.toMatchObject({ error: "OC_API_RUNTIME_UNAVAILABLE" });
  });

  it("strips the stable ingress prefix and follows endpoint replacement", async () => {
    const first = await listen((request, response) => {
      response.end(`first:${request.url}`);
    });
    const second = await listen((request, response) => {
      response.end(`second:${request.url}`);
    });
    const proxy = new ApiProxy();
    proxy.setRuntime(first);
    const web = await listen((request, response) => {
      if (!proxy.handle(request, response)) response.writeHead(404).end();
    });

    await expect(fetch(`${web}/api/v1/health?fresh=1`).then((response) => response.text())).resolves.toBe(
      "first:/v1/health?fresh=1",
    );
    proxy.setRuntime(second);
    await expect(fetch(`${web}/api/v1/health`).then((response) => response.text())).resolves.toBe("second:/v1/health");
  });

  it("closes an active event stream when its leased endpoint is replaced", async () => {
    let resolveClosed: () => void = () => undefined;
    const firstClosed = new Promise<void>((resolve) => {
      resolveClosed = resolve;
    });
    const first = await listen((_request, response) => {
      response.once("close", resolveClosed);
      response.writeHead(200, { "content-type": "text/event-stream" });
      response.write("event: project.snapshot\ndata: {}\n\n");
    });
    const second = await listen((_request, response) => response.end("replacement"));
    const proxy = new ApiProxy();
    proxy.setRuntime(first);
    const web = await listen((request, response) => {
      if (!proxy.handle(request, response)) response.writeHead(404).end();
    });

    const stream = await fetch(`${web}/api/v1/events`);
    await stream.body?.getReader().read();
    proxy.setRuntime(second);
    await withTimeout(firstClosed);
    await expect(fetch(`${web}/api/v1/events`).then((response) => response.text())).resolves.toBe("replacement");
  });
});

async function listen(handler: RequestListener): Promise<string> {
  const server = createServer(handler);
  servers.push(server);
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("test server did not bind TCP");
  return `http://127.0.0.1:${address.port}`;
}

async function withTimeout(promise: Promise<void>): Promise<void> {
  let timeout: ReturnType<typeof setTimeout> | undefined;
  await Promise.race([
    promise,
    new Promise<never>((_resolve, reject) => {
      timeout = setTimeout(() => reject(new Error("timed out waiting for stream closure")), 1000);
    }),
  ]);
  clearTimeout(timeout);
}
