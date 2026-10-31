import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e", fullyParallel: false, workers: 1, retries: 0,
  timeout: 180000, expect: { timeout: 15000 },
  reporter: [["list"], ["html", { open: "never" }]],
  use: { baseURL: "http://127.0.0.1:13000", browserName: "chromium", viewport: { width: 1360, height: 900 },
    trace: "retain-on-failure", screenshot: "only-on-failure" },
});
