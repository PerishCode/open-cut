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
  mode: string;
  source: string;
  ready: boolean;
  endpoints?: Array<{ name: string; url: string }>;
};

export type BrokerStatus = {
  schema: 1;
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
  onCommand?: (command: SidecarCommand) => void | Promise<void>;
};

type ServerEvent = {
  type: "registered" | "command";
  command?: SidecarCommand;
};

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
  readonly #socket: WebSocket;
  readonly #heartbeat: NodeJS.Timeout;
  readonly #launch: SidecarLaunch;

  private constructor(socket: WebSocket, heartbeat: NodeJS.Timeout, launch: SidecarLaunch) {
    this.#socket = socket;
    this.#heartbeat = heartbeat;
    this.#launch = launch;
  }

  static async connect(options: ConnectOptions): Promise<SidecarConnection> {
    const launch = options.launch ?? loadSidecarLaunch();
    const socket = new WebSocket(`ws://${launch.descriptor.address}/v1/sessions/register`, {
      headers: { Authorization: `Bearer ${launch.token}` },
    });
    await new Promise<void>((resolve, reject) => {
      const finish = (error?: Error) => {
        clearTimeout(timeout);
        socket.off("error", onError);
        socket.off("close", onClose);
        socket.off("message", onMessage);
        if (error) reject(error); else resolve();
      };
      const onError = (error: Error) => finish(error);
      const onClose = () => finish(new Error("sidecar connection closed before registration acknowledgement"));
      const onMessage = (raw: WebSocket.RawData) => {
        const event = JSON.parse(raw.toString()) as ServerEvent;
        if (event.type === "registered") finish();
      };
      const timeout = setTimeout(() => finish(new Error("sidecar registration acknowledgement timed out")), 5_000);
      socket.once("error", onError);
      socket.once("close", onClose);
      socket.on("message", onMessage);
      socket.once("open", () => {
        socket.send(JSON.stringify({
          type: "register",
          channel: launch.channel,
          namespace: launch.namespace,
          sessionId: launch.descriptor.sessionId,
          generation: launch.descriptor.generation,
          app: options.app,
          mode: launch.mode,
          source: launch.source,
        }));
      });
    });

    socket.on("message", (raw) => {
      const event = JSON.parse(raw.toString()) as ServerEvent;
      if (event.type === "command" && event.command && options.onCommand) {
        void options.onCommand(event.command);
      }
    });
    const heartbeat = setInterval(() => {
      if (socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: "heartbeat" }));
    }, options.heartbeatIntervalMs ?? 5_000);
    heartbeat.unref();
    return new SidecarConnection(socket, heartbeat, launch);
  }

  async delegateSidecar(subject: string, ttlSeconds = 900): Promise<SidecarLaunch> {
    return delegateSidecar(this.#launch, subject, ttlSeconds);
  }

  async prepareLatestUpdate(): Promise<UpdateTransition> {
    return prepareLatestUpdate(this.#launch);
  }

  publishEndpoint(name: string, url: string): void {
    this.#send({ type: "endpoint", name, url });
  }

  ready(): void {
    this.#send({ type: "ready" });
  }

  close(code = 0): void {
    clearInterval(this.#heartbeat);
    if (this.#socket.readyState === WebSocket.OPEN) {
      this.#socket.send(JSON.stringify({ type: "exiting", code }));
      this.#socket.close();
    }
  }

  #send(event: object): void {
    if (this.#socket.readyState !== WebSocket.OPEN) throw new Error("sidecar connection is not open");
    this.#socket.send(JSON.stringify(event));
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
  });
  if (!response.ok) throw new Error(`sidecar status returned ${response.status}`);
  const status = await response.json() as BrokerStatus;
  if (status.schema !== 1 || status.sessionId !== launch.descriptor.sessionId || !Array.isArray(status.sessions)) {
    throw new Error("broker returned an invalid status document");
  }
  return status;
}

export async function prepareLatestUpdate(launch: SidecarLaunch): Promise<UpdateTransition> {
  const response = await fetch(`http://${launch.descriptor.address}/v1/update/transition`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${launch.token}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ action: "prepare-latest" }),
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
