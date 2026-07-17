import { Button, Heading, ProjectList, Stack, Text, TextField } from "@open-cut/components";
import { type DurableID, useCreateProject, useProjects } from "@open-cut/contracts";
import { useState } from "react";

export function RuntimeSummary({ onOpen }: { onOpen?: (projectId: DurableID) => void }) {
  const projects = useProjects();
  const write = useCreateProject();
  const [name, setName] = useState("Untitled story");

  const createAndOpen = async () => {
    const created = await write.create({ requestId: `ui:create-project:${crypto.randomUUID()}`, name });
    if (created && onOpen) onOpen(created.project.project.id);
  };

  return (
    <Stack>
      <Text tone="eyebrow">OPEN CUT</Text>
      <Heading>Start with a story.</Heading>
      <Text>
        A local workspace where you and your agent turn footage and writing into an editable, reversible video timeline.
        A project is one story: its script and its sequence, side by side.
      </Text>
      {onOpen && projects.projects.length > 0 ? (
        <ProjectList
          label="Projects"
          projects={projects.projects.map((project) => ({ id: project.id, name: project.name }))}
          onOpen={(id) => {
            const match = projects.projects.find((project) => project.id === id);
            if (match) onOpen(match.id);
          }}
        />
      ) : null}
      <TextField
        disabled={write.pending}
        label="Name your story"
        maxLength={200}
        placeholder="Product demo, short film, essay…"
        value={name}
        onChange={setName}
      />
      <Button
        disabled={write.pending || name.trim().length === 0}
        onPress={() => void createAndOpen().catch(() => undefined)}
      >
        {write.pending ? "Creating…" : "Create and open"}
      </Button>
      {write.error ? <Text>Could not create project: {write.error.message}</Text> : null}
    </Stack>
  );
}
