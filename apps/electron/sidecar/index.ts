import { SidecarConnection, controlCommand, type SessionStatus } from "@open-cut/sidecar-client";
import { app } from "electron";
import { startElectronApp, type ElectronApp } from "../src/main/app.js";
import { registerOcWebProtocol, type OcWebProtocol } from "../src/main/oc-protocol-electron.js";

let electron: ElectronApp | undefined;
let headlessWeb: OcWebProtocol | undefined;
let sidecar: SidecarConnection | undefined;
let unsubscribe: (() => void) | undefined;
let stopping: Promise<void> | undefined;
let webLease: string | undefined;
let reconciliation = Promise.resolve();
const headless = process.env.OC_DELIVERY_HEADLESS === "1";

function stop(code = 0, requestCellShutdown = false): Promise<void> {
  stopping ??= (async () => {
    unsubscribe?.();
    if (requestCellShutdown) {
      try {
        await sidecar?.shutdownCell();
      } catch {
        // The cell may already be stopping; local cleanup still has to converge.
      }
    }
    sidecar?.close(code);
    headlessWeb?.close();
    electron?.close();
    if (!electron) app.quit();
  })();
  return stopping;
}

async function reconcileWeb(web: SessionStatus | undefined): Promise<void> {
  if (stopping) return;
  const endpoint = web?.ready ? web.endpoints?.find((candidate) => candidate.name === "http")?.url : undefined;
  if (!web?.ready || !endpoint) {
    if (webLease === undefined) return;
    webLease = undefined;
    sidecar?.notReady();
    headlessWeb?.setWebRuntime(undefined);
    await electron?.unavailable();
    return;
  }

  const lease = `${web.instanceId}\n${endpoint}`;
  if (lease === webLease) return;
  webLease = undefined;
  sidecar?.notReady();
  if (electron) {
    await electron.activateWeb(endpoint);
  } else if (headlessWeb) {
    headlessWeb.setWebRuntime(endpoint);
    await headlessWeb.verifyEntry();
  }
  if (stopping) return;
  webLease = lease;
  sidecar?.ready();
}

async function main(): Promise<void> {
  sidecar = await SidecarConnection.connect({
    app: "electron",
    onCommand: async (command) => {
      if (command === controlCommand.show) electron?.show();
      if (command === controlCommand.shutdown) await stop();
    },
  });

  if (headless) {
    await app.whenReady();
    headlessWeb = registerOcWebProtocol();
  } else {
    electron = await startElectronApp();
  }
  unsubscribe = sidecar.watchApp("web", (web) => {
    reconciliation = reconciliation
      .then(() => reconcileWeb(web))
      .catch(async (error: unknown) => {
        webLease = undefined;
        sidecar?.notReady();
        headlessWeb?.setWebRuntime(undefined);
        console.error(error instanceof Error ? error.stack ?? error.message : String(error));
        await electron?.unavailable();
      });
  });
}

app.on("before-quit", (event) => {
  if (stopping) return;
  event.preventDefault();
  void stop(0, true);
});

// Electron emits `ready` only after the main entry finishes its first event-loop
// turn. Top-level-awaiting main() would deadlock startElectronApp's whenReady().
void main().catch(async (error: unknown) => {
  console.error(error instanceof Error ? error.stack ?? error.message : String(error));
  try {
    await stop(1);
  } finally {
    app.exit(1);
  }
});
