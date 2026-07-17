import { type ReactNode, useState } from "react";

import styles from "./theme.module.css";

export type TabDefinition = {
  id: string;
  label: string;
  content: ReactNode;
};

export type TabsProps = {
  label: string;
  tabs: TabDefinition[];
  initialTabId?: string;
};

export function Tabs({ label, tabs, initialTabId }: TabsProps) {
  const [activeId, setActiveId] = useState(initialTabId ?? tabs[0]?.id);
  const active = tabs.find((tab) => tab.id === activeId) ?? tabs[0];
  if (!active) return null;
  return (
    <div className={styles.tabs}>
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
            onClick={() => setActiveId(tab.id)}
          >
            {tab.label}
          </button>
        ))}
      </div>
      <div
        aria-labelledby={`tab-${active.id}`}
        className={styles.tabPanel}
        id={`tab-panel-${active.id}`}
        role="tabpanel"
      >
        {active.content}
      </div>
    </div>
  );
}
