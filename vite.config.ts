import react from "@vitejs/plugin-react";
import path from "node:path";
import { defineConfig } from "vite";

const rootDir = import.meta.dirname;
// Uncomment these local Workbench source aliases when developing both repositories together.
// const workbenchDir = "/data/project/260627-antd-workbench/workspace";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(rootDir, "src"),
      // "@lwmacct/260627-antd-workbench/global.css": path.resolve(workbenchDir, "src/styles/global.css"),
      // "@lwmacct/260627-antd-workbench": path.resolve(workbenchDir, "src/index.ts"),
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
      "/": {
        target: "http://localhost:23198",
        bypass(request) {
          const authorization = request.headers.authorization ?? "";
          if (authorization.startsWith("Bearer dp.")) {
            return undefined;
          }
          return request.url;
        },
      },
    },
  },
});
