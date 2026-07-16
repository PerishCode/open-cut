import { type ClientRequest, type IncomingMessage, request as requestHttp, type ServerResponse } from "node:http";

import type { ProxyUISession } from "./ui-session.js";

const apiPrefix = "/api";
export const developmentSessionCookie = "open_cut_dev_ui";
const uiSessionHeader = "x-open-cut-ui-session";
const internalAuthPrefix = "/api/v1/auth/";
const internalPlatformPrefix = "/api/v1/internal/platform/";

export class ApiProxy {
  readonly #active = new Set<ClientRequest>();
  #endpoint: string | undefined;
  #uiSession: ProxyUISession | undefined;

  setRuntime(raw: string | undefined): void {
    const endpoint = raw === undefined ? undefined : normalizeApiRuntimeUrl(raw);
    if (endpoint === this.#endpoint) return;
    this.#endpoint = endpoint;
    for (const request of this.#active) request.destroy(new Error("API runtime endpoint changed"));
  }

  setUISession(session: ProxyUISession | undefined): void {
    this.#uiSession = session;
  }

  browserCookie(): string | undefined {
    const session = this.#uiSession;
    if (!session) return undefined;
    const maximumAge = Math.max(0, Math.floor((session.expiresAt - Date.now()) / 1000));
    return `${developmentSessionCookie}=${session.browserBinding}; Path=/; HttpOnly; SameSite=Strict; Max-Age=${maximumAge}`;
  }

  handle(request: IncomingMessage, response: ServerResponse): boolean {
    const incoming = new URL(request.url ?? "/", "http://127.0.0.1");
    if (incoming.pathname !== apiPrefix && !incoming.pathname.startsWith(`${apiPrefix}/`)) return false;
    if (incoming.pathname.startsWith(internalAuthPrefix) || incoming.pathname.startsWith(internalPlatformPrefix)) {
      writeProxyError(
        response,
        404,
        "OC_API_ROUTE_NOT_FOUND",
        "Internal authorization bootstrap is not a browser route",
      );
      return true;
    }
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

    const localSession = this.#uiSession;
    const privilegedSession = singleHeader(request.headers[uiSessionHeader]);
    if (localSession) {
      if (localSession.expiresAt <= Date.now()) {
        writeProxyError(response, 503, "OC_UI_SESSION_EXPIRED", "The development UI session expired");
        return;
      }
      if (
        !privilegedSession &&
        cookieValue(request.headers.cookie, developmentSessionCookie) !== localSession.browserBinding
      ) {
        writeProxyError(
          response,
          401,
          "OC_UI_SESSION_REQUIRED",
          "The browser is not bound to this development UI session",
        );
        return;
      }
    }

    await new Promise<void>((resolve) => {
      const upstream = requestHttp(
        target,
        {
          method: request.method,
          headers: forwardedHeaders(request.headers, target.host, privilegedSession ?? localSession?.apiSession),
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
      this.#active.add(upstream);
      upstream.once("close", () => this.#active.delete(upstream));
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
  localSession: string | undefined,
): Record<string, string | string[] | undefined> {
  const forwarded: Record<string, string | string[] | undefined> = { ...headers, host };
  delete forwarded.connection;
  delete forwarded["proxy-connection"];
  delete forwarded["keep-alive"];
  delete forwarded.upgrade;
  delete forwarded.cookie;
  delete forwarded["x-open-cut-cli-grant"];
  delete forwarded["x-open-cut-cli-challenge"];
  delete forwarded["x-open-cut-cli-signature"];
  if (localSession) forwarded[uiSessionHeader] = localSession;
  return forwarded;
}

function cookieValue(header: string | undefined, name: string): string | undefined {
  for (const entry of header?.split(";") ?? []) {
    const separator = entry.indexOf("=");
    if (separator < 0 || entry.slice(0, separator).trim() !== name) continue;
    return entry.slice(separator + 1).trim();
  }
  return undefined;
}

function singleHeader(value: string | string[] | undefined): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
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
