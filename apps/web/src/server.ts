import { createServer, type Server } from "node:http";

export type WebServer = {
  url: string;
  close(): Promise<void>;
};

const document = `<!doctype html><html><head><meta charset="utf-8"><title>Open Cut</title></head><body><main id="app">Open Cut</main></body></html>`;

export async function startWebServer(host = "127.0.0.1", port = 0): Promise<WebServer> {
  const server: Server = createServer((_request, response) => {
    response.writeHead(200, { "content-type": "text/html; charset=utf-8" });
    response.end(document);
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, host, resolve);
  });
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("web server did not bind TCP");
  return {
    url: `http://${host}:${address.port}`,
    close: () => new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve())),
  };
}
