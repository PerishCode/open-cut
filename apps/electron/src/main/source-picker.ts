import { type BrowserWindow, dialog } from "electron";

import { isDroppedSourceToken } from "./dropped-source.js";

const maximumRequestBytes = 16 << 10;

export async function handleSourcePickerRequest(
  request: Request,
  window: BrowserWindow,
  apiRuntimeUrl: string | undefined,
  uiSession: string | undefined,
  consumeDroppedSource?: (token: string) => string | undefined,
): Promise<Response> {
  if (!apiRuntimeUrl || !uiSession) {
    return platformError(503, "OC_SOURCE_AUTHORITY_UNAVAILABLE", "Source authority is not ready");
  }
  const requestBody = await readSelectionRequest(request);
  if (!requestBody) {
    return platformError(422, "OC_SOURCE_SELECTION_INVALID", "Source selection request is invalid");
  }
  let sourcePath: string;
  let bookmark: string | undefined;
  if (requestBody.sourceToken) {
    sourcePath = consumeDroppedSource?.(requestBody.sourceToken) ?? "";
    if (!sourcePath) return platformError(410, "OC_DROPPED_SOURCE_EXPIRED", "Dropped source authority expired");
  } else {
    const selection = await dialog.showOpenDialog(window, {
      title: "Choose footage or audio",
      properties: ["openFile"],
      securityScopedBookmarks: process.platform === "darwin" && Boolean(process.mas),
      filters: [
        {
          name: "Media",
          extensions: ["mp4", "mov", "m4v", "mkv", "webm", "avi", "mp3", "m4a", "wav", "aac", "flac"],
        },
        { name: "All files", extensions: ["*"] },
      ],
    });
    const selectedPath = selection.filePaths.length === 1 ? selection.filePaths[0] : undefined;
    if (selection.canceled || !selectedPath) {
      return new Response(null, { status: 204, headers: { "cache-control": "no-store" } });
    }
    sourcePath = selectedPath;
    bookmark = selection.bookmarks?.[0];
  }
  const response = await fetch(new URL("v1/internal/platform/source-grants", normalizeApiRuntimeUrl(apiRuntimeUrl)), {
    method: "POST",
    headers: { "content-type": "application/json", "x-open-cut-ui-session": uiSession },
    body: JSON.stringify({
      requestId: requestBody.requestId,
      path: sourcePath,
      ...(bookmark ? { bookmark } : {}),
    }),
  });
  const body = await response.arrayBuffer();
  return new Response(body, {
    status: response.status,
    headers: {
      "cache-control": "no-store",
      "content-type": response.headers.get("content-type") ?? "application/json; charset=utf-8",
    },
  });
}

async function readSelectionRequest(
  request: Request,
): Promise<{ requestId: string; sourceToken?: string } | undefined> {
  const body = await request.arrayBuffer();
  if (body.byteLength === 0 || body.byteLength > maximumRequestBytes) return undefined;
  try {
    const value = JSON.parse(new TextDecoder().decode(body)) as unknown;
    if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
    const record = value as Record<string, unknown>;
    const keys = Object.keys(record).sort();
    if (
      (keys.length !== 1 && keys.length !== 2) ||
      keys[0] !== "requestId" ||
      (keys.length === 2 && keys[1] !== "sourceToken") ||
      typeof record.requestId !== "string"
    )
      return undefined;
    if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(record.requestId)) return undefined;
    if (
      record.sourceToken !== undefined &&
      (typeof record.sourceToken !== "string" || !isDroppedSourceToken(record.sourceToken))
    )
      return undefined;
    return {
      requestId: record.requestId,
      ...(typeof record.sourceToken === "string" ? { sourceToken: record.sourceToken } : {}),
    };
  } catch {
    return undefined;
  }
}

function platformError(status: number, code: string, message: string): Response {
  return new Response(JSON.stringify({ error: code, message }), {
    status,
    headers: { "cache-control": "no-store", "content-type": "application/json; charset=utf-8" },
  });
}

function normalizeApiRuntimeUrl(raw: string): string {
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
