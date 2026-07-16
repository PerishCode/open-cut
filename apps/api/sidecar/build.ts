import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";

const packageRoot = resolve(import.meta.dirname, "..");
const sidecar = join(packageRoot, "dist", "sidecar", "api-sidecar.exe");

mkdirSync(dirname(sidecar), { recursive: true });
execFileSync("go", ["build", "-trimpath", "-o", sidecar, "./sidecar"], {
  cwd: packageRoot,
  stdio: "inherit",
});
execFileSync(sidecar, ["media-tools", "build"], {
  cwd: packageRoot,
  stdio: "inherit",
});
