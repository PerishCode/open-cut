import { Button, Heading, ProjectList, Stack, Status, Text, TextField } from "@open-cut/components";
import { useCreateProject, useProjects } from "@open-cut/contracts";
import { useState } from "react";

import { AgentAccess } from "./agent-access.js";

export function RuntimeSummary({ onOpen }: { onOpen?: (projectId: string) => void }) {
  const projects = useProjects();
  const write = useCreateProject();
  const [name, setName] = useState("Untitled story");
  const status = projects.status === "connecting" ? "pending" : projects.status;

  return (
    <Stack>
      <Text tone="eyebrow">OPEN CUT</Text>
      <Heading>Start with a story.</Heading>
      <Text>
        Every project begins with a narrative document, an exact main sequence, and video, audio, and caption tracks.
      </Text>
      <Status state={status}>
        {projects.status === "ready"
          ? `Workspace synchronized at activity ${projects.activityCursor}`
          : `Workspace ${status}`}
      </Status>
      {onOpen && projects.projects.length > 0 ? (
        <ProjectList
          label="Projects"
          projects={projects.projects.map((project) => ({ id: project.id, name: project.name }))}
          onOpen={onOpen}
        />
      ) : null}
      {projects.status === "ready" && projects.projects.length === 0 ? (
        <Text>No projects yet — name your first story below.</Text>
      ) : null}
      <TextField
        disabled={write.pending}
        label="Project name"
        maxLength={200}
        placeholder="Product demo, short film, essay…"
        value={name}
        onChange={setName}
      />
      <Button
        disabled={write.pending || name.trim().length === 0}
        onPress={() => void write.create({ requestId: `ui:create-project:${crypto.randomUUID()}`, name })}
      >
        {write.pending ? "Creating…" : "Create project"}
      </Button>
      {write.error ? <Text>Could not create project: {write.error.message}</Text> : null}
      <AgentAccess />
    </Stack>
  );
}
