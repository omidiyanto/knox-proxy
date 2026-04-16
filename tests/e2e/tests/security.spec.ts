import { test, expect, Page } from '@playwright/test';

/**
 * ═══════════════════════════════════════════════════════════════════════════
 * Knox E2E — Security Enforcement Tests
 * ═══════════════════════════════════════════════════════════════════════════
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

test.describe('Security — URL Blocking', () => {
  test.beforeEach(async ({ page }) => {
    await loginViaKeycloak(page, TEST_USER, TEST_PASS);
  });

  test('navigating to /settings should redirect to /home/workflows', async ({ page }) => {
    await page.goto('/settings');

    // The injected JS should redirect us
    await page.waitForTimeout(3000);

    const url = page.url();
    expect(url).toContain('/home/workflows');
  });

  test('navigating to /credentials should redirect to /home/workflows', async ({ page }) => {
    await page.goto('/credentials');

    await page.waitForTimeout(3000);

    const url = page.url();
    expect(url).toContain('/home/workflows');
  });
});

test.describe('Security — Webhook Blocking', () => {
  test('GET /webhook should return 403', async ({ request }) => {
    const resp = await request.get('/webhook');
    expect(resp.status()).toBe(403);
  });

  test('GET /webhook-test should return 403', async ({ request }) => {
    const resp = await request.get('/webhook-test');
    expect(resp.status()).toBe(403);
  });

  test('POST /webhook should return 403', async ({ request }) => {
    const resp = await request.post('/webhook', {
      data: { test: true },
    });
    expect(resp.status()).toBe(403);
  });
});

test.describe('Security — Mutation Policy', () => {
  test('unauthenticated mutation should return 401', async ({ request }) => {
    const resp = await request.patch('/rest/workflows/test-id', {
      headers: { 'Accept': 'application/json' },
      data: { name: 'hacked' },
    });
    expect(resp.status()).toBe(401);
  });
});
