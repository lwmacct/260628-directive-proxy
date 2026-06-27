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
    port: 40175,
    strictPort: true,
    proxy: {
      "/api": {
        target: "http://localhost:40174",
        changeOrigin: true,
      },
    },
  },
});
