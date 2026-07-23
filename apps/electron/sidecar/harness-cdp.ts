import { lifecycleMode, presentation, sidecarEnvironment } from "@open-cut/sidecar-client";

export const harnessCDPPortEnvironment = "OPEN_CUT_HARNESS_CDP_PORT";

type ElectronCommandLine = Readonly<{
  appendSwitch(name: string, value?: string): void;
}>;

export function configureHarnessCDP(
  commandLine: ElectronCommandLine,
  environment: Readonly<Record<string, string | undefined>>,
): number | undefined {
  const rawPort = environment[harnessCDPPortEnvironment];
  if (rawPort === undefined) return undefined;
  const mode = environment[sidecarEnvironment.mode];
  if (
    (mode !== lifecycleMode.packaged && mode !== lifecycleMode.harness && mode !== lifecycleMode.dev) ||
    environment[sidecarEnvironment.presentation] !== presentation.interactive
  ) {
    throw new Error("Electron CDP automation is restricted to interactive packaged and development surfaces");
  }
  const port = Number(rawPort);
  if (!/^[1-9][0-9]{3,4}$/.test(rawPort) || !Number.isSafeInteger(port) || port < 1024 || port > 65_535) {
    throw new Error("Electron CDP automation port is invalid");
  }
  commandLine.appendSwitch("remote-debugging-address", "127.0.0.1");
  commandLine.appendSwitch("remote-debugging-port", rawPort);
  // Chromium otherwise stops producing compositor frames for an occluded
  // macOS window, making read-only CDP captures time out unless tooling steals focus.
  commandLine.appendSwitch("disable-backgrounding-occluded-windows");
  return port;
}
