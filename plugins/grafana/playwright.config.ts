import { defineConfig, devices } from '@playwright/test';

// Project-owned configuration: the unused Grafana auth scaffold limited Node
// to <=24. The local lab uses anonymous access; authenticated installations can
// supply their own Playwright storage state without depending on that fixture.
export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: 'list',
  use: {
    baseURL: process.env.GRAFANA_URL || 'http://localhost:3480',
    storageState: process.env.GRAFANA_STORAGE_STATE || undefined,
    trace: 'on-first-retry',
  },
  projects: [
    { name: 'api', testMatch: '**/*.api.spec.ts' },
    { name: 'chromium', testIgnore: '**/*.api.spec.ts', use: { ...devices['Desktop Chrome'] } },
  ],
});
