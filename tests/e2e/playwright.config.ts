import { defineConfig } from '@playwright/test';

/**
 * Knox E2E Test Configuration
 *
 * Environment variables (from tests/.test-env):
 *   KNOX_TEST_BASE_URL  — Knox proxy URL (default: http://localhost:8443)
 *   KEYCLOAK_URL        — Keycloak URL (default: http://localhost:8080)
 */
export default defineConfig({
  testDir: './tests',
  fullyParallel: false, // Run sequentially — login state matters
  retries: 1,
  workers: 1,
  timeout: 60_000, // 60s — Keycloak login can be slow on CI

  reporter: [
    ['html', { open: 'never' }],
    ['list'],
  ],

  use: {
    baseURL: process.env.KNOX_TEST_BASE_URL || 'http://localhost:8443',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'on-first-retry',
    actionTimeout: 15_000,
    navigationTimeout: 30_000,
  },

  projects: [
    {
      name: 'chromium',
      use: {
        browserName: 'chromium',
        headless: true,
        viewport: { width: 1280, height: 720 },
      },
    },
  ],
});
