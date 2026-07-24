import { Button, Stack, Status, Text } from "@open-cut/components";
import { useCLIAuthorization } from "@open-cut/contracts";

export function AgentAccess() {
  const cli = useCLIAuthorization();
  const pendingPairings = cli.pairings.filter((pairing) => pairing.status === "pending");
  const pendingUpgrades = cli.scopeUpgrades.filter((upgrade) => upgrade.status === "pending");
  const activePairings = cli.pairings.filter((pairing) => pairing.status === "active");
  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">LOCAL AGENT CLI</Text>
      {pendingPairings.map((pairing) => {
        const request = cliScopeSummary(pairing.scopes);
        return (
          <Stack key={pairing.id} spacing="compact">
            <Status state="pending">New CLI access request</Status>
            <Text>{request.text}</Text>
            {!request.supported ? (
              <Status state="unavailable">Update Open Cut before reviewing additional permissions.</Status>
            ) : null}
            <Button
              disabled={cli.pending || !request.supported}
              onPress={() => void cli.approve(pairing.id).catch(() => undefined)}
            >
              Approve CLI
            </Button>
            <Button disabled={cli.pending} onPress={() => void cli.deny(pairing.id).catch(() => undefined)}>
              Deny CLI
            </Button>
          </Stack>
        );
      })}
      {pendingUpgrades.map((upgrade) => {
        const pairing = cli.pairings.find((candidate) => candidate.id === upgrade.grantId);
        const request = cliScopeSummary(upgrade.requestedScopes);
        return (
          <Stack key={upgrade.id} spacing="compact">
            <Status state="pending">CLI permission upgrade</Status>
            {pairing ? <Text>Current access · {cliScopeSummary(pairing.scopes).text}</Text> : null}
            <Text>Requested access · {request.text}</Text>
            {!request.supported ? (
              <Status state="unavailable">Update Open Cut before reviewing additional permissions.</Status>
            ) : null}
            <Button
              disabled={cli.pending || !request.supported}
              onPress={() => void cli.approveScopeUpgrade(upgrade.id).catch(() => undefined)}
            >
              Approve scope upgrade
            </Button>
            <Button disabled={cli.pending} onPress={() => void cli.denyScopeUpgrade(upgrade.id).catch(() => undefined)}>
              Deny scope upgrade
            </Button>
          </Stack>
        );
      })}
      {activePairings.map((pairing) => (
        <Stack key={pairing.id} spacing="compact">
          <Status state="ready">CLI access active</Status>
          <Text>{cliScopeSummary(pairing.scopes).text}</Text>
          <Button
            disabled={cli.pending}
            variant="danger"
            onPress={() => void cli.revoke(pairing.id).catch(() => undefined)}
          >
            Revoke CLI access
          </Button>
        </Stack>
      ))}
      {pendingPairings.length === 0 && pendingUpgrades.length === 0 && activePairings.length === 0 ? (
        <Text>No CLI access configured.</Text>
      ) : null}
      {cli.error ? (
        <Stack spacing="compact">
          <Status state="unavailable">Could not update CLI access.</Status>
          <Button disabled={cli.pending} onPress={() => void cli.refresh().catch(() => undefined)}>
            Check again
          </Button>
        </Stack>
      ) : null}
    </Stack>
  );
}

type CLIScopeSummary = Readonly<{ text: string; supported: boolean }>;

function cliScopeSummary(scopes: readonly string[]): CLIScopeSummary {
  const unknown = scopes.filter((scope) => cliScopeLabel(scope) === undefined);
  const changes = uniqueSortedResources(scopes, "change");
  const views = uniqueSortedResources(scopes, "view").filter((resource) => !changes.includes(resource));
  const parts = [
    changes.length > 0 ? `Can change ${formatResourceList(changes)}` : undefined,
    views.length > 0 ? `Can view ${formatResourceList(views)}` : undefined,
    unknown.length > 0
      ? `${unknown.length} additional ${unknown.length === 1 ? "permission needs" : "permissions need"} review`
      : undefined,
  ].filter((part): part is string => part !== undefined);
  return { text: parts.join(" · "), supported: unknown.length === 0 };
}

function uniqueSortedResources(scopes: readonly string[], access: "change" | "view"): string[] {
  return [
    ...new Set(
      scopes.flatMap((scope) => {
        const label = cliScopeLabel(scope);
        return label?.access === access ? [label.resource] : [];
      }),
    ),
  ].sort((left, right) => left.localeCompare(right));
}

function formatResourceList(resources: readonly string[]): string {
  if (resources.length < 2) return resources[0] ?? "no product data";
  if (resources.length === 2) return `${resources[0]} and ${resources[1]}`;
  return `${resources.slice(0, -1).join(", ")}, and ${resources.at(-1)}`;
}

function cliScopeLabel(scope: string): Readonly<{ access: "change" | "view"; resource: string }> | undefined {
  switch (scope) {
    case "activity:read":
      return { access: "view", resource: "activity" };
    case "asset:read":
      return { access: "view", resource: "assets" };
    case "edit:read":
      return { access: "view", resource: "edits" };
    case "edit:write":
      return { access: "change", resource: "edits" };
    case "export:read":
      return { access: "view", resource: "exports" };
    case "export:write":
      return { access: "change", resource: "exports" };
    case "product:read":
      return { access: "view", resource: "product status" };
    case "project:read":
      return { access: "view", resource: "projects" };
    case "run:read":
      return { access: "view", resource: "Agent runs" };
    case "run:write":
      return { access: "change", resource: "Agent runs" };
    default:
      return undefined;
  }
}
