import { render, screen } from "@solidjs/testing-library";
import { describe, it, expect, vi, beforeEach } from "vitest";
import Dashboard from "./Dashboard";
import { apiClient } from "../api/client";

const defaultGuardrails = [
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

const defaultPermissions = {
  role: "developer",
  can_inspect_sessions: true,
  can_manage_roles: false,
  can_manage_templates: true,
  can_initiate_bulk_actions: false,
  requires_owner_approval_for_role_changes: true,
  guardrails: defaultGuardrails,
};

vi.mock("../api/client", () => ({
  apiClient: {
    listSessions: vi.fn(),
    getPermissions: vi.fn(),
  }
}));

describe("Dashboard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (apiClient.getPermissions as any).mockResolvedValue(defaultPermissions);
  });

  it("renders loading state initially", async () => {
    (apiClient.listSessions as any).mockReturnValue(new Promise(() => {}));
    render(() => <Dashboard />);
    expect(screen.getByText("Loading sessions...")).toBeDefined();
  });

  it("renders sessions list when loaded", async () => {
    const mockSessions = {
      sessions: [
        { id: "session-123456789", provider_type: "native", state: "running", current_task: "T1" },
      ],
    };
    (apiClient.listSessions as any).mockResolvedValue(mockSessions);

    render(() => <Dashboard />);

    const idCell = await screen.findByText(/session-/);
    expect(idCell).toBeDefined();
    expect(screen.getByText("native")).toBeDefined();
    expect(screen.getByText("running")).toBeDefined();
    expect(screen.getByText("T1")).toBeDefined();
    expect(await screen.findByText("Management guardrails")).toBeDefined();
    expect(await screen.findByText("Role escalations")).toBeDefined();
  });

  it("renders empty list when no sessions", async () => {
    (apiClient.listSessions as any).mockResolvedValue({ sessions: [] });
    render(() => <Dashboard />);
    await screen.findByText("ID");
    expect(screen.queryByText("Loading sessions...")).toBeNull();
  });

  it("shows guardrail fallback when policy missing", async () => {
    (apiClient.listSessions as any).mockResolvedValue({ sessions: [] });
    (apiClient.getPermissions as any).mockResolvedValue({
      ...defaultPermissions,
      guardrails: [],
    });

    render(() => <Dashboard />);

    expect(await screen.findByText("Guardrail policy unavailable.")).toBeDefined();
  });
});
