import { render, screen } from "@solidjs/testing-library";
import { describe, it, expect, vi, beforeEach } from "vitest";
import Dashboard from "./Dashboard";
import { apiClient } from "../api/client";

vi.mock("../api/client", () => ({
  apiClient: {
    listSessions: vi.fn()
  }
}));

describe("Dashboard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders loading state initially", async () => {
    (apiClient.listSessions as any).mockReturnValue(new Promise(() => {})); // Never resolves
    render(() => <Dashboard />);
    expect(screen.getByText("Loading sessions...")).toBeDefined();
  });

  it("renders sessions list when loaded", async () => {
    const mockSessions = {
      sessions: [
        { id: "session-123456789", provider_type: "native", state: "running", current_task: "T1" }
      ]
    };
    (apiClient.listSessions as any).mockResolvedValue(mockSessions);

    render(() => <Dashboard />);

    const idCell = await screen.findByText(/session-/);
    expect(idCell).toBeDefined();
    expect(screen.getByText("native")).toBeDefined();
    expect(screen.getByText("running")).toBeDefined();
    expect(screen.getByText("T1")).toBeDefined();
  });

  it("renders empty list when no sessions", async () => {
    (apiClient.listSessions as any).mockResolvedValue({ sessions: [] });
    render(() => <Dashboard />);
    
    // Wait for the table to appear (signaling loading is done)
    await screen.findByText("ID");
    
    expect(screen.queryByText("Loading sessions...")).toBeNull();
  });
});
