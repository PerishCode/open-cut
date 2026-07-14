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

import { type Contracts, createContracts, type Project, type ProjectState, type ProjectUpserted } from "./projects.js";

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

export type PutProjectHook = Readonly<{
  put(project: Project): Promise<ProjectUpserted>;
  pending: boolean;
  error?: Error;
}>;

export function usePutProject(): PutProjectHook {
  const contracts = useContracts();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<Error>();
  const put = useCallback(
    async (project: Project) => {
      setPending(true);
      setError(undefined);
      try {
        return await contracts.projects.write.put(project);
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
  return useMemo(() => ({ put, pending, error }), [put, pending, error]);
}
