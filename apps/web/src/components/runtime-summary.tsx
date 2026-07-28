import { Button, Heading, ProjectList, Stack, Text, TextField } from "@open-cut/components";
import { type DurableID, useCreateProject, useProjects } from "@open-cut/contracts";
import { useState } from "react";

export function RuntimeSummary({
  currentProject,
  onOpen,
  onReturn,
}: {
  currentProject?: Readonly<{ id: DurableID; name: string }>;
  onOpen?: (projectId: DurableID) => void;
  onReturn?: () => void;
}) {
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
      {currentProject && onReturn ? (
        <Button variant="primary" onPress={onReturn}>
          Return to {currentProject.name}
        </Button>
      ) : null}
      {onOpen && projects.projects.length > 0 ? (
        <Stack spacing="compact">
          <Heading level={2} tone="eyebrow">
            PROJECTS
          </Heading>
          <ProjectList
            label="Projects"
            projects={projects.projects.map((project) => ({
              id: project.id,
              name: project.name,
              ...(project.id === currentProject?.id ? { meta: "Open now" } : {}),
            }))}
            onOpen={(id) => {
              const match = projects.projects.find((project) => project.id === id);
              if (match) onOpen(match.id);
            }}
          />
        </Stack>
      ) : null}
      <Stack spacing="compact">
        <Heading level={2} tone="eyebrow">
          NEW PROJECT
        </Heading>
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
          variant="primary"
          onPress={() => void createAndOpen().catch(() => undefined)}
        >
          {write.pending ? "Creating…" : "Create and open"}
        </Button>
        {write.error ? <Text>Project could not be created. Review the name and try again.</Text> : null}
      </Stack>
    </Stack>
  );
}
