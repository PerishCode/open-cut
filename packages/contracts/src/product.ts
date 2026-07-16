import { showProductStatus } from "@open-cut/openapi/product";

export type ProductFeature =
  | "asset-frame-inspection"
  | "sequence-preview"
  | "sequence-export"
  | "source-preview"
  | "local-transcription";
export type ProductFeatureState = "available" | "unavailable";
export type ProductFeatureUnavailableReason = "not-installed" | "not-qualified" | "invalid-closure";

export type ProductFeatureAvailability = Readonly<{
  feature: ProductFeature;
  state: ProductFeatureState;
  reason?: ProductFeatureUnavailableReason;
}>;

export type ProductStatusSnapshot = Readonly<{
  schema: "open-cut/product-status/v1";
  features: readonly ProductFeatureAvailability[];
}>;

export interface ProductStatusPort {
  read(signal?: AbortSignal): Promise<ProductStatusSnapshot>;
}

const expectedFeatures = [
  "asset-frame-inspection",
  "sequence-preview",
  "sequence-export",
  "source-preview",
  "local-transcription",
] as const;

export function createProductStatusPort(): ProductStatusPort {
  return {
    read: async (signal) => {
      const response = await showProductStatus({ signal });
      if (response.status !== 200) throw new Error(`show product status returned ${response.status}`);
      return normalizeProductStatus(response.data);
    },
  };
}

export function isProductFeatureAvailable(snapshot: ProductStatusSnapshot, feature: ProductFeature): boolean {
  return snapshot.features.find((current) => current.feature === feature)?.state === "available";
}

function normalizeProductStatus(value: unknown): ProductStatusSnapshot {
  const status = asRecord(value);
  if (status.schema !== "open-cut/product-status/v1" || !Array.isArray(status.features)) {
    throw new Error("product status is invalid");
  }
  if (status.features.length !== expectedFeatures.length) throw new Error("product feature set is incomplete");
  const features = status.features.map((value, index) => {
    const expected = expectedFeatures[index];
    if (!expected) throw new Error("product feature set is incomplete");
    return normalizeFeature(value, expected);
  });
  return { schema: "open-cut/product-status/v1", features };
}

function normalizeFeature(value: unknown, expected: ProductFeature): ProductFeatureAvailability {
  const feature = asRecord(value);
  if (feature.feature !== expected || (feature.state !== "available" && feature.state !== "unavailable")) {
    throw new Error("product feature availability is invalid");
  }
  if (feature.state === "available") {
    if (feature.reason !== undefined) throw new Error("available product feature has an unavailable reason");
    return { feature: expected, state: "available" };
  }
  if (
    feature.reason !== "not-installed" &&
    feature.reason !== "not-qualified" &&
    feature.reason !== "invalid-closure"
  ) {
    throw new Error("unavailable product feature has no closed reason");
  }
  return { feature: expected, state: "unavailable", reason: feature.reason };
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("product status is invalid");
  return value as Record<string, unknown>;
}
