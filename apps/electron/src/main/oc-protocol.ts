export const OC_WEB_SCHEME = "oc";
export const OC_WEB_HOST = "app";
export const OC_WEB_ENTRY_URL = `${OC_WEB_SCHEME}://${OC_WEB_HOST}/`;
export const OC_PLATFORM_SOURCE_GRANT_PATH = "/_open-cut/platform/source-grants";
export const OC_PLATFORM_EXPORT_SAVE_PATH = "/_open-cut/platform/export-save-as";
export const OC_PLATFORM_EXPORT_REVEAL_PATH = "/_open-cut/platform/export-reveal";

type ProtocolFetch = (request: Request) => Promise<Response>;
export type PlatformRequestHandler = (request: Request) => Promise<Response>;

type RetryOptions = {
  attempts?: number;
  backoffMs?: number;
  delay?: (milliseconds: number) => Promise<void>;
};

const retryableMethods = new Set(["GET", "HEAD"]);
const retryAttempts = 3;
const retryBackoffMs = 150;
const reservedAuthorityHeaders = [
  "x-open-cut-ui-session",
  "x-open-cut-cli-grant",
  "x-open-cut-cli-challenge",
  "x-open-cut-cli-signature",
] as const;

export function normalizeWebRuntimeUrl(raw: string): string {
  const url = new URL(raw);
  const loopback = url.hostname === "127.0.0.1" || url.hostname === "[::1]" || url.hostname === "::1";
  if (url.protocol !== "http:" || !loopback || !url.port) {
    throw new Error("Web runtime endpoint must be an explicit loopback HTTP port");
  }
  if (url.username || url.password || url.pathname !== "/" || url.search || url.hash) {
    throw new Error("Web runtime endpoint must be an origin without credentials, path, query, or fragment");
  }
  return `${url.origin}/`;
}

export function toWebRuntimeUrl(webRuntimeUrl: string, requestUrl: string): string {
  const incoming = new URL(requestUrl);
  if (incoming.protocol !== `${OC_WEB_SCHEME}:` || incoming.host !== OC_WEB_HOST) {
    throw new Error(`Only ${OC_WEB_ENTRY_URL} requests can use the Web protocol`);
  }
  const target = new URL(normalizeWebRuntimeUrl(webRuntimeUrl));
  target.pathname = incoming.pathname;
  target.search = incoming.search;
  return target.toString();
}

export async function handleOcWebRequest(
  request: Request,
  webRuntimeUrl: string | undefined,
  fetchImpl: ProtocolFetch,
  options: RetryOptions = {},
  uiSession?: string,
  platformRequest?: PlatformRequestHandler,
): Promise<Response> {
  if (!webRuntimeUrl) {
    return errorResponse(503, "OC_WEB_RUNTIME_UNAVAILABLE", "The Web sidecar has no active endpoint");
  }

  let target: string;
  try {
    target = toWebRuntimeUrl(webRuntimeUrl, request.url);
  } catch (error) {
    return errorResponse(404, "OC_WEB_PROTOCOL_NOT_FOUND", errorMessage(error));
  }

  for (const header of reservedAuthorityHeaders) {
    if (request.headers.has(header)) {
      return errorResponse(400, "OC_WEB_AUTHORITY_HEADER_FORBIDDEN", "Renderer requests may not set authority headers");
    }
  }

  const incoming = new URL(request.url);
  if (
    incoming.pathname === OC_PLATFORM_SOURCE_GRANT_PATH ||
    incoming.pathname === OC_PLATFORM_EXPORT_SAVE_PATH ||
    incoming.pathname === OC_PLATFORM_EXPORT_REVEAL_PATH
  ) {
    if (incoming.search || request.method !== "POST") {
      return errorResponse(405, "OC_PLATFORM_REQUEST_INVALID", "Platform actions require POST without a query");
    }
    if (!platformRequest) {
      return errorResponse(503, "OC_PLATFORM_UNAVAILABLE", "This surface cannot perform platform actions");
    }
    return platformRequest(request);
  }
  if (incoming.pathname.startsWith("/api/v1/internal/platform/")) {
    return errorResponse(404, "OC_API_ROUTE_NOT_FOUND", "Internal platform routes are not renderer routes");
  }
  if ((incoming.pathname === "/api" || incoming.pathname.startsWith("/api/")) && !uiSession) {
    return errorResponse(503, "OC_UI_SESSION_UNAVAILABLE", "Electron main has no active UI session");
  }
  const forwarded = new Request(target, request);
  if (uiSession && (incoming.pathname === "/api" || incoming.pathname.startsWith("/api/"))) {
    const headers = new Headers(forwarded.headers);
    headers.set("x-open-cut-ui-session", uiSession);
    request = new Request(forwarded, { headers });
  } else {
    request = forwarded;
  }

  const attempts = retryableMethods.has(request.method) ? (options.attempts ?? retryAttempts) : 1;
  const backoffMs = options.backoffMs ?? retryBackoffMs;
  const delay = options.delay ?? defaultDelay;
  let lastError: unknown;

  for (let attempt = 1; attempt <= attempts; attempt += 1) {
    try {
      return await fetchImpl(request);
    } catch (error) {
      lastError = error;
      if (attempt === attempts) break;
      const waitMs = backoffMs * attempt;
      console.warn("[open-cut electron] oc:// proxy fetch failed; retrying", {
        attempt,
        attempts,
        message: errorMessage(error),
        waitMs,
      });
      await delay(waitMs);
    }
  }

  return errorResponse(502, "OC_WEB_PROTOCOL_PROXY_FAILED", errorMessage(lastError));
}

function errorResponse(status: number, code: string, message: string): Response {
  return new Response(JSON.stringify({ error: code, message }), {
    status,
    headers: {
      "cache-control": "no-store",
      "content-type": "application/json; charset=utf-8",
    },
  });
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function defaultDelay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}
