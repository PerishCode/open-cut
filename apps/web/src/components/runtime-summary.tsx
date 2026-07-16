import { Button, Heading, Stack, Status, Text, TextField } from "@open-cut/components";
import { useCreateProject, useProjects } from "@open-cut/contracts";
import { useState } from "react";

import { AgentAccess } from "./agent-access.js";

export function RuntimeSummary() {
  const projects = useProjects();
  const write = useCreateProject();
  const [name, setName] = useState("Untitled story");
  const status = projects.status === "connecting" ? "pending" : projects.status;
  const names = projects.projects.map((project) => project.name).join(", ");

  return (
    <Stack>
      <Text tone="eyebrow">OPEN CUT · DAY 0</Text>
      <Heading>Start with a story.</Heading>
      <Text>
        Every project begins with a narrative document, an exact main sequence, and video, audio, and caption tracks.
      </Text>
      <Status state={status}>
        {projects.status === "ready"
          ? `Workspace synchronized at activity ${projects.activityCursor}`
          : `Workspace ${status}`}
      </Status>
      <Text>{names ? `Projects: ${names}` : "No projects yet."}</Text>
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
