import { mkdtempSync } from "node:fs";
import { createServer, type IncomingMessage, type RequestListener, type Server, type ServerResponse } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { bootstrapDevelopmentUISession } from "../../sidecar/ui-session.js";

const servers: Server[] = [];

afterEach(async () => {
  await Promise.all(servers.splice(0).map((server) => new Promise<void>((resolve) => server.close(() => resolve()))));
});

describe("development UI session bootstrap", () => {
  it("signs the API challenge through the lifecycle Unix socket", async () => {
    const socket = join(mkdtempSync(join(tmpdir(), "open-cut-signer-")), "signer.sock");
    const signer = createServer(
      asyncHandler(async (request, response) => {
        const body = await readJSON(request);
        expect(body).toEqual({ schema: 1, role: "first-party-ui", payload: "Y2Fub25pY2Fs" });
        response.setHeader("content-type", "application/json");
        response.end(
          JSON.stringify({
            schema: 1,
            installationId: "installation-web-test",
            installationGeneration: 3,
            role: "first-party-ui",
            signature: "signed-challenge",
          }),
        );
      }),
    );
    servers.push(signer);
    await new Promise<void>((resolve, reject) => {
      signer.once("error", reject);
      signer.listen(socket, resolve);
    });

    const api = await listenTCP(
      asyncHandler(async (request, response) => {
        const body = await readJSON(request);
        response.setHeader("content-type", "application/json");
        if (request.url === "/v1/auth/ui/challenges") {
          expect(body).toMatchObject({ origin: "http://127.0.0.1:4173" });
          response.end(
            JSON.stringify({
              schema: "open-cut/ui-challenge/v1",
              nonce: "nonce",
              installationId: "installation-web-test",
              installationGeneration: 3,
              role: "first-party-ui",
              signingPayload: "Y2Fub25pY2Fs",
            }),
          );
          return;
        }
        expect(request.url).toBe("/v1/auth/ui/sessions");
        expect(body).toEqual({ nonce: "nonce", signature: "signed-challenge" });
        response.end(
          JSON.stringify({
            schema: "open-cut/ui-session/v1",
            session: "oc_ui_api-session",
            expiresAt: "2027-07-14T12:00:00Z",
          }),
        );
      }),
    );

    const session = await bootstrapDevelopmentUISession(api, "http://127.0.0.1:4173", socket);
    expect(session.apiSession).toBe("oc_ui_api-session");
    expect(session.browserBinding).toMatch(/^[A-Za-z0-9_-]{43}$/);
  });
});

async function listenTCP(handler: RequestListener): Promise<string> {
  const server = createServer(handler);
  servers.push(server);
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("server did not bind TCP");
  return `http://127.0.0.1:${address.port}/`;
}

async function readJSON(request: IncomingMessage): Promise<unknown> {
  const chunks: Buffer[] = [];
  for await (const chunk of request) chunks.push(Buffer.from(chunk));
  return JSON.parse(Buffer.concat(chunks).toString("utf8")) as unknown;
}

function asyncHandler(handler: (request: IncomingMessage, response: ServerResponse) => Promise<void>): RequestListener {
  return (request, response) => {
    void handler(request, response).catch((error: unknown) => response.destroy(error as Error));
  };
}
