import "./style.css";

const app = document.querySelector<HTMLElement>("#app");
if (!app) throw new Error("Open Cut web root is missing");

app.innerHTML = `
  <section class="shell">
    <p class="eyebrow">OPEN CUT · DAY 0</p>
    <h1>Peer sidecars,<br />one control plane.</h1>
    <p class="summary">
      Vite is running behind the Web sidecar. Electron discovered this endpoint
      through the shared TCP broker; neither process owns the other.
    </p>
    <div class="status" role="status">
      <span class="pulse" aria-hidden="true"></span>
      <span>Web runtime ready</span>
    </div>
  </section>
`;
