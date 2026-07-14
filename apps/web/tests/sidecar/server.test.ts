import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { resolveWebRoot } from "../../sidecar/server.js";

describe("Web sidecar topology directory", () => {
  it("accepts only the topology-controlled Web package root", () => {
    const root = mkdtempSync(join(tmpdir(), "open-cut-web-"));
    writeFileSync(join(root, "package.json"), JSON.stringify({ name: "@open-cut/web" }));
    expect(resolveWebRoot(root)).toBe(root);
  });

  it("does not search parent directories for a matching package", () => {
    const root = mkdtempSync(join(tmpdir(), "open-cut-other-"));
    writeFileSync(join(root, "package.json"), JSON.stringify({ name: "other" }));
    expect(() => resolveWebRoot(root)).toThrow(/not @open-cut\/web/);
  });
});
