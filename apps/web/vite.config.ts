import { defineConfig } from "vite";

export default defineConfig({
  build: {
    emptyOutDir: true,
    outDir: "dist/web",
  },
  server: {
    host: "127.0.0.1",
  },
});
