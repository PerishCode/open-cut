// @vitest-environment jsdom

import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { SourceImportSurface } from "../../src/components/source-import-surface.js";

describe("SourceImportSurface", () => {
  it("passes only a browser File into the Creator import boundary", () => {
    const onSelect = vi.fn();
    render(<SourceImportSurface disabled={false} onSelect={onSelect} />);
    const file = new File(["fixture"], "fixture.mov", { type: "video/quicktime" });

    fireEvent.change(screen.getByLabelText("Drop footage here or choose a local file"), {
      target: { files: [file] },
    });

    expect(onSelect).toHaveBeenCalledWith(file);
    expect(screen.queryByText(/\/private\/|[A-Z]:\\/)).toBeNull();
  });
});
