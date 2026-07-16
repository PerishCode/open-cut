import { Button, Stack, Text } from "@open-cut/components";
import type { ProductResource, ProductResourceSnapshot } from "@open-cut/contracts";
import { useContracts } from "@open-cut/contracts";
import { useCallback, useEffect, useState } from "react";

type ResourceState =
  | Readonly<{ status: "loading" }>
  | Readonly<{ status: "unavailable"; error: Error }>
  | Readonly<{ status: "ready"; snapshot: ProductResourceSnapshot }>;

export function ProductResources() {
  const contracts = useContracts();
  const [state, setState] = useState<ResourceState>({ status: "loading" });
  const [acquiring, setAcquiring] = useState<ProductResource["name"]>();

  const load = useCallback(
    async (signal?: AbortSignal) => {
      try {
        const snapshot = await contracts.resources.list(signal);
        if (!signal?.aborted) setState({ status: "ready", snapshot });
      } catch (value) {
        if (!signal?.aborted) {
          setState({ status: "unavailable", error: value instanceof Error ? value : new Error(String(value)) });
        }
      }
    },
    [contracts],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  const active =
    state.status === "ready" &&
    state.snapshot.resources.some((resource) => resource.state === "queued" || resource.state === "acquiring");
  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    const timer = setTimeout(() => void load(controller.signal), 750);
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [active, load, state]);

  const acquire = useCallback(
    async (resource: ProductResource) => {
      setAcquiring(resource.name);
      try {
        const result = await contracts.resources.acquire({
          name: resource.name,
          requestId: `ui:product-resource:${crypto.randomUUID()}`,
        });
        setState((current) =>
          current.status !== "ready"
            ? current
            : {
                status: "ready",
                snapshot: {
                  ...current.snapshot,
                  resources: current.snapshot.resources.map((item) =>
                    item.name === result.resource.name ? result.resource : item,
                  ),
                },
              },
        );
      } catch (value) {
        setState({ status: "unavailable", error: value instanceof Error ? value : new Error(String(value)) });
      } finally {
        setAcquiring(undefined);
      }
    },
    [contracts],
  );

  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">OFFLINE RESOURCES</Text>
      {state.status === "loading" ? <Text>Checking local model resources…</Text> : null}
      {state.status === "unavailable" ? (
        <Stack spacing="compact">
          <Text>Local resource state is unavailable.</Text>
          <Button onPress={() => void load()}>Retry resource check</Button>
        </Stack>
      ) : null}
      {state.status === "ready" && state.snapshot.resources.length === 0 ? (
        <Text>No optional local resources are declared by this build.</Text>
      ) : null}
      {state.status === "ready"
        ? state.snapshot.resources.map((resource) => (
            <ProductResourceRow
              acquiring={acquiring === resource.name}
              key={resource.name}
              onAcquire={() => void acquire(resource)}
              resource={resource}
            />
          ))
        : null}
    </Stack>
  );
}

function ProductResourceRow({
  resource,
  acquiring,
  onAcquire,
}: {
  resource: ProductResource;
  acquiring: boolean;
  onAcquire: () => void;
}) {
  const canAcquire = resource.state === "not-acquired" || resource.state === "failed" || resource.state === "cancelled";
  return (
    <Stack spacing="compact">
      <Text>Multilingual local transcription</Text>
      <Text tone="eyebrow">
        {resource.version} · {formatBytes(resource.byteSize)} · {resourceStatus(resource)}
      </Text>
      {resource.failureCode ? <Text>Acquisition failed · {resource.failureCode}</Text> : null}
      {canAcquire ? (
        <Button disabled={acquiring} onPress={onAcquire}>
          {acquiring
            ? "Authorizing…"
            : resource.state === "not-acquired"
              ? "Download for offline use"
              : "Retry download"}
        </Button>
      ) : null}
    </Stack>
  );
}

function resourceStatus(resource: ProductResource): string {
  switch (resource.state) {
    case "not-acquired":
      return "not downloaded";
    case "queued":
      return "download queued";
    case "acquiring":
      return `downloading ${Math.floor(resource.progressBasisPoints / 100)}%`;
    case "ready":
      return "ready offline";
    case "failed":
      return "download failed";
    case "cancelled":
      return "download cancelled";
  }
}

function formatBytes(value: string): string {
  const mebibytes = Number(BigInt(value)) / (1024 * 1024);
  return mebibytes >= 1024 ? `${(mebibytes / 1024).toFixed(1)} GiB` : `${Math.ceil(mebibytes)} MiB`;
}
