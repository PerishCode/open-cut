import { app, BrowserWindow } from "electron";

import { registerOcWebProtocol } from "./oc-protocol-electron.js";

const navigationTimeoutMs = 15_000;
const unavailableDocument = encodeURIComponent(`<!doctype html>
<html><head><meta charset="utf-8"><style>
  :root { color-scheme: dark; font-family: system-ui, sans-serif; background: #11120f; color: #f4f1ea; }
  body { min-height: 100vh; margin: 0; display: grid; place-items: center; }
  main { text-align: center; }
  span { display: inline-block; width: 8px; height: 8px; margin-right: 10px; border-radius: 50%; background: #b5ff58; }
  p { color: rgba(244, 241, 234, .62); }
</style></head><body><main><h1><span></span>Open Cut</h1><p>Waiting for the Web sidecar…</p></main></body></html>`);

export type ElectronApp = {
  activateWeb(webRuntimeUrl: string): Promise<void>;
  unavailable(): Promise<void>;
  show(): void;
  close(): void;
};

export async function startElectronApp(): Promise<ElectronApp> {
  await app.whenReady();
  const webProtocol = registerOcWebProtocol();
  const window = new BrowserWindow({ width: 1100, height: 760, show: false });
  let navigation = 0;

  const load = async (url: string): Promise<void> => {
    const current = ++navigation;
    console.info(`[open-cut electron] navigating ${url}`);
    try {
      await withTimeout(window.loadURL(url), navigationTimeoutMs, () => window.webContents.stop());
      if (current !== navigation || window.isDestroyed()) return;
      console.info(`[open-cut electron] loaded ${window.webContents.getURL()}`);
      window.show();
    } catch (error) {
      if (current !== navigation) return;
      throw error;
    }
  };

  app.on("window-all-closed", () => app.quit());
  await load(`data:text/html;charset=utf-8,${unavailableDocument}`);

  return {
    async activateWeb(webRuntimeUrl) {
      webProtocol.setWebRuntime(webRuntimeUrl);
      try {
        await load(webProtocol.entryUrl);
      } catch (error) {
        webProtocol.setWebRuntime(undefined);
        throw error;
      }
    },
    unavailable() {
      webProtocol.setWebRuntime(undefined);
      return load(`data:text/html;charset=utf-8,${unavailableDocument}`);
    },
    show: () => window.show(),
    close: () => {
      webProtocol.close();
      app.quit();
    },
  };
}

async function withTimeout<T>(promise: Promise<T>, milliseconds: number, onTimeout: () => void): Promise<T> {
  let timeout: NodeJS.Timeout | undefined;
  try {
    return await Promise.race([
      promise,
      new Promise<never>((_resolve, reject) => {
        timeout = setTimeout(() => {
          onTimeout();
          reject(new Error(`Electron navigation timed out after ${milliseconds}ms`));
        }, milliseconds);
      }),
    ]);
  } finally {
    if (timeout) clearTimeout(timeout);
  }
}
