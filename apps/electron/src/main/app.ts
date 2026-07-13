import { app, BrowserWindow } from "electron";

export type ElectronApp = {
  show(): void;
  close(): void;
};

export async function startElectronApp(url = "data:text/html,<main>Open Cut</main>"): Promise<ElectronApp> {
  await app.whenReady();
  const window = new BrowserWindow({ width: 1100, height: 760, show: false });
  await window.loadURL(url);
  window.show();
  return {
    show: () => window.show(),
    close: () => app.quit(),
  };
}
