import { describe, it, expect, vi, beforeEach } from "vitest";
import { apiClient } from "./client";

const permissionsPayload = {
  role: "developer",
  can_inspect_sessions: true,
  can_manage_roles: false,
  can_manage_templates: true,
  can_initiate_bulk_actions: false,
  requires_owner_approval_for_role_changes: true,
};

describe("apiClient", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
    document.cookie = "orbitmesh-csrf-token=test-token";
    window.localStorage.clear();
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
    expect(result.sessions[0].state).toBe("running");
    expect(window.localStorage.getItem("orbitmesh:sessions")).toBeTruthy();
  });

  it("listSessions merges cached sessions", async () => {
    const cached = {
      sessions: [
        { id: "cached", provider_type: "native", state: "idle" }
      ]
    };
    window.localStorage.setItem("orbitmesh:sessions", JSON.stringify(cached));

    const mockResponse = {
      sessions: [
        { id: "live", provider_type: "native", state: "running" }
      ]
    };

    (fetch as any).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockResponse)
    });

    const result = await apiClient.listSessions();
    expect(result.sessions).toHaveLength(2);
    expect(result.sessions[0].id).toBe("live");
    expect(result.sessions[0].state).toBe("running");
    expect(result.sessions[1].id).toBe("cached");
    expect(result.sessions[1].state).toBe("idle");
  });

  it("listSessions returns cached sessions on failure", async () => {
    const cached = {
      sessions: [
        { id: "cached", provider_type: "native", state: "idle" }
      ]
    };
    window.localStorage.setItem("orbitmesh:sessions", JSON.stringify(cached));

    (fetch as any).mockResolvedValue({
      ok: false,
      text: () => Promise.resolve("Error message")
    });

    const result = await apiClient.listSessions();
    expect(result.sessions[0].id).toBe("cached");
    expect(result.sessions[0].state).toBe("idle");
  });

  it("createSession sends POST request with CSRF token", async () => {
    const req = { provider_type: "native", working_dir: "/tmp" };
    const mockResponse = { id: "1", ...req, state: "idle" };

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
    expect(result.state).toBe("idle");
  });

  it("createTaskSession sends task metadata", async () => {
    const mockResponse = { id: "99", provider_type: "adk", state: "idle" };

    (fetch as any).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockResponse)
    });

    const result = await apiClient.createTaskSession({
      taskId: "task-9",
      taskTitle: "Start agent flow",
      providerType: "adk",
    });

    expect(fetch).toHaveBeenCalledWith("/api/sessions", expect.objectContaining({
      method: "POST",
      headers: expect.objectContaining({
        "Content-Type": "application/json",
        "X-CSRF-Token": "test-token"
      }),
      body: JSON.stringify({
        provider_type: "adk",
        task_id: "task-9",
        task_title: "Start agent flow"
      })
    }));
    expect(result.state).toBe("idle");
  });

  it("createDockSession tags dock sessions", async () => {
    const mockResponse = { id: "dock-1", provider_type: "claude-ws", state: "idle" };

    (fetch as any).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockResponse)
    });

    const result = await apiClient.createDockSession();

    expect(fetch).toHaveBeenCalledWith("/api/sessions", expect.objectContaining({
      method: "POST",
      headers: expect.objectContaining({
        "Content-Type": "application/json",
        "X-CSRF-Token": "test-token"
      }),
      body: JSON.stringify({
        provider_type: "claude-ws",
        session_kind: "dock"
      })
    }));
    expect(result.state).toBe("idle");
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

  it("getTerminal fetches terminal by id", async () => {
    const mockResponse = { id: "term-1", terminal_kind: "pty" };

    (fetch as any).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockResponse)
    });

    const result = await apiClient.getTerminal("term-1");
    expect(fetch).toHaveBeenCalledWith("/api/v1/terminals/term-1");
    expect(result).toEqual(mockResponse);
  });

  it("getTerminalSnapshotById fetches terminal snapshot", async () => {
    const mockResponse = { rows: 2, cols: 2, lines: ["hi", ""] };

    (fetch as any).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockResponse)
    });

    const result = await apiClient.getTerminalSnapshotById("term-1");
    expect(fetch).toHaveBeenCalledWith("/api/v1/terminals/term-1/snapshot");
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

  it("sendSessionInput sends POST request with CSRF token", async () => {
    (fetch as any).mockResolvedValue({ ok: true });

    await apiClient.sendSessionInput("1", "hello");

    expect(fetch).toHaveBeenCalledWith(
      "/api/sessions/1/input",
      expect.objectContaining({
        method: "POST",
        headers: expect.objectContaining({
          "Content-Type": "application/json",
          "X-CSRF-Token": "test-token",
        }),
        body: JSON.stringify({ input: "hello" }),
      }),
    );
  });

  it("getPermissions fetches permissions", async () => {
    (fetch as any).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(permissionsPayload),
    });

    const result = await apiClient.getPermissions();
    expect(fetch).toHaveBeenCalledWith("/api/v1/me/permissions");
    expect(result).toEqual(permissionsPayload);
  });

  it("throws error when response is not ok", async () => {
    (fetch as any).mockResolvedValue({
      ok: false,
      text: () => Promise.resolve("Error message")
    });

    await expect(apiClient.listSessions()).rejects.toThrow("Error message");
  });

  it("prefers JSON error payloads when available", async () => {
    (fetch as any).mockResolvedValue({
      ok: false,
      text: () => Promise.resolve(JSON.stringify({ error: "Unknown provider type" }))
    });

    await expect(apiClient.createSession({ provider_type: "mystery" } as any)).rejects.toThrow(
      "Unknown provider type"
    );
  });
});
