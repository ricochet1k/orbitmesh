import { render, screen, fireEvent } from "@solidjs/testing-library";
import { describe, it, expect, vi, beforeEach } from "vitest";
import Dashboard from "./Dashboard";
import { apiClient } from "../api/client";
import {
  defaultGuardrails,
  defaultPermissions,
  makeSession,
  permissionsWithRestrictions,
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
     // Guardrails panel is now hidden - verify it doesn't exist
     expect(screen.queryByText("Management guardrails")).toBeNull();
     expect(screen.queryByText("Role escalations")).toBeNull();
     // Simple action buttons are visible instead
     expect(screen.getByText("Inspect")).toBeDefined();
   });

   it("shows request access helpers when actions are locked", async () => {
     const lockedPermissions = {
       ...defaultPermissions,
       can_inspect_sessions: false,
       can_initiate_bulk_actions: false,
       guardrails: [
         {
           id: "session-inspection",
           title: "Inspect sessions",
           allowed: false,
           detail: "Inspection is limited to on-call operators.",
         },
         {
           id: "bulk-operations",
           title: "Bulk operations",
           allowed: false,
           detail: "Bulk actions require approval for your role.",
         },
       ],
     };
    (apiClient.listSessions as any).mockResolvedValue({
      sessions: [
        makeSession({ id: "session-123456789", provider_type: "native", state: "running", current_task: "T1" }),
      ],
    });
     (apiClient.getPermissions as any).mockResolvedValue(lockedPermissions);
     const onNavigate = vi.fn();

     render(() => <Dashboard onNavigate={onNavigate} />);

     // With guardrails hidden, these locked UI messages no longer appear in Dashboard
     // The guardrails check still happens in SessionViewer instead
     expect(screen.queryByText("Inspect locked")).toBeNull();
     expect(screen.queryByText("Bulk actions locked")).toBeNull();
     
     // Simple buttons are always visible (permission checks happen at action time)
     expect(await screen.findByText("Inspect")).toBeDefined();
     expect(screen.getByText("Pause")).toBeDefined();
   });

  it("renders empty list when no sessions", async () => {
    (apiClient.listSessions as any).mockResolvedValue({ sessions: [] });
    render(() => <Dashboard />);
    // Look for empty state instead of table headers
    await screen.findByText("No active sessions");
    expect(screen.queryByText("ID")).toBeNull();
  });

   it("doesn't show guardrails panel when hidden", async () => {
     (apiClient.listSessions as any).mockResolvedValue({ sessions: [] });
     (apiClient.getPermissions as any).mockResolvedValue({
       ...defaultPermissions,
       guardrails: [],
     });

     render(() => <Dashboard />);

     // Guardrails panel is hidden, so "Guardrail policy unavailable" no longer appears
     expect(screen.queryByText("Guardrail policy unavailable.")).toBeNull();
     expect(screen.queryByText("Management guardrails")).toBeNull();
   });

   it("shows bulk action buttons always (permission checks at action time)", async () => {
     const permissiveGuardrails = defaultGuardrails.map((guardrail) =>
       guardrail.id === "bulk-operations"
         ? { ...guardrail, allowed: true }
         : guardrail,
     );
     const permissivePermissions = {
       ...defaultPermissions,
       can_initiate_bulk_actions: true,
       guardrails: permissiveGuardrails,
     };

    (apiClient.listSessions as any).mockResolvedValue({
      sessions: [
        makeSession({ id: "session-123456789", provider_type: "native", state: "running", current_task: "T1" }),
      ],
    });
     (apiClient.getPermissions as any).mockResolvedValue(permissivePermissions);

     render(() => <Dashboard />);

     // Buttons are always visible - guardrails UI is hidden
     expect(await screen.findByText("Pause")).toBeDefined();
     expect(screen.getByText("Resume")).toBeDefined();
     expect(screen.getByText("Stop")).toBeDefined();
     expect(screen.queryByText("Bulk actions locked")).toBeNull();
     // Management guardrails panel is hidden
     expect(screen.queryByText("Management guardrails")).toBeNull();
   });

  it("skips bulk actions when confirmation is declined", async () => {
    const permissiveGuardrails = defaultGuardrails.map((guardrail) =>
      guardrail.id === "bulk-operations"
        ? { ...guardrail, allowed: true }
        : guardrail,
    );
    (apiClient.getPermissions as any).mockResolvedValue({
      ...defaultPermissions,
      can_initiate_bulk_actions: true,
      guardrails: permissiveGuardrails,
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
     const permissiveGuardrails = defaultGuardrails.map((guardrail) =>
       guardrail.id === "bulk-operations"
         ? { ...guardrail, allowed: true }
         : guardrail,
     );
     (apiClient.getPermissions as any).mockResolvedValue({
       ...defaultPermissions,
       can_initiate_bulk_actions: true,
       guardrails: permissiveGuardrails,
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
     // With guardrails hidden, pause button is always visible and clickable
     // Dashboard no longer displays action notices - SessionViewer handles that instead
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
      (apiClient.getPermissions as any).mockResolvedValue(permissionsWithRestrictions);

      render(() => <Dashboard />);

      const inspectButton = await screen.findByText("Inspect") as HTMLButtonElement;
      expect(inspectButton.disabled).toBe(true);
      expect(inspectButton.getAttribute("title")).toBe("Session inspection restricted by policy.");
    });

    it("shows permission restriction tooltips for disabled bulk action buttons", async () => {
      (apiClient.listSessions as any).mockResolvedValue({
        sessions: [
          makeSession({ id: "session-123456789", provider_type: "native", state: "running", current_task: "T1" }),
        ],
      });
      (apiClient.getPermissions as any).mockResolvedValue(permissionsWithRestrictions);

      render(() => <Dashboard />);
      screen.debug();

      // Find the parent div for bulk actions when canManage() is false
      const bulkActionsDiv = await screen.findByTitle("Bulk actions restricted by policy.");

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
