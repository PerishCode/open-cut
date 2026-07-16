import { acquireProductResource, listProductResources } from "@open-cut/openapi/product";
import {
  type CursorString,
  cursorString,
  type DurableID,
  durableID,
  type UInt64String,
  uint64String,
} from "./exact.js";

export type ProductResourceKind = "transcription-model";
export type ProductResourceState = "not-acquired" | "queued" | "acquiring" | "ready" | "failed" | "cancelled";

export type ProductResource = Readonly<{
  name: string;
  kind: ProductResourceKind;
  version: string;
  profile: string;
  byteSize: UInt64String;
  state: ProductResourceState;
  progressBasisPoints: number;
  resourceId?: DurableID;
  jobId?: DurableID;
  failureCode?: string;
  updatedAt?: string;
}>;

export type ProductResourceSnapshot = Readonly<{
  schema: "open-cut/product-resource-snapshot/v1";
  resources: readonly ProductResource[];
}>;

export type AcquireProductResourceInput = Readonly<{
  name: string;
  requestId: string;
}>;

export type ProductResourceAcquired = Readonly<{
  resource: ProductResource;
  activityCursor: CursorString;
  replayed: boolean;
}>;

export interface ProductResourcePort {
  list(signal?: AbortSignal): Promise<ProductResourceSnapshot>;
  acquire(input: AcquireProductResourceInput, signal?: AbortSignal): Promise<ProductResourceAcquired>;
}

export function createProductResourcePort(): ProductResourcePort {
  return {
    list: async (signal) => {
      const response = await listProductResources({ signal });
      if (response.status !== 200) throw new Error(`list product resources returned ${response.status}`);
      return normalizeSnapshot(response.data);
    },
    acquire: async (input, signal) => {
      const normalized = normalizeAcquireInput(input);
      const response = await acquireProductResource(normalized.name, { requestId: normalized.requestId }, { signal });
      if (response.status !== 200) throw new Error(`acquire product resource returned ${response.status}`);
      return normalizeAcquired(response.data);
    },
  };
}

function normalizeSnapshot(value: unknown): ProductResourceSnapshot {
  const snapshot = asRecord(value);
  if (snapshot.schema !== "open-cut/product-resource-snapshot/v1" || !Array.isArray(snapshot.resources)) {
    throw new Error("product resource snapshot is invalid");
  }
  if (snapshot.resources.length > 128) throw new Error("product resource snapshot is too large");
  const resources = snapshot.resources.map(normalizeResource);
  for (let index = 1; index < resources.length; index += 1) {
    if ((resources[index - 1]?.name ?? "") >= (resources[index]?.name ?? "")) {
      throw new Error("product resource snapshot is not canonical");
    }
  }
  return { schema: snapshot.schema, resources };
}

function normalizeAcquired(value: unknown): ProductResourceAcquired {
  const result = asRecord(value);
  if (typeof result.replayed !== "boolean") throw new Error("product resource acquisition is invalid");
  return {
    resource: normalizeResource(result.resource),
    activityCursor: cursorString(result.activityCursor),
    replayed: result.replayed,
  };
}

function normalizeResource(value: unknown): ProductResource {
  const resource = asRecord(value);
  if (
    typeof resource.name !== "string" ||
    !/^[a-z][a-z0-9.-]{0,127}$/.test(resource.name) ||
    resource.kind !== "transcription-model" ||
    typeof resource.version !== "string" ||
    resource.version.length < 1 ||
    resource.version.length > 128 ||
    typeof resource.profile !== "string" ||
    resource.profile.length < 1 ||
    resource.profile.length > 128 ||
    !isBasisPoints(resource.progressBasisPoints) ||
    !isResourceState(resource.state)
  ) {
    throw new Error("product resource is invalid");
  }
  const normalized: ProductResource = {
    name: resource.name,
    kind: resource.kind,
    version: resource.version,
    profile: resource.profile,
    byteSize: uint64String(resource.byteSize),
    state: resource.state,
    progressBasisPoints: resource.progressBasisPoints,
    ...(resource.resourceId === undefined ? {} : { resourceId: durableID(resource.resourceId) }),
    ...(resource.jobId === undefined ? {} : { jobId: durableID(resource.jobId) }),
    ...(resource.failureCode === undefined ? {} : { failureCode: failureCode(resource.failureCode) }),
    ...(resource.updatedAt === undefined ? {} : { updatedAt: instant(resource.updatedAt) }),
  };
  validateStateShape(normalized);
  return normalized;
}

function validateStateShape(resource: ProductResource): void {
  switch (resource.state) {
    case "not-acquired":
      if (
        resource.progressBasisPoints !== 0 ||
        resource.resourceId !== undefined ||
        resource.jobId !== undefined ||
        resource.failureCode !== undefined ||
        resource.updatedAt !== undefined
      ) {
        throw new Error("not-acquired product resource has execution state");
      }
      return;
    case "queued":
    case "acquiring":
      if (!resource.jobId || !resource.updatedAt || resource.resourceId || resource.failureCode) {
        throw new Error("active product resource state is invalid");
      }
      return;
    case "ready":
      if (
        !resource.resourceId ||
        !resource.jobId ||
        !resource.updatedAt ||
        resource.failureCode ||
        resource.progressBasisPoints !== 10_000
      ) {
        throw new Error("ready product resource state is invalid");
      }
      return;
    case "failed":
      if (!resource.jobId || !resource.updatedAt || !resource.failureCode || resource.resourceId) {
        throw new Error("failed product resource state is invalid");
      }
      return;
    case "cancelled":
      if (!resource.jobId || !resource.updatedAt || resource.resourceId) {
        throw new Error("cancelled product resource state is invalid");
      }
  }
}

function normalizeAcquireInput(input: AcquireProductResourceInput): AcquireProductResourceInput {
  if (!/^[a-z][a-z0-9.-]{0,127}$/.test(input.name)) throw new Error("product resource name is invalid");
  if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(input.requestId)) {
    throw new Error("product resource request identity is invalid");
  }
  return input;
}

function isResourceState(value: unknown): value is ProductResourceState {
  return (
    value === "not-acquired" ||
    value === "queued" ||
    value === "acquiring" ||
    value === "ready" ||
    value === "failed" ||
    value === "cancelled"
  );
}

function isBasisPoints(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= 0 && value <= 10_000;
}

function failureCode(value: unknown): string {
  if (typeof value !== "string" || !/^[a-z][a-z0-9-]{0,63}$/.test(value)) {
    throw new Error("product resource failure code is invalid");
  }
  return value;
}

function instant(value: unknown): string {
  if (typeof value !== "string" || Number.isNaN(Date.parse(value))) {
    throw new Error("product resource instant is invalid");
  }
  return value;
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("product resource payload is invalid");
  }
  return value as Record<string, unknown>;
}
