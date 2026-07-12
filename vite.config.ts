import react from "@vitejs/plugin-react";
import path from "node:path";
import { defineConfig } from "vite";

const rootDir = import.meta.dirname;

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(rootDir, "src"),
    },
    dedupe: ["react", "react-dom"],
  },
  build: {
    chunkSizeWarningLimit: 5120,
  },
  server: {
    host: "0.0.0.0",
    port: 23199,
    strictPort: true,
    proxy: {
      "/oidcauth": {
        target: "http://localhost:23198",
      },
      "/api": {
        target: "http://localhost:23198",
      },
      "/": {
        target: "http://localhost:23198",
        bypass(request) {
          const authorization = request.headers.authorization ?? "";
          if (authorization.startsWith("Bearer dproxy.")) {
            return undefined;
          }
          return request.url;
        },
      },
    },
  },
});
