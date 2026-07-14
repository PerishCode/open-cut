import { execFile } from "node:child_process";
import { cp, mkdir, readdir, readFile, rm, stat, writeFile } from "node:fs/promises";
import { dirname, join, relative, resolve } from "node:path";
import { promisify } from "node:util";

import { generate, type Options } from "orval";

const execute = promisify(execFile);

export const packageRoot = resolve(import.meta.dirname, "..");
export const repositoryRoot = resolve(packageRoot, "../..");

export type GenerationManifest = {
  schema: 1;
  baseUrl: string;
  generator: "orval@8.21.0";
  openapi: "3.1.0";
};

export async function renderArtifacts(destination: string, baseUrl: string): Promise<void> {
  validateBaseUrl(baseUrl);
  const specRoot = join(destination, "spec");
  const generatedRoot = join(destination, "src", "generated");
  await mkdir(specRoot, { recursive: true });
  await mkdir(generatedRoot, { recursive: true });

  const { stdout, stderr } = await execute("go", ["run", "./apps/api/sidecar", "openapi"], {
    cwd: repositoryRoot,
    encoding: "utf8",
    maxBuffer: 16 * 1024 * 1024,
  });
  if (stderr.trim()) throw new Error(`API OpenAPI export wrote to stderr: ${stderr.trim()}`);
  const document = JSON.parse(stdout) as { openapi?: unknown };
  if (document.openapi !== "3.1.0")
    throw new Error(`API exported unsupported OpenAPI version ${String(document.openapi)}`);
  const specPath = join(specRoot, "openapi.json");
  await writeFile(specPath, `${JSON.stringify(document)}\n`, "utf8");

  const options: Options = {
    input: { target: specPath },
    output: {
      target: generatedRoot,
      schemas: join(generatedRoot, "model"),
      mode: "tags-split",
      client: "fetch",
      baseUrl,
      clean: true,
      indexFiles: true,
      override: { header: false },
    },
  };
  await generate(options, packageRoot);
  await normalizeGeneratedSources(generatedRoot);

  const manifest: GenerationManifest = {
    schema: 1,
    baseUrl,
    generator: "orval@8.21.0",
    openapi: "3.1.0",
  };
  await writeFile(join(destination, "generation.json"), `${JSON.stringify(manifest, null, 2)}\n`, "utf8");
}

async function normalizeGeneratedSources(root: string): Promise<void> {
  for (const filename of await files(root)) {
    if (!filename.endsWith(".ts")) continue;
    const path = join(root, filename);
    const source = await readFile(path, "utf8");
    await writeFile(path, `${source.trimEnd()}\n`, "utf8");
  }
}

export async function replaceCommittedArtifacts(rendered: string): Promise<void> {
  const generated = join(packageRoot, "src", "generated");
  const spec = join(packageRoot, "spec");
  await rm(generated, { recursive: true, force: true });
  await rm(spec, { recursive: true, force: true });
  await mkdir(dirname(generated), { recursive: true });
  await cp(join(rendered, "src", "generated"), generated, { recursive: true });
  await cp(join(rendered, "spec"), spec, { recursive: true });
  await cp(join(rendered, "generation.json"), join(packageRoot, "generation.json"));
}

export async function assertCommittedArtifacts(rendered: string): Promise<void> {
  for (const path of ["generation.json", "spec", "src/generated"]) {
    const expected = join(rendered, path);
    const actual = join(packageRoot, path);
    const difference = await firstDifference(expected, actual);
    if (difference) throw new Error(`OpenAPI artifacts are stale at ${path}${difference}`);
  }
}

export async function loadManifest(): Promise<GenerationManifest> {
  const value = JSON.parse(await readFile(join(packageRoot, "generation.json"), "utf8")) as GenerationManifest;
  if (value.schema !== 1 || value.generator !== "orval@8.21.0" || value.openapi !== "3.1.0") {
    throw new Error("generation.json does not describe the supported OpenAPI generator");
  }
  validateBaseUrl(value.baseUrl);
  return value;
}

function validateBaseUrl(baseUrl: string): void {
  if (baseUrl.trim() !== baseUrl || baseUrl === "" || /[?#]$/.test(baseUrl)) {
    throw new Error("--base-url must be a non-empty URL prefix without trailing query or fragment delimiters");
  }
  if (!baseUrl.startsWith("/") && !/^https?:\/\//.test(baseUrl)) {
    throw new Error("--base-url must be an absolute HTTP(S) URL or a root-relative path");
  }
  if (baseUrl !== "/" && baseUrl.endsWith("/")) throw new Error("--base-url must not have a trailing slash");
}

async function firstDifference(expected: string, actual: string): Promise<string | undefined> {
  const [expectedInfo, actualInfo] = await Promise.all([stat(expected), stat(actual).catch(() => undefined)]);
  if (
    !actualInfo ||
    expectedInfo.isFile() !== actualInfo.isFile() ||
    expectedInfo.isDirectory() !== actualInfo.isDirectory()
  ) {
    return " (artifact type differs)";
  }
  if (expectedInfo.isFile()) {
    const [left, right] = await Promise.all([readFile(expected), readFile(actual)]);
    return left.equals(right) ? undefined : "";
  }
  const expectedFiles = await files(expected);
  const actualFiles = await files(actual);
  if (expectedFiles.join("\n") !== actualFiles.join("\n")) return " (file set differs)";
  for (const filename of expectedFiles) {
    const [left, right] = await Promise.all([readFile(join(expected, filename)), readFile(join(actual, filename))]);
    if (!left.equals(right)) return `/${filename}`;
  }
  return undefined;
}

async function files(root: string): Promise<string[]> {
  const result: string[] = [];
  const visit = async (directory: string): Promise<void> => {
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) await visit(path);
      else if (entry.isFile()) result.push(relative(root, path));
      else throw new Error(`generated artifact is not a regular file: ${path}`);
    }
  };
  const info = await readdir(root, { withFileTypes: true }).catch((error: unknown) => {
    throw new Error(`generated artifact root is unavailable: ${root}`, { cause: error });
  });
  if (info.length === 0) return [];
  await visit(root);
  return result.sort();
}
