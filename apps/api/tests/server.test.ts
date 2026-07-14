import { afterEach, describe, expect, it } from "vitest";

import { startApiServer, type ApiServer } from "../src/server.js";

let server: ApiServer | undefined;

afterEach(async () => {
  await server?.close();
  server = undefined;
});

describe("API server", () => {
  it("serves the app health contract", async () => {
    server = await startApiServer();
    const response = await fetch(`${server.url}/health`);
    expect(response.status).toBe(200);
    await expect(response.json()).resolves.toEqual({ ok: true, service: "api" });
  });
});
