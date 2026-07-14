import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { assertCommittedArtifacts, loadManifest, renderArtifacts } from "./lib.ts";

const manifest = await loadManifest();
const temporary = await mkdtemp(join(tmpdir(), "open-cut-openapi-check-"));
try {
  await renderArtifacts(temporary, manifest.baseUrl);
  await assertCommittedArtifacts(temporary);
} finally {
  await rm(temporary, { recursive: true, force: true });
}
