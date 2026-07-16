import { ipcMain, type WebContents } from "electron";

import { droppedSourceIPCChannel } from "../platform-channel.js";
import { DroppedSourceStager } from "./dropped-source.js";
import { OC_WEB_HOST, OC_WEB_SCHEME } from "./oc-protocol.js";

export type DroppedSourceIPC = Readonly<{
  consume(owner: WebContents, token: string): string | undefined;
  close(): void;
}>;

export function registerDroppedSourceIPC(stager = new DroppedSourceStager()): DroppedSourceIPC {
  ipcMain.handle(droppedSourceIPCChannel, (event, sourcePath: unknown) => {
    if (!trustedRendererURL(event.senderFrame?.url ?? event.sender.getURL()) || typeof sourcePath !== "string") {
      throw new Error("Dropped source caller is not trusted");
    }
    if (process.platform === "darwin" && process.mas) {
      throw new Error("Dropped sources require the trusted picker in this distribution");
    }
    return stager.stage(event.sender.id, sourcePath);
  });
  return {
    consume: (owner, token) => stager.consume(owner.id, token),
    close: () => {
      ipcMain.removeHandler(droppedSourceIPCChannel);
      stager.clear();
    },
  };
}

function trustedRendererURL(raw: string): boolean {
  try {
    const url = new URL(raw);
    return url.protocol === `${OC_WEB_SCHEME}:` && url.host === OC_WEB_HOST;
  } catch {
    return false;
  }
}
