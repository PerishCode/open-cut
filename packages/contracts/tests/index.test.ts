import { describe, expect, it } from "vitest";

import { runtimePeer } from "../src/index.js";

describe("product runtime peer contracts", () => {
  it("keeps Web discovery identifiers in one pure public contract", () => {
    expect(runtimePeer.web).toEqual({ app: "web", httpEndpoint: "http" });
  });
});
