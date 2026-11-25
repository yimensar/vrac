import { defineConfig, devices } from "@playwright/test"

// End-to-end tests drive the built interface against a real server and a
// fresh database under ../tmp/e2e. Run with `npm run e2e` in `nix develop`.
const port = 8098

export default defineConfig({
  testDir: "e2e",
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? "list" : "line",
  use: { baseURL: `http://localhost:${port}`, trace: "off" },
  projects: [
    { name: "setup", testMatch: /setup\.ts/ },
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"], storageState: "../tmp/e2e/state.json" },
      dependencies: ["setup"],
    },
  ],
  webServer: {
    command: `rm -rf ../tmp/e2e && mkdir -p ../tmp/e2e && go build -o ../tmp/e2e/vrac ../cmd/vrac && ../tmp/e2e/vrac serve -data ../tmp/e2e/data -listen 127.0.0.1:${port} -timezone Europe/Paris`,
    url: `http://localhost:${port}/healthz`,
    reuseExistingServer: false,
    timeout: 180_000,
  },
})
