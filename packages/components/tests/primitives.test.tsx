import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { FileField, Heading, MediaPlayer, Status, Surface, TextAreaField, TextField } from "../src/index.js";

describe("atomic components", () => {
  it("provides semantic structure without consumer styling props", () => {
    render(
      <Surface label="Workspace">
        <Heading>Open Cut</Heading>
        <Status state="ready">Ready</Status>
        <TextField label="Project name" value="Story" onChange={() => undefined} />
      </Surface>,
    );
    expect(screen.getByRole("main", { name: "Workspace" })).toBeTruthy();
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Open Cut");
    expect(screen.getByRole("status").textContent).toContain("Ready");
    expect(screen.getByRole("textbox", { name: "Project name" })).toBeTruthy();
  });

  it("owns the native browser media surface behind one semantic atom", () => {
    const onActuator = vi.fn();
    const view = render(
      <MediaPlayer label="Source preview" mimeType="video/webm" onActuator={onActuator} source="/api/media/opaque" />,
    );
    const player = screen.getByLabelText("Source preview");
    expect(player.tagName).toBe("VIDEO");
    expect(player.getAttribute("src")).toBe("/api/media/opaque");
    const actuator = onActuator.mock.calls.at(-1)?.[0];
    actuator.seekToSeconds(1.25);
    expect(actuator.readCurrentTimeSeconds()).toBe(1.25);
    view.unmount();
    expect(onActuator).toHaveBeenLastCalledWith(undefined);
  });

  it("owns bounded multiline text input as a semantic atom", () => {
    const onChange = vi.fn();
    render(<TextAreaField label="Agent task" maxLength={8000} rows={5} value="Draft" onChange={onChange} />);
    const input = screen.getByRole("textbox", { name: "Agent task" });
    expect(input.getAttribute("maxlength")).toBe("8000");
    expect(input.getAttribute("rows")).toBe("5");
    fireEvent.change(input, { target: { value: "Draft a clear opening" } });
    expect(onChange).toHaveBeenCalledWith("Draft a clear opening");
  });

  it("normalizes file selection and drop behind one semantic atom", () => {
    const onSelect = vi.fn();
    render(<FileField label="Drop footage or browse" accept="video/*,audio/*" onSelect={onSelect} />);
    const input = screen.getByLabelText("Drop footage or browse");
    const selected = new File(["selected"], "selected.mov", { type: "video/quicktime" });
    fireEvent.change(input, { target: { files: [selected] } });
    expect(onSelect).toHaveBeenLastCalledWith(selected);

    const dropped = new File(["dropped"], "dropped.wav", { type: "audio/wav" });
    fireEvent.drop(screen.getByText("Drop footage or browse"), { dataTransfer: { files: [dropped] } });
    expect(onSelect).toHaveBeenLastCalledWith(dropped);
  });
});
