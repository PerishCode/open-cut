import assert from "node:assert/strict";

import type { BrowserWindow } from "electron";
import { afterEach, describe, it, vi } from "vitest";

const showOpenDialog = vi.hoisted(() => vi.fn());

vi.mock("electron", () => ({ dialog: { showOpenDialog } }));

import { handleSourcePickerRequest } from "../src/main/source-picker.js";

afterEach(() => {
  showOpenDialog.mockReset();
  vi.unstubAllGlobals();
});

describe("source picker request", () => {
  it("consumes a dropped token and exposes the path only to the internal SourceGrant call", async () => {
    const token = `drop.${"A".repeat(32)}`;
    const sourcePath = "/private/creator/fixture.mov";
    let internalBody: Record<string, unknown> | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        internalBody = JSON.parse(String(init?.body)) as Record<string, unknown>;
        return new Response('{"grant":{"id":"opaque"}}', {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      }),
    );
    const consume = vi.fn(() => sourcePath);

    const response = await handleSourcePickerRequest(
      new Request("oc://app/_open-cut/platform/source-grants", {
        method: "POST",
        body: JSON.stringify({ requestId: "ui:source-grant:1", sourceToken: token }),
      }),
      {} as BrowserWindow,
      "http://127.0.0.1:43100/",
      "oc_ui_hidden",
      consume,
    );

    assert.equal(response.status, 200);
    assert.deepEqual(internalBody, { requestId: "ui:source-grant:1", path: sourcePath });
    assert.deepEqual(consume.mock.calls, [[token]]);
    assert.equal(showOpenDialog.mock.calls.length, 0);
  });

  it("returns a typed expiry without opening a picker or calling the API", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const response = await handleSourcePickerRequest(
      new Request("oc://app/_open-cut/platform/source-grants", {
        method: "POST",
        body: JSON.stringify({ requestId: "ui:source-grant:2", sourceToken: `drop.${"B".repeat(32)}` }),
      }),
      {} as BrowserWindow,
      "http://127.0.0.1:43100/",
      "oc_ui_hidden",
      () => undefined,
    );

    assert.equal(response.status, 410);
    assert.equal((await response.json()).error, "OC_DROPPED_SOURCE_EXPIRED");
    assert.equal(fetchMock.mock.calls.length, 0);
    assert.equal(showOpenDialog.mock.calls.length, 0);
  });

  it("keeps the trusted picker as the default and cancellation as an empty result", async () => {
    showOpenDialog.mockResolvedValue({ canceled: true, filePaths: [] });
    const response = await handleSourcePickerRequest(
      new Request("oc://app/_open-cut/platform/source-grants", {
        method: "POST",
        body: JSON.stringify({ requestId: "ui:source-grant:3" }),
      }),
      {} as BrowserWindow,
      "http://127.0.0.1:43100/",
      "oc_ui_hidden",
    );
    assert.equal(response.status, 204);
    assert.equal(showOpenDialog.mock.calls.length, 1);
  });
});
