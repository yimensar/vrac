import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vitest/config"

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(import.meta.dirname, "./src") } },
  build: { outDir: "dist", emptyOutDir: false },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8080",
      "/oauth": "http://127.0.0.1:8080",
      "/.well-known": "http://127.0.0.1:8080",
      "/mcp": "http://127.0.0.1:8080",
    },
  },
  test: { globals: true, environment: "jsdom", setupFiles: ["./src/test-setup.ts"], css: false },
})
