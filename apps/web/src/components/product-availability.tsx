import { Button, Stack, Status, Text } from "@open-cut/components";
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
  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">LOCAL CREATION FEATURES</Text>
      {state.status === "loading" ? <Status state="pending">Checking local features</Status> : null}
      {state.status === "unavailable" ? (
        <>
          <Status state="unavailable">Could not check local features</Status>
          <Button onPress={onRetry}>Check again</Button>
        </>
      ) : null}
      {state.status === "ready" ? <FeatureStatuses snapshot={state.snapshot} /> : null}
    </Stack>
  );
}

function FeatureStatuses({ snapshot }: { snapshot: ProductStatusSnapshot }) {
  const unavailable = snapshot.features.filter((feature) => feature.state === "unavailable");
  if (unavailable.length === 0) return <Status state="ready">Local preview, export, and transcription ready</Status>;
  return unavailable.map((feature) => (
    <Status key={feature.feature} state="unavailable">
      {featureLabel(feature)} · {reasonLabel(feature)}
    </Status>
  ));
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
        return "Engine and language model not installed";
      case "not-qualified":
        return "Not included in this build";
      case "invalid-closure":
        return "Local transcription files need repair";
      default:
        return "Unavailable";
    }
  }
  switch (feature.reason as ProductFeatureUnavailableReason | undefined) {
    case "not-installed":
      return "Local media tools not installed";
    case "not-qualified":
      return "Not included in this build";
    case "invalid-closure":
      return "Local media tools need repair";
    default:
      return "Unavailable";
  }
}
