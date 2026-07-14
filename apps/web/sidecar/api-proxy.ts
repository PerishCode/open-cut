import { type IncomingMessage, request as requestHttp, type ServerResponse } from "node:http";

const apiPrefix = "/api";

export class ApiProxy {
  #endpoint: string | undefined;

  setRuntime(raw: string | undefined): void {
    this.#endpoint = raw === undefined ? undefined : normalizeApiRuntimeUrl(raw);
  }

  handle(request: IncomingMessage, response: ServerResponse): boolean {
    const incoming = new URL(request.url ?? "/", "http://127.0.0.1");
    if (incoming.pathname !== apiPrefix && !incoming.pathname.startsWith(`${apiPrefix}/`)) return false;
    void this.#forward(request, response, incoming);
    return true;
  }

  async #forward(request: IncomingMessage, response: ServerResponse, incoming: URL): Promise<void> {
    if (!this.#endpoint) {
      writeProxyError(response, 503, "OC_API_RUNTIME_UNAVAILABLE", "The API sidecar has no active endpoint");
      return;
    }
    const target = new URL(this.#endpoint);
    target.pathname = incoming.pathname.slice(apiPrefix.length) || "/";
    target.search = incoming.search;

    await new Promise<void>((resolve) => {
      const upstream = requestHttp(
        target,
        {
          method: request.method,
          headers: forwardedHeaders(request.headers, target.host),
        },
        (upstreamResponse) => {
          response.writeHead(upstreamResponse.statusCode ?? 502, upstreamResponse.headers);
          upstreamResponse.pipe(response);
          upstreamResponse.once("end", resolve);
          upstreamResponse.once("error", (error) => {
            if (!response.headersSent) writeProxyError(response, 502, "OC_API_PROXY_FAILED", error.message);
            else response.destroy(error);
            resolve();
          });
        },
      );
      upstream.once("error", (error) => {
        if (!response.headersSent) writeProxyError(response, 502, "OC_API_PROXY_FAILED", error.message);
        else response.destroy(error);
        resolve();
      });
      request.pipe(upstream);
    });
  }
}

export function normalizeApiRuntimeUrl(raw: string): string {
  const url = new URL(raw);
  const loopback = url.hostname === "127.0.0.1" || url.hostname === "[::1]" || url.hostname === "::1";
  if (url.protocol !== "http:" || !loopback || !url.port) {
    throw new Error("API runtime endpoint must be an explicit loopback HTTP port");
  }
  if (url.username || url.password || url.pathname !== "/" || url.search || url.hash) {
    throw new Error("API runtime endpoint must be an origin without credentials, path, query, or fragment");
  }
  return `${url.origin}/`;
}

function forwardedHeaders(
  headers: IncomingMessage["headers"],
  host: string,
): Record<string, string | string[] | undefined> {
  const forwarded: Record<string, string | string[] | undefined> = { ...headers, host };
  delete forwarded.connection;
  delete forwarded["proxy-connection"];
  delete forwarded["keep-alive"];
  delete forwarded.upgrade;
  return forwarded;
}

function writeProxyError(response: ServerResponse, status: number, code: string, message: string): void {
  if (response.writableEnded) return;
  const body = JSON.stringify({ error: code, message });
  response.writeHead(status, {
    "cache-control": "no-store",
    "content-length": Buffer.byteLength(body),
    "content-type": "application/json; charset=utf-8",
  });
  response.end(body);
}
