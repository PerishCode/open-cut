import {
  createContext,
  type PropsWithChildren,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  useSyncExternalStore,
} from "react";
import type { CLIPairing, CLIScopeUpgrade, CLIScopeUpgradeDecision } from "./authorization.js";
import type { DurableID } from "./exact.js";
import {
  type Contracts,
  type CreateProjectInput,
  createContracts,
  type ProjectCreated,
  type ProjectState,
} from "./projects.js";

const ContractsContext = createContext<Contracts | undefined>(undefined);

export type ContractsProviderProps = PropsWithChildren<{ contracts?: Contracts }>;

export function ContractsProvider({ children, contracts }: ContractsProviderProps) {
  const owned = useMemo(() => createContracts(), []);
  const current = contracts ?? owned;
  useEffect(() => {
    current.start();
    return () => current.close();
  }, [current]);
  return <ContractsContext.Provider value={current}>{children}</ContractsContext.Provider>;
}

export function useContracts(): Contracts {
  const contracts = useContext(ContractsContext);
  if (!contracts) throw new Error("ContractsProvider is required");
  return contracts;
}

export function useProjects(): ProjectState {
  const contracts = useContracts();
  return useSyncExternalStore(
    contracts.projects.read.subscribe,
    contracts.projects.read.getSnapshot,
    contracts.projects.read.getSnapshot,
  );
}

export type CreateProjectHook = Readonly<{
  create(input: CreateProjectInput): Promise<ProjectCreated>;
  pending: boolean;
  error?: Error;
}>;

export function useCreateProject(): CreateProjectHook {
  const contracts = useContracts();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<Error>();
  const create = useCallback(
    async (input: CreateProjectInput) => {
      setPending(true);
      setError(undefined);
      try {
        return await contracts.projects.write.create(input);
      } catch (value) {
        const next = value instanceof Error ? value : new Error(String(value));
        setError(next);
        throw next;
      } finally {
        setPending(false);
      }
    },
    [contracts],
  );
  return useMemo(() => ({ create, pending, error }), [create, pending, error]);
}

export type CLIAuthorizationHook = Readonly<{
  pairings: readonly CLIPairing[];
  scopeUpgrades: readonly CLIScopeUpgrade[];
  pending: boolean;
  error?: Error;
  refresh(): Promise<void>;
  approve(id: DurableID): Promise<CLIPairing>;
  deny(id: DurableID): Promise<CLIPairing>;
  revoke(id: DurableID): Promise<CLIPairing>;
  approveScopeUpgrade(id: DurableID): Promise<CLIScopeUpgradeDecision>;
  denyScopeUpgrade(id: DurableID): Promise<CLIScopeUpgradeDecision>;
}>;

export function useCLIAuthorization(): CLIAuthorizationHook {
  const contracts = useContracts();
  const [pairings, setPairings] = useState<readonly CLIPairing[]>([]);
  const [scopeUpgrades, setScopeUpgrades] = useState<readonly CLIScopeUpgrade[]>([]);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<Error>();
  const run = useCallback(async <Result,>(operation: () => Promise<Result>): Promise<Result> => {
    setPending(true);
    setError(undefined);
    try {
      return await operation();
    } catch (value) {
      const next = value instanceof Error ? value : new Error(String(value));
      setError(next);
      throw next;
    } finally {
      setPending(false);
    }
  }, []);
  const refresh = useCallback(
    () =>
      run(async () => {
        const next = await contracts.authorization.readCLI();
        setPairings(next.pairings);
        setScopeUpgrades(next.scopeUpgrades);
      }),
    [contracts, run],
  );
  const decide = useCallback(
    (id: DurableID, approve: boolean) =>
      run(async () => {
        const result = approve
          ? await contracts.authorization.approveCLIPairing(id)
          : await contracts.authorization.denyCLIPairing(id);
        setPairings((current) => current.map((pairing) => (pairing.id === result.id ? result : pairing)));
        return result;
      }),
    [contracts, run],
  );
  const revoke = useCallback(
    (id: DurableID) =>
      run(async () => {
        const result = await contracts.authorization.revokeCLIPairing(id);
        setPairings((current) => current.map((pairing) => (pairing.id === result.id ? result : pairing)));
        return result;
      }),
    [contracts, run],
  );
  const decideScopeUpgrade = useCallback(
    (id: DurableID, approve: boolean) =>
      run(async () => {
        const result = approve
          ? await contracts.authorization.approveCLIScopeUpgrade(id)
          : await contracts.authorization.denyCLIScopeUpgrade(id);
        setScopeUpgrades((current) =>
          current.map((upgrade) => (upgrade.id === result.upgrade.id ? result.upgrade : upgrade)),
        );
        setPairings((current) => current.map((pairing) => (pairing.id === result.grant.id ? result.grant : pairing)));
        return result;
      }),
    [contracts, run],
  );
  useEffect(() => {
    void refresh().catch(() => undefined);
  }, [refresh]);
  return useMemo(
    () => ({
      pairings,
      scopeUpgrades,
      pending,
      error,
      refresh,
      approve: (id) => decide(id, true),
      deny: (id) => decide(id, false),
      revoke,
      approveScopeUpgrade: (id) => decideScopeUpgrade(id, true),
      denyScopeUpgrade: (id) => decideScopeUpgrade(id, false),
    }),
    [pairings, scopeUpgrades, pending, error, refresh, decide, revoke, decideScopeUpgrade],
  );
}
