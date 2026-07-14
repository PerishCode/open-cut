import { randomUUID } from "node:crypto";

import WebSocket from "ws";

export type ControlDescriptor = {
  schema: 1;
  protocol: "sidecar.v1";
  address: string;
  pid: number;
  sessionId: string;
  generation: number;
  startedAt: string;
};

export type SidecarLaunch = {
  descriptor: ControlDescriptor;
  token: string;
  channel: string;
  namespace: string;
  mode: string;
  source: string;
};

export type SidecarCommand = "show" | "shutdown";

export type DelegatedCapability = {
  subject: string;
  token: string;
  expiresAt: string;
};

export type SidecarStatus = {
  subject: string;
  app: string;
  instanceId: string;
  mode: string;
  source: string;
  ready: boolean;
  connectedAt: string;
  lastHeartbeat: string;
  endpoints?: Array<{ name: string; url: string }>;
};

export type BrokerStatus = {
  schema: 1;
  revision: number;
  channel: string;
  namespace: string;
  sessionId: string;
  generation: number;
  sessions: SidecarStatus[];
};

export type UpdateTransition = {
  status: "current" | "prepared";
  version: string;
  restartRequired: boolean;
};

export type ConnectOptions = {
  app: string;
  launch?: SidecarLaunch;
  heartbeatIntervalMs?: number;
  reconcileIntervalMs?: number;
  capabilityRenewIntervalMs?: number;
  onCommand?: (command: SidecarCommand) => void | Promise<void>;
};

type ServerEvent = {
  type: "registered" | "command" | "status";
  command?: SidecarCommand;
  status?: BrokerStatus;
};

type StatusListener = (status: BrokerStatus) => void;
type AppStatusListener = (status: SidecarStatus | undefined, broker: BrokerStatus) => void;

const REQUIRED_ENV = [
  "OC_SIDECAR_CONTROL",
  "OC_SIDECAR_TOKEN",
  "OC_SIDECAR_CHANNEL",
  "OC_SIDECAR_NAMESPACE",
  "OC_SIDECAR_MODE",
  "OC_SIDECAR_SOURCE",
] as const;

export function loadSidecarLaunch(env: NodeJS.ProcessEnv = process.env): SidecarLaunch {
  for (const name of REQUIRED_ENV) {
    if (!env[name]) throw new Error(`${name} is required for sidecar startup`);
  }
  const descriptor = JSON.parse(env.OC_SIDECAR_CONTROL!) as ControlDescriptor;
  if (descriptor.schema !== 1 || descriptor.protocol !== "sidecar.v1" || !descriptor.address || !descriptor.sessionId) {
    throw new Error("OC_SIDECAR_CONTROL is not a valid sidecar.v1 descriptor");
  }
  return {
    descriptor,
    token: env.OC_SIDECAR_TOKEN!,
    channel: env.OC_SIDECAR_CHANNEL!,
    namespace: env.OC_SIDECAR_NAMESPACE!,
    mode: env.OC_SIDECAR_MODE!,
    source: env.OC_SIDECAR_SOURCE!,
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
  #status: BrokerStatus | undefined;
  #desiredReady = false;
  #closed = false;
  #polling = false;

  private constructor(options: ConnectOptions, launch: SidecarLaunch) {
    this.#options = options;
    this.#launch = launch;
    this.#heartbeat = setInterval(() => this.#send({ type: "heartbeat" }), options.heartbeatIntervalMs ?? 5_000);
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
    const connection = new SidecarConnection(options, options.launch ?? loadSidecarLaunch());
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

  async waitForReady(app: string, timeoutMs = 30_000): Promise<SidecarStatus> {
    return new Promise<SidecarStatus>((resolve, reject) => {
      let settled = false;
      let unsubscribe: () => void = () => undefined;
      let timeout: NodeJS.Timeout | undefined;
      const onStatus = (peer: SidecarStatus | undefined) => {
        if (!peer?.ready || settled) return;
        settled = true;
        if (timeout) clearTimeout(timeout);
        unsubscribe();
        resolve(peer);
      };
      unsubscribe = this.watchApp(app, onStatus);
      if (settled) unsubscribe();
      timeout = setTimeout(() => {
        if (settled) return;
        settled = true;
        unsubscribe();
        reject(new Error(`sidecar ${app} did not reach READY within ${timeoutMs}ms`));
      }, timeoutMs);
    });
  }

  async delegateSidecar(subject: string, ttlSeconds = 900): Promise<SidecarLaunch> {
    return delegateSidecar(this.#launch, subject, ttlSeconds);
  }

  async prepareLatestUpdate(): Promise<UpdateTransition> {
    return prepareLatestUpdate(this.#launch);
  }

  async shutdownCell(): Promise<number> {
    return controlCell(this.#launch, "shutdown");
  }

  publishEndpoint(name: string, url: string): void {
    this.#endpoints.set(name, url);
    if (this.#desiredReady) this.#send({ type: "endpoint", name, url });
  }

  ready(): void {
    this.#desiredReady = true;
    this.#replayState();
  }

  notReady(): void {
    this.#desiredReady = false;
    this.#send({ type: "state", ready: false });
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
      socket.send(JSON.stringify({ type: "exiting", code }));
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
    const socket = new WebSocket(`ws://${launch.descriptor.address}/v1/sessions/register`, {
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
        if (event.type === "registered") finish();
        if (event.type === "status" && event.status) this.#acceptStatus(event.status);
        if (event.type === "command" && event.command && this.#options.onCommand) {
          void Promise.resolve(this.#options.onCommand(event.command)).catch((error: unknown) => {
            console.error(error instanceof Error ? error.stack ?? error.message : String(error));
          });
        }
      });
      socket.once("open", () => {
        socket.send(JSON.stringify({
          type: "register",
          channel: launch.channel,
          namespace: launch.namespace,
          sessionId: launch.descriptor.sessionId,
          generation: launch.descriptor.generation,
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
      this.#send({ type: "state", ready: false });
      return;
    }
    for (const [name, url] of this.#endpoints) this.#send({ type: "endpoint", name, url });
    this.#send({ type: "state", ready: true });
  }

  #send(event: object): void {
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

  #acceptStatus(status: BrokerStatus): void {
    if (status.sessionId !== this.#launch.descriptor.sessionId || status.generation !== this.#launch.descriptor.generation) return;
    if (this.#status && status.revision < this.#status.revision) return;
    this.#status = status;
    this.#notify(status);
  }

  #publishDisconnectedSnapshot(): void {
    if (!this.#status || this.#status.sessions.length === 0) return;
    this.#status = { ...this.#status, sessions: [] };
    this.#notify(this.#status);
  }

  #notify(status: BrokerStatus): void {
    for (const listener of this.#listeners) {
      try {
        listener(status);
      } catch (error) {
        console.error(error instanceof Error ? error.stack ?? error.message : String(error));
      }
    }
  }
}

export async function delegateSidecar(parent: SidecarLaunch, subject: string, ttlSeconds = 900): Promise<SidecarLaunch> {
  const response = await fetch(`http://${parent.descriptor.address}/v1/capabilities/sidecar`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${parent.token}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ subject, ttlSeconds }),
    signal: AbortSignal.timeout(5_000),
  });
  if (!response.ok) throw new Error(`sidecar capability delegation returned ${response.status}`);
  const delegated = await response.json() as DelegatedCapability;
  if (delegated.subject !== subject || !delegated.token || !delegated.expiresAt) {
    throw new Error("broker returned an invalid delegated capability");
  }
  return { ...parent, token: delegated.token, source: "payload" };
}

export async function getBrokerStatus(launch: SidecarLaunch): Promise<BrokerStatus> {
  const response = await fetch(`http://${launch.descriptor.address}/v1/status`, {
    headers: { Authorization: `Bearer ${launch.token}` },
    signal: AbortSignal.timeout(5_000),
  });
  if (!response.ok) throw new Error(`sidecar status returned ${response.status}`);
  const status = await response.json() as BrokerStatus;
  if (
    status.schema !== 1 ||
    !Number.isSafeInteger(status.revision) ||
    status.revision < 0 ||
    status.sessionId !== launch.descriptor.sessionId ||
    !Array.isArray(status.sessions)
  ) {
    throw new Error("broker returned an invalid status document");
  }
  return status;
}

export async function renewCapability(launch: SidecarLaunch, ttlSeconds: number): Promise<DelegatedCapability> {
  const response = await fetch(`http://${launch.descriptor.address}/v1/capabilities/renew`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${launch.token}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ ttlSeconds }),
    signal: AbortSignal.timeout(5_000),
  });
  if (!response.ok) throw new Error(`sidecar capability renewal returned ${response.status}`);
  const renewed = await response.json() as DelegatedCapability;
  if (!renewed.token || !renewed.expiresAt) throw new Error("broker returned an invalid renewed capability");
  return renewed;
}

export async function controlCell(launch: SidecarLaunch, command: SidecarCommand): Promise<number> {
  const response = await fetch(`http://${launch.descriptor.address}/v1/control`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${launch.token}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ command }),
    signal: AbortSignal.timeout(5_000),
  });
  if (!response.ok) throw new Error(`sidecar lifecycle control returned ${response.status}`);
  const result = await response.json() as { accepted?: unknown };
  if (!Number.isSafeInteger(result.accepted) || (result.accepted as number) < 0) {
    throw new Error("broker returned an invalid lifecycle response");
  }
  return result.accepted as number;
}

export async function prepareLatestUpdate(launch: SidecarLaunch): Promise<UpdateTransition> {
  const response = await fetch(`http://${launch.descriptor.address}/v1/update/transition`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${launch.token}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ action: "prepare-latest" }),
    signal: AbortSignal.timeout(5_000),
  });
  if (!response.ok) throw new Error(`update transition returned ${response.status}`);
  const transition = await response.json() as UpdateTransition;
  if (!transition.version || !["current", "prepared"].includes(transition.status) || typeof transition.restartRequired !== "boolean") {
    throw new Error("broker returned an invalid update transition");
  }
  return transition;
}

export function sidecarLaunchEnv(launch: SidecarLaunch): NodeJS.ProcessEnv {
  return {
    OC_SIDECAR_CONTROL: JSON.stringify(launch.descriptor),
    OC_SIDECAR_TOKEN: launch.token,
    OC_SIDECAR_CHANNEL: launch.channel,
    OC_SIDECAR_NAMESPACE: launch.namespace,
    OC_SIDECAR_MODE: launch.mode,
    OC_SIDECAR_SOURCE: launch.source,
  };
}

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}
