import { spawn, type ChildProcess } from "node:child_process";
import { dirname, resolve } from "node:path";
import {
  getBrokerStatus,
  sidecarLaunchEnv,
  type SidecarConnection,
  type SidecarLaunch,
  type SidecarStatus,
} from "@open-cut/sidecar-client";
import { loadPayloadTopology, resolvePayloadEntry } from "./topology.js";

export type PayloadChildren = {
  sessions: SidecarStatus[];
  stop(): Promise<void>;
};

export async function startPayloadChildren(
  resourcesRoot: string,
  runtime: SidecarConnection,
  runtimeLaunch: SidecarLaunch,
): Promise<PayloadChildren> {
  const topology = await loadPayloadTopology(resourcesRoot);
  const children: ChildProcess[] = [];
  try {
    for (const definition of topology.sidecars) {
      const childLaunch = await runtime.delegateSidecar(definition.app);
      const entry = resolvePayloadEntry(resourcesRoot, definition.entry);
      const child = spawn(process.execPath, [entry], {
        cwd: dirname(dirname(dirname(entry))),
        env: {
          ...process.env,
          ...sidecarLaunchEnv(childLaunch),
          ELECTRON_RUN_AS_NODE: "1",
        },
        stdio: ["ignore", "inherit", "inherit"],
      });
      children.push(child);
    }
    const sessions = await waitForReady(runtimeLaunch, topology.sidecars.map((sidecar) => sidecar.app), children, 30_000);
    return { sessions, stop: () => stopChildren(children) };
  } catch (error) {
    await stopChildren(children);
    throw error;
  }
}

async function waitForReady(
  launch: SidecarLaunch,
  apps: string[],
  children: ChildProcess[],
  timeoutMs: number,
): Promise<SidecarStatus[]> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    for (const child of children) {
      if (child.exitCode !== null) throw new Error(`payload child exited before READY with code ${child.exitCode}`);
    }
    const status = await getBrokerStatus(launch);
    const sessions = apps.map((app) => status.sessions.find((session) => session.subject === app));
    if (sessions.every((session) => session?.ready)) return sessions as SidecarStatus[];
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`payload children did not reach READY within ${timeoutMs}ms`);
}

async function stopChildren(children: ChildProcess[]): Promise<void> {
  for (const child of children) {
    if (child.exitCode === null) child.kill("SIGTERM");
  }
  await Promise.all(children.map((child) => new Promise<void>((done) => {
    if (child.exitCode !== null) return done();
    const timeout = setTimeout(() => {
      if (child.exitCode === null) child.kill("SIGKILL");
      done();
    }, 3_000);
    child.once("exit", () => {
      clearTimeout(timeout);
      done();
    });
  })));
}

export function webEndpoint(sessions: SidecarStatus[]): string {
  const endpoint = sessions.find((session) => session.app === "web")?.endpoints?.find((candidate) => candidate.name === "http");
  if (!endpoint) throw new Error("web sidecar did not publish an HTTP endpoint");
  return endpoint.url;
}
