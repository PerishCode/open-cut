import { Heading, Stack, Status, Text } from "@open-cut/components";
import { useEffect, useState } from "react";

import { readApiHealth } from "../lib/api.js";

type ApiState = "pending" | "ready" | "unavailable";

export function RuntimeSummary() {
  const [api, setApi] = useState<ApiState>("pending");

  useEffect(() => {
    const request = new AbortController();
    void readApiHealth(request.signal).then(
      (health) => setApi(health.ok ? "ready" : "unavailable"),
      (error: unknown) => {
        if (!(error instanceof DOMException && error.name === "AbortError")) setApi("unavailable");
      },
    );
    return () => request.abort();
  }, []);

  return (
    <Stack>
      <Text tone="eyebrow">OPEN CUT · DAY 0</Text>
      <Heading>Peer sidecars, one control plane.</Heading>
      <Text>
        React is running behind the Web sidecar. Product API traffic uses one stable generated client while peer
        endpoints remain continuously leased by the control plane.
      </Text>
      <Status state={api}>{api === "ready" ? "API runtime ready" : `API runtime ${api}`}</Status>
    </Stack>
  );
}
