import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import { request as requestHttp } from "node:http";

const signerSocketEnvironment = "OC_LIFECYCLE_SIGNER_SOCKET";
const platformHostEnvironment = "OC_PLATFORM_HOST";

export type UISession = Readonly<{ token: string; expiresAt: number }>;

export async function bootstrapUISession(apiEndpoint: string): Promise<UISession> {
  const challenge = asRecord(
    await postJSON(new URL("v1/auth/ui/challenges", apiEndpoint), {
      clientInstance: `electron-${randomUUID()}`,
      origin: "oc://app",
    }),
  );
  const signingPayload = requiredString(challenge.signingPayload, "UI challenge signing payload");
  const signature = await signWithLifecycle(signingPayload);
  if (
    signature.installationId !== challenge.installationId ||
    signature.installationGeneration !== challenge.installationGeneration ||
    signature.role !== challenge.role
  ) {
    throw new Error("platform signer identity does not match the API challenge");
  }
  const exchanged = asRecord(
    await postJSON(new URL("v1/auth/ui/sessions", apiEndpoint), {
      nonce: requiredString(challenge.nonce, "UI challenge nonce"),
      signature: signature.signature,
    }),
  );
  const token = requiredString(exchanged.session, "UI session");
  const expiresAt = Date.parse(requiredString(exchanged.expiresAt, "UI session expiry"));
  if (exchanged.schema !== "open-cut/ui-session/v1" || !token.startsWith("oc_ui_") || !Number.isFinite(expiresAt)) {
    throw new Error("API returned an invalid UI session");
  }
  return { token, expiresAt };
}

type LifecycleSignature = Readonly<{
  installationId: string;
  installationGeneration: number;
  role: string;
  signature: string;
}>;

async function signWithLifecycle(payload: string): Promise<LifecycleSignature> {
  if (!/^[A-Za-z0-9_-]+$/.test(payload)) throw new Error("UI challenge payload is not canonical base64url");
  const request = { schema: 1, role: "first-party-ui", payload };
  const signerSocket = process.env[signerSocketEnvironment];
  const response = signerSocket
    ? await unixJSON(signerSocket, request)
    : await platformHostJSON(requiredEnvironment(platformHostEnvironment), request);
  const result = asRecord(response);
  if (
    result.schema !== 1 ||
    typeof result.installationGeneration !== "number" ||
    !Number.isSafeInteger(result.installationGeneration) ||
    result.installationGeneration < 1
  ) {
    throw new Error("lifecycle signer returned an invalid response");
  }
  return {
    installationId: requiredString(result.installationId, "signer installation identity"),
    installationGeneration: result.installationGeneration,
    role: requiredString(result.role, "signer role"),
    signature: requiredString(result.signature, "signer signature"),
  };
}

function unixJSON(socketPath: string, value: unknown): Promise<unknown> {
  const body = JSON.stringify(value);
  return new Promise((resolve, reject) => {
    const request = requestHttp(
      {
        socketPath,
        path: "/v1/sign",
        method: "POST",
        headers: { "content-length": Buffer.byteLength(body), "content-type": "application/json" },
      },
      (response) => {
        collectJSON(response.statusCode ?? 500, response, resolve, reject);
      },
    );
    request.once("error", reject);
    request.end(body);
  });
}

function platformHostJSON(executable: string, value: unknown): Promise<unknown> {
  return new Promise((resolve, reject) => {
    const child = spawn(executable, ["__sign"], { stdio: ["pipe", "pipe", "pipe"] });
    const stdout: Buffer[] = [];
    const stderr: Buffer[] = [];
    let bytes = 0;
    child.stdout.on("data", (chunk: Buffer) => {
      bytes += chunk.length;
      if (bytes > 256 << 10) child.kill();
      else stdout.push(chunk);
    });
    child.stderr.on("data", (chunk: Buffer) => stderr.push(chunk));
    child.once("error", reject);
    child.once("close", (code) => {
      if (code !== 0) {
        reject(new Error(`platform signer failed with ${code}: ${Buffer.concat(stderr).toString("utf8").trim()}`));
        return;
      }
      try {
        resolve(JSON.parse(Buffer.concat(stdout).toString("utf8")) as unknown);
      } catch (error) {
        reject(error);
      }
    });
    child.stdin.end(JSON.stringify(value));
  });
}

function collectJSON(
  status: number,
  response: NodeJS.ReadableStream,
  resolve: (value: unknown) => void,
  reject: (reason?: unknown) => void,
): void {
  const chunks: Buffer[] = [];
  response.on("data", (chunk: Buffer) => chunks.push(chunk));
  response.once("error", reject);
  response.once("end", () => {
    const raw = Buffer.concat(chunks).toString("utf8");
    if (status !== 200) {
      reject(new Error(`lifecycle signer returned ${status}: ${raw.trim()}`));
      return;
    }
    try {
      resolve(JSON.parse(raw) as unknown);
    } catch (error) {
      reject(error);
    }
  });
}

async function postJSON(url: URL, value: unknown): Promise<unknown> {
  const response = await fetch(url, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(value),
  });
  const raw = await response.text();
  if (!response.ok) throw new Error(`API UI bootstrap returned ${response.status}: ${raw}`);
  return JSON.parse(raw) as unknown;
}

function requiredEnvironment(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required for installed UI signing`);
  return value;
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("expected an object response");
  return value as Record<string, unknown>;
}

function requiredString(value: unknown, label: string): string {
  if (typeof value !== "string" || value.length === 0) throw new Error(`${label} is missing`);
  return value;
}
