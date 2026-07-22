import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, it } from "vitest";

import { DeliveryReceiptStore, handleExportRevealRequest, regularFileIdentity } from "../src/main/export-reveal.js";

describe("Export reveal", () => {
  it("reuses a session-bound receipt without exposing its absolute path", async () => {
    const root = await mkdtemp(join(tmpdir(), "open-cut-export-reveal-"));
    try {
      const destinationPath = join(root, "story.webm");
      await writeFile(destinationPath, "export");
      const targetIdentity = await regularFileIdentity(destinationPath);
      assert.ok(targetIdentity);
      const receipts = new DeliveryReceiptStore();
      const deliveryReceipt = receipts.create({
        uiSession: "oc_ui_one",
        destinationPath,
        displayName: "story.webm",
        targetIdentity,
      });
      const revealed: string[] = [];

      for (let call = 0; call < 2; call += 1) {
        const response = await handleExportRevealRequest(
          revealRequest(deliveryReceipt),
          "oc_ui_one",
          receipts,
          (path) => revealed.push(path),
        );
        assert.equal(response.status, 200);
        const result = await response.text();
        assert.equal(result.includes(root), false);
        assert.deepEqual(JSON.parse(result), { status: "revealed", displayName: "story.webm" });
      }
      assert.deepEqual(revealed, [destinationPath, destinationPath]);

      const wrongSession = await handleExportRevealRequest(revealRequest(deliveryReceipt), "oc_ui_two", receipts, () =>
        assert.fail("wrong UI session revealed a path"),
      );
      assert.equal(wrongSession.status, 410);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("revokes a receipt when the exact destination identity changes", async () => {
    const root = await mkdtemp(join(tmpdir(), "open-cut-export-reveal-change-"));
    try {
      const destinationPath = join(root, "story.webm");
      await writeFile(destinationPath, "old");
      const targetIdentity = await regularFileIdentity(destinationPath);
      assert.ok(targetIdentity);
      const receipts = new DeliveryReceiptStore();
      const deliveryReceipt = receipts.create({
        uiSession: "oc_ui_one",
        destinationPath,
        displayName: "story.webm",
        targetIdentity,
      });
      await writeFile(destinationPath, "changed-export");

      const changed = await handleExportRevealRequest(revealRequest(deliveryReceipt), "oc_ui_one", receipts, () =>
        assert.fail("changed destination was revealed"),
      );
      assert.equal(changed.status, 409);
      const replay = await handleExportRevealRequest(revealRequest(deliveryReceipt), "oc_ui_one", receipts, () =>
        assert.fail("revoked receipt was revealed"),
      );
      assert.equal(replay.status, 410);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("expires without sliding and evicts the oldest receipt at bounded capacity", () => {
    const receipts = new DeliveryReceiptStore();
    const value = {
      uiSession: "oc_ui_one",
      destinationPath: "/opaque/to-web",
      displayName: "story.webm",
      targetIdentity: "identity",
    };
    const expired = receipts.create(value, 0);
    assert.equal(receipts.resolve(expired, "oc_ui_one", 30 * 60_000), undefined);

    const tokens = Array.from({ length: 33 }, () => receipts.create(value, 1));
    assert.equal(receipts.resolve(tokens[0] ?? "", "oc_ui_one", 2), undefined);
    assert.ok(receipts.resolve(tokens[32] ?? "", "oc_ui_one", 2));
    receipts.clear();
    assert.equal(receipts.resolve(tokens[32] ?? "", "oc_ui_one", 2), undefined);
  });

  it("preserves a receipt across a controlled UI session renewal", () => {
    const receipts = new DeliveryReceiptStore();
    const token = receipts.create({
      uiSession: "oc_ui_old",
      destinationPath: "/opaque/to-web",
      displayName: "story.webm",
      targetIdentity: "identity",
    });

    receipts.rebind("oc_ui_old", "oc_ui_new");

    assert.equal(receipts.resolve(token, "oc_ui_old"), undefined);
    assert.ok(receipts.resolve(token, "oc_ui_new"));
  });
});

function revealRequest(deliveryReceipt: string): Request {
  return new Request("oc://app/_open-cut/platform/export-reveal", {
    method: "POST",
    body: JSON.stringify({ deliveryReceipt }),
  });
}
