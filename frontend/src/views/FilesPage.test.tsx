import { fireEvent, render, screen } from "@solidjs/testing-library"
import { beforeEach, describe, expect, it, vi } from "vitest"
import FilesPage from "./FilesPage"

const mockNavigate = vi.fn()
const mockUseSearch = vi.fn()
const mockActiveProjectId = vi.fn()
const mockProjects = vi.fn()

vi.mock("@tanstack/solid-router", () => ({
  useSearch: () => mockUseSearch,
  useNavigate: () => mockNavigate,
}))

vi.mock("../state/project", () => ({
  useProjectStore: () => ({
    activeProjectId: mockActiveProjectId,
    projects: mockProjects,
  }),
}))

vi.mock("../components/FileTree", () => ({
  default: (props: { onSelect: (path: string, isDir: boolean) => void }) => (
    <div>
      <button type="button" onClick={() => props.onSelect("src/main.ts", false)}>
        Select file
      </button>
    </div>
  ),
}))

vi.mock("../components/FileEditor", () => ({
  default: (props: {
    path: string | null
    isTreeOpen?: boolean
    onToggleTree?: () => void
  }) => (
    <div>
      <button
        type="button"
        aria-label={props.isTreeOpen ? "Hide files" : "Browse files"}
        onClick={() => props.onToggleTree?.()}
      >
        toggle
      </button>
      <span>Editor path: {props.path ?? "none"}</span>
    </div>
  ),
}))

describe("FilesPage", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockUseSearch.mockReturnValue({})
    mockActiveProjectId.mockReturnValue("proj-1")
    mockProjects.mockReturnValue([{ id: "proj-1", name: "Demo Project" }])
  })

  it("shows empty state when no project is selected", async () => {
    mockActiveProjectId.mockReturnValue(null)

    render(() => <FilesPage />)

    expect(await screen.findByText("No project selected")).toBeDefined()
  })

  it("toggles the mobile file menu", async () => {
    const { container } = render(() => <FilesPage />)

    const panel = container.querySelector("#files-tree-panel") as HTMLElement
    const toggle = container.querySelector(".editor-tree-toggle") as HTMLButtonElement

    expect(panel.classList.contains("open")).toBe(false)
    await fireEvent.click(toggle)
    expect(panel.classList.contains("open")).toBe(true)
    await fireEvent.click(toggle)
    expect(panel.classList.contains("open")).toBe(false)
  })

  it("closes the mobile menu after selecting a file", async () => {
    const { container } = render(() => <FilesPage />)
    const toggle = container.querySelector(".editor-tree-toggle") as HTMLButtonElement

    await fireEvent.click(toggle)
    const panel = container.querySelector("#files-tree-panel") as HTMLElement
    expect(panel.classList.contains("open")).toBe(true)

    await fireEvent.click(screen.getByRole("button", { name: "Select file" }))

    expect(panel.classList.contains("open")).toBe(false)
    expect(mockNavigate).toHaveBeenCalledWith({ to: "/files", search: { path: "src/main.ts" } })
  })
})
