import { describe, expect, it } from "vitest";

import { CREATOR_WINDOW_CONTRACT } from "../src/main/window-contract.js";

describe("creator window contract", () => {
  it("keeps the four-region editor usable at the product minimum", () => {
    expect(CREATOR_WINDOW_CONTRACT.minimum).toEqual({
      width: 1280,
      height: 800,
    });
  });

  it("opens at the preferred editing size without weakening the minimum", () => {
    expect(CREATOR_WINDOW_CONTRACT.default.width).toBeGreaterThanOrEqual(CREATOR_WINDOW_CONTRACT.minimum.width);
    expect(CREATOR_WINDOW_CONTRACT.default.height).toBeGreaterThanOrEqual(CREATOR_WINDOW_CONTRACT.minimum.height);
    expect(CREATOR_WINDOW_CONTRACT.default).toEqual({
      width: 1440,
      height: 900,
    });
  });
});
