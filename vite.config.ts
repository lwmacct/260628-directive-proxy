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
  },
  build: {
    chunkSizeWarningLimit: 5120,
  },
  server: {
    host: "0.0.0.0",
    port: 23199,
    strictPort: true,
    proxy: {
      "/api": {
        target: "http://localhost:23198",
        changeOrigin: true,
      },
      "/": {
        target: "http://localhost:23198",
        changeOrigin: true,
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
