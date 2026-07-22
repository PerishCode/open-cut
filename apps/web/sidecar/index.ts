import { runtimePeer } from "@open-cut/contracts/runtime-peer";
import { controlCommand, lifecycleMode, type SessionStatus, SidecarConnection } from "@open-cut/sidecar-client";
import { startWebServer, type WebServer } from "./server.js";
import { bootstrapDevelopmentUISession } from "./ui-session.js";

let web: WebServer | undefined;
let sidecar: SidecarConnection | undefined;
let unsubscribe: (() => void) | undefined;
let renewal: NodeJS.Timeout | undefined;
let stopping: Promise<void> | undefined;
let apiLease: string | undefined;
let browserBinding: string | undefined;
let reconciliation = Promise.resolve();

function stop(code = 0): Promise<void> {
  stopping ??= (async () => {
    unsubscribe?.();
    if (renewal) clearTimeout(renewal);
    await web?.close();
    sidecar?.close(code);
  })();
  return stopping;
}

async function reconcileAPI(api: SessionStatus | undefined, force = false): Promise<void> {
  if (stopping || !sidecar || !web) return;
  const endpoint = api?.ready
    ? api.endpoints?.find((candidate) => candidate.name === runtimePeer.api.httpEndpoint)?.url
    : undefined;
  const lease = api?.ready && endpoint ? `${api.instanceId}\n${endpoint}` : undefined;
  if (!force && lease === apiLease) return;
  // Preserve the browser binding and peer READY state while only the hidden
  // upstream API credential rotates inside the same exact API lease.
  const renewing = force && lease !== undefined && lease === apiLease;
  if (renewal) clearTimeout(renewal);
  renewal = undefined;
  if (!renewing) {
    apiLease = undefined;
    browserBinding = undefined;
    sidecar.notReady();
    web.setApiRuntime(undefined);
    web.setUISession(undefined);
  }
  if (!endpoint || !lease) return;

  web.setApiRuntime(endpoint);
  if (sidecar.mode === lifecycleMode.dev) {
    const issued = await bootstrapDevelopmentUISession(endpoint, web.url);
    if (stopping) return;
    const session = browserBinding ? { ...issued, browserBinding } : issued;
    web.setUISession(session);
    browserBinding = session.browserBinding;
    const renewAfter = Math.max(1_000, Math.floor((session.expiresAt - Date.now()) / 2));
    renewal = setTimeout(() => {
      reconciliation = reconciliation.catch(() => undefined).then(() => reconcileAPI(api, true));
    }, renewAfter);
    renewal.unref();
  }
  apiLease = lease;
  if (!renewing) sidecar.ready();
}

sidecar = await SidecarConnection.connect({
  onCommand: async (command) => {
    if (command === controlCommand.shutdown) await stop();
  },
  onAbandoned: () => {
    console.error("control broker stayed unreachable beyond the reconnect window; failing closed");
    void stop(1);
  },
});
web = await startWebServer(sidecar.mode);
sidecar.publishEndpoint(runtimePeer.web.httpEndpoint, web.url);
unsubscribe = sidecar.watchApp(runtimePeer.api.app, (api) => {
  reconciliation = reconciliation
    .then(() => reconcileAPI(api))
    .catch((error: unknown) => {
      apiLease = undefined;
      browserBinding = undefined;
      sidecar?.notReady();
      web?.setApiRuntime(undefined);
      web?.setUISession(undefined);
      console.error(error instanceof Error ? (error.stack ?? error.message) : String(error));
    });
});

process.once("SIGINT", () => void stop());
process.once("SIGTERM", () => void stop());
