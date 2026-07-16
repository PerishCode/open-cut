import type { ReactNode } from "react";

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
};

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
}: EditorShellProps) {
  return (
    <main aria-label="Creator workspace" className={styles.editorShell}>
      <header className={styles.editorHeader}>
        <span className={styles.editorBrand}>{brand}</span>
        <h1 className={styles.editorTitle}>{title}</h1>
        <div className={styles.editorStatus}>{status}</div>
        <div className={styles.editorActions}>{actions}</div>
      </header>
      <section className={styles.editorBody}>
        <EditorPane label={sidebarLabel} tone="sidebar">
          {sidebar}
        </EditorPane>
        <EditorPane label={viewerLabel} tone="viewer">
          {viewer}
        </EditorPane>
        <EditorPane label={inspectorLabel} tone="inspector">
          {inspector}
        </EditorPane>
      </section>
      <section aria-label={timelineLabel} className={styles.editorTimeline}>
        <div className={styles.editorPaneHeader}>{timelineLabel}</div>
        <div className={styles.editorTimelineContent}>{timeline}</div>
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
