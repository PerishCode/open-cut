import { randomBytes } from "node:crypto";
import { lstat } from "node:fs/promises";

const maximumRequestBytes = 4 << 10;
const deliveryReceiptTTL = 30 * 60_000;
const deliveryReceiptLimit = 32;
const deliveryReceiptPattern = /^delivery\.[A-Za-z0-9_-]{32}$/;

type DeliveryReceipt = Readonly<{
  uiSession: string;
  destinationPath: string;
  displayName: string;
  targetIdentity: string;
  expiresAt: number;
}>;

export class DeliveryReceiptStore {
  readonly #receipts = new Map<string, DeliveryReceipt>();

  create(value: Omit<DeliveryReceipt, "expiresAt">, now = Date.now()): string {
    this.#cleanup(now);
    while (this.#receipts.size >= deliveryReceiptLimit) {
      const oldest = this.#receipts.keys().next().value;
      if (typeof oldest !== "string") break;
      this.#receipts.delete(oldest);
    }
    const token = `delivery.${randomBytes(24).toString("base64url")}`;
    this.#receipts.set(token, { ...value, expiresAt: now + deliveryReceiptTTL });
    return token;
  }

  resolve(token: string, uiSession: string, now = Date.now()): DeliveryReceipt | undefined {
    this.#cleanup(now);
    const receipt = this.#receipts.get(token);
    if (!receipt || receipt.uiSession !== uiSession) return undefined;
    return receipt;
  }

  revoke(token: string): void {
    this.#receipts.delete(token);
  }

  clear(): void {
    this.#receipts.clear();
  }

  #cleanup(now: number): void {
    for (const [token, receipt] of this.#receipts) {
      if (now >= receipt.expiresAt) this.#receipts.delete(token);
    }
  }
}

export async function handleExportRevealRequest(
  request: Request,
  uiSession: string | undefined,
  receipts: DeliveryReceiptStore,
  revealItem: (path: string) => void,
): Promise<Response> {
  if (!uiSession) return platformError(503, "OC_EXPORT_AUTHORITY_UNAVAILABLE", "Export reveal authority is not ready");
  const token = await readReceiptToken(request);
  if (!token) return platformError(422, "OC_EXPORT_REVEAL_INVALID", "Export reveal request is invalid");
  const receipt = receipts.resolve(token, uiSession);
  if (!receipt) return platformError(410, "OC_EXPORT_RECEIPT_EXPIRED", "Export delivery receipt expired");
  const identity = await regularFileIdentity(receipt.destinationPath);
  if (!identity || identity !== receipt.targetIdentity) {
    receipts.revoke(token);
    return platformError(409, "OC_EXPORT_DESTINATION_CHANGED", "Saved export changed after delivery");
  }
  try {
    revealItem(receipt.destinationPath);
  } catch (error) {
    return platformError(502, "OC_EXPORT_REVEAL_FAILED", errorMessage(error));
  }
  return new Response(JSON.stringify({ status: "revealed", displayName: receipt.displayName }), {
    status: 200,
    headers: responseHeaders(),
  });
}

export async function regularFileIdentity(path: string): Promise<string | undefined> {
  try {
    const info = await lstat(path);
    if (!info.isFile() || info.isSymbolicLink()) return undefined;
    return `${info.dev}:${info.ino}:${info.size}:${info.mtimeMs}:${info.birthtimeMs}`;
  } catch {
    return undefined;
  }
}

async function readReceiptToken(request: Request): Promise<string | undefined> {
  const body = await request.arrayBuffer();
  if (body.byteLength === 0 || body.byteLength > maximumRequestBytes) return undefined;
  try {
    const value = JSON.parse(new TextDecoder().decode(body)) as unknown;
    if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
    const record = value as Record<string, unknown>;
    if (Object.keys(record).length !== 1 || typeof record.deliveryReceipt !== "string") return undefined;
    return deliveryReceiptPattern.test(record.deliveryReceipt) ? record.deliveryReceipt : undefined;
  } catch {
    return undefined;
  }
}

function platformError(status: number, code: string, message: string): Response {
  return new Response(JSON.stringify({ error: code, message }), { status, headers: responseHeaders() });
}

function responseHeaders(): Record<string, string> {
  return { "cache-control": "no-store", "content-type": "application/json; charset=utf-8" };
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
