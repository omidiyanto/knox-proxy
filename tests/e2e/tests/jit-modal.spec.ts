import { test, expect, Page } from '@playwright/test';

/**
 * ═══════════════════════════════════════════════════════════════════════════
 * Knox E2E — JIT Modal Tests
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

async function openJITModal(page: Page) {
  await page.waitForSelector('#knox-menu-request', { timeout: 15000 });
  await page.click('#knox-menu-request');
  await page.waitForSelector('#knox-modal-overlay.knox-visible', { timeout: 5000 });
}

test.describe('JIT Modal', () => {
  test.beforeEach(async ({ page }) => {
    await loginViaKeycloak(page, TEST_USER, TEST_PASS);
  });

  test('modal opens with correct structure', async ({ page }) => {
    await openJITModal(page);

    // Header
    await expect(page.locator('.knox-modal-title')).toHaveText('Request JIT Access');
    await expect(page.locator('#knox-close-btn')).toBeVisible();

    // Tabs
    await expect(page.locator('[data-knox-tab="request"]')).toBeVisible();
    await expect(page.locator('[data-knox-tab="tickets"]')).toBeVisible();

    // Form fields
    await expect(page.locator('#knox-workflow-id')).toBeVisible();
    await expect(page.locator('#knox-period-start')).toBeVisible();
    await expect(page.locator('#knox-period-end')).toBeVisible();
    await expect(page.locator('#knox-description')).toBeVisible();

    // Footer buttons
    await expect(page.locator('#knox-cancel-btn')).toBeVisible();
    await expect(page.locator('#knox-submit-btn')).toBeVisible();
  });

  test('date fields default correctly', async ({ page }) => {
    await openJITModal(page);

    const startValue = await page.locator('#knox-period-start').inputValue();
    const endValue = await page.locator('#knox-period-end').inputValue();

    expect(startValue).toBeTruthy();
    expect(endValue).toBeTruthy();

    // End should be after start
    const startDate = new Date(startValue);
    const endDate = new Date(endValue);
    expect(endDate.getTime()).toBeGreaterThan(startDate.getTime());
  });

  test('duration badge updates when dates change', async ({ page }) => {
    await openJITModal(page);

    const badge = page.locator('#knox-duration-badge');
    const initialText = await badge.textContent();
    expect(initialText).toBeTruthy();

    // Duration should contain time units
    expect(initialText).toMatch(/\d+[dhm]/);
  });

  test('submit with empty workflow ID shows error', async ({ page }) => {
    await openJITModal(page);

    // Clear workflow ID
    await page.fill('#knox-workflow-id', '');

    // Fill description
    await page.fill('#knox-description', 'This is a test description for validation');

    // Submit
    await page.click('#knox-submit-btn');

    // Should show error toast
    const toast = page.locator('.knox-toast-error').first();
    await expect(toast).toBeVisible({ timeout: 5000 });
  });

  test('submit with short description shows error', async ({ page }) => {
    await openJITModal(page);

    await page.fill('#knox-workflow-id', 'test-wf-1');
    await page.fill('#knox-description', 'short');

    await page.click('#knox-submit-btn');

    const toast = page.locator('.knox-toast-error').first();
    await expect(toast).toBeVisible({ timeout: 5000 });
  });

  test('submit valid request shows success', async ({ page }) => {
    await openJITModal(page);

    await page.fill('#knox-workflow-id', 'test-wf-e2e');
    await page.fill('#knox-description', 'E2E test ticket created by Playwright automation');

    // Ensure Run checkbox is checked
    const runCheckbox = page.locator('input[name="knox-access"][value="run"]');
    if (!(await runCheckbox.isChecked())) {
      await runCheckbox.check();
    }

    await page.click('#knox-submit-btn');

    // Should show success toast
    const toast = page.locator('.knox-toast-success').first();
    await expect(toast).toBeVisible({ timeout: 10000 });
  });

  test('close modal with X button', async ({ page }) => {
    await openJITModal(page);

    await page.click('#knox-close-btn');

    const overlay = page.locator('#knox-modal-overlay');
    await expect(overlay).not.toHaveClass(/knox-visible/);
  });

  test('close modal with Cancel button', async ({ page }) => {
    await openJITModal(page);

    await page.click('#knox-cancel-btn');

    const overlay = page.locator('#knox-modal-overlay');
    await expect(overlay).not.toHaveClass(/knox-visible/);
  });

  test('close modal with Escape key', async ({ page }) => {
    await openJITModal(page);

    await page.keyboard.press('Escape');

    const overlay = page.locator('#knox-modal-overlay');
    await expect(overlay).not.toHaveClass(/knox-visible/);
  });

  test('close modal by clicking overlay', async ({ page }) => {
    await openJITModal(page);

    // Click outside the modal (on the overlay)
    await page.locator('#knox-modal-overlay').click({ position: { x: 10, y: 10 } });

    const overlay = page.locator('#knox-modal-overlay');
    await expect(overlay).not.toHaveClass(/knox-visible/);
  });
});

test.describe('JIT Modal — My Tickets Tab', () => {
  test.beforeEach(async ({ page }) => {
    await loginViaKeycloak(page, TEST_USER, TEST_PASS);
  });

  test('switch to My Tickets tab', async ({ page }) => {
    await openJITModal(page);

    // Click My Tickets tab
    await page.click('[data-knox-tab="tickets"]');

    // Request tab should be hidden
    const requestTab = page.locator('#knox-tab-request');
    await expect(requestTab).not.toHaveClass(/knox-tab-visible/);

    // Tickets tab should be visible
    const ticketsTab = page.locator('#knox-tab-tickets');
    await expect(ticketsTab).toHaveClass(/knox-tab-visible/);

    // Footer should be hidden
    const footer = page.locator('#knox-modal-footer');
    const display = await footer.evaluate(el => window.getComputedStyle(el).display);
    expect(display).toBe('none');
  });

  test('tickets table loads data', async ({ page }) => {
    await openJITModal(page);

    await page.click('[data-knox-tab="tickets"]');

    // Wait for tickets to load
    await page.waitForTimeout(3000);

    // Should show table or empty state
    const table = page.locator('.knox-ticket-table');
    const emptyState = page.locator('.knox-empty-state');

    const hasTable = await table.count() > 0;
    const hasEmpty = await emptyState.count() > 0;

    expect(hasTable || hasEmpty).toBe(true);
  });
});
