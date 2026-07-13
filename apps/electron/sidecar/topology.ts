import { readFile } from "node:fs/promises";
import { isAbsolute, resolve, sep } from "node:path";

export type PayloadSidecar = {
  app: string;
  entry: string;
};

export type PayloadTopology = {
  schema: 1;
  sidecars: PayloadSidecar[];
};

const APP_PATTERN = /^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$/;

export async function loadPayloadTopology(resourcesRoot: string): Promise<PayloadTopology> {
  const document = JSON.parse(await readFile(resolve(resourcesRoot, "payload-topology.json"), "utf8")) as PayloadTopology;
  if (document.schema !== 1 || !Array.isArray(document.sidecars) || document.sidecars.length === 0) {
    throw new Error("payload topology must contain at least one sidecar");
  }
  const seen = new Set<string>();
  for (const sidecar of document.sidecars) {
    if (!APP_PATTERN.test(sidecar.app) || seen.has(sidecar.app)) throw new Error("payload topology contains an invalid app");
    if (!safeEntry(resourcesRoot, sidecar.entry)) throw new Error(`payload topology contains unsafe entry ${sidecar.entry}`);
    seen.add(sidecar.app);
  }
  return document;
}

export function resolvePayloadEntry(resourcesRoot: string, entry: string): string {
  if (!safeEntry(resourcesRoot, entry)) throw new Error(`unsafe payload entry ${entry}`);
  return resolve(resourcesRoot, entry);
}

function safeEntry(resourcesRoot: string, entry: string): boolean {
  if (!entry || isAbsolute(entry) || entry.includes("\\")) return false;
  const target = resolve(resourcesRoot, entry);
  return target.startsWith(resolve(resourcesRoot) + sep) && !entry.split("/").includes("..");
}
