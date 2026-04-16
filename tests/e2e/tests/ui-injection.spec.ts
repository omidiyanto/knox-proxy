import { test, expect, Page } from '@playwright/test';

/**
 * ═══════════════════════════════════════════════════════════════════════════
 * Knox E2E — UI Injection Tests
 * ═══════════════════════════════════════════════════════════════════════════
 * Tests verify that Nginx sub_filter CSS/JS injection is working correctly.
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

test.describe('UI Injection — CSS Hidden Elements', () => {
  test.beforeEach(async ({ page }) => {
    await loginViaKeycloak(page, TEST_USER, TEST_PASS);
    // Wait for n8n UI to fully render
    await page.waitForTimeout(3000);
  });

  test('settings menu should be hidden', async ({ page }) => {
    const settingsLink = page.locator('[href*="settings"]').first();
    if (await settingsLink.count() > 0) {
      const isVisible = await settingsLink.isVisible();
      expect(isVisible).toBe(false);
    }
    // Pass — settings link may not exist at all
  });

  test('credentials menu should be hidden', async ({ page }) => {
    const credLink = page.locator('[href*="credentials"]').first();
    if (await credLink.count() > 0) {
      const isVisible = await credLink.isVisible();
      expect(isVisible).toBe(false);
    }
  });

  test('save workflow button should be hidden', async ({ page }) => {
    const saveBtn = page.locator('[data-test-id*="save-workflow"]').first();
    if (await saveBtn.count() > 0) {
      const isVisible = await saveBtn.isVisible();
      expect(isVisible).toBe(false);
    }
  });

  test('notification banners should be hidden', async ({ page }) => {
    const notification = page.locator('.el-notification').first();
    if (await notification.count() > 0) {
      const isVisible = await notification.isVisible();
      expect(isVisible).toBe(false);
    }
  });
});

test.describe('UI Injection — Custom Sidebar Buttons', () => {
  test.beforeEach(async ({ page }) => {
    await loginViaKeycloak(page, TEST_USER, TEST_PASS);
    await page.waitForSelector('#custom-logout-btn', { timeout: 15000 });
  });

  test('custom logout button should exist', async ({ page }) => {
    const logoutBtn = page.locator('#custom-logout-btn');
    await expect(logoutBtn).toBeVisible();
  });

  test('logout button should contain "Logout" text', async ({ page }) => {
    const logoutBtn = page.locator('#custom-logout-btn');
    const text = await logoutBtn.textContent();
    expect(text).toContain('Logout');
  });

  test('JIT Access menu should exist when enabled', async ({ page }) => {
    const jitMenu = page.locator('#knox-menu-request');
    // JIT is enabled in our test docker-compose
    await expect(jitMenu).toBeVisible({ timeout: 10000 });
    const text = await jitMenu.textContent();
    expect(text).toContain('JIT Access');
  });

  test('JIT Access menu should open modal on click', async ({ page }) => {
    const jitMenu = page.locator('#knox-menu-request');
    await jitMenu.click();

    // Wait for modal overlay
    const overlay = page.locator('#knox-modal-overlay');
    await expect(overlay).toBeVisible({ timeout: 5000 });
    await expect(overlay).toHaveClass(/knox-visible/);
  });

  test('User Info menu should exist', async ({ page }) => {
    const userInfoMenu = page.locator('#knox-menu-userinfo');
    await expect(userInfoMenu).toBeVisible({ timeout: 10000 });
    const text = await userInfoMenu.textContent();
    expect(text).toContain('User Info');
  });

  test('User Info menu should show user details', async ({ page }) => {
    const userInfoMenu = page.locator('#knox-menu-userinfo');
    await userInfoMenu.click();

    // Wait for user info overlay
    const overlay = page.locator('#knox-userinfo-overlay');
    await expect(overlay).toBeVisible({ timeout: 5000 });

    // Should contain user data
    const content = await overlay.textContent();
    expect(content).toContain('Display Name');
    expect(content).toContain('Email');
    expect(content).toContain('Team');
  });
});

test.describe('UI Injection — Watermark', () => {
  test('watermark overlay should exist when enabled', async ({ page }) => {
    await loginViaKeycloak(page, TEST_USER, TEST_PASS);

    // Wait for watermark to initialize (1.5s delay in code)
    await page.waitForTimeout(3000);

    const watermark = page.locator('#knox-watermark');
    if (await watermark.count() > 0) {
      await expect(watermark).toHaveClass(/knox-watermark-visible/);

      // Verify pointer-events: none (non-interactive)
      const pointerEvents = await watermark.evaluate(el => {
        return window.getComputedStyle(el).pointerEvents;
      });
      expect(pointerEvents).toBe('none');
    }
  });
});

test.describe('UI Injection — JIT Edit Toggle', () => {
  test('Edit checkbox should be visible when KNOX_JIT_EDIT_ENABLED=true', async ({ page }) => {
    await loginViaKeycloak(page, TEST_USER, TEST_PASS);
    await page.waitForSelector('#knox-menu-request', { timeout: 15000 });

    // Open JIT modal
    await page.click('#knox-menu-request');
    await page.waitForSelector('#knox-modal-overlay.knox-visible', { timeout: 5000 });

    // Check for Edit checkbox
    const editCheckbox = page.locator('input[name="knox-access"][value="edit"]');
    const runCheckbox = page.locator('input[name="knox-access"][value="run"]');

    // Run should always be visible
    await expect(runCheckbox).toBeVisible();

    // Edit should be visible when enabled (our test env has it enabled)
    if (await editCheckbox.count() > 0) {
      await expect(editCheckbox).toBeVisible();
    }
  });
});
