import { test, expect } from '@playwright/test';

test('codeflow explorer basic functionality', async ({ page }) => {
  // Mock the graph query endpoint
  await page.route('**/api/v1/codeflow/query', async route => {
    const json = {
      columns: ['n', 'r', 'm'],
      rows: [
        {
          n: { id: "node1", labels: ["File"], props: { name: "test.go" } },
          m: { id: "node2", labels: ["Function"], props: { name: "main" } },
          r: { id: "edge1", type: "DEFINES", start_id: "node1", end_id: "node2" }
        }
      ]
    };
    await route.fulfill({ json });
  });

  // Start at dashboard
  await page.goto('/');

  // Navigate to CodeFlow Explorer
  await page.click('text="CodeFlow Explorer"');
  await expect(page).toHaveURL(/.*\/dashboard\/codeflow\/explorer/);

  // Verify UI elements
  await expect(page.locator('h1', { hasText: 'CodeFlow Explorer' })).toBeVisible();
  const runBtn = page.locator('button', { hasText: 'Run Query' });
  await expect(runBtn).toBeVisible();

  // Pick a sample query
  const select = page.locator('select');
  await select.selectOption({ label: 'API Handlers' });
  const textarea = page.locator('textarea');
  await expect(textarea).toHaveValue(/HANDLES_ROUTE/);

  // Click run
  await runBtn.click();

  // Wait for the graph canvas to appear
  // We mock the API so it should finish quickly. Sigma js canvas will be rendered inside SigmaGraph
  const canvas = page.locator('.sigma-scene');
  await expect(canvas).toBeAttached({ timeout: 5000 });
});
