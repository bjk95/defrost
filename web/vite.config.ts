import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": "http://127.0.0.1:6969",
    },
  },
  build: {
    // Output to internal/serve/assets/dist so the Go //go:embed directive
    // in internal/serve/assets/assets.go (`//go:embed all:dist`) can
    // pick up the SPA bundle from a sibling-of-the-embed-file location.
    // The dist directory is committed so `go install` works without a
    // Node toolchain.
    outDir: path.resolve(__dirname, "../internal/serve/assets/dist"),
    emptyOutDir: true,
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./vitest.setup.ts"],
    css: true,
  },
});
