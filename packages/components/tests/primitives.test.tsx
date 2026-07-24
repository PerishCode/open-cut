import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  Button,
  ControlStrip,
  EditorShell,
  EditorSplit,
  FileField,
  Heading,
  MediaPlayer,
  MessageContent,
  PanelDock,
  ResourceCard,
  Status,
  Surface,
  Tabs,
  TextAreaField,
  TextField,
  TimelineSurface,
  TokenSelection,
} from "../src/index.js";

describe("atomic components", () => {
  afterEach(() => {
    cleanup();
  });

  it("exposes a closed visual hierarchy without changing native button behavior", () => {
    const onPress = vi.fn();
    render(
      <>
        <Button label="Commit edit" variant="primary" onPress={onPress}>
          Commit
        </Button>
        <Button pressed onPress={onPress}>
          Review
        </Button>
        <Button variant="quiet" onPress={onPress}>
          Refresh
        </Button>
        <Button disabled variant="danger" onPress={onPress}>
          Delete
        </Button>
      </>,
    );

    const buttons = screen.getAllByRole("button");
    expect(new Set(buttons.map((button) => button.className)).size).toBe(4);
    const commit = screen.getByRole("button", { name: "Commit edit" });
    expect(commit.textContent).toBe("Commit");
    fireEvent.click(commit);
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    expect(onPress).toHaveBeenCalledTimes(3);
    expect(screen.getByRole("button", { name: "Review" }).getAttribute("aria-pressed")).toBe("true");
  });

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
    const onPlaybackPosition = vi.fn();
    const onReady = vi.fn();
    const view = render(
      <MediaPlayer
        label="Source preview"
        mimeType="video/webm"
        onActuator={onActuator}
        onPlaybackPosition={onPlaybackPosition}
        onReady={onReady}
        source="/api/media/opaque"
        transport={<span>Persistent transport</span>}
      />,
    );
    const player = screen.getByLabelText("Source preview");
    expect(player.tagName).toBe("VIDEO");
    expect(player.getAttribute("src")).toBe("/api/media/opaque");
    const transport = screen.getByText("Persistent transport");
    expect(player.nextElementSibling?.contains(transport)).toBe(true);
    const actuator = onActuator.mock.calls.at(-1)?.[0];
    actuator.seekToSeconds(1.25);
    expect(actuator.readCurrentTimeSeconds()).toBe(1.25);
    fireEvent.loadedMetadata(player);
    expect(onReady).toHaveBeenCalledTimes(1);
    fireEvent.timeUpdate(player);
    expect(onPlaybackPosition).toHaveBeenLastCalledWith(1.25);
    Object.defineProperty(player, "currentTime", { configurable: true, value: 2.5, writable: true });
    fireEvent.pause(player);
    expect(onPlaybackPosition).toHaveBeenLastCalledWith(2.5);
    view.unmount();
    expect(onActuator).toHaveBeenLastCalledWith(undefined);
  });

  it("owns bounded multiline text input as a semantic atom", () => {
    const onChange = vi.fn();
    render(
      <TextAreaField
        keyboardShortcuts="Control+Enter Meta+Enter"
        label="Agent task"
        maxLength={8000}
        rows={5}
        value="Draft"
        onChange={onChange}
      />,
    );
    const input = screen.getByRole("textbox", { name: "Agent task" });
    expect(input.getAttribute("aria-keyshortcuts")).toBe("Control+Enter Meta+Enter");
    expect(input.getAttribute("maxlength")).toBe("8000");
    expect(input.getAttribute("rows")).toBe("5");
    fireEvent.change(input, { target: { value: "Draft a clear opening" } });
    expect(onChange).toHaveBeenCalledWith("Draft a clear opening");
  });

  it("presents a safe bounded message subset without activating HTML or links", () => {
    const { container } = render(
      <MessageContent
        text={
          'Changed the ending with `edit apply`.\n\n- Kept the final beat\n- Preserved `A1`\n\n1. Review\n2. Export\n\n```json\n{"status":"ready"}\n```\n\n<a href="https://example.com">unsafe</a> [docs](https://example.com)'
        }
      />,
    );

    expect(screen.getByText("edit apply").tagName).toBe("CODE");
    const lists = screen.getAllByRole("list");
    expect(lists.map((list) => list.tagName)).toEqual(["UL", "OL"]);
    expect(within(lists[0] as HTMLElement).getAllByRole("listitem")).toHaveLength(2);
    expect(within(lists[1] as HTMLElement).getAllByRole("listitem")).toHaveLength(2);
    expect(screen.getByText('{"status":"ready"}').parentElement?.tagName).toBe("PRE");
    expect(container.querySelector('[data-language="json"]')).toBeTruthy();
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.getByText(/<a href="https:\/\/example.com">unsafe<\/a>/)).toBeTruthy();
    expect(screen.getByText(/\[docs\]\(https:\/\/example.com\)/)).toBeTruthy();
  });

  it("keeps exact transcript tokens inline, selectable, and semantically pressed", () => {
    const onSelect = vi.fn();
    render(
      <TokenSelection
        items={[
          { id: "hello", label: "Hello", selected: true, text: "Hello" },
          { id: "space", label: "space", selected: false, text: " " },
          { id: "world", label: "world", selected: false, text: "world" },
        ]}
        label="Transcript segment 1 tokens"
        onSelect={onSelect}
      />,
    );

    const group = screen.getByRole("group", { name: "Transcript segment 1 tokens" });
    expect(group.textContent).toBe("Hello␠world");
    const hello = screen.getByRole("button", { name: "Selected token 1 · Hello" });
    expect(hello.getAttribute("aria-pressed")).toBe("true");
    expect(hello.tabIndex).toBe(0);
    const world = screen.getByRole("button", { name: "Select token 3 · world" });
    expect(world.getAttribute("aria-pressed")).toBe("false");
    expect(world.tabIndex).toBe(-1);
    fireEvent.focus(hello);
    fireEvent.keyDown(hello, { key: "End" });
    expect(document.activeElement).toBe(world);
    expect(world.tabIndex).toBe(0);
    fireEvent.click(world);
    expect(onSelect).toHaveBeenCalledWith("world");
  });

  it("groups a scannable resource identity, state, detail, and actions", () => {
    const elementRef = { current: null as HTMLElement | null };
    render(
      <ResourceCard
        actions={<button type="button">Open source</button>}
        elementRef={elementRef}
        eyebrow="WebM"
        selected
        status={<Status state="ready">Ready</Status>}
        title="interview.webm"
      >
        04:12 · 1920 × 1080
      </ResourceCard>,
    );

    const card = screen.getByRole("article");
    expect(elementRef.current).toBe(card);
    expect(card.getAttribute("aria-current")).toBe("true");
    expect(within(card).getByText("interview.webm")).toBeTruthy();
    expect(within(card).getByRole("status").textContent).toContain("Ready");
    expect(within(card).getByRole("button", { name: "Open source" })).toBeTruthy();
  });

  it("offers closed card emphasis without changing article semantics", () => {
    render(
      <>
        <ResourceCard title="Default card" />
        <ResourceCard emphasis="quiet" title="Quiet card" />
        <ResourceCard emphasis="strong" title="Strong card" />
      </>,
    );

    const cards = ["Default card", "Quiet card", "Strong card"].map(
      (title) => screen.getByText(title).closest("article") as HTMLElement,
    );
    expect(cards.every((card) => card.tagName === "ARTICLE")).toBe(true);
    expect(new Set(cards.map((card) => card.className)).size).toBe(3);
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

  it("owns a bounded primary and secondary editor work area", () => {
    render(
      <EditorSplit
        primary="Preview and range"
        primaryLabel="Source preview"
        secondary="Placement settings"
        secondaryLabel="Source placement"
      />,
    );

    expect(screen.getByRole("region", { name: "Source preview" }).textContent).toBe("Preview and range");
    expect(screen.getByRole("complementary", { name: "Source placement" }).textContent).toBe("Placement settings");
  });

  it("offers compact semantic tabs for nested editor modes", () => {
    render(
      <Tabs
        density="compact"
        initialTabId="range"
        label="Source viewer panels"
        tabs={[
          { id: "range", label: "Range", content: "Source range" },
          { id: "streams", label: "Streams", content: "Source streams" },
        ]}
      />,
    );

    const range = screen.getByRole("tab", { name: "Range" });
    const streams = screen.getByRole("tab", { name: "Streams" });
    expect(range.getAttribute("aria-selected")).toBe("true");
    const panel = screen.getByRole("tabpanel");
    panel.scrollTop = 120;
    fireEvent.click(streams);
    expect(streams.getAttribute("aria-selected")).toBe("true");
    expect(panel.textContent).toBe("Source streams");
    expect(panel.scrollTop).toBe(0);
  });

  it("normalizes file selection and drop behind one semantic atom", () => {
    const onSelect = vi.fn();
    render(<FileField label="Drop footage or browse" accept="video/*,audio/*" onSelect={onSelect} />);
    const input = screen.getByLabelText("Drop footage or browse");
    expect(screen.getByText("Choose file")).toBeTruthy();
    expect(screen.getByText("No file selected")).toBeTruthy();
    const selected = new File(["selected"], "selected.mov", { type: "video/quicktime" });
    fireEvent.change(input, { target: { files: [selected] } });
    expect(onSelect).toHaveBeenLastCalledWith(selected);
    expect(screen.getByText("selected.mov")).toBeTruthy();

    const dropped = new File(["dropped"], "dropped.wav", { type: "audio/wav" });
    fireEvent.drop(screen.getByText("Drop footage or browse"), { dataTransfer: { files: [dropped] } });
    expect(onSelect).toHaveBeenLastCalledWith(dropped);
    expect(screen.getByText("dropped.wav")).toBeTruthy();
  });

  it("keeps Sources, Viewer, Timeline, and Agent as one resizable editor workspace", () => {
    const props = {
      brand: "OPEN CUT",
      inspector: <span>Agent conversation</span>,
      inspectorLabel: "Agent",
      sidebar: <span>Media bin</span>,
      sidebarLabel: "Sources",
      timeline: <span>Track lanes</span>,
      timelineLabel: "Timeline",
      title: "Story",
      viewer: <span>Program picture</span>,
      viewerLabel: "Viewer",
    };
    const view = render(<EditorShell {...props} timelineScrollKey="timeline" />);

    expect(screen.getByRole("region", { name: "Sources" })).toBeTruthy();
    expect(screen.getByRole("region", { name: "Viewer" })).toBeTruthy();
    const timeline = screen.getByRole("region", { name: "Timeline" });
    expect(timeline).toBeTruthy();
    expect(screen.getByRole("region", { name: "Agent" })).toBeTruthy();
    expect(screen.getByRole("separator", { name: "Resize Sources" })).toBeTruthy();
    expect(screen.getByRole("separator", { name: "Resize Timeline" })).toBeTruthy();
    expect(screen.getByRole("separator", { name: "Resize Agent" })).toBeTruthy();
    const timelineContent = timeline.lastElementChild as HTMLElement;
    timelineContent.scrollTop = 120;
    view.rerender(<EditorShell {...props} timelineScrollKey="rough-cut" />);
    expect(timelineContent.scrollTop).toBe(0);
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
          {
            id: "guide-1",
            trackId: "audio-1",
            label: "Guide reference",
            startSeconds: 0,
            durationSeconds: 4,
            selectable: false,
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
    const toolbar = screen.getByRole("toolbar", { name: "Timeline view controls" });
    expect(toolbar.tabIndex).toBe(0);
    expect(toolbar.getAttribute("aria-keyshortcuts")).toBe("Home 0 - =");
    expect(screen.getByRole("group", { name: "Timeline zoom" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Select Opening shot on V1 at 00:05.00" }));
    expect(onItemSelect).toHaveBeenCalledWith("clip-1");
    expect(screen.getByText("Guide reference")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Guide reference/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Seek A1" }));
    expect(onSeek).toHaveBeenCalledWith(0);
    fireEvent.click(screen.getByRole("button", { name: "Zoom timeline in" }));
    expect(screen.getByText("8×")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Fit timeline" }));
    expect(screen.getByText("1×")).toBeTruthy();
    fireEvent.keyDown(toolbar, { key: "=" });
    expect(screen.getByText("2×")).toBeTruthy();
    fireEvent.keyDown(toolbar, { key: "-" });
    expect(screen.getByText("1×")).toBeTruthy();
    onSeek.mockClear();
    fireEvent.keyDown(toolbar, { key: "Home" });
    expect(onSeek).toHaveBeenCalledWith(0);
    fireEvent.keyDown(screen.getByRole("button", { name: "Zoom timeline in" }), { key: "=" });
    expect(screen.getByText("1×")).toBeTruthy();
  });

  it("exposes move and trim affordances only when gestures are enabled", () => {
    const view = render(
      <TimelineSurface
        durationSeconds={60}
        itemGesturesEnabled={false}
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
        onItemSelect={() => undefined}
        onSeek={() => undefined}
        playheadSeconds={7}
        tracks={[{ id: "video-1", label: "V1", kind: "video" }]}
      />,
    );

    expect(screen.queryByRole("button", { name: "Trim in Opening shot on V1 at 00:05.00" })).toBeNull();
    expect(screen.getByRole("button", { name: "Select Opening shot on V1 at 00:05.00" })).toBeTruthy();

    view.rerender(
      <TimelineSurface
        durationSeconds={60}
        itemGesturesEnabled
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
        onItemMove={() => undefined}
        onItemSelect={() => undefined}
        onItemTrimEnd={() => undefined}
        onItemTrimStart={() => undefined}
        onSeek={() => undefined}
        playheadSeconds={7}
        tracks={[{ id: "video-1", label: "V1", kind: "video" }]}
      />,
    );

    expect(screen.getByRole("button", { name: "Move Opening shot on V1 at 00:05.00" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Trim in Opening shot on V1 at 00:05.00" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Trim out Opening shot on V1 at 00:05.00" })).toBeTruthy();
  });

  it("reports move and trim targets in presentation seconds and cancels on Escape", () => {
    const onItemMove = vi.fn();
    const onItemTrimStart = vi.fn();
    const onItemTrimEnd = vi.fn();
    const onItemSelect = vi.fn();
    render(
      <TimelineSurface
        durationSeconds={100}
        itemGesturesEnabled
        items={[
          {
            id: "clip-1",
            trackId: "video-1",
            label: "Opening shot",
            startSeconds: 10,
            durationSeconds: 20,
            selected: true,
            linked: true,
          },
        ]}
        onItemMove={onItemMove}
        onItemSelect={onItemSelect}
        onItemTrimEnd={onItemTrimEnd}
        onItemTrimStart={onItemTrimStart}
        onSeek={() => undefined}
        playheadSeconds={12}
        tracks={[{ id: "video-1", label: "V1", kind: "video" }]}
      />,
    );

    const lane = document.querySelector("[data-timeline-lane='video-1']");
    expect(lane).toBeTruthy();
    vi.spyOn(lane as HTMLElement, "getBoundingClientRect").mockReturnValue({
      x: 0,
      y: 0,
      top: 0,
      left: 0,
      bottom: 40,
      right: 1000,
      width: 1000,
      height: 40,
      toJSON: () => ({}),
    });

    const body = screen.getByRole("button", { name: "Move Opening shot on V1 at 00:10.00" });
    fireEvent.pointerDown(body, { pointerId: 1, button: 0, clientX: 100, clientY: 10 });
    fireEvent.pointerMove(body, { pointerId: 1, clientX: 250, clientY: 10 });
    fireEvent.pointerUp(body, { pointerId: 1, clientX: 250, clientY: 10 });
    // Native browsers synthesize click after pointer-up; it must not reselect mid-commit.
    fireEvent.click(body);
    expect(onItemMove).toHaveBeenCalledTimes(1);
    expect(onItemMove).toHaveBeenCalledWith("clip-1", 25);
    expect(onItemSelect).not.toHaveBeenCalled();

    const trimIn = screen.getByRole("button", { name: "Trim in Opening shot on V1 at 00:10.00" });
    fireEvent.pointerDown(trimIn, { pointerId: 2, button: 0, clientX: 100, clientY: 10 });
    fireEvent.pointerMove(trimIn, { pointerId: 2, clientX: 150, clientY: 10 });
    fireEvent.pointerUp(trimIn, { pointerId: 2, clientX: 150, clientY: 10 });
    expect(onItemTrimStart).toHaveBeenCalledWith("clip-1", 15);

    const trimOut = screen.getByRole("button", { name: "Trim out Opening shot on V1 at 00:10.00" });
    fireEvent.pointerDown(trimOut, { pointerId: 3, button: 0, clientX: 300, clientY: 10 });
    fireEvent.pointerMove(trimOut, { pointerId: 3, clientX: 250, clientY: 10 });
    fireEvent.pointerUp(trimOut, { pointerId: 3, clientX: 250, clientY: 10 });
    expect(onItemTrimEnd).toHaveBeenCalledWith("clip-1", 25);

    onItemMove.mockClear();
    fireEvent.pointerDown(body, { pointerId: 4, button: 0, clientX: 100, clientY: 10 });
    fireEvent.pointerMove(body, { pointerId: 4, clientX: 300, clientY: 10 });
    fireEvent.keyDown(window, { key: "Escape" });
    fireEvent.pointerUp(body, { pointerId: 4, clientX: 300, clientY: 10 });
    fireEvent.click(body);
    expect(onItemMove).not.toHaveBeenCalled();
    expect(onItemSelect).not.toHaveBeenCalled();
  });

  it("keeps a compact control strip near the canvas without consumer styling props", () => {
    const onKeyDown = vi.fn();
    render(
      <ControlStrip
        hint="Choose scope and Alignment"
        keyboardShortcuts="ArrowLeft ArrowRight"
        label="Timeline selection policy"
        summary="SELECTED · V1 · Timeline 00:00 → 00:02.10"
        onKeyDown={onKeyDown}
      >
        <button type="button">Linked A/V</button>
        <button type="button">Preserve</button>
      </ControlStrip>,
    );

    const strip = screen.getByRole("region", { name: "Timeline selection policy" });
    expect(within(strip).getByText("SELECTED · V1 · Timeline 00:00 → 00:02.10")).toBeTruthy();
    expect(within(strip).getByText("Choose scope and Alignment")).toBeTruthy();
    expect(within(strip).getByRole("button", { name: "Linked A/V" })).toBeTruthy();
    expect(within(strip).getByRole("button", { name: "Preserve" })).toBeTruthy();
    expect(strip.getAttribute("aria-keyshortcuts")).toBe("ArrowLeft ArrowRight");
    expect(strip.tabIndex).toBe(0);
    fireEvent.keyDown(strip, { key: "ArrowLeft" });
    expect(onKeyDown).toHaveBeenCalledTimes(1);
  });

  it("renders the policy accessory inside the same Timeline editor unit as the canvas", () => {
    render(
      <TimelineSurface
        accessory={
          <ControlStrip label="Timeline selection policy" summary="SELECTED · V1">
            <button type="button">Linked A/V</button>
            <button type="button">Preserve</button>
          </ControlStrip>
        }
        durationSeconds={60}
        items={[
          {
            id: "clip-1",
            trackId: "video-1",
            label: "Opening shot",
            startSeconds: 0,
            durationSeconds: 2.1,
            selected: true,
          },
        ]}
        onItemSelect={() => undefined}
        onSeek={() => undefined}
        playheadSeconds={0}
        tracks={[
          { id: "video-1", label: "V1", kind: "video" },
          { id: "audio-1", label: "A1", kind: "audio" },
        ]}
      />,
    );

    const editor = screen.getByRole("region", { name: "Timeline editor" });
    const canvas = within(editor).getByRole("region", { name: "Timeline canvas" });
    const accessory = editor.querySelector("[data-timeline-accessory]");
    expect(canvas.hasAttribute("data-timeline-canvas")).toBe(true);
    expect(accessory).toBeTruthy();
    expect(accessory?.parentElement).toBe(editor);
    expect(canvas.parentElement).toBe(editor);
    expect(within(editor).getByRole("region", { name: "Timeline selection policy" })).toBeTruthy();
    expect(within(editor).getByText("V1")).toBeTruthy();
    expect(within(editor).getByRole("button", { name: "Linked A/V" })).toBeTruthy();
  });
});
