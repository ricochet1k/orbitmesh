import { render, screen, fireEvent } from "@solidjs/testing-library"
import { describe, it, expect, vi, beforeEach } from "vitest"
import { apiClient } from "../../api/client"
import { resetSessionStore } from "../../state/sessions"
import { resetTerminalStore } from "../../state/terminals"
import { makeSession, makeTerminal } from "../../test/fixtures"
import { TerminalsView } from "./index"

const mockNavigate = vi.fn()

vi.mock("@tanstack/solid-router", () => ({
  createFileRoute: () => () => ({}),
  useNavigate: () => mockNavigate,
}))

vi.mock("../../api/client", () => ({
  apiClient: {
    listTerminals: vi.fn(),
    deleteTerminal: vi.fn(),
    listSessions: vi.fn(),
    getCachedSessions: vi.fn(),
  },
}))

describe("TerminalsView", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    ;(apiClient.getCachedSessions as any).mockReturnValue(undefined)
    ;(apiClient.listSessions as any).mockResolvedValue({ sessions: [] })
    resetSessionStore()
    resetTerminalStore()
  })

  it("renders terminal list when loaded", async () => {
    const now = new Date().toISOString()
    ;(apiClient.listTerminals as any).mockResolvedValue({
      terminals: [
        makeTerminal({ id: "terminal-1", session_id: "session-1", last_updated_at: now }),
        makeTerminal({ id: "terminal-2", session_id: undefined, last_updated_at: now }),
      ],
    })
    ;(apiClient.listSessions as any).mockResolvedValue({
      sessions: [makeSession({ id: "session-1", state: "running", updated_at: now })],
    })

    render(() => <TerminalsView />)

    expect(await screen.findByText("terminal-1")).toBeDefined()
    expect(await screen.findByText("terminal-2")).toBeDefined()
    expect(screen.getAllByText("Open viewer").length).toBeGreaterThan(0)

    const disabledButton = screen
      .getAllByText("Open viewer")
      .find((button) => (button as HTMLButtonElement).disabled)
    expect(disabledButton).toBeDefined()
  })

  it("renders empty state when no terminals", async () => {
    ;(apiClient.listTerminals as any).mockResolvedValue({ terminals: [] })

    render(() => <TerminalsView />)

    await screen.findByText("No terminals yet")
    expect(screen.queryByTestId("terminals-list")).toBeNull()
  })

  it("calls delete when killing a terminal", async () => {
    const now = new Date().toISOString()
    ;(apiClient.listTerminals as any).mockResolvedValue({
      terminals: [makeTerminal({ id: "terminal-1", session_id: "session-1", last_updated_at: now })],
    })
    ;(apiClient.listSessions as any).mockResolvedValue({
      sessions: [makeSession({ id: "session-1", state: "running", updated_at: now })],
    })
    ;(apiClient.deleteTerminal as any).mockResolvedValue(undefined)
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true)

    render(() => <TerminalsView />)

    const killButton = await screen.findByText("Kill terminal")
    fireEvent.click(killButton)

    expect(confirmSpy).toHaveBeenCalled()
    expect(apiClient.deleteTerminal).toHaveBeenCalledWith("terminal-1")

    confirmSpy.mockRestore()
  })
})
