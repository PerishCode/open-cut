import { join } from "node:path";
import { SidecarConnection, loadSidecarLaunch } from "@open-cut/sidecar-client";
import { app } from "electron";
import { startElectronApp, type ElectronApp } from "../src/main/app.js";
import { startPayloadChildren, webEndpoint, type PayloadChildren } from "./supervisor.js";

let electron: ElectronApp | undefined;
let sidecar: SidecarConnection | undefined;
let children: PayloadChildren | undefined;

const launch = loadSidecarLaunch();
const resourcesRoot = process.env.OC_PAYLOAD_RESOURCES ?? join(process.resourcesPath, "payload");
const headless = process.env.OC_DELIVERY_HEADLESS === "1";

async function stop(code = 0): Promise<void> {
  await children?.stop();
  sidecar?.close(code);
  if (electron) electron.close(); else app.quit();
}

sidecar = await SidecarConnection.connect({
  app: "electron",
  launch,
  onCommand: async (command) => {
    if (command === "show") electron?.show();
    if (command === "shutdown") await stop();
  },
});
children = await startPayloadChildren(resourcesRoot, sidecar, launch);
if (!headless) electron = await startElectronApp(webEndpoint(children.sessions));
sidecar.ready();

app.once("before-quit", () => {
  void children?.stop();
  sidecar?.close(0);
});
