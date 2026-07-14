import { randomUUID } from "node:crypto";

import WebSocket from "ws";

import {
  controlCommand,
  eventType,
  protocolVersion,
  operations,
  sidecarEnvironment,
  type ClientEvent,
  type ControlCommand,
  type ControlResponse,
  type RenewResponse,
  type ServerEvent,
  type SidecarLaunch,
  type SessionStatus,
  type Status,
} from "./generated.js";

export { controlCommand } from "./generated.js";
export type { ControlCommand, SessionStatus } from "./generated.js";

export type ConnectOptions = {
  app: string;
  heartbeatIntervalMs?: number;
  reconcileIntervalMs?: number;
  capabilityRenewIntervalMs?: number;
  onCommand?: (command: ControlCommand) => void | Promise<void>;
};

type StatusListener = (status: Status) => void;
type AppStatusListener = (status: SessionStatus | undefined, broker: Status) => void;

function loadSidecarLaunch(env: NodeJS.ProcessEnv = process.env): SidecarLaunch {
  for (const name of Object.values(sidecarEnvironment)) {
    if (!env[name]) throw new Error(`${name} is required for sidecar startup`);
  }
  const control = JSON.parse(env[sidecarEnvironment.control]!) as SidecarLaunch["control"];
  if (control.schema !== 1 || control.protocol !== protocolVersion || !control.address || !control.sessionId) {
    throw new Error(`${sidecarEnvironment.control} is not a valid ${protocolVersion} descriptor`);
  }
  return {
    control,
    token: env[sidecarEnvironment.token]!,
    channel: env[sidecarEnvironment.channel]!,
    namespace: env[sidecarEnvironment.namespace]!,
    mode: env[sidecarEnvironment.mode]!,
    source: env[sidecarEnvironment.source]!,
  };
}

export class SidecarConnection {
  readonly #launch: SidecarLaunch;
  readonly #options: ConnectOptions;
  readonly #instanceId = randomUUID();
  readonly #listeners = new Set<StatusListener>();
  readonly #endpoints = new Map<string, string>();
  readonly #heartbeat: NodeJS.Timeout;
  readonly #reconcile: NodeJS.Timeout;
  readonly #renewal: NodeJS.Timeout;

  #socket: WebSocket | undefined;
  #connecting: Promise<void> | undefined;
  #status: Status | undefined;
  #desiredReady = false;
  #closed = false;
  #polling = false;

  private constructor(options: ConnectOptions, launch: SidecarLaunch) {
    this.#options = options;
    this.#launch = launch;
    this.#heartbeat = setInterval(() => this.#send({ type: eventType.heartbeat }), options.heartbeatIntervalMs ?? 5_000);
    this.#heartbeat.unref();
    this.#reconcile = setInterval(() => void this.#pollStatus(), options.reconcileIntervalMs ?? 2_000);
    this.#reconcile.unref();
    this.#renewal = setInterval(
      () => void this.#renewCapability(),
      options.capabilityRenewIntervalMs ?? 12 * 60 * 60 * 1_000,
    );
    this.#renewal.unref();
  }

  static async connect(options: ConnectOptions): Promise<SidecarConnection> {
    const connection = new SidecarConnection(options, loadSidecarLaunch());
    await connection.#ensureConnected();
    await connection.#pollStatus();
    return connection;
  }

  subscribe(listener: StatusListener): () => void {
    this.#listeners.add(listener);
    if (this.#status) listener(this.#status);
    return () => this.#listeners.delete(listener);
  }

  watchApp(app: string, listener: AppStatusListener): () => void {
    return this.subscribe((status) => {
      listener(status.sessions.find((session) => session.app === app), status);
    });
  }

  get mode(): string {
    return this.#launch.mode;
  }

  async shutdownCell(): Promise<number> {
    return controlCell(this.#launch, controlCommand.shutdown);
  }

  publishEndpoint(name: string, url: string): void {
    this.#endpoints.set(name, url);
    if (this.#desiredReady) this.#send({ type: eventType.endpoint, name, url });
  }

  ready(): void {
    this.#desiredReady = true;
    this.#replayState();
  }

  notReady(): void {
    this.#desiredReady = false;
    this.#send({ type: eventType.state, ready: false });
  }

  close(code = 0): void {
    if (this.#closed) return;
    this.#closed = true;
    clearInterval(this.#heartbeat);
    clearInterval(this.#reconcile);
    clearInterval(this.#renewal);
    const socket = this.#socket;
    this.#socket = undefined;
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: eventType.exiting, code }));
      socket.close();
    } else {
      socket?.close();
    }
  }

  async #ensureConnected(): Promise<void> {
    if (this.#closed) throw new Error("sidecar connection is closed");
    if (this.#socket?.readyState === WebSocket.OPEN) return;
    if (this.#connecting) return this.#connecting;
    this.#connecting = this.#reconnectLoop();
    try {
      await this.#connecting;
    } finally {
      this.#connecting = undefined;
    }
  }

  async #reconnectLoop(): Promise<void> {
    let delayMs = 50;
    while (!this.#closed) {
      try {
        await this.#open();
        return;
      } catch {
        if (this.#closed) break;
        await delay(delayMs);
        delayMs = Math.min(delayMs * 2, 2_000);
      }
    }
    throw new Error("sidecar connection closed while reconnecting");
  }

  async #open(): Promise<void> {
    const launch = this.#launch;
    const socket = new WebSocket(`ws://${launch.control.address}${operations.registerSession.path}`, {
      headers: { Authorization: `Bearer ${launch.token}` },
    });
    this.#socket = socket;

    await new Promise<void>((resolve, reject) => {
      let acknowledged = false;
      const timeout = setTimeout(() => finish(new Error("sidecar registration acknowledgement timed out")), 5_000);
      const finish = (error?: Error) => {
        if (acknowledged) return;
        acknowledged = true;
        clearTimeout(timeout);
        if (error) reject(error); else resolve();
      };
      socket.on("message", (raw) => {
        let event: ServerEvent;
        try {
          event = JSON.parse(raw.toString()) as ServerEvent;
        } catch {
          socket.close();
          return;
        }
        if (event.type === eventType.registered) finish();
        if (event.type === eventType.status) this.#acceptStatus(event.status);
        if (event.type === eventType.command && this.#options.onCommand) {
          void Promise.resolve(this.#options.onCommand(event.command)).catch((error: unknown) => {
            console.error(error instanceof Error ? error.stack ?? error.message : String(error));
          });
        }
      });
      socket.once("open", () => {
        socket.send(JSON.stringify({
          type: eventType.register,
          channel: launch.channel,
          namespace: launch.namespace,
          sessionId: launch.control.sessionId,
          generation: launch.control.generation,
          app: this.#options.app,
          instanceId: this.#instanceId,
          mode: launch.mode,
          source: launch.source,
        }));
      });
      socket.once("error", (error) => finish(error));
      socket.once("close", () => finish(new Error("sidecar connection closed before registration acknowledgement")));
    });

    if (this.#closed) {
      socket.close();
      throw new Error("sidecar connection closed during registration");
    }
    socket.on("close", () => {
      if (this.#socket !== socket) return;
      this.#socket = undefined;
      this.#publishDisconnectedSnapshot();
      if (!this.#closed) void this.#ensureConnected().catch(() => undefined);
    });
    socket.on("error", () => undefined);
    this.#replayState();
  }

  #replayState(): void {
    if (!this.#desiredReady) {
      this.#send({ type: eventType.state, ready: false });
      return;
    }
    for (const [name, url] of this.#endpoints) this.#send({ type: eventType.endpoint, name, url });
    this.#send({ type: eventType.state, ready: true });
  }

  #send(event: ClientEvent): void {
    const socket = this.#socket;
    if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify(event));
  }

  async #pollStatus(): Promise<void> {
    if (this.#closed || this.#polling) return;
    this.#polling = true;
    try {
      this.#acceptStatus(await getBrokerStatus(this.#launch));
    } catch {
      if (this.#socket?.readyState !== WebSocket.OPEN) this.#publishDisconnectedSnapshot();
    } finally {
      this.#polling = false;
    }
  }

  async #renewCapability(): Promise<void> {
    if (this.#closed) return;
    try {
      const renewed = await renewCapability(this.#launch, 7 * 24 * 60 * 60);
      this.#launch.token = renewed.token;
    } catch {
      // The next interval retries without changing the last valid capability.
    }
  }

  #acceptStatus(status: Status): void {
    if (status.sessionId !== this.#launch.control.sessionId || status.generation !== this.#launch.control.generation) return;
    if (this.#status && status.revision < this.#status.revision) return;
    this.#status = status;
    this.#notify(status);
  }

  #publishDisconnectedSnapshot(): void {
    if (!this.#status || this.#status.sessions.length === 0) return;
    this.#status = { ...this.#status, sessions: [] };
    this.#notify(this.#status);
  }

  #notify(status: Status): void {
    for (const listener of this.#listeners) {
      try {
        listener(status);
      } catch (error) {
        console.error(error instanceof Error ? error.stack ?? error.message : String(error));
      }
    }
  }
}

async function getBrokerStatus(launch: SidecarLaunch): Promise<Status> {
  const response = await fetch(`http://${launch.control.address}${operations.status.path}`, {
    method: operations.status.method,
    headers: { Authorization: `Bearer ${launch.token}` },
    signal: AbortSignal.timeout(5_000),
  });
  if (!response.ok) throw new Error(`sidecar status returned ${response.status}`);
  const status = await response.json() as Status;
  if (
    status.schema !== 1 ||
    !Number.isSafeInteger(status.revision) ||
    status.revision < 0 ||
    status.sessionId !== launch.control.sessionId ||
    !Array.isArray(status.sessions)
  ) {
    throw new Error("broker returned an invalid status document");
  }
  return status;
}

async function renewCapability(launch: SidecarLaunch, ttlSeconds: number): Promise<RenewResponse> {
  const response = await fetch(`http://${launch.control.address}${operations.renewCapability.path}`, {
    method: operations.renewCapability.method,
    headers: {
      Authorization: `Bearer ${launch.token}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ ttlSeconds }),
    signal: AbortSignal.timeout(5_000),
  });
  if (!response.ok) throw new Error(`sidecar capability renewal returned ${response.status}`);
  const renewed = await response.json() as RenewResponse;
  if (!renewed.token || !renewed.expiresAt) throw new Error("broker returned an invalid renewed capability");
  return renewed;
}

async function controlCell(launch: SidecarLaunch, command: ControlCommand): Promise<number> {
  const response = await fetch(`http://${launch.control.address}${operations.broadcastControl.path}`, {
    method: operations.broadcastControl.method,
    headers: {
      Authorization: `Bearer ${launch.token}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ command }),
    signal: AbortSignal.timeout(5_000),
  });
  if (!response.ok) throw new Error(`sidecar lifecycle control returned ${response.status}`);
  const result = await response.json() as ControlResponse;
  if (!Number.isSafeInteger(result.accepted) || (result.accepted as number) < 0) {
    throw new Error("broker returned an invalid lifecycle response");
  }
  return result.accepted as number;
}

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}
