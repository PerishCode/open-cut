import { Surface } from "@open-cut/components";
import { useProjects } from "@open-cut/contracts";

import { CreatorWorkspace } from "../components/creator-workspace.js";
import { RuntimeSummary } from "../components/runtime-summary.js";

export function HomeView() {
  const projects = useProjects();
  const project = projects.projects[0];
  if (project) return <CreatorWorkspace project={project} />;
  return (
    <Surface label="Open Cut runtime">
      <RuntimeSummary />
    </Surface>
  );
}
