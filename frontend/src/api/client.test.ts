import { describe, it, expect, vi, beforeEach } from "vitest";
import { apiClient } from "./client";

describe("apiClient", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
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

  it("createSession sends POST request", async () => {
    const req = { provider_type: "native", working_dir: "/tmp" };
    const mockResponse = { id: "1", ...req, state: "created" };

    (fetch as any).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockResponse)
    });

    const result = await apiClient.createSession(req as any);
    expect(fetch).toHaveBeenCalledWith("/api/sessions", expect.objectContaining({
      method: "POST",
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

  it("stopSession sends DELETE request", async () => {
    (fetch as any).mockResolvedValue({ ok: true });

    await apiClient.stopSession("1");
    expect(fetch).toHaveBeenCalledWith("/api/sessions/1", expect.objectContaining({
      method: "DELETE"
    }));
  });

  it("throws error when response is not ok", async () => {
    (fetch as any).mockResolvedValue({
      ok: false,
      text: () => Promise.resolve("Error message")
    });

    await expect(apiClient.listSessions()).rejects.toThrow("Error message");
  });
});
