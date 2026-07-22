import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import {
  EditorShell,
  FileField,
  Heading,
  MediaPlayer,
  PanelDock,
  ResourceCard,
  Status,
  Surface,
  TextAreaField,
  TextField,
  TimelineSurface,
} from "../src/index.js";

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

  it("groups a scannable resource identity, state, detail, and actions", () => {
    render(
      <ResourceCard
        actions={<button type="button">Open source</button>}
        eyebrow="WebM"
        selected
        status={<Status state="ready">Ready</Status>}
        title="interview.webm"
      >
        04:12 · 1920 × 1080
      </ResourceCard>,
    );

    const card = screen.getByRole("article");
    expect(card.getAttribute("aria-current")).toBe("true");
    expect(within(card).getByText("interview.webm")).toBeTruthy();
    expect(within(card).getByRole("status").textContent).toContain("Ready");
    expect(within(card).getByRole("button", { name: "Open source" })).toBeTruthy();
  });

  it("keeps panel controls and composer around an independently scrolling feed", () => {
    render(
      <PanelDock footer={<button type="button">Send</button>} header="Agent ready" label="Agent collaboration">
        Conversation
      </PanelDock>,
    );

    const panel = screen.getByRole("region", { name: "Agent collaboration" });
    expect(within(panel).getByText("Agent ready")).toBeTruthy();
    expect(within(panel).getByText("Conversation")).toBeTruthy();
    expect(within(panel).getByRole("button", { name: "Send" })).toBeTruthy();
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

  it("keeps Sources, Viewer, Timeline, and Agent as one resizable editor workspace", () => {
    render(
      <EditorShell
        brand="OPEN CUT"
        inspector={<span>Agent conversation</span>}
        inspectorLabel="Agent"
        sidebar={<span>Media bin</span>}
        sidebarLabel="Sources"
        timeline={<span>Track lanes</span>}
        timelineLabel="Timeline"
        title="Story"
        viewer={<span>Program picture</span>}
        viewerLabel="Viewer"
      />,
    );

    expect(screen.getByRole("region", { name: "Sources" })).toBeTruthy();
    expect(screen.getByRole("region", { name: "Viewer" })).toBeTruthy();
    expect(screen.getByRole("region", { name: "Timeline" })).toBeTruthy();
    expect(screen.getByRole("region", { name: "Agent" })).toBeTruthy();
    expect(screen.getByRole("separator", { name: "Resize Sources" })).toBeTruthy();
    expect(screen.getByRole("separator", { name: "Resize Timeline" })).toBeTruthy();
    expect(screen.getByRole("separator", { name: "Resize Agent" })).toBeTruthy();
  });

  it("projects real tracks, items, playhead, seeking, and zoom as a spatial timeline", () => {
    const onItemSelect = vi.fn();
    const onSeek = vi.fn();
    render(
      <TimelineSurface
        durationSeconds={60}
        items={[
          {
            id: "clip-1",
            trackId: "video-1",
            label: "Opening shot",
            startSeconds: 5,
            durationSeconds: 8,
            selected: true,
          },
        ]}
        onItemSelect={onItemSelect}
        onSeek={onSeek}
        playheadSeconds={7}
        tracks={[
          { id: "video-1", label: "V1", kind: "video" },
          { id: "audio-1", label: "A1", kind: "audio" },
        ]}
      />,
    );

    expect(screen.getByText("V1")).toBeTruthy();
    expect(screen.getByText("A1")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Select Opening shot" }));
    expect(onItemSelect).toHaveBeenCalledWith("clip-1");
    fireEvent.click(screen.getByRole("button", { name: "Seek A1" }));
    expect(onSeek).toHaveBeenCalledWith(0);
    fireEvent.click(screen.getByRole("button", { name: "Zoom timeline in" }));
    expect(screen.getByText("2×")).toBeTruthy();
  });
});
