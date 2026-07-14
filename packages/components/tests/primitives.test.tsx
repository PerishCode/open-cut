import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Heading, Status, Surface } from "../src/index.js";

describe("atomic components", () => {
  it("provides semantic structure without consumer styling props", () => {
    render(
      <Surface label="Workspace">
        <Heading>Open Cut</Heading>
        <Status state="ready">Ready</Status>
      </Surface>,
    );
    expect(screen.getByRole("main", { name: "Workspace" })).toBeTruthy();
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Open Cut");
    expect(screen.getByRole("status").textContent).toContain("Ready");
  });
});
