import { SidecarConnection, controlCommand } from "@open-cut/sidecar-client";
import { runtimePeer } from "@open-cut/contracts";
import { startWebServer, type WebServer } from "./server.js";

let web: WebServer | undefined;
let sidecar: SidecarConnection | undefined;
let stopping: Promise<void> | undefined;

function stop(code = 0): Promise<void> {
  stopping ??= (async () => {
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
sidecar.publishEndpoint(runtimePeer.web.httpEndpoint, web.url);
sidecar.ready();

process.once("SIGINT", () => void stop());
process.once("SIGTERM", () => void stop());
