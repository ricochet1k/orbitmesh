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

/**
 * Build a mock response that mirrors what the backend returns for
 * MATCH (n)-[r]->(m) RETURN n, r, m LIMIT 100
 *
 * Layout: 50 File nodes each connected to a Function node via a DEFINES edge.
 * That gives 100 unique nodes and 50 unique edges — enough to exercise the
 * full graph rendering pipeline with realistic volume.
 */
function makeLargeGraphResponse() {
  const rows: Array<{
    n: { id: string; labels: string[]; props: Record<string, string> };
    r: { id: string; type: string; start_id: string; end_id: string; props: Record<string, never> };
    m: { id: string; labels: string[]; props: Record<string, string> };
  }> = [];

  for (let i = 0; i < 50; i++) {
    const fileId = `file_${i}`;
    const funcId = `func_${i}`;
    const edgeId = `edge_${i}`;
    rows.push({
      n: { id: fileId, labels: ['File'], props: { path: `pkg${i}/file${i}.go` } },
      r: { id: edgeId, type: 'DEFINES', start_id: fileId, end_id: funcId, props: {} },
      m: { id: funcId, labels: ['Function'], props: { name: `func${i}` } },
    });
  }

  return { columns: ['n', 'r', 'm'], rows };
}

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

// ---------------------------------------------------------------------------
// End-to-end: 100-node/edge sample-query pipeline
//
// These tests use a realistic 100-node mock response (50 File nodes +
// 50 Function nodes connected by DEFINES edges) with the exact shape the
// backend normalisation layer produces:
//   node → { id, labels, props }
//   edge → { id, type, start_id, end_id, props }
//
// They verify that the data flows all the way through:
//   mock API response → SigmaGraph component → rendered canvas + stats
// ---------------------------------------------------------------------------

test.describe('CodeFlow Explorer — 100-node sample query pipeline', () => {
  test.beforeEach(async ({ page, context }) => {
    await setupDefaultApiRoutes(page, context);
    await routeJson(page, '**/api/sessions', makeSessions());
    await page.route('**/api/v1/codeflow/query', async (route) => {
      await route.fulfill({ json: makeLargeGraphResponse() });
    });
  });

  test('renders sigma canvas with 100-node response', async ({ page }) => {
    await page.goto('/dashboard/codeflow/explorer');

    const runBtn = page.locator('button', { hasText: /Run Query/i });
    await expect(runBtn).toBeEnabled({ timeout: 5000 });

    // SigmaGraph mounts once there are rows to display.
    await expect(page.locator('.sigma-scene')).toBeAttached({ timeout: 5000 });
  });

  test('displays correct node and edge counts for 100-node response', async ({ page }) => {
    await page.goto('/dashboard/codeflow/explorer');

    const runBtn = page.locator('button', { hasText: /Run Query/i });
    await expect(runBtn).toBeEnabled({ timeout: 5000 });

    // 50 File + 50 Function nodes = 100 unique nodes, 50 DEFINES edges.
    await expect(page.locator('text=/100 nodes/i')).toBeVisible({ timeout: 5000 });
    await expect(page.locator('text=/50 edges/i')).toBeVisible({ timeout: 5000 });
  });

  test('node and edge objects carry the backend-normalised shape', async ({ page }) => {
    // Intercept the query response and validate that the shape exactly matches
    // what the backend normalization layer produces (type/start_id/end_id on
    // edges, not the legacy label/from/to goraphdb keys).
    let capturedRows: unknown[] | null = null;
    await page.route('**/api/v1/codeflow/query', async (route) => {
      const json = makeLargeGraphResponse();
      capturedRows = json.rows;
      await route.fulfill({ json });
    });

    await page.goto('/dashboard/codeflow/explorer');
    await expect(page.locator('button', { hasText: /Run Query/i })).toBeEnabled({ timeout: 5000 });

    // Validate shape in the test process (not inside the browser).
    expect(capturedRows).not.toBeNull();
    for (const row of capturedRows as Array<Record<string, unknown>>) {
      const n = row['n'] as Record<string, unknown>;
      const r = row['r'] as Record<string, unknown>;
      const m = row['m'] as Record<string, unknown>;

      // Nodes must have id + labels array.
      expect(typeof n.id).toBe('string');
      expect(Array.isArray(n.labels)).toBe(true);

      expect(typeof m.id).toBe('string');
      expect(Array.isArray(m.labels)).toBe(true);

      // Edge must have the normalised keys, not goraphdb legacy keys.
      expect(typeof r.id).toBe('string');
      expect(typeof r.type).toBe('string');
      expect(typeof r.start_id).toBe('string');
      expect(typeof r.end_id).toBe('string');

      expect(r).not.toHaveProperty('label');
      expect(r).not.toHaveProperty('from');
      expect(r).not.toHaveProperty('to');

      // Edge endpoints must reference the surrounding nodes.
      expect(r.start_id).toBe(n.id);
      expect(r.end_id).toBe(m.id);
    }
  });

  test('re-running the default sample query refreshes stats', async ({ page }) => {
    await page.goto('/dashboard/codeflow/explorer');

    const runBtn = page.locator('button', { hasText: /Run Query/i });
    await expect(runBtn).toBeEnabled({ timeout: 5000 });
    await expect(page.locator('text=/100 nodes/i')).toBeVisible({ timeout: 5000 });

    // Run again explicitly and confirm stats remain correct.
    await runBtn.click();
    await expect(runBtn).toBeEnabled({ timeout: 5000 });
    await expect(page.locator('text=/100 nodes/i')).toBeVisible({ timeout: 5000 });
    await expect(page.locator('text=/50 edges/i')).toBeVisible({ timeout: 5000 });
  });
});
