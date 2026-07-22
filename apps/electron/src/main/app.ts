import { fileURLToPath } from "node:url";

import { app, BrowserWindow, shell } from "electron";

import { registerDroppedSourceIPC } from "./dropped-source-ipc.js";
import { DeliveryReceiptStore, handleExportRevealRequest } from "./export-reveal.js";
import { DestinationGrantStore, handleExportSaveRequest } from "./export-save.js";
import { OC_PLATFORM_EXPORT_REVEAL_PATH, OC_PLATFORM_EXPORT_SAVE_PATH } from "./oc-protocol.js";
import { registerOcWebProtocol } from "./oc-protocol-electron.js";
import { handleSourcePickerRequest } from "./source-picker.js";

const navigationTimeoutMs = 15_000;
const startupPlaceholderDelayMs = 500;
const unavailableDocument = encodeURIComponent(`<!doctype html>
<html><head><meta charset="utf-8"><style>
  :root { color-scheme: dark; font-family: system-ui, sans-serif; background: #11120f; color: #f4f1ea; }
  body { min-height: 100vh; margin: 0; display: grid; place-items: center; }
  main { text-align: center; }
  span { display: inline-block; width: 8px; height: 8px; margin-right: 10px; border-radius: 50%; background: #b5ff58; }
  p { color: rgba(244, 241, 234, .62); }
</style></head><body><main><h1><span></span>Open Cut</h1><p>Waiting for the Web sidecar…</p></main></body></html>`);

export type ElectronApp = {
  activateWeb(webRuntimeUrl: string, apiRuntimeUrl: string, uiSession: string): Promise<void>;
  renewUISession(apiRuntimeUrl: string, uiSession: string): void;
  unavailable(): Promise<void>;
  show(): void;
  close(): void;
};

export async function startElectronApp(): Promise<ElectronApp> {
  await app.whenReady();
  const window = new BrowserWindow({
    width: 1440,
    height: 900,
    minWidth: 1280,
    minHeight: 800,
    show: false,
    webPreferences: {
      backgroundThrottling: false,
      contextIsolation: true,
      nodeIntegration: false,
      preload: fileURLToPath(new URL("../preload.cjs", import.meta.url)),
      sandbox: true,
    },
  });
  let apiRuntimeUrl: string | undefined;
  let activeUISession: string | undefined;
  const droppedSources = registerDroppedSourceIPC();
  const destinationGrants = new DestinationGrantStore();
  const deliveryReceipts = new DeliveryReceiptStore();
  const webProtocol = registerOcWebProtocol((request) => {
    const path = new URL(request.url).pathname;
    if (path === OC_PLATFORM_EXPORT_SAVE_PATH) {
      return handleExportSaveRequest(
        request,
        window,
        apiRuntimeUrl,
        activeUISession,
        destinationGrants,
        deliveryReceipts,
      );
    }
    if (path === OC_PLATFORM_EXPORT_REVEAL_PATH) {
      return handleExportRevealRequest(request, activeUISession, deliveryReceipts, (target) =>
        shell.showItemInFolder(target),
      );
    }
    return handleSourcePickerRequest(request, window, apiRuntimeUrl, activeUISession, (token) =>
      droppedSources.consume(window.webContents, token),
    );
  });
  let navigation = 0;
  let startupPlaceholder: NodeJS.Timeout | undefined;

  const cancelStartupPlaceholder = (): void => {
    if (!startupPlaceholder) return;
    clearTimeout(startupPlaceholder);
    startupPlaceholder = undefined;
  };

  const load = async (url: string, reveal = true): Promise<void> => {
    const current = ++navigation;
    console.info(`[open-cut electron] navigating ${url}`);
    try {
      await withTimeout(window.loadURL(url), navigationTimeoutMs, () => window.webContents.stop());
      if (current !== navigation || window.isDestroyed()) return;
      console.info(`[open-cut electron] loaded ${window.webContents.getURL()}`);
      if (reveal) window.show();
    } catch (error) {
      if (current !== navigation) return;
      throw error;
    }
  };

  app.on("window-all-closed", () => app.quit());
  await load(`data:text/html;charset=utf-8,${unavailableDocument}`, false);
  startupPlaceholder = setTimeout(() => {
    startupPlaceholder = undefined;
    if (!window.isDestroyed()) window.show();
  }, startupPlaceholderDelayMs);
  startupPlaceholder.unref();

  return {
    async activateWeb(webRuntimeUrl, nextAPIRuntimeUrl, uiSession) {
      cancelStartupPlaceholder();
      destinationGrants.clear();
      deliveryReceipts.clear();
      apiRuntimeUrl = nextAPIRuntimeUrl;
      activeUISession = uiSession;
      webProtocol.setWebRuntime(webRuntimeUrl);
      webProtocol.setUISession(uiSession);
      try {
        await load(webProtocol.entryUrl);
      } catch (error) {
        apiRuntimeUrl = undefined;
        activeUISession = undefined;
        destinationGrants.clear();
        deliveryReceipts.clear();
        webProtocol.setWebRuntime(undefined);
        webProtocol.setUISession(undefined);
        throw error;
      }
    },
    renewUISession(nextAPIRuntimeUrl, uiSession) {
      if (!activeUISession || apiRuntimeUrl !== nextAPIRuntimeUrl) {
        throw new Error("UI session renewal does not match the active API lease");
      }
      deliveryReceipts.rebind(activeUISession, uiSession);
      activeUISession = uiSession;
      webProtocol.setUISession(uiSession);
    },
    unavailable() {
      cancelStartupPlaceholder();
      apiRuntimeUrl = undefined;
      activeUISession = undefined;
      destinationGrants.clear();
      deliveryReceipts.clear();
      webProtocol.setWebRuntime(undefined);
      webProtocol.setUISession(undefined);
      return load(`data:text/html;charset=utf-8,${unavailableDocument}`);
    },
    show: () => {
      cancelStartupPlaceholder();
      window.show();
    },
    close: () => {
      cancelStartupPlaceholder();
      droppedSources.close();
      destinationGrants.clear();
      deliveryReceipts.clear();
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
