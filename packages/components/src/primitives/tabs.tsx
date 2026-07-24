import { type ReactNode, useEffect, useRef, useState } from "react";

import styles from "./tabs.module.css";

export type TabDefinition = {
  id: string;
  label: string;
  content: ReactNode;
};

export type TabsProps = {
  label: string;
  tabs: TabDefinition[];
  density?: "default" | "compact";
  initialTabId?: string;
  activeTabId?: string;
  onTabChange?(tabId: string): void;
};

export function Tabs({ activeTabId, density = "default", label, onTabChange, tabs, initialTabId }: TabsProps) {
  const [internalActiveId, setInternalActiveId] = useState(initialTabId ?? tabs[0]?.id);
  const activeId = activeTabId ?? internalActiveId;
  const active = tabs.find((tab) => tab.id === activeId) ?? tabs[0];
  const activePanelRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (activePanelRef.current) activePanelRef.current.scrollTop = 0;
  }, [active?.id]);
  if (!active) return null;
  return (
    <div className={density === "compact" ? `${styles.tabs} ${styles.tabsCompact}` : styles.tabs}>
      <div aria-label={label} className={styles.tabList} role="tablist">
        {tabs.map((tab) => (
          <button
            aria-controls={`tab-panel-${tab.id}`}
            aria-selected={tab.id === active.id}
            className={tab.id === active.id ? `${styles.tab} ${styles.tabActive}` : styles.tab}
            id={`tab-${tab.id}`}
            key={tab.id}
            role="tab"
            type="button"
            onClick={() => {
              if (activeTabId === undefined) setInternalActiveId(tab.id);
              onTabChange?.(tab.id);
            }}
          >
            {tab.label}
          </button>
        ))}
      </div>
      <div
        aria-labelledby={`tab-${active.id}`}
        className={styles.tabPanel}
        id={`tab-panel-${active.id}`}
        ref={activePanelRef}
        role="tabpanel"
      >
        {active.content}
      </div>
    </div>
  );
}
