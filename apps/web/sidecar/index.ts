import { SidecarConnection } from "@open-cut/sidecar-client";
import { startWebServer, type WebServer } from "../src/server.js";

let web: WebServer | undefined;
let sidecar: SidecarConnection | undefined;

async function stop(code = 0): Promise<void> {
  await web?.close();
  sidecar?.close(code);
}

sidecar = await SidecarConnection.connect({
  app: "web",
  onCommand: async (command) => {
    if (command === "shutdown") await stop();
  },
});
web = await startWebServer();
sidecar.publishEndpoint("http", web.url);
sidecar.ready();

process.once("SIGINT", () => void stop());
process.once("SIGTERM", () => void stop());
