import { contextBridge, ipcRenderer, webUtils } from "electron";

// Sandboxed preloads must remain one CommonJS file; keep this value in sync
// with platform-channel.ts and expose only the narrow operation, never IPC.
const droppedSourceIPCChannel = "open-cut:stage-dropped-source";

contextBridge.exposeInMainWorld("openCutPlatform", {
  stageDroppedSource(file: File): Promise<string> {
    const sourcePath = webUtils.getPathForFile(file);
    if (!sourcePath) return Promise.reject(new Error("Dropped source has no trusted local identity"));
    return ipcRenderer.invoke(droppedSourceIPCChannel, sourcePath) as Promise<string>;
  },
});
