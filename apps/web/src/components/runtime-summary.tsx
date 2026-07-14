import { Button, Heading, Stack, Status, Text } from "@open-cut/components";
import { useProjects, usePutProject } from "@open-cut/contracts";

export function RuntimeSummary() {
  const projects = useProjects();
  const write = usePutProject();
  const status = projects.status === "connecting" ? "pending" : projects.status;
  const names = projects.projects.map((project) => project.name).join(", ");

  return (
    <Stack>
      <Text tone="eyebrow">OPEN CUT · DAY 0</Text>
      <Heading>Peer sidecars, one control plane.</Heading>
      <Text>
        React consumes product read and write ports from Contracts. OpenAPI and the reconnecting event stream stay
        encapsulated behind that boundary.
      </Text>
      <Status state={status}>
        {projects.status === "ready" ? `Projects synchronized at revision ${projects.revision}` : `Projects ${status}`}
      </Status>
      <Text>{names ? `Projects: ${names}` : "No projects yet."}</Text>
      <Button
        disabled={write.pending}
        onPress={() =>
          void write.put({ id: "day-0", name: "Day 0", description: "Contracts cold-start validation project" })
        }
      >
        {write.pending ? "Saving…" : "Create Day 0 project"}
      </Button>
    </Stack>
  );
}
