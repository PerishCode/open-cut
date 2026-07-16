import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp, readdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import type { BrowserWindow } from "electron";
import { afterEach, describe, it, vi } from "vitest";

const showSaveDialog = vi.hoisted(() => vi.fn());

vi.mock("electron", () => ({ dialog: { showSaveDialog } }));

import { DeliveryReceiptStore } from "../src/main/export-reveal.js";
import { DestinationGrantStore, handleExportSaveRequest } from "../src/main/export-save.js";

const projectId = "018f0a60-7b80-7a01-8000-000000000101";
const artifactId = "018f0a60-7b80-7a01-8000-000000000102";

afterEach(() => {
  showSaveDialog.mockReset();
  vi.unstubAllGlobals();
});

describe("Export Save As", () => {
  it("streams a lease to a same-directory stage and publishes a digest-verified new file", async () => {
    const root = await mkdtemp(join(tmpdir(), "open-cut-export-save-"));
    try {
      const destination = join(root, "story.webm");
      const content = new TextEncoder().encode("verified-export-bytes");
      const digest = sha256(content);
      showSaveDialog.mockResolvedValue({ canceled: false, filePath: destination });
      vi.stubGlobal("fetch", deliveryFetch(content, digest));

      const response = await handleExportSaveRequest(
        saveRequest({}),
        {} as BrowserWindow,
        "http://127.0.0.1:43100/",
        "oc_ui_hidden",
        new DestinationGrantStore(),
        new DeliveryReceiptStore(),
      );

      assert.equal(response.status, 200);
      assert.deepEqual(new Uint8Array(await readFile(destination)), content);
      assert.deepEqual(await readdir(root), ["story.webm"]);
      const receipt = (await response.json()) as Record<string, unknown>;
      assert.equal(receipt.contentSha256, digest);
      assert.match(String(receipt.deliveryReceipt), /^delivery\.[A-Za-z0-9_-]{32}$/);
      assert.equal(JSON.stringify(receipt).includes(root), false);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("requires and consumes an opaque overwrite grant while preserving old bytes on digest failure", async () => {
    const root = await mkdtemp(join(tmpdir(), "open-cut-export-overwrite-"));
    try {
      const destination = join(root, "story.webm");
      await writeFile(destination, "old-export");
      showSaveDialog.mockResolvedValue({ canceled: false, filePath: destination });
      const grants = new DestinationGrantStore();
      const selection = await handleExportSaveRequest(
        saveRequest({}),
        {} as BrowserWindow,
        "http://127.0.0.1:43100/",
        "oc_ui_hidden",
        grants,
        new DeliveryReceiptStore(),
      );
      assert.equal(selection.status, 409);
      const conflict = (await selection.json()) as Record<string, unknown>;
      assert.match(String(conflict.destinationGrant), /^destination\.[A-Za-z0-9_-]{32}$/);
      assert.equal(String(await readFile(destination)), "old-export");

      const content = new TextEncoder().encode("new-export");
      const declared = sha256(new TextEncoder().encode("different"));
      vi.stubGlobal("fetch", deliveryFetch(content, declared));
      const failed = await handleExportSaveRequest(
        saveRequest({ destinationGrant: String(conflict.destinationGrant), overwrite: true }),
        {} as BrowserWindow,
        "http://127.0.0.1:43100/",
        "oc_ui_hidden",
        grants,
        new DeliveryReceiptStore(),
      );
      assert.equal(failed.status, 502);
      assert.equal(String(await readFile(destination)), "old-export");
      assert.deepEqual(await readdir(root), ["story.webm"]);

      const replay = await handleExportSaveRequest(
        saveRequest({ destinationGrant: String(conflict.destinationGrant), overwrite: true }),
        {} as BrowserWindow,
        "http://127.0.0.1:43100/",
        "oc_ui_hidden",
        grants,
        new DeliveryReceiptStore(),
      );
      assert.equal(replay.status, 410);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });
});

function saveRequest(extra: Record<string, unknown>): Request {
  return new Request("oc://app/_open-cut/platform/export-save-as", {
    method: "POST",
    body: JSON.stringify({
      projectId,
      artifactId,
      suggestedName: "story.webm",
      ...extra,
    }),
  });
}

function deliveryFetch(content: Uint8Array, digest: string) {
  let call = 0;
  return vi.fn(async () => {
    call += 1;
    if (call === 1) {
      return new Response(
        JSON.stringify({
          schema: "open-cut/sequence-export-delivery-lease/v1",
          artifactId,
          mimeType: "video/webm",
          byteLength: String(content.byteLength),
          contentSha256: digest,
          contentUrl: "/v1/internal/platform/export-content/oc_export_fixture",
          expiresAt: "2026-07-16T00:00:00Z",
        }),
        { status: 200 },
      );
    }
    return new Response(new Uint8Array(content).buffer, {
      status: 200,
      headers: {
        "content-length": String(content.byteLength),
        "content-type": "video/webm",
        "x-open-cut-content-sha256": digest,
      },
    });
  });
}

function sha256(value: Uint8Array): string {
  return `sha256:${createHash("sha256").update(value).digest("hex")}`;
}
