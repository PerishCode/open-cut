import { ContractsProvider } from "@open-cut/contracts";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { HomeView } from "./views/home-view.js";

const root = document.querySelector<HTMLElement>("#app");
if (!root) throw new Error("Open Cut web root is missing");

createRoot(root).render(
  <StrictMode>
    <ContractsProvider>
      <HomeView />
    </ContractsProvider>
  </StrictMode>,
);
