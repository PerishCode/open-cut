export type HealthResponse = {
  ok: true;
  service: "api";
};

export const runtimePeer = {
  api: { app: "api", httpEndpoint: "http" },
  web: { app: "web", httpEndpoint: "http" },
} as const;
