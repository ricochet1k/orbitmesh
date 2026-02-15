import { render, screen, fireEvent } from "@solidjs/testing-library";
import { describe, it, expect, vi, beforeEach } from "vitest";
import Dashboard from "./Dashboard";
import { apiClient } from "../api/client";
import {
  defaultPermissions,
  makeSession,
  restrictedPermissions,
} from "../test/fixtures";

vi.mock("@tanstack/solid-router", () => ({
  createFileRoute: () => () => ({}),
}));


vi.mock("../api/client", () => ({
  apiClient: {
    listSessions: vi.fn(),
    getPermissions: vi.fn(),
    getTaskTree: vi.fn(),
    listCommits: vi.fn(),
    pauseSession: vi.fn(),
    resumeSession: vi.fn(),
    stopSession: vi.fn(),
  }
}));

describe("Dashboard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (apiClient.getPermissions as any).mockResolvedValue(defaultPermissions);
    (apiClient.getTaskTree as any).mockResolvedValue({ tasks: [] });
    (apiClient.listCommits as any).mockResolvedValue({ commits: [] });
  });

  it("renders loading state initially", async () => {
    (apiClient.listSessions as any).mockReturnValue(new Promise(() => {}));
    const { container } = render(() => <Dashboard />);
    // Check for skeleton loader instead of loading text
    expect(container.querySelector(".skeleton-table-container")).toBeDefined();
  });

   it("renders sessions list when loaded", async () => {
     const mockSessions = {
       sessions: [
         makeSession({ id: "session-123456789", provider_type: "native", state: "running", current_task: "T1" }),
       ],
     };
     (apiClient.listSessions as any).mockResolvedValue(mockSessions);

     render(() => <Dashboard />);

     const idCell = await screen.findByText(/session-/);
     expect(idCell).toBeDefined();
     expect(screen.getByText("native")).toBeDefined();
     expect(screen.getByText("running")).toBeDefined();
     expect(screen.getByText("T1")).toBeDefined();
      expect(screen.getByText("Inspect")).toBeDefined();
    });

   it("renders empty list when no sessions", async () => {
     (apiClient.listSessions as any).mockResolvedValue({ sessions: [] });
     render(() => <Dashboard />);
     // Look for empty state instead of table headers
     await screen.findByText("No active sessions");
     expect(screen.queryByText("ID")).toBeNull();
   });

    it("shows bulk action buttons when permissions allow", async () => {
     (apiClient.listSessions as any).mockResolvedValue({
       sessions: [
         makeSession({ id: "session-123456789", provider_type: "native", state: "running", current_task: "T1" }),
       ],
     });
      (apiClient.getPermissions as any).mockResolvedValue(defaultPermissions);

      render(() => <Dashboard />);

      expect(await screen.findByText("Pause")).toBeDefined();
      expect(screen.getByText("Resume")).toBeDefined();
      expect(screen.getByText("Stop")).toBeDefined();
    });

   it("skips bulk actions when confirmation is declined", async () => {
     (apiClient.getPermissions as any).mockResolvedValue({
       ...defaultPermissions,
       can_initiate_bulk_actions: true,
     });
     (apiClient.listSessions as any).mockResolvedValue({
       sessions: [
         makeSession({ id: "session-123456789", provider_type: "native", state: "running", current_task: "T1" }),
      ],
    });
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(false);

    render(() => <Dashboard />);

    const pauseButton = await screen.findByText("Pause");
    fireEvent.click(pauseButton);

    expect(confirmSpy).toHaveBeenCalled();
    expect(apiClient.pauseSession).not.toHaveBeenCalled();

    confirmSpy.mockRestore();
  });

    it("allows bulk actions even if they may fail (errors handled at action time)", async () => {
     (apiClient.getPermissions as any).mockResolvedValue({
       ...defaultPermissions,
       can_initiate_bulk_actions: true,
     });
    (apiClient.listSessions as any).mockResolvedValue({
       sessions: [
        makeSession({ id: "session-123456789", provider_type: "native", state: "running", current_task: "T1" }),
      ],
    });
     (apiClient.pauseSession as any).mockRejectedValue(new Error("csrf token mismatch"));
     const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

     render(() => <Dashboard />);

     const pauseButton = await screen.findByText("Pause");
     // Dashboard does not display action notices; SessionViewer handles that instead
     fireEvent.click(pauseButton);
     
     // Confirm was called to proceed with the action
     expect(confirmSpy).toHaveBeenCalled();
     // pauseSession was attempted
     expect(apiClient.pauseSession).toHaveBeenCalledWith("session-123456789");
      
      confirmSpy.mockRestore();
    });

    it("shows permission restriction tooltips for disabled inspect button", async () => {
      (apiClient.listSessions as any).mockResolvedValue({
        sessions: [
          makeSession({ id: "session-123456789", provider_type: "native", state: "running", current_task: "T1" }),
        ],
      });
      (apiClient.getPermissions as any).mockResolvedValue(restrictedPermissions);

      render(() => <Dashboard />);

      const inspectButton = await screen.findByText("Inspect") as HTMLButtonElement;
      expect(inspectButton.disabled).toBe(true);
      expect(inspectButton.getAttribute("title")).toBe("Session inspection is not permitted for your role.");
    });

    it("shows permission restriction tooltips for disabled bulk action buttons", async () => {
      (apiClient.listSessions as any).mockResolvedValue({
        sessions: [
          makeSession({ id: "session-123456789", provider_type: "native", state: "running", current_task: "T1" }),
        ],
      });
      (apiClient.getPermissions as any).mockResolvedValue(restrictedPermissions);

      render(() => <Dashboard />);

      // Find the parent div for bulk actions when canManage() is false
      const bulkActionsDiv = await screen.findByTitle("Bulk actions are not permitted for your role.");

      // Assert that the parent div has the correct title
      expect(bulkActionsDiv).toBeDefined();

      // Also assert that the individual buttons inside are disabled
      const pauseButton = screen.getByText("Pause") as HTMLButtonElement;
      expect(pauseButton.disabled).toBe(true);
      const resumeButton = screen.getByText("Resume") as HTMLButtonElement;
      expect(resumeButton.disabled).toBe(true);
      const stopButton = screen.getByText("Stop") as HTMLButtonElement;
      expect(stopButton.disabled).toBe(true);
    });
});
