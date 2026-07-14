import { Surface } from "@open-cut/components";

import { RuntimeSummary } from "../components/runtime-summary.js";

export function HomeView() {
  return (
    <Surface label="Open Cut runtime">
      <RuntimeSummary />
    </Surface>
  );
}
