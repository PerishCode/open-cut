import { randomBytes, randomUUID } from "node:crypto";
import { request as requestHttp } from "node:http";

const signerSocketEnvironment = "OC_LIFECYCLE_SIGNER_SOCKET";
const signerPath = "/v1/sign";
// Credential rotation preserves the browser proxy identity for this sidecar process.
const processClientInstance = `web-sidecar-${randomUUID()}`;

export type ProxyUISession = Readonly<{
  apiSession: string;
  browserBinding: string;
  expiresAt: number;
}>;

export async function bootstrapDevelopmentUISession(
  apiEndpoint: string,
  origin: string,
  signerSocket = process.env[signerSocketEnvironment],
  clientInstance = processClientInstance,
): Promise<ProxyUISession> {
  if (!signerSocket) throw new Error(`${signerSocketEnvironment} is required for browser development`);
  const challenge = await postJSON(new URL("v1/auth/ui/challenges", apiEndpoint), {
    clientInstance,
    origin,
  });
  const challengeRecord = asRecord(challenge);
  const signingPayload = requiredString(challengeRecord.signingPayload, "UI challenge signing payload");
  const signature = await signWithLifecycle(signerSocket, signingPayload);
  if (
    signature.installationId !== challengeRecord.installationId ||
    signature.installationGeneration !== challengeRecord.installationGeneration ||
    signature.role !== challengeRecord.role
  ) {
    throw new Error("lifecycle signer identity does not match the API challenge");
  }
  const exchanged = asRecord(
    await postJSON(new URL("v1/auth/ui/sessions", apiEndpoint), {
      nonce: requiredString(challengeRecord.nonce, "UI challenge nonce"),
      signature: signature.signature,
    }),
  );
  const expiresAt = Date.parse(requiredString(exchanged.expiresAt, "UI session expiry"));
  const apiSession = requiredString(exchanged.session, "UI session");
  if (
    exchanged.schema !== "open-cut/ui-session/v1" ||
    !apiSession.startsWith("oc_ui_") ||
    !Number.isFinite(expiresAt)
  ) {
    throw new Error("API returned an invalid UI session");
  }
  return {
    apiSession,
    browserBinding: randomBytes(32).toString("base64url"),
    expiresAt,
  };
}

type LifecycleSignature = Readonly<{
  installationId: string;
  installationGeneration: number;
  role: string;
  signature: string;
}>;

async function signWithLifecycle(socketPath: string, payload: string): Promise<LifecycleSignature> {
  if (!/^[A-Za-z0-9_-]+$/.test(payload)) throw new Error("UI challenge payload is not canonical base64url");
  const response = await unixJSON(socketPath, signerPath, {
    schema: 1,
    role: "first-party-ui",
    payload,
  });
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

function unixJSON(socketPath: string, path: string, value: unknown): Promise<unknown> {
  const body = JSON.stringify(value);
  return new Promise((resolve, reject) => {
    const request = requestHttp(
      {
        socketPath,
        path,
        method: "POST",
        headers: { "content-length": Buffer.byteLength(body), "content-type": "application/json" },
      },
      (response) => {
        const chunks: Buffer[] = [];
        response.on("data", (chunk: Buffer) => chunks.push(chunk));
        response.once("error", reject);
        response.once("end", () => {
          const raw = Buffer.concat(chunks).toString("utf8");
          if (response.statusCode !== 200) {
            reject(new Error(`lifecycle signer returned ${response.statusCode}: ${raw.trim()}`));
            return;
          }
          try {
            resolve(JSON.parse(raw) as unknown);
          } catch (error) {
            reject(error);
          }
        });
      },
    );
    request.once("error", reject);
    request.end(body);
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

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("expected an object response");
  return value as Record<string, unknown>;
}

function requiredString(value: unknown, label: string): string {
  if (typeof value !== "string" || value.length === 0) throw new Error(`${label} is missing`);
  return value;
}
