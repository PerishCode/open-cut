import { Button, Stack, Text } from "@open-cut/components";
import type {
  ProductFeatureAvailability,
  ProductFeatureUnavailableReason,
  ProductStatusSnapshot,
} from "@open-cut/contracts";

export type ProductAvailabilityState =
  | Readonly<{ status: "loading" }>
  | Readonly<{ status: "unavailable"; error: Error }>
  | Readonly<{ status: "ready"; snapshot: ProductStatusSnapshot }>;

export function ProductAvailability({ state, onRetry }: { state: ProductAvailabilityState; onRetry: () => void }) {
  if (state.status === "loading") return <Text>Checking local creation features…</Text>;
  if (state.status === "unavailable") {
    return (
      <Stack spacing="compact">
        <Text>Local creation feature status is unavailable.</Text>
        <Button onPress={onRetry}>Retry feature check</Button>
      </Stack>
    );
  }
  const unavailable = state.snapshot.features.filter((feature) => feature.state === "unavailable");
  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">LOCAL CREATION FEATURES</Text>
      {unavailable.length === 0 ? <Text>Local creation features are ready.</Text> : null}
      {unavailable.map((feature) => (
        <Text key={feature.feature}>
          {featureLabel(feature)} · {reasonLabel(feature)}
        </Text>
      ))}
    </Stack>
  );
}

function featureLabel(feature: ProductFeatureAvailability): string {
  switch (feature.feature) {
    case "asset-frame-inspection":
      return "Agent frame inspection";
    case "sequence-preview":
      return "Sequence preview";
    case "sequence-export":
      return "Final sequence export";
    case "source-preview":
      return "Source preview";
    case "local-transcription":
      return "Local transcription";
  }
}

function reasonLabel(feature: ProductFeatureAvailability): string {
  if (feature.feature === "local-transcription") {
    switch (feature.reason) {
      case "not-installed":
        return "engine or model catalog is not installed";
      case "not-qualified":
        return "not qualified for this build";
      case "invalid-closure":
        return "installed transcription closure is invalid";
      default:
        return "unavailable";
    }
  }
  switch (feature.reason as ProductFeatureUnavailableReason | undefined) {
    case "not-installed":
      return "media tools are not installed";
    case "not-qualified":
      return "not qualified for this build";
    case "invalid-closure":
      return "installed media closure is invalid";
    default:
      return "unavailable";
  }
}
