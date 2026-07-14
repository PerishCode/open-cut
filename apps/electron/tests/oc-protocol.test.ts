import assert from "node:assert/strict";
import { describe, it } from "vitest";

import {
  handleOcWebRequest,
  normalizeWebRuntimeUrl,
  OC_WEB_ENTRY_URL,
  toWebRuntimeUrl,
} from "../src/main/oc-protocol.js";

describe("oc:// Web protocol", () => {
  it("keeps one stable renderer entry URL", () => {
    assert.equal(OC_WEB_ENTRY_URL, "oc://app/");
  });

  it("rewrites path and query onto the current loopback Web runtime", () => {
    assert.equal(
      toWebRuntimeUrl("http://127.0.0.1:41731/", "oc://app/projects/one?view=timeline#ignored"),
      "http://127.0.0.1:41731/projects/one?view=timeline",
    );
  });

  it("accepts only explicit loopback HTTP origins", () => {
    assert.equal(normalizeWebRuntimeUrl("http://127.0.0.1:41731"), "http://127.0.0.1:41731/");
    assert.equal(normalizeWebRuntimeUrl("http://[::1]:41731/"), "http://[::1]:41731/");
    for (const endpoint of [
      "https://127.0.0.1:41731/",
      "http://example.com:41731/",
      "http://127.0.0.1/",
      "http://user:secret@127.0.0.1:41731/",
      "http://127.0.0.1:41731/nested",
    ]) {
      assert.throws(() => normalizeWebRuntimeUrl(endpoint));
    }
  });

  it("proxies the original method and body without exposing the HTTP URL as navigation", async () => {
    let captured: Request | undefined;
    const response = await handleOcWebRequest(
      new Request("oc://app/api/render?draft=1", { method: "POST", body: "clip" }),
      "http://127.0.0.1:41731/",
      async (request) => {
        captured = request;
        return new Response("ok");
      },
    );

    assert.equal(response.status, 200);
    assert.equal(captured?.url, "http://127.0.0.1:41731/api/render?draft=1");
    assert.equal(captured?.method, "POST");
    assert.equal(await captured?.text(), "clip");
  });

  it("rejects other oc:// hosts without contacting the Web runtime", async () => {
    let called = false;
    const response = await handleOcWebRequest(
      new Request("oc://elsewhere/private"),
      "http://127.0.0.1:41731/",
      async () => {
        called = true;
        return new Response();
      },
    );

    assert.equal(response.status, 404);
    assert.equal(called, false);
  });

  it("returns a normal 503 response while no Web lease is active", async () => {
    const response = await handleOcWebRequest(
      new Request(OC_WEB_ENTRY_URL),
      undefined,
      async () => new Response(),
    );
    assert.equal(response.status, 503);
    assert.equal((await response.json()).error, "OC_WEB_RUNTIME_UNAVAILABLE");
  });

  it("absorbs one transient GET failure before returning the Web response", async () => {
    let calls = 0;
    const response = await handleOcWebRequest(
      new Request(OC_WEB_ENTRY_URL),
      "http://127.0.0.1:41731/",
      async () => {
        calls += 1;
        if (calls === 1) throw new Error("socket reset");
        return new Response("ready");
      },
      { delay: async () => undefined },
    );

    assert.equal(calls, 2);
    assert.equal(await response.text(), "ready");
  });

  it("turns a non-idempotent proxy failure into a 502 response", async () => {
    const response = await handleOcWebRequest(
      new Request("oc://app/api/render", { method: "POST" }),
      "http://127.0.0.1:41731/",
      async () => {
        throw new Error("connection refused");
      },
    );

    assert.equal(response.status, 502);
    assert.equal((await response.json()).error, "OC_WEB_PROTOCOL_PROXY_FAILED");
  });
});
