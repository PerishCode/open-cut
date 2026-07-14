import { runtimePeer } from "@open-cut/contracts";
import { controlCommand, SidecarConnection } from "@open-cut/sidecar-client";
import { type ApiServer, startApiServer } from "../src/server.js";

let api: ApiServer | undefined;
let sidecar: SidecarConnection | undefined;
let stopping: Promise<void> | undefined;

function stop(code = 0): Promise<void> {
  stopping ??= (async () => {
    await api?.close();
    sidecar?.close(code);
  })();
  return stopping;
}

sidecar = await SidecarConnection.connect({
  onCommand: async (command) => {
    if (command === controlCommand.shutdown) await stop();
  },
});
api = await startApiServer();
sidecar.publishEndpoint(runtimePeer.api.httpEndpoint, api.url);
sidecar.ready();

process.once("SIGINT", () => void stop());
process.once("SIGTERM", () => void stop());
