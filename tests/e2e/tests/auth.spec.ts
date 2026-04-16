import { test, expect, Page } from '@playwright/test';

/**
 * ═══════════════════════════════════════════════════════════════════════════
 * Knox E2E — Authentication Flow Tests
 * ═══════════════════════════════════════════════════════════════════════════
 */

const TEST_USER = process.env.TEST_USER || 'testuser';
const TEST_PASS = process.env.TEST_PASS || 'testpass';

/**
 * Helper: Login via Keycloak
 */
async function loginViaKeycloak(page: Page, username: string, password: string) {
  // Navigate to Knox — should redirect to Keycloak
  await page.goto('/');

  // Wait for Keycloak login page
  await page.waitForSelector('#username, input[name="username"]', { timeout: 30000 });

  // Fill credentials
  await page.fill('#username, input[name="username"]', username);
  await page.fill('#password, input[name="password"]', password);

  // Submit form
  await page.click('#kc-login, input[type="submit"]');

  // Wait for redirect back to n8n
  await page.waitForURL('**/home/**', { timeout: 30000 });
}

test.describe('Authentication Flow', () => {
  test('should redirect unauthenticated user to Keycloak', async ({ page }) => {
    // Navigate to Knox root
    const response = await page.goto('/');

    // Should eventually land on Keycloak login page
    await page.waitForSelector('#username, input[name="username"]', { timeout: 30000 });

    // Verify we're on Keycloak
    const url = page.url();
    expect(url).toContain('/realms/');
    expect(url).toContain('/login-actions/');
  });

  test('should login via Keycloak and land on n8n', async ({ page }) => {
    await loginViaKeycloak(page, TEST_USER, TEST_PASS);

    // Verify we're on n8n UI
    const url = page.url();
    expect(url).toContain('/home');

    // Verify n8n page loaded (workflow list or canvas)
    await page.waitForSelector('body', { timeout: 10000 });
    const title = await page.title();
    expect(title.length).toBeGreaterThan(0);
  });

  test('should maintain session across pages', async ({ page }) => {
    await loginViaKeycloak(page, TEST_USER, TEST_PASS);

    // Navigate to different pages
    await page.goto('/home/workflows');
    expect(page.url()).toContain('/home');

    // Should NOT redirect to login
    await page.waitForTimeout(2000);
    expect(page.url()).not.toContain('/realms/');
  });

  test('should logout via custom button', async ({ page }) => {
    await loginViaKeycloak(page, TEST_USER, TEST_PASS);

    // Wait for custom logout button to be injected
    await page.waitForSelector('#custom-logout-btn', { timeout: 15000 });

    // Click logout
    await page.click('#custom-logout-btn');

    // Should redirect away from n8n
    await page.waitForTimeout(3000);
    const url = page.url();

    // After logout, should be on Keycloak login or Knox login page
    const isLoggedOut = url.includes('/realms/') || url.includes('/auth/login');
    expect(isLoggedOut).toBe(true);
  });

  test('should redirect to login when session is cleared', async ({ page, context }) => {
    await loginViaKeycloak(page, TEST_USER, TEST_PASS);

    // Clear cookies to simulate expired session
    await context.clearCookies();

    // Navigate — should redirect to login
    await page.goto('/home/workflows');

    // Should end up on Keycloak or Knox login
    await page.waitForTimeout(3000);
    const url = page.url();
    const redirected = url.includes('/realms/') || url.includes('/auth/');
    expect(redirected).toBe(true);
  });
});
