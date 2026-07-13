import { SidecarConnection } from "@open-cut/sidecar-client";
import { startApiServer, type ApiServer } from "../src/server.js";

let api: ApiServer | undefined;
let sidecar: SidecarConnection | undefined;

async function stop(code = 0): Promise<void> {
  await api?.close();
  sidecar?.close(code);
}

sidecar = await SidecarConnection.connect({
  app: "api",
  onCommand: async (command) => {
    if (command === "shutdown") await stop();
  },
});
api = await startApiServer();
sidecar.publishEndpoint("http", api.url);
sidecar.ready();

process.once("SIGINT", () => void stop());
process.once("SIGTERM", () => void stop());
