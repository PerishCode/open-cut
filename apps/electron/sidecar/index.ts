import { runtimePeer } from "@open-cut/contracts/runtime-peer";
import { controlCommand, presentation, type SessionStatus, SidecarConnection } from "@open-cut/sidecar-client";
import { app } from "electron";
import { type ElectronApp, startElectronApp } from "../src/main/app.js";
import { type OcWebProtocol, registerOcWebProtocol } from "../src/main/oc-protocol-electron.js";
import { configureHarnessCDP } from "./harness-cdp.js";
import { bootstrapUISession } from "./ui-session.js";

const cdpPort = configureHarnessCDP(app.commandLine, process.env);

let electron: ElectronApp | undefined;
let headlessWeb: OcWebProtocol | undefined;
let sidecar: SidecarConnection | undefined;
let unsubscribeWeb: (() => void) | undefined;
let unsubscribeAPI: (() => void) | undefined;
let renewal: NodeJS.Timeout | undefined;
let stopping: Promise<void> | undefined;
let webPeer: SessionStatus | undefined;
let apiPeer: SessionStatus | undefined;
let activeLease: string | undefined;
let reconciliation = Promise.resolve();

function stop(code = 0, requestCellShutdown = false): Promise<void> {
  stopping ??= (async () => {
    unsubscribeWeb?.();
    unsubscribeAPI?.();
    if (renewal) clearTimeout(renewal);
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

function peerEndpoint(peer: SessionStatus | undefined, name: string): string | undefined {
  return peer?.ready ? peer.endpoints?.find((candidate) => candidate.name === name)?.url : undefined;
}

async function clearActiveLease(showUnavailable: boolean): Promise<void> {
  activeLease = undefined;
  if (renewal) clearTimeout(renewal);
  renewal = undefined;
  sidecar?.notReady();
  headlessWeb?.setUISession(undefined);
  headlessWeb?.setWebRuntime(undefined);
  if (showUnavailable) await electron?.unavailable();
}

async function reconcilePeers(force = false): Promise<void> {
  if (stopping || !sidecar) return;
  const webEndpoint = peerEndpoint(webPeer, runtimePeer.web.httpEndpoint);
  const apiEndpoint = peerEndpoint(apiPeer, runtimePeer.api.httpEndpoint);
  const lease =
    webEndpoint && apiEndpoint && webPeer && apiPeer
      ? `${webPeer.instanceId}\n${webEndpoint}\n${apiPeer.instanceId}\n${apiEndpoint}`
      : undefined;
  if (!force && lease === activeLease) return;
  // Rotating the short-lived UI credential inside one exact peer lease is an
  // authority refresh, not a topology transition. Keep renderer state live.
  const renewing = force && lease !== undefined && lease === activeLease;
  if (renewing) {
    if (renewal) clearTimeout(renewal);
    renewal = undefined;
  } else {
    const wasActive = activeLease !== undefined;
    await clearActiveLease(wasActive);
  }
  if (!lease || !webEndpoint || !apiEndpoint) return;

  const session = await bootstrapUISession(apiEndpoint);
  if (stopping) return;
  if (electron) {
    if (renewing) electron.renewUISession(apiEndpoint, session.token);
    else await electron.activateWeb(webEndpoint, apiEndpoint, session.token);
  } else if (headlessWeb) {
    headlessWeb.setWebRuntime(webEndpoint);
    headlessWeb.setUISession(session.token);
    await headlessWeb.verifyEntry();
  }
  if (stopping) return;
  activeLease = lease;
  const renewAfter = Math.max(1_000, Math.floor((session.expiresAt - Date.now()) / 2));
  renewal = setTimeout(() => {
    reconciliation = reconciliation.catch(() => undefined).then(() => reconcilePeers(true));
  }, renewAfter);
  renewal.unref();
  if (!renewing) sidecar.ready();
}

function scheduleReconciliation(): void {
  reconciliation = reconciliation
    .then(() => reconcilePeers())
    .catch(async (error: unknown) => {
      await clearActiveLease(true);
      console.error(error instanceof Error ? (error.stack ?? error.message) : String(error));
    });
}

async function main(): Promise<void> {
  sidecar = await SidecarConnection.connect({
    onCommand: async (command) => {
      if (command === controlCommand.show) electron?.show();
      if (command === controlCommand.shutdown) await stop();
    },
    onAbandoned: () => {
      console.error("control broker stayed unreachable beyond the reconnect window; failing closed");
      void stop(1);
    },
  });
  if (cdpPort !== undefined) {
    sidecar.publishEndpoint(runtimePeer.payload.cdpEndpoint, `http://127.0.0.1:${cdpPort}`);
  }

  if (sidecar.presentation === presentation.headless) {
    await app.whenReady();
    headlessWeb = registerOcWebProtocol();
  } else {
    electron = await startElectronApp();
  }
  unsubscribeWeb = sidecar.watchApp(runtimePeer.web.app, (peer) => {
    webPeer = peer;
    scheduleReconciliation();
  });
  unsubscribeAPI = sidecar.watchApp(runtimePeer.api.app, (peer) => {
    apiPeer = peer;
    scheduleReconciliation();
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
  console.error(error instanceof Error ? (error.stack ?? error.message) : String(error));
  try {
    await stop(1);
  } finally {
    app.exit(1);
  }
});
