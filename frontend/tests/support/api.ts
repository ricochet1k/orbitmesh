import type { BrowserContext, Page, Request, TestInfo } from "@playwright/test"
import { csrfCookie, defaultPermissions, emptyCommits, emptyProviders, emptyTaskTree } from "./fixtures"

type RouteJsonOptions = {
  status?: number
  method?: string
  label?: string
}

type MethodRoute = {
  status?: number
  json?: unknown
  body?: string
}

type ApiLogEntry = {
  direction: "request" | "response"
  method?: string
  url: string
  status?: number
  postData?: string
}

const formatRouteError = (request: Request, expectedMethod: string, label?: string) => {
  const postData = request.postData()
  const hint = label ? ` [${label}]` : ""
  return [
    `Route method mismatch${hint}.`,
    `Expected: ${expectedMethod}`,
    `Actual: ${request.method()}`,
    `URL: ${request.url()}`,
    `Payload: ${postData ?? "<empty>"}`,
  ].join(" ")
}

const formatRouteMethodsError = (request: Request, expectedMethods: string[], label?: string) => {
  const postData = request.postData()
  const hint = label ? ` [${label}]` : ""
  return [
    `Route method mismatch${hint}.`,
    `Expected: ${expectedMethods.join(", ")}`,
    `Actual: ${request.method()}`,
    `URL: ${request.url()}`,
    `Payload: ${postData ?? "<empty>"}`,
  ].join(" ")
}

export const routeJson = async (
  page: Page,
  url: string | RegExp,
  json: unknown,
  options: RouteJsonOptions = {},
) => {
  await page.route(url, async (route) => {
    const request = route.request()
    if (options.method && request.method() !== options.method) {
      throw new Error(formatRouteError(request, options.method, options.label))
    }
    await route.fulfill({ status: options.status ?? 200, json })
  })
}

export const setupDefaultApiRoutes = async (page: Page, context: BrowserContext) => {
  await context.addCookies([csrfCookie])
  await routeJson(page, "**/api/v1/me/permissions", defaultPermissions, {
    label: "default-permissions",
  })
  await routeJson(page, "**/api/v1/tasks/tree", emptyTaskTree, { label: "task-tree" })
  await routeJson(page, "**/api/v1/commits**", emptyCommits, { label: "commits" })
  await routeJson(page, "**/api/v1/providers", emptyProviders, { label: "providers" })
  await page.route("**/api/sessions/*/events", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      headers: {
        "cache-control": "no-cache",
        connection: "keep-alive",
      },
      body: "event: heartbeat\ndata: {}\n\n",
    })
  })
}

export const routeByMethod = async (
  page: Page,
  url: string | RegExp,
  handlers: Record<string, MethodRoute>,
  label?: string,
) => {
  await page.route(url, async (route) => {
    const request = route.request()
    const handler = handlers[request.method()]
    if (!handler) {
      throw new Error(formatRouteMethodsError(request, Object.keys(handlers), label))
    }
    if (handler.json !== undefined) {
      await route.fulfill({ status: handler.status ?? 200, json: handler.json })
      return
    }
    await route.fulfill({ status: handler.status ?? 200, body: handler.body ?? "" })
  })
}

export const installApiLogger = (page: Page) => {
  const entries: ApiLogEntry[] = []
  const recordRequest = (request: Request) => {
    if (!request.url().includes("/api/")) {
      return
    }
    entries.push({
      direction: "request",
      method: request.method(),
      url: request.url(),
      postData: request.postData() ?? undefined,
    })
  }
  const recordResponse = async (response: { url: () => string; status: () => number }) => {
    const url = response.url()
    if (!url.includes("/api/")) {
      return
    }
    entries.push({
      direction: "response",
      url,
      status: response.status(),
    })
  }

  page.on("request", recordRequest)
  page.on("response", recordResponse)

  const attachOnFailure = async (testInfo: TestInfo) => {
    if (testInfo.status === "passed") {
      return
    }
    await testInfo.attach("api-log.json", {
      body: JSON.stringify(entries, null, 2),
      contentType: "application/json",
    })
  }

  return { entries, attachOnFailure }
}
