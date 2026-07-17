import { Surface } from "@open-cut/components";
import { type DurableID, useProjects } from "@open-cut/contracts";
import { useState } from "react";

import { CreatorWorkspace } from "../components/creator-workspace.js";
import { RuntimeSummary } from "../components/runtime-summary.js";

export function HomeView() {
  const projects = useProjects();
  const [selectedId, setSelectedId] = useState<DurableID>();
  const selected = projects.projects.find((project) => project.id === selectedId);
  const project = selected ?? (projects.projects.length === 1 ? projects.projects[0] : undefined);
  if (project) {
    const onExit = projects.projects.length > 1 ? () => setSelectedId(undefined) : undefined;
    return <CreatorWorkspace key={project.id} project={project} onExit={onExit} />;
  }
  return (
    <Surface label="Open Cut runtime">
      <RuntimeSummary onOpen={setSelectedId} />
    </Surface>
  );
}
