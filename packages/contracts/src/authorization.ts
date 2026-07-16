import {
  approveCliPairing,
  approveCliScopeUpgrade,
  denyCliPairing,
  denyCliScopeUpgrade,
  listCliPairings,
  revokeCliPairing,
} from "@open-cut/openapi/authorization";

import {
  type DigestString,
  type DurableID,
  digestString,
  durableID,
  type RevisionString,
  revisionString,
} from "./exact.js";

export type CLIPairingStatus = "pending" | "active" | "denied" | "revoked" | "expired";
export type CLIScopeUpgradeStatus = "pending" | "approved" | "denied" | "expired" | "superseded";

export type CLIPairing = Readonly<{
  id: DurableID;
  installationId: string;
  agentId: DurableID;
  publicKeyFingerprint: DigestString;
  scopes: readonly string[];
  revision: RevisionString;
  scopeDigest: DigestString;
  status: CLIPairingStatus;
  createdAt: string;
  expiresAt: string;
  decidedAt?: string;
  revokedAt?: string;
}>;

export type CLIScopeUpgrade = Readonly<{
  id: DurableID;
  grantId: DurableID;
  fromRevision: RevisionString;
  requestedScopes: readonly string[];
  requestedScopeDigest: DigestString;
  status: CLIScopeUpgradeStatus;
  createdAt: string;
  expiresAt: string;
  decidedAt?: string;
}>;

export type CLIAuthorizationSnapshot = Readonly<{
  pairings: readonly CLIPairing[];
  scopeUpgrades: readonly CLIScopeUpgrade[];
}>;

export type CLIScopeUpgradeDecision = Readonly<{
  upgrade: CLIScopeUpgrade;
  grant: CLIPairing;
}>;

export interface AuthorizationPort {
  readCLI(signal?: AbortSignal): Promise<CLIAuthorizationSnapshot>;
  approveCLIPairing(id: DurableID, signal?: AbortSignal): Promise<CLIPairing>;
  denyCLIPairing(id: DurableID, signal?: AbortSignal): Promise<CLIPairing>;
  revokeCLIPairing(id: DurableID, signal?: AbortSignal): Promise<CLIPairing>;
  approveCLIScopeUpgrade(id: DurableID, signal?: AbortSignal): Promise<CLIScopeUpgradeDecision>;
  denyCLIScopeUpgrade(id: DurableID, signal?: AbortSignal): Promise<CLIScopeUpgradeDecision>;
}

export function createAuthorizationPort(): AuthorizationPort {
  return {
    readCLI: async (signal) => {
      const response = await listCliPairings({ signal });
      if (response.status !== 200) throw new Error(`list CLI authorization returned ${response.status}`);
      const body = asRecord(response.data);
      if (!Array.isArray(body.grants) || !Array.isArray(body.upgrades)) {
        throw new Error("CLI authorization snapshot is invalid");
      }
      return {
        pairings: body.grants.map(normalizeCLIPairing),
        scopeUpgrades: body.upgrades.map(normalizeCLIScopeUpgrade),
      };
    },
    approveCLIPairing: async (id, signal) => {
      const response = await approveCliPairing(id, { signal });
      if (response.status !== 200) throw new Error(`approve CLI pairing returned ${response.status}`);
      return normalizeCLIPairing(response.data);
    },
    denyCLIPairing: async (id, signal) => {
      const response = await denyCliPairing(id, { signal });
      if (response.status !== 200) throw new Error(`deny CLI pairing returned ${response.status}`);
      return normalizeCLIPairing(response.data);
    },
    revokeCLIPairing: async (id, signal) => {
      const response = await revokeCliPairing(id, { signal });
      if (response.status !== 200) throw new Error(`revoke CLI pairing returned ${response.status}`);
      return normalizeCLIPairing(response.data);
    },
    approveCLIScopeUpgrade: async (id, signal) => {
      const response = await approveCliScopeUpgrade(id, { signal });
      if (response.status !== 200) throw new Error(`approve CLI scope upgrade returned ${response.status}`);
      return normalizeScopeUpgradeDecision(response.data);
    },
    denyCLIScopeUpgrade: async (id, signal) => {
      const response = await denyCliScopeUpgrade(id, { signal });
      if (response.status !== 200) throw new Error(`deny CLI scope upgrade returned ${response.status}`);
      return normalizeScopeUpgradeDecision(response.data);
    },
  };
}

function normalizeCLIPairing(value: unknown): CLIPairing {
  const pairing = asRecord(value);
  if (
    typeof pairing.installationId !== "string" ||
    pairing.installationId.length < 1 ||
    pairing.installationId.length > 128 ||
    !validScopes(pairing.scopes) ||
    !isPairingStatus(pairing.status)
  ) {
    throw new Error("CLI pairing is invalid");
  }
  return {
    id: durableID(pairing.id),
    installationId: pairing.installationId,
    agentId: durableID(pairing.agentId),
    publicKeyFingerprint: digestString(pairing.publicKeyFingerprint),
    scopes: [...pairing.scopes],
    revision: positiveRevision(pairing.revision),
    scopeDigest: digestString(pairing.scopeDigest),
    status: pairing.status,
    createdAt: instant(pairing.createdAt),
    expiresAt: instant(pairing.expiresAt),
    ...(pairing.decidedAt === undefined ? {} : { decidedAt: instant(pairing.decidedAt) }),
    ...(pairing.revokedAt === undefined ? {} : { revokedAt: instant(pairing.revokedAt) }),
  };
}

function normalizeCLIScopeUpgrade(value: unknown): CLIScopeUpgrade {
  const upgrade = asRecord(value);
  if (!validScopes(upgrade.requestedScopes) || !isScopeUpgradeStatus(upgrade.status)) {
    throw new Error("CLI scope upgrade is invalid");
  }
  return {
    id: durableID(upgrade.id),
    grantId: durableID(upgrade.grantId),
    fromRevision: positiveRevision(upgrade.fromRevision),
    requestedScopes: [...upgrade.requestedScopes],
    requestedScopeDigest: digestString(upgrade.requestedScopeDigest),
    status: upgrade.status,
    createdAt: instant(upgrade.createdAt),
    expiresAt: instant(upgrade.expiresAt),
    ...(upgrade.decidedAt === undefined ? {} : { decidedAt: instant(upgrade.decidedAt) }),
  };
}

function normalizeScopeUpgradeDecision(value: unknown): CLIScopeUpgradeDecision {
  const decision = asRecord(value);
  return { upgrade: normalizeCLIScopeUpgrade(decision.upgrade), grant: normalizeCLIPairing(decision.grant) };
}

function validScopes(value: unknown): value is string[] {
  return (
    Array.isArray(value) &&
    value.length > 0 &&
    value.length <= 64 &&
    value.every((scope) => typeof scope === "string" && /^[a-z][a-z0-9-]*:[a-z][a-z0-9-]*$/.test(scope))
  );
}

function isPairingStatus(value: unknown): value is CLIPairingStatus {
  return value === "pending" || value === "active" || value === "denied" || value === "revoked" || value === "expired";
}

function isScopeUpgradeStatus(value: unknown): value is CLIScopeUpgradeStatus {
  return (
    value === "pending" || value === "approved" || value === "denied" || value === "expired" || value === "superseded"
  );
}

function positiveRevision(value: unknown): RevisionString {
  const revision = revisionString(value);
  if (revision === "0") throw new Error("CLI authorization revision must be positive");
  return revision;
}

function instant(value: unknown): string {
  if (typeof value !== "string" || Number.isNaN(Date.parse(value)))
    throw new Error("CLI authorization instant is invalid");
  return value;
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("expected an object response");
  return value as Record<string, unknown>;
}
