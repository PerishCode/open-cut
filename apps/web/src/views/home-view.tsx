import { Surface } from "@open-cut/components";
import { type DurableID, useProjects } from "@open-cut/contracts";
import { useState } from "react";

import { CreatorWorkspace } from "../components/creator-workspace.js";
import { RuntimeSummary } from "../components/runtime-summary.js";

export function HomeView() {
  const projects = useProjects();
  const [selectedId, setSelectedId] = useState<DurableID>();
  const [showProjects, setShowProjects] = useState(false);
  const selected = projects.projects.find((project) => project.id === selectedId);
  const project = showProjects
    ? undefined
    : (selected ?? (projects.projects.length === 1 ? projects.projects[0] : undefined));
  if (project) {
    return <CreatorWorkspace key={project.id} project={project} onExit={() => setShowProjects(true)} />;
  }
  return (
    <Surface label="Open Cut runtime">
      <RuntimeSummary
        onOpen={(projectId) => {
          setSelectedId(projectId);
          setShowProjects(false);
        }}
      />
    </Surface>
  );
}
