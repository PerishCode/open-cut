import { runtimePeer } from "@open-cut/contracts";
import { controlCommand, SidecarConnection } from "@open-cut/sidecar-client";
import { startWebServer, type WebServer } from "./server.js";

let web: WebServer | undefined;
let sidecar: SidecarConnection | undefined;
let unsubscribe: (() => void) | undefined;
let stopping: Promise<void> | undefined;

function stop(code = 0): Promise<void> {
  stopping ??= (async () => {
    unsubscribe?.();
    await web?.close();
    sidecar?.close(code);
  })();
  return stopping;
}

sidecar = await SidecarConnection.connect({
  onCommand: async (command) => {
    if (command === controlCommand.shutdown) await stop();
  },
});
web = await startWebServer(sidecar.mode);
unsubscribe = sidecar.watchApp(runtimePeer.api.app, (api) => {
  const endpoint = api?.ready
    ? api.endpoints?.find((candidate) => candidate.name === runtimePeer.api.httpEndpoint)?.url
    : undefined;
  try {
    web?.setApiRuntime(endpoint);
  } catch (error) {
    web?.setApiRuntime(undefined);
    console.error(error instanceof Error ? (error.stack ?? error.message) : String(error));
  }
});
sidecar.publishEndpoint(runtimePeer.web.httpEndpoint, web.url);
sidecar.ready();

process.once("SIGINT", () => void stop());
process.once("SIGTERM", () => void stop());
