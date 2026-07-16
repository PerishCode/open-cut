import { createServer, type RequestListener, type Server } from "node:http";
import { afterEach, describe, expect, it } from "vitest";

import { ApiProxy, developmentSessionCookie, normalizeApiRuntimeUrl } from "../../sidecar/api-proxy.js";

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

  it("binds a development browser cookie to a hidden upstream UI session", async () => {
    const api = await listen((request, response) => {
      expect(request.headers["x-open-cut-ui-session"]).toBe("oc_ui_hidden");
      expect(request.headers.cookie).toBeUndefined();
      expect(request.headers["x-open-cut-cli-signature"]).toBeUndefined();
      response.end("authorized");
    });
    const proxy = new ApiProxy();
    proxy.setRuntime(api);
    proxy.setUISession({
      apiSession: "oc_ui_hidden",
      browserBinding: "browser-binding",
      expiresAt: Date.now() + 60_000,
    });
    const web = await listen((request, response) => {
      if (!proxy.handle(request, response)) response.writeHead(404).end();
    });

    const rejected = await fetch(`${web}/api/v1/projects`);
    expect(rejected.status).toBe(401);
    const accepted = await fetch(`${web}/api/v1/projects`, {
      headers: {
        cookie: `${developmentSessionCookie}=browser-binding`,
        "x-open-cut-cli-signature": "must-not-forward",
      },
    });
    expect(accepted.status).toBe(200);
    await expect(accepted.text()).resolves.toBe("authorized");
  });

  it("preserves session-bound media range requests and responses", async () => {
    const api = await listen((request, response) => {
      expect(request.url).toMatch(/^\/v1\/media\/content\/oc_media_/);
      expect(request.headers.range).toBe("bytes=4-7");
      expect(request.headers["x-open-cut-ui-session"]).toBe("oc_ui_media");
      response.writeHead(206, {
        "accept-ranges": "bytes",
        "content-length": "4",
        "content-range": "bytes 4-7/20",
        "content-type": "video/webm",
        etag: '"sha256-media"',
      });
      response.end("cut-");
    });
    const proxy = new ApiProxy();
    proxy.setRuntime(api);
    proxy.setUISession({
      apiSession: "oc_ui_media",
      browserBinding: "media-browser",
      expiresAt: Date.now() + 60_000,
    });
    const web = await listen((request, response) => {
      if (!proxy.handle(request, response)) response.writeHead(404).end();
    });
    const response = await fetch(`${web}/api/v1/media/content/oc_media_opaque`, {
      headers: { cookie: `${developmentSessionCookie}=media-browser`, range: "bytes=4-7" },
    });
    expect(response.status).toBe(206);
    expect(response.headers.get("content-range")).toBe("bytes 4-7/20");
    expect(response.headers.get("etag")).toBe('"sha256-media"');
    await expect(response.text()).resolves.toBe("cut-");
  });

  it("does not expose internal UI bootstrap routes through the browser proxy", async () => {
    const proxy = new ApiProxy();
    const web = await listen((request, response) => {
      if (!proxy.handle(request, response)) response.writeHead(404).end();
    });
    const response = await fetch(`${web}/api/v1/auth/ui/challenges`, { method: "POST" });
    expect(response.status).toBe(404);
    await expect(response.json()).resolves.toMatchObject({ error: "OC_API_ROUTE_NOT_FOUND" });

    const platform = await fetch(`${web}/api/v1/internal/platform/source-grants`, { method: "POST" });
    expect(platform.status).toBe(404);
    await expect(platform.json()).resolves.toMatchObject({ error: "OC_API_ROUTE_NOT_FOUND" });
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
