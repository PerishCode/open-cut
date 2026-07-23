import {
  type CSSProperties,
  type KeyboardEvent,
  type PointerEvent,
  type ReactNode,
  useEffect,
  useRef,
  useState,
} from "react";

import styles from "./theme.module.css";

export type EditorShellProps = {
  brand: string;
  title: string;
  status?: ReactNode;
  actions?: ReactNode;
  sidebarLabel: string;
  sidebar: ReactNode;
  viewerLabel: string;
  viewer: ReactNode;
  inspectorLabel: string;
  inspector: ReactNode;
  timelineLabel: string;
  timeline: ReactNode;
  timelineScrollKey?: string;
};

type ResizeTarget = "source" | "agent" | "timeline";

const sourceMinimum = 240;
const agentMinimum = 320;
const mainMinimum = 560;
const viewerMinimum = 260;
const timelineMinimum = 220;
const gutterSize = 10;

export function EditorShell({
  brand,
  title,
  status,
  actions,
  sidebarLabel,
  sidebar,
  viewerLabel,
  viewer,
  inspectorLabel,
  inspector,
  timelineLabel,
  timeline,
  timelineScrollKey,
}: EditorShellProps) {
  const shellRef = useRef<HTMLElement>(null);
  const mainRef = useRef<HTMLElement>(null);
  const timelineContentRef = useRef<HTMLDivElement>(null);
  const [sourceWidth, setSourceWidth] = useState(288);
  const [agentWidth, setAgentWidth] = useState(360);
  const [timelineHeight, setTimelineHeight] = useState(292);

  useEffect(() => {
    if (timelineScrollKey !== undefined && timelineContentRef.current) {
      timelineContentRef.current.scrollTop = 0;
    }
  }, [timelineScrollKey]);

  const resize = (target: ResizeTarget, clientX: number, clientY: number) => {
    const shell = shellRef.current?.getBoundingClientRect();
    const main = mainRef.current?.getBoundingClientRect();
    if (!shell || !main) return;
    if (target === "source") {
      setSourceWidth(clamp(clientX - shell.left, sourceMinimum, shell.width - agentWidth - mainMinimum - gutterSize));
      return;
    }
    if (target === "agent") {
      setAgentWidth(clamp(shell.right - clientX, agentMinimum, shell.width - sourceWidth - mainMinimum - gutterSize));
      return;
    }
    setTimelineHeight(clamp(main.bottom - clientY, timelineMinimum, main.height - viewerMinimum - gutterSize));
  };

  const style = {
    "--oc-editor-source-width": `${sourceWidth}px`,
    "--oc-editor-agent-width": `${agentWidth}px`,
    "--oc-editor-timeline-height": `${timelineHeight}px`,
  } as CSSProperties;

  return (
    <main aria-label="Creator workspace" className={styles.editorShell} ref={shellRef}>
      <header className={styles.editorHeader}>
        <span className={styles.editorBrand}>{brand}</span>
        <h1 className={styles.editorTitle}>{title}</h1>
        <div className={styles.editorStatus}>{status}</div>
        <div className={styles.editorActions}>{actions}</div>
      </header>
      <section className={styles.editorWorkspace} style={style}>
        <EditorPane label={sidebarLabel} tone="sidebar">
          {sidebar}
        </EditorPane>
        <ResizeHandle
          label={`Resize ${sidebarLabel}`}
          maximum={480}
          minimum={sourceMinimum}
          onResize={(x, y) => resize("source", x, y)}
          orientation="vertical"
          value={sourceWidth}
        />
        <section className={styles.editorMain} ref={mainRef}>
          <EditorPane label={viewerLabel} tone="viewer">
            {viewer}
          </EditorPane>
          <ResizeHandle
            label={`Resize ${timelineLabel}`}
            maximum={520}
            minimum={timelineMinimum}
            onResize={(x, y) => resize("timeline", x, y)}
            orientation="horizontal"
            value={timelineHeight}
          />
          <section aria-label={timelineLabel} className={styles.editorTimeline}>
            <div className={styles.editorPaneHeader}>{timelineLabel}</div>
            <div className={styles.editorTimelineContent} ref={timelineContentRef}>
              {timeline}
            </div>
          </section>
        </section>
        <ResizeHandle
          label={`Resize ${inspectorLabel}`}
          maximum={520}
          minimum={agentMinimum}
          onResize={(x, y) => resize("agent", x, y)}
          orientation="vertical"
          value={agentWidth}
        />
        <EditorPane label={inspectorLabel} tone="inspector">
          {inspector}
        </EditorPane>
      </section>
    </main>
  );
}

type EditorPaneProps = {
  children: ReactNode;
  label: string;
  tone: "sidebar" | "viewer" | "inspector";
};

function EditorPane({ children, label, tone }: EditorPaneProps) {
  const className =
    tone === "viewer" ? styles.editorViewer : tone === "inspector" ? styles.editorInspector : styles.editorSidebar;
  return (
    <section aria-label={label} className={className}>
      <div className={styles.editorPaneHeader}>{label}</div>
      <div className={styles.editorPaneContent}>{children}</div>
    </section>
  );
}

function ResizeHandle({
  label,
  maximum,
  minimum,
  onResize,
  orientation,
  value,
}: {
  label: string;
  maximum: number;
  minimum: number;
  onResize(clientX: number, clientY: number): void;
  orientation: "horizontal" | "vertical";
  value: number;
}) {
  const move = (event: PointerEvent<HTMLElement>) => {
    if (event.currentTarget.hasPointerCapture(event.pointerId)) onResize(event.clientX, event.clientY);
  };
  const key = (event: KeyboardEvent<HTMLElement>) => {
    const delta = event.shiftKey ? 32 : 12;
    if (orientation === "vertical" && (event.key === "ArrowLeft" || event.key === "ArrowRight")) {
      event.preventDefault();
      const direction = event.key === "ArrowLeft" ? -delta : delta;
      const rect = event.currentTarget.getBoundingClientRect();
      onResize(rect.left + direction, rect.top);
    }
    if (orientation === "horizontal" && (event.key === "ArrowUp" || event.key === "ArrowDown")) {
      event.preventDefault();
      const direction = event.key === "ArrowUp" ? -delta : delta;
      const rect = event.currentTarget.getBoundingClientRect();
      onResize(rect.left, rect.top + direction);
    }
  };
  return (
    <hr
      aria-label={label}
      aria-orientation={orientation}
      aria-valuemax={maximum}
      aria-valuemin={minimum}
      aria-valuenow={Math.round(value)}
      className={orientation === "vertical" ? styles.editorResizeVertical : styles.editorResizeHorizontal}
      tabIndex={0}
      onKeyDown={key}
      onPointerDown={(event) => event.currentTarget.setPointerCapture(event.pointerId)}
      onPointerMove={move}
    />
  );
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(Math.max(value, minimum), Math.max(minimum, maximum));
}
