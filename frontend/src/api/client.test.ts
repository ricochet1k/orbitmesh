import { describe, it, expect, vi, beforeEach } from "vitest";
import { apiClient } from "./client";

const guardrailsPayload = [
  {
    id: "session-inspection",
    title: "Inspect sessions",
    allowed: true,
    detail: "Live telemetry stays read-only unless your guardrail allows inspection.",
  },
  {
    id: "role-escalation",
    title: "Role escalations",
    allowed: false,
    detail: "Role edits are hidden until an owner approves escalation.",
  },
  {
    id: "template-authoring",
    title: "Template authoring",
    allowed: true,
    detail: "Template workflows stay available for curated drafts.",
  },
  {
    id: "bulk-operations",
    title: "Bulk operations",
    allowed: false,
    detail: "Bulk commits require higher-level guardrails before they become active.",
  },
  {
    id: "csrf-protection",
    title: "CSRF validation",
    allowed: true,
    detail: "State-changing requests double-submit a SameSite cookie and header.",
  },
  {
    id: "audit-integrity",
    title: "Audit integrity",
    allowed: true,
    detail: "High-privilege changes generate immutable audit events and alerts.",
  },
];

const permissionsPayload = {
  role: "developer",
  can_inspect_sessions: true,
  can_manage_roles: false,
  can_manage_templates: true,
  can_initiate_bulk_actions: false,
  requires_owner_approval_for_role_changes: true,
  guardrails: guardrailsPayload,
};

describe("apiClient", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
    document.cookie = "orbitmesh-csrf-token=test-token";
  });

  it("listSessions fetches sessions successfully", async () => {
    const mockResponse = {
      sessions: [
        { id: "1", provider_type: "native", state: "running" }
      ]
    };

    (fetch as any).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockResponse)
    });

    const result = await apiClient.listSessions();
    expect(fetch).toHaveBeenCalledWith("/api/sessions");
    expect(result).toEqual(mockResponse);
  });

  it("createSession sends POST request with CSRF token", async () => {
    const req = { provider_type: "native", working_dir: "/tmp" };
    const mockResponse = { id: "1", ...req, state: "created" };

    (fetch as any).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockResponse)
    });

    const result = await apiClient.createSession(req as any);
    expect(fetch).toHaveBeenCalledWith("/api/sessions", expect.objectContaining({
      method: "POST",
      headers: expect.objectContaining({
        "Content-Type": "application/json",
        "X-CSRF-Token": "test-token"
      }),
      body: JSON.stringify(req)
    }));
    expect(result).toEqual(mockResponse);
  });

  it("getSession fetches single session", async () => {
    const mockResponse = { id: "1", state: "running", metrics: { tokens_in: 0, tokens_out: 0, request_count: 0 } };

    (fetch as any).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockResponse)
    });

    const result = await apiClient.getSession("1");
    expect(fetch).toHaveBeenCalledWith("/api/sessions/1");
    expect(result).toEqual(mockResponse);
  });

  it("stopSession sends DELETE request with CSRF token", async () => {
    (fetch as any).mockResolvedValue({ ok: true });

    await apiClient.stopSession("1");
    expect(fetch).toHaveBeenCalledWith("/api/sessions/1", expect.objectContaining({
      method: "DELETE",
      headers: expect.objectContaining({
        "X-CSRF-Token": "test-token"
      }),
    }));
  });

  it("getPermissions fetches guardrail details", async () => {
    (fetch as any).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(permissionsPayload),
    });

    const result = await apiClient.getPermissions();
    expect(fetch).toHaveBeenCalledWith("/api/v1/me/permissions");
    expect(result).toEqual(permissionsPayload);
  });

  it("sanitizes guardrail guidance content", async () => {
    const dirtyGuardrails = guardrailsPayload.map((guardrail) =>
      guardrail.id === "bulk-operations"
        ? { ...guardrail, detail: "Use <em>token</em>: abc123 and Bearer abc.def" }
        : guardrail,
    );
    const dirtyPermissions = { ...permissionsPayload, guardrails: dirtyGuardrails };

    (fetch as any).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(dirtyPermissions),
    });

    const result = await apiClient.getPermissions();
    const sanitizedDetail = result.guardrails.find((item) => item.id === "bulk-operations")?.detail;
    expect(sanitizedDetail).toBe("Use token: [redacted] and Bearer [redacted]");
  });

  it("defaults missing guardrails to an empty array", async () => {
    const payloadWithoutGuardrails = { ...permissionsPayload } as any;
    delete payloadWithoutGuardrails.guardrails;

    (fetch as any).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(payloadWithoutGuardrails),
    });

    const result = await apiClient.getPermissions();
    expect(result.guardrails).toEqual([]);
  });

  it("throws error when response is not ok", async () => {
    (fetch as any).mockResolvedValue({
      ok: false,
      text: () => Promise.resolve("Error message")
    });

    await expect(apiClient.listSessions()).rejects.toThrow("Error message");
  });
});
