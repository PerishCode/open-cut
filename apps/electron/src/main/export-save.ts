import { createHash, randomBytes, randomUUID } from "node:crypto";
import { link, lstat, open, rename, unlink } from "node:fs/promises";
import { basename, dirname, join } from "node:path";

import { type BrowserWindow, dialog } from "electron";

import { type DeliveryReceiptStore, regularFileIdentity } from "./export-reveal.js";

const maximumRequestBytes = 16 << 10;
const destinationGrantTTL = 5 * 60_000;
const destinationGrantLimit = 32;
const durableIDPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const digestPattern = /^sha256:[0-9a-f]{64}$/;
const destinationGrantPattern = /^destination\.[A-Za-z0-9_-]{32}$/;

type SaveRequest = Readonly<{
  projectId: string;
  artifactId: string;
  suggestedName: string;
  destinationGrant?: string;
  overwrite: boolean;
}>;

type DestinationGrant = Readonly<{
  projectId: string;
  artifactId: string;
  destinationPath: string;
  displayName: string;
  targetIdentity?: string;
  expiresAt: number;
}>;

type DeliveryLease = Readonly<{
  contentUrl: string;
  byteLength: bigint;
  contentSha256: string;
}>;

export class DestinationGrantStore {
  readonly #grants = new Map<string, DestinationGrant>();

  create(value: Omit<DestinationGrant, "expiresAt">, now = Date.now()): string {
    this.#cleanup(now);
    if (this.#grants.size >= destinationGrantLimit) throw new Error("Destination grant capacity is exhausted");
    const token = `destination.${randomBytes(24).toString("base64url")}`;
    this.#grants.set(token, { ...value, expiresAt: now + destinationGrantTTL });
    return token;
  }

  consume(token: string, now = Date.now()): DestinationGrant | undefined {
    this.#cleanup(now);
    const grant = this.#grants.get(token);
    this.#grants.delete(token);
    return grant;
  }

  clear(): void {
    this.#grants.clear();
  }

  #cleanup(now: number): void {
    for (const [token, grant] of this.#grants) {
      if (now >= grant.expiresAt) this.#grants.delete(token);
    }
  }
}

export async function handleExportSaveRequest(
  request: Request,
  window: BrowserWindow,
  apiRuntimeUrl: string | undefined,
  uiSession: string | undefined,
  grants: DestinationGrantStore,
  receipts: DeliveryReceiptStore,
): Promise<Response> {
  if (!apiRuntimeUrl || !uiSession) {
    return platformError(503, "OC_EXPORT_AUTHORITY_UNAVAILABLE", "Export delivery authority is not ready");
  }
  const input = await readSaveRequest(request);
  if (!input) return platformError(422, "OC_EXPORT_SAVE_INVALID", "Export Save As request is invalid");

  let grant: DestinationGrant;
  if (input.destinationGrant) {
    const consumed = grants.consume(input.destinationGrant);
    if (!consumed) {
      return platformError(410, "OC_EXPORT_DESTINATION_EXPIRED", "Export destination authority expired");
    }
    if (consumed.projectId !== input.projectId || consumed.artifactId !== input.artifactId || !input.overwrite) {
      return platformError(422, "OC_EXPORT_SAVE_INVALID", "Export destination authority does not match");
    }
    grant = consumed;
    const target = await existingTarget(grant.destinationPath);
    if (target.state !== "file" || target.identity !== grant.targetIdentity) {
      return platformError(409, "OC_EXPORT_DESTINATION_CHANGED", "Export destination changed after approval");
    }
  } else {
    const selection = await dialog.showSaveDialog(window, {
      title: "Save exported video",
      defaultPath: input.suggestedName,
      filters: [{ name: "WebM video", extensions: ["webm"] }],
      properties: ["createDirectory", "showOverwriteConfirmation"],
    });
    if (selection.canceled || !selection.filePath) {
      return new Response(null, { status: 204, headers: { "cache-control": "no-store" } });
    }
    grant = {
      projectId: input.projectId,
      artifactId: input.artifactId,
      destinationPath: selection.filePath,
      displayName: basename(selection.filePath),
      expiresAt: Date.now() + destinationGrantTTL,
    };
    const target = await existingTarget(grant.destinationPath);
    if (target.state === "invalid") {
      return platformError(422, "OC_EXPORT_DESTINATION_INVALID", "Export destination is not a regular file");
    }
    if (target.state === "file") {
      const token = grants.create({
        projectId: grant.projectId,
        artifactId: grant.artifactId,
        destinationPath: grant.destinationPath,
        displayName: grant.displayName,
        targetIdentity: target.identity,
      });
      return new Response(
        JSON.stringify({
          error: "OC_EXPORT_OVERWRITE_REQUIRED",
          destinationGrant: token,
          displayName: grant.displayName,
        }),
        { status: 409, headers: responseHeaders() },
      );
    }
  }

  const lease = await createDeliveryLease(apiRuntimeUrl, uiSession, input.projectId, input.artifactId, request.signal);
  if (lease instanceof Response) return lease;
  try {
    const targetIdentity = await copyDelivery(
      apiRuntimeUrl,
      uiSession,
      lease,
      grant.destinationPath,
      input.overwrite,
      grant.targetIdentity,
      request.signal,
    );
    const deliveryReceipt = receipts.create({
      uiSession,
      destinationPath: grant.destinationPath,
      displayName: grant.displayName,
      targetIdentity,
    });
    return new Response(
      JSON.stringify({
        status: "saved",
        displayName: grant.displayName,
        byteLength: lease.byteLength.toString(),
        contentSha256: lease.contentSha256,
        deliveryReceipt,
      }),
      { status: 200, headers: responseHeaders() },
    );
  } catch (error) {
    return platformError(502, "OC_EXPORT_DELIVERY_FAILED", errorMessage(error));
  }
}

async function readSaveRequest(request: Request): Promise<SaveRequest | undefined> {
  const body = await request.arrayBuffer();
  if (body.byteLength === 0 || body.byteLength > maximumRequestBytes) return undefined;
  try {
    const value = JSON.parse(new TextDecoder().decode(body)) as unknown;
    if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
    const record = value as Record<string, unknown>;
    const allowed = new Set(["projectId", "artifactId", "suggestedName", "destinationGrant", "overwrite"]);
    if (Object.keys(record).some((key) => !allowed.has(key))) return undefined;
    if (
      typeof record.projectId !== "string" ||
      typeof record.artifactId !== "string" ||
      !durableIDPattern.test(record.projectId) ||
      !durableIDPattern.test(record.artifactId) ||
      typeof record.suggestedName !== "string" ||
      !validSuggestedName(record.suggestedName)
    )
      return undefined;
    const destinationGrant = record.destinationGrant;
    const overwrite = record.overwrite === true;
    if (
      destinationGrant !== undefined &&
      (typeof destinationGrant !== "string" || !destinationGrantPattern.test(destinationGrant))
    )
      return undefined;
    if ((destinationGrant === undefined && overwrite) || (destinationGrant !== undefined && !overwrite))
      return undefined;
    return {
      projectId: record.projectId,
      artifactId: record.artifactId,
      suggestedName: record.suggestedName,
      ...(typeof destinationGrant === "string" ? { destinationGrant } : {}),
      overwrite,
    };
  } catch {
    return undefined;
  }
}

async function createDeliveryLease(
  apiRuntimeUrl: string,
  uiSession: string,
  projectId: string,
  artifactId: string,
  signal: AbortSignal,
): Promise<DeliveryLease | Response> {
  const path = `v1/internal/platform/projects/${projectId}/export-artifacts/${artifactId}/leases`;
  const response = await fetch(new URL(path, normalizeApiRuntimeUrl(apiRuntimeUrl)), {
    method: "POST",
    headers: { "x-open-cut-ui-session": uiSession },
    signal,
  });
  const body = await response.arrayBuffer();
  if (!response.ok) {
    return new Response(body, {
      status: response.status,
      headers: {
        "cache-control": "no-store",
        "content-type": response.headers.get("content-type") ?? "application/json; charset=utf-8",
      },
    });
  }
  try {
    const value = JSON.parse(new TextDecoder().decode(body)) as Record<string, unknown>;
    if (
      value.schema !== "open-cut/sequence-export-delivery-lease/v1" ||
      value.artifactId !== artifactId ||
      value.mimeType !== "video/webm" ||
      typeof value.byteLength !== "string" ||
      !/^[1-9][0-9]*$/.test(value.byteLength) ||
      typeof value.contentSha256 !== "string" ||
      !digestPattern.test(value.contentSha256) ||
      typeof value.contentUrl !== "string" ||
      !/^\/v1\/internal\/platform\/export-content\/oc_export_[A-Za-z0-9_-]+$/.test(value.contentUrl)
    )
      throw new Error("Export delivery lease is invalid");
    return {
      contentUrl: value.contentUrl,
      byteLength: BigInt(value.byteLength),
      contentSha256: value.contentSha256,
    };
  } catch (error) {
    return platformError(502, "OC_EXPORT_LEASE_INVALID", errorMessage(error));
  }
}

async function copyDelivery(
  apiRuntimeUrl: string,
  uiSession: string,
  lease: DeliveryLease,
  destinationPath: string,
  overwrite: boolean,
  targetIdentity: string | undefined,
  signal: AbortSignal,
): Promise<string> {
  const response = await fetch(new URL(lease.contentUrl.slice(1), normalizeApiRuntimeUrl(apiRuntimeUrl)), {
    headers: { "x-open-cut-ui-session": uiSession },
    signal,
  });
  if (!response.ok || !response.body) throw new Error(`Export content returned HTTP ${response.status}`);
  if (response.headers.get("x-open-cut-content-sha256") !== lease.contentSha256) {
    throw new Error("Export content digest binding changed");
  }
  if (response.headers.get("content-length") !== lease.byteLength.toString()) {
    throw new Error("Export content length binding changed");
  }
  const stagePath = join(dirname(destinationPath), `.open-cut-${randomUUID()}.part`);
  const stage = await open(stagePath, "wx", 0o600);
  let staged = true;
  try {
    const digest = createHash("sha256");
    const reader = response.body.getReader();
    let written = 0n;
    try {
      while (true) {
        const chunk = await reader.read();
        if (chunk.done) break;
        written += BigInt(chunk.value.byteLength);
        if (written > lease.byteLength) throw new Error("Export content exceeded its declared length");
        digest.update(chunk.value);
        await stage.write(chunk.value);
      }
    } finally {
      reader.releaseLock();
    }
    if (written !== lease.byteLength) throw new Error("Export content ended before its declared length");
    if (`sha256:${digest.digest("hex")}` !== lease.contentSha256) throw new Error("Export content digest mismatch");
    await stage.sync();
    await stage.close();
    if (overwrite) {
      const target = await existingTarget(destinationPath);
      if (target.state !== "file" || target.identity !== targetIdentity) {
        throw new Error("Export destination changed after approval");
      }
      await rename(stagePath, destinationPath);
    } else {
      await link(stagePath, destinationPath);
      await unlink(stagePath);
    }
    staged = false;
    await syncParent(destinationPath);
    const identity = await regularFileIdentity(destinationPath);
    if (!identity) throw new Error("Published export is not a regular file");
    return identity;
  } finally {
    await stage.close().catch(() => undefined);
    if (staged) await unlink(stagePath).catch(() => undefined);
  }
}

async function existingTarget(
  path: string,
): Promise<Readonly<{ state: "missing" | "invalid" }> | Readonly<{ state: "file"; identity: string }>> {
  try {
    const info = await lstat(path);
    return info.isFile() && !info.isSymbolicLink()
      ? {
          state: "file",
          identity: `${info.dev}:${info.ino}:${info.size}:${info.mtimeMs}:${info.birthtimeMs}`,
        }
      : { state: "invalid" };
  } catch (error) {
    return { state: isNotFound(error) ? "missing" : "invalid" };
  }
}

async function syncParent(path: string): Promise<void> {
  if (process.platform === "win32") return;
  const directory = await open(dirname(path), "r");
  try {
    await directory.sync();
  } finally {
    await directory.close();
  }
}

function validSuggestedName(value: string): boolean {
  return (
    value.length > 5 &&
    value.length <= 128 &&
    value === basename(value) &&
    value.endsWith(".webm") &&
    !/[\0\r\n]/.test(value)
  );
}

function platformError(status: number, code: string, message: string): Response {
  return new Response(JSON.stringify({ error: code, message }), { status, headers: responseHeaders() });
}

function responseHeaders(): Record<string, string> {
  return { "cache-control": "no-store", "content-type": "application/json; charset=utf-8" };
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

function isNotFound(error: unknown): boolean {
  return Boolean(error && typeof error === "object" && "code" in error && error.code === "ENOENT");
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
