import { createServer, type Server } from "node:http";
import type { HealthResponse } from "@open-cut/contracts";

export type ApiServer = {
  url: string;
  close(): Promise<void>;
};

export async function startApiServer(host = "127.0.0.1", port = 0): Promise<ApiServer> {
  const server: Server = createServer((request, response) => {
    if (request.url === "/health") {
      const body: HealthResponse = { ok: true, service: "api" };
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify(body));
      return;
    }
    response.writeHead(404).end();
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, host, resolve);
  });
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("API server did not bind TCP");
  return {
    url: `http://${host}:${address.port}`,
    close: () => new Promise<void>((resolve, reject) => server.close((error) => (error ? reject(error) : resolve()))),
  };
}
