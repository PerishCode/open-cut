import { Button, Stack, Text } from "@open-cut/components";
import { useCLIAuthorization } from "@open-cut/contracts";

export function AgentAccess() {
  const cli = useCLIAuthorization();
  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">LOCAL AGENT CLI</Text>
      {cli.pairings
        .filter((pairing) => pairing.status === "pending")
        .map((pairing) => (
          <Stack key={pairing.id} spacing="compact">
            <Text>Pending key {pairing.publicKeyFingerprint.slice(0, 18)}…</Text>
            <Text>Requested scopes: {pairing.scopes.join(", ")}</Text>
            <Button disabled={cli.pending} onPress={() => void cli.approve(pairing.id)}>
              Approve CLI
            </Button>
            <Button disabled={cli.pending} onPress={() => void cli.deny(pairing.id)}>
              Deny CLI
            </Button>
          </Stack>
        ))}
      {cli.scopeUpgrades
        .filter((upgrade) => upgrade.status === "pending")
        .map((upgrade) => {
          const pairing = cli.pairings.find((candidate) => candidate.id === upgrade.grantId);
          return (
            <Stack key={upgrade.id} spacing="compact">
              <Text>
                Scope upgrade for {pairing ? `${pairing.publicKeyFingerprint.slice(0, 18)}…` : upgrade.grantId}
              </Text>
              <Text>Current grant revision: {upgrade.fromRevision}</Text>
              <Text>Requested scopes: {upgrade.requestedScopes.join(", ")}</Text>
              <Button disabled={cli.pending} onPress={() => void cli.approveScopeUpgrade(upgrade.id)}>
                Approve scope upgrade
              </Button>
              <Button disabled={cli.pending} onPress={() => void cli.denyScopeUpgrade(upgrade.id)}>
                Deny scope upgrade
              </Button>
            </Stack>
          );
        })}
      {cli.pairings
        .filter((pairing) => pairing.status === "active")
        .map((pairing) => (
          <Stack key={pairing.id} spacing="compact">
            <Text>Active key {pairing.publicKeyFingerprint.slice(0, 18)}…</Text>
            <Text>Granted scopes: {pairing.scopes.join(", ")}</Text>
            <Button disabled={cli.pending} onPress={() => void cli.revoke(pairing.id)}>
              Revoke CLI
            </Button>
          </Stack>
        ))}
      {cli.pairings.every((pairing) => pairing.status !== "pending") &&
      cli.scopeUpgrades.every((upgrade) => upgrade.status !== "pending") ? (
        <Text>No pending CLI access.</Text>
      ) : null}
      {cli.error ? <Text>Could not load CLI access: {cli.error.message}</Text> : null}
    </Stack>
  );
}
