import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { renderArtifacts, replaceCommittedArtifacts } from "./lib.ts";

const args = process.argv.slice(2).filter((argument) => argument !== "--");
const baseUrlIndex = args.indexOf("--base-url");
const baseUrl = args[baseUrlIndex + 1];
if (baseUrlIndex < 0 || !baseUrl || args.length !== 2) {
  throw new Error("usage: pnpm generate -- --base-url <url>");
}

const temporary = await mkdtemp(join(tmpdir(), "open-cut-openapi-"));
try {
  await renderArtifacts(temporary, baseUrl);
  await replaceCommittedArtifacts(temporary);
} finally {
  await rm(temporary, { recursive: true, force: true });
}
