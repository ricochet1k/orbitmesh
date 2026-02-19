import { defineConfig, devices } from "@playwright/test";
import { reservePort } from "./tests/helpers/free-port.js";

const port = reservePort("E2E_FRONTEND_PORT", 4174);

export default defineConfig({
  testDir: "tests/e2e",
  globalSetup: "./tests/helpers/e2e-harness.ts",
  outputDir: "test-results/e2e-artifacts",
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
});
