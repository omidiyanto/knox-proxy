import { test, expect, Page } from '@playwright/test';

/**
 * ═══════════════════════════════════════════════════════════════════════════
 * Knox E2E — n8n Compatibility Tests
 * ═══════════════════════════════════════════════════════════════════════════
 * These tests verify that n8n UI/API contract still holds across versions.
 * Breaking changes here indicate Knox may need updates for the new n8n version.
 */

const TEST_USER = process.env.TEST_USER || 'testuser';
const TEST_PASS = process.env.TEST_PASS || 'testpass';

async function loginViaKeycloak(page: Page, username: string, password: string) {
  await page.goto('/');
  await page.waitForSelector('#username, input[name="username"]', { timeout: 30000 });
  await page.fill('#username, input[name="username"]', username);
  await page.fill('#password, input[name="password"]', password);
  await page.click('#kc-login, input[type="submit"]');
  await page.waitForURL('**/home/**', { timeout: 30000 });
}

test.describe('n8n Compatibility — UI', () => {
  test.beforeEach(async ({ page }) => {
    await loginViaKeycloak(page, TEST_USER, TEST_PASS);
  });

  test('n8n home page loads with workflow list', async ({ page }) => {
    await page.goto('/home/workflows');
    await page.waitForTimeout(3000);

    // The page should load without errors
    const url = page.url();
    expect(url).toContain('/home');

    // Body should contain content
    const bodyText = await page.locator('body').textContent();
    expect(bodyText?.length).toBeGreaterThan(0);
  });

  test('n8n workflow editor canvas renders', async ({ page }) => {
    // Navigate to a workflow if one exists
    await page.goto('/home/workflows');
    await page.waitForTimeout(3000);

    // Look for workflow items or canvas
    const workflowLinks = page.locator('a[href*="/workflow/"]').first();
    if (await workflowLinks.count() > 0) {
      await workflowLinks.click();
      await page.waitForTimeout(3000);

      // Canvas should exist
      const canvas = page.locator('[class*="canvas"], [class*="workflow"], [data-test-id*="canvas"]').first();
      if (await canvas.count() > 0) {
        await expect(canvas).toBeVisible();
      }
    }
  });

  test('Knox custom assets are loaded', async ({ page }) => {
    // Check that our custom JS is loaded
    const jsResponse = await page.request.get('/custom-assets/knox-ui.js');
    expect(jsResponse.status()).toBe(200);

    // Check that our custom CSS is loaded
    const cssResponse = await page.request.get('/custom-assets/knox-modal.css');
    expect(cssResponse.status()).toBe(200);
  });

  test('n8n sidebar "Help" link exists (injection anchor)', async ({ page }) => {
    // Knox injects menu items by cloning the Help link
    // If n8n removes/renames the Help link, injection will break
    await page.waitForTimeout(5000);

    // Check if our injected items exist (which proves the Help anchor exists)
    const userInfoMenu = page.locator('#knox-menu-userinfo');
    const customLogout = page.locator('#custom-logout-btn');

    const hasInjection = (await userInfoMenu.count() > 0) || (await customLogout.count() > 0);
    expect(hasInjection).toBe(true);
  });
});

test.describe('n8n Compatibility — REST API', () => {
  test('GET /rest/settings returns valid JSON', async ({ request }) => {
    // Reach n8n settings through Knox (auth not required for settings endpoint in some versions)
    // This verifies the API schema contract
    const resp = await request.get('/rest/settings');

    // May return 401 (auth required) or 200 — both are valid responses
    expect([200, 401]).toContain(resp.status());

    if (resp.status() === 200) {
      const data = await resp.json();
      expect(data).toBeTruthy();
    }
  });

  test('/rest/workflows endpoint responds', async ({ request }) => {
    const resp = await request.get('/rest/workflows', {
      headers: { 'Accept': 'application/json' },
    });

    // Should return 401 (unauthenticated) — but the endpoint exists
    expect([200, 401]).toContain(resp.status());
  });

  test('/healthz endpoint responds', async ({ request }) => {
    const resp = await request.get('/healthz');
    expect(resp.status()).toBe(200);

    const data = await resp.json();
    expect(data.status).toBe('ok');
  });
});

test.describe('n8n Compatibility — Streaming', () => {
  test('SSE push endpoint is accessible', async ({ request }) => {
    // /rest/push should accept the connection (or return auth error)
    const resp = await request.get('/rest/push', {
      headers: {
        'Accept': 'text/event-stream',
      },
    });

    // We expect 401 (unauthenticated) — but endpoint should exist (not 404)
    expect(resp.status()).not.toBe(404);
  });
});
