// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ProductAvailability } from "../../src/components/product-availability.js";

describe("ProductAvailability", () => {
  it("turns invalid resource closure states into repair guidance", () => {
    render(
      <ProductAvailability
        onRetry={vi.fn()}
        state={{
          status: "ready",
          snapshot: {
            schema: "open-cut/product-status/v1",
            features: [
              {
                feature: "local-transcription",
                state: "unavailable",
                reason: "invalid-closure",
              },
              {
                feature: "sequence-export",
                state: "unavailable",
                reason: "invalid-closure",
              },
            ],
          },
        }}
      />,
    );

    expect(screen.getByText("Local transcription · Local transcription files need repair")).toBeTruthy();
    expect(screen.getByText("Final sequence export · Local media tools need repair")).toBeTruthy();
    expect(screen.queryByText(/closure/i)).toBeNull();
  });
});
