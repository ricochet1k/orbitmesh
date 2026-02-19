import { defineConfig, devices } from "@playwright/test";
import { reservePort } from "./tests/helpers/free-port.js";

const port = reservePort("UI_MOCK_PORT", 4173);

export default defineConfig({
  testDir: "tests/ui",
  outputDir: "test-results/ui-artifacts",
  timeout: 10_000,
  fullyParallel: true,
  expect: {
    timeout: 2_000,
  },
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
  webServer: {
    command: `VITE_DISABLE_WS_PROXY=1 npm run dev -- --host 127.0.0.1 --port ${port}`,
    url: `http://127.0.0.1:${port}`,
    reuseExistingServer: false,
    timeout: 10_000,
  },
});
