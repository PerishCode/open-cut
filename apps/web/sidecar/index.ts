import { SidecarConnection, controlCommand } from "@open-cut/sidecar-client";
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
  app: "web",
  onCommand: async (command) => {
    if (command === controlCommand.shutdown) await stop();
  },
});
web = await startWebServer(sidecar.mode);
sidecar.publishEndpoint("http", web.url);
sidecar.ready();

process.once("SIGINT", () => void stop());
process.once("SIGTERM", () => void stop());
