import { test, expect } from '@playwright/test';
import { setupDefaultApiRoutes, routeJson } from '../support/api';
import { makeSessions } from '../support/fixtures';

const MOCK_GRAPH_RESPONSE = {
  columns: ['n', 'r', 'm'],
  rows: [
    {
      n: { id: 'node1', labels: ['File'], props: { name: 'test.go' } },
      m: { id: 'node2', labels: ['Function'], props: { name: 'main' } },
      r: { id: 'edge1', type: 'DEFINES', start_id: 'node1', end_id: 'node2' },
    },
  ],
};

test.describe('CodeFlow Explorer', () => {
  test.beforeEach(async ({ page, context }) => {
    await setupDefaultApiRoutes(page, context);
    await routeJson(page, '**/api/sessions', makeSessions());

    // Mock the codeflow query endpoint for all queries
    await page.route('**/api/v1/codeflow/query', async (route) => {
      await route.fulfill({ json: MOCK_GRAPH_RESPONSE });
    });
  });

  test('page loads with heading and query controls', async ({ page }) => {
    await page.goto('/dashboard/codeflow/explorer');

    await expect(page.locator('h1', { hasText: 'CodeFlow Explorer' })).toBeVisible();
    await expect(page.locator('textarea')).toBeVisible();
    await expect(page.locator('button', { hasText: /Run Query/i })).toBeVisible();
    await expect(page.locator('select')).toBeVisible();
  });

  test('runs default query on mount and renders graph', async ({ page }) => {
    await page.goto('/dashboard/codeflow/explorer');

    // Wait for the button to become enabled — this means the onMount query resolved
    const runBtn = page.locator('button', { hasText: /Run Query/i });
    await expect(runBtn).toBeEnabled({ timeout: 5000 });

    // The initial query returned rows → SigmaGraph should be mounted → sigma creates its canvas
    const sigmaCanvas = page.locator('.sigma-scene');
    await expect(sigmaCanvas).toBeAttached({ timeout: 5000 });
  });

  test('selecting a sample query updates the textarea', async ({ page }) => {
    await page.goto('/dashboard/codeflow/explorer');

    // Wait for page to settle
    await expect(page.locator('h1', { hasText: 'CodeFlow Explorer' })).toBeVisible();

    const select = page.locator('select');
    await select.selectOption({ label: 'API Handlers' });

    const textarea = page.locator('textarea');
    await expect(textarea).toHaveValue(/HANDLES_ROUTE/);
  });

  test('clicking Run Query after selecting a sample query fetches data and renders graph', async ({ page }) => {
    await page.goto('/dashboard/codeflow/explorer');

    // Wait for initial onMount query to complete so the button is enabled
    const runBtn = page.locator('button', { hasText: /Run Query/i });
    await expect(runBtn).toBeEnabled({ timeout: 5000 });

    // Select the "API Handlers" sample query
    const select = page.locator('select');
    await select.selectOption({ label: 'API Handlers' });
    const textarea = page.locator('textarea');
    await expect(textarea).toHaveValue(/HANDLES_ROUTE/);

    // Start waiting for the network request before clicking
    const queryRequest = page.waitForRequest(
      (req) => req.url().includes('/api/v1/codeflow/query') && req.method() === 'POST',
    );

    await runBtn.click();

    // Verify the request was fired with the expected query
    const req = await queryRequest;
    const body = JSON.parse(req.postData() ?? '{}') as { query: string };
    expect(body.query).toContain('HANDLES_ROUTE');

    // Graph canvas should remain attached (mounted from the initial query)
    const sigmaCanvas = page.locator('.sigma-scene');
    await expect(sigmaCanvas).toBeAttached({ timeout: 5000 });
  });

  test('shows error message when query fails', async ({ page }) => {
    // Override to simulate a server error
    await page.route('**/api/v1/codeflow/query', async (route) => {
      await route.fulfill({ status: 500, json: { error: 'internal server error' } });
    });

    await page.goto('/dashboard/codeflow/explorer');

    // Wait for the initial query to fail and the error to appear
    await expect(page.locator('text=/Error:/i')).toBeVisible({ timeout: 5000 });

    // Graph canvas should NOT be in the DOM since no data was returned
    const sigmaCanvas = page.locator('.sigma-scene');
    await expect(sigmaCanvas).not.toBeAttached({ timeout: 2000 });
  });

  test('shows empty state message when query returns no rows', async ({ page }) => {
    await page.route('**/api/v1/codeflow/query', async (route) => {
      await route.fulfill({ json: { columns: ['n'], rows: [] } });
    });

    await page.goto('/dashboard/codeflow/explorer');

    const runBtn = page.locator('button', { hasText: /Run Query/i });
    await expect(runBtn).toBeEnabled({ timeout: 5000 });

    await expect(page.locator('text=/No results found/i')).toBeVisible({ timeout: 5000 });
    await expect(page.locator('.sigma-scene')).not.toBeAttached({ timeout: 2000 });
  });
});
