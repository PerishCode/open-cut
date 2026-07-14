import { createReadStream, readFileSync } from "node:fs";
import { stat } from "node:fs/promises";
import { createServer, type Server, type ServerResponse } from "node:http";
import { extname, join, relative, resolve, sep } from "node:path";

import { type LifecycleMode, lifecycleMode } from "@open-cut/sidecar-client";
import type { ViteDevServer } from "vite";

export type WebServer = {
  url: string;
  close(): Promise<void>;
};

export async function startWebServer(mode: LifecycleMode, host = "127.0.0.1", port = 0): Promise<WebServer> {
  const webRoot = resolveWebRoot();
  if (mode === lifecycleMode.dev) return startViteServer(webRoot, host, port);
  return startStaticServer(join(webRoot, "dist", "web"), host, port);
}

async function startViteServer(webRoot: string, host: string, port: number): Promise<WebServer> {
  const { createServer: createViteServer } = await import("vite");
  let vite: ViteDevServer | undefined;
  const server = createServer((request, response) => {
    if (!vite) {
      response.writeHead(503);
      response.end();
      return;
    }
    vite.middlewares(request, response, () => {
      response.writeHead(404);
      response.end();
    });
  });
  await listen(server, host, port);
  const address = server.address();
  if (!address || typeof address === "string") {
    await closeServer(server);
    throw new Error("Vite did not bind loopback TCP");
  }
  try {
    vite = await createViteServer({
      appType: "spa",
      configFile: join(webRoot, "vite.config.ts"),
      root: webRoot,
      server: {
        middlewareMode: true,
        ws: { clientPort: address.port, host, server },
      },
    });
  } catch (error) {
    await closeServer(server);
    throw error;
  }
  return {
    url: `http://${host}:${address.port}`,
    close: async () => {
      await vite?.close();
      await closeServer(server);
    },
  };
}

async function startStaticServer(distRoot: string, host: string, port: number): Promise<WebServer> {
  const indexPath = join(distRoot, "index.html");
  await stat(indexPath);
  const server = createServer(async (request, response) => {
    try {
      if (request.method !== "GET" && request.method !== "HEAD") {
        response.writeHead(405, { allow: "GET, HEAD" });
        response.end();
        return;
      }
      const requestUrl = new URL(request.url ?? "/", `http://${host}`);
      const pathname = decodeURIComponent(requestUrl.pathname);
      const requested = pathname === "/" ? "index.html" : pathname.slice(1);
      const candidate = resolve(distRoot, requested);
      const contained = relative(distRoot, candidate);
      if (contained === ".." || contained.startsWith(`..${sep}`)) {
        response.writeHead(404).end();
        return;
      }
      const asset = (await regularFile(candidate)) ? candidate : indexPath;
      await sendFile(asset, request.method === "HEAD", response);
    } catch {
      if (!response.headersSent) response.writeHead(500);
      response.end();
    }
  });
  await listen(server, host, port);
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("Web server did not bind loopback TCP");
  return {
    url: `http://${host}:${address.port}`,
    close: () => closeServer(server),
  };
}

function listen(server: Server, host: string, port: number): Promise<void> {
  return new Promise<void>((resolveListen, reject) => {
    server.once("error", reject);
    server.listen(port, host, resolveListen);
  });
}

function closeServer(server: Server): Promise<void> {
  return new Promise<void>((resolveClose, reject) => {
    server.close((error) => (error ? reject(error) : resolveClose()));
  });
}

async function regularFile(filename: string): Promise<boolean> {
  try {
    return (await stat(filename)).isFile();
  } catch {
    return false;
  }
}

async function sendFile(filename: string, head: boolean, response: ServerResponse): Promise<void> {
  const info = await stat(filename);
  response.writeHead(200, {
    "cache-control": filename.endsWith("index.html") ? "no-cache" : "public, max-age=31536000, immutable",
    "content-length": info.size,
    "content-type": contentType(filename),
  });
  if (head) {
    response.end();
    return;
  }
  await new Promise<void>((resolveSend, reject) => {
    const stream = createReadStream(filename);
    stream.once("error", reject);
    response.once("error", reject);
    response.once("finish", resolveSend);
    stream.pipe(response);
  });
}

function contentType(filename: string): string {
  switch (extname(filename).toLowerCase()) {
    case ".css":
      return "text/css; charset=utf-8";
    case ".html":
      return "text/html; charset=utf-8";
    case ".ico":
      return "image/x-icon";
    case ".jpg":
    case ".jpeg":
      return "image/jpeg";
    case ".js":
      return "text/javascript; charset=utf-8";
    case ".json":
      return "application/json; charset=utf-8";
    case ".png":
      return "image/png";
    case ".svg":
      return "image/svg+xml";
    case ".webp":
      return "image/webp";
    default:
      return "application/octet-stream";
  }
}

export function resolveWebRoot(directory = process.cwd()): string {
  let manifest: { name?: unknown };
  try {
    manifest = JSON.parse(readFileSync(join(directory, "package.json"), "utf8")) as { name?: unknown };
  } catch (error) {
    throw new Error(`runtime topology directory has no readable package.json: ${directory}`, { cause: error });
  }
  if (manifest.name !== "@open-cut/web") {
    throw new Error(`runtime topology directory is not @open-cut/web: ${directory}`);
  }
  return directory;
}
