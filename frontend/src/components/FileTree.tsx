import { createSignal, onMount, For, Show, JSX } from "solid-js"
import {
  BsChevronRight,
  BsChevronDown,
  BsFiletypeTsx,
  BsFiletypeMd,
  BsImage,
  BsFileText,
  BsFolder,
  BsFolderFill,
  BsFileCode,
} from "solid-icons/bs"
import { apiClient } from "../api/client"
import type { FileEntry } from "../api/files"

interface FileTreeProps {
  projectId: string
  selectedPath: string | null
  onSelect: (path: string, isDir: boolean) => void
}

function getFileIcon(entry: FileEntry): JSX.Element {
  if (entry.is_dir) return <BsFolder />

  const ext = entry.name.split(".").pop()?.toLowerCase() ?? ""

  if (ext === "ts" || ext === "tsx") return <BsFiletypeTsx />
  if (ext === "md" || ext === "mdx") return <BsFiletypeMd />
  if (["png", "jpg", "jpeg", "gif", "svg", "webp", "ico"].includes(ext)) return <BsImage />
  if (["go", "py", "rs", "css", "html", "json", "yaml", "yml", "sh"].includes(ext))
    return <BsFileCode />

  return <BsFileText />
}

function sortEntries(entries: FileEntry[]): FileEntry[] {
  return [...entries].sort((a, b) => {
    // Directories first, then files, both alphabetically
    if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1
    return a.name.localeCompare(b.name)
  })
}

interface FileTreeNodeProps {
  entry: FileEntry
  depth: number
  projectId: string
  selectedPath: string | null
  onSelect: (path: string, isDir: boolean) => void
  dirContents: () => Map<string, FileEntry[]>
  loadingDirs: () => Set<string>
  expandedDirs: () => Set<string>
  onToggleDir: (path: string) => void
}

function FileTreeNode(props: FileTreeNodeProps): JSX.Element {
  const isExpanded = () => props.expandedDirs().has(props.entry.path)
  const isLoading = () => props.loadingDirs().has(props.entry.path)
  const isSelected = () => props.selectedPath === props.entry.path
  const children = () => props.dirContents().get(props.entry.path)

  const indentStyle = () => ({ "padding-left": `${props.depth * 16}px` })

  const handleClick = () => {
    if (props.entry.is_dir) {
      props.onToggleDir(props.entry.path)
    } else {
      props.onSelect(props.entry.path, false)
    }
  }

  const dirIcon = () => {
    if (props.entry.is_dir) {
      return isExpanded() ? <BsFolderFill /> : <BsFolder />
    }
    return getFileIcon(props.entry)
  }

  const chevron = () => {
    if (!props.entry.is_dir) return null
    return isExpanded() ? <BsChevronDown /> : <BsChevronRight />
  }

  return (
    <>
      <div
        class={`file-tree-item${props.entry.is_dir ? " dir" : ""}${isSelected() ? " selected" : ""}`}
        style={indentStyle()}
        onClick={handleClick}
        role={props.entry.is_dir ? "button" : "option"}
        aria-selected={isSelected()}
        aria-expanded={props.entry.is_dir ? isExpanded() : undefined}
      >
        <span class="file-tree-icon file-tree-chevron">{chevron()}</span>
        <span class="file-tree-icon">{dirIcon()}</span>
        <span class="file-tree-label">{props.entry.name}</span>
        <Show when={isLoading()}>
          <span class="file-tree-loading">...</span>
        </Show>
      </div>

      <Show when={props.entry.is_dir && isExpanded()}>
        <div class="file-tree-children">
          <Show
            when={children() !== undefined}
            fallback={
              <Show when={!isLoading()}>
                {/* children not yet loaded but also not loading — shouldn't normally happen */}
                <div class="file-tree-loading" style={{ "padding-left": `${(props.depth + 1) * 16}px` }}>
                  ...
                </div>
              </Show>
            }
          >
            <For each={sortEntries(children()!)}>
              {(child) => (
                <FileTreeNode
                  entry={child}
                  depth={props.depth + 1}
                  projectId={props.projectId}
                  selectedPath={props.selectedPath}
                  onSelect={props.onSelect}
                  dirContents={props.dirContents}
                  loadingDirs={props.loadingDirs}
                  expandedDirs={props.expandedDirs}
                  onToggleDir={props.onToggleDir}
                />
              )}
            </For>
          </Show>
        </div>
      </Show>
    </>
  )
}

export default function FileTree(props: FileTreeProps): JSX.Element {
  const [dirContents, setDirContents] = createSignal<Map<string, FileEntry[]>>(new Map())
  const [expandedDirs, setExpandedDirs] = createSignal<Set<string>>(new Set())
  const [loadingDirs, setLoadingDirs] = createSignal<Set<string>>(new Set())
  const [rootLoading, setRootLoading] = createSignal(true)
  const [rootError, setRootError] = createSignal<string | null>(null)

  async function loadDir(dirPath: string): Promise<void> {
    // Already loaded or currently loading — skip
    if (dirContents().has(dirPath) || loadingDirs().has(dirPath)) return

    setLoadingDirs((prev) => new Set([...prev, dirPath]))
    try {
      const entries = await apiClient.listFiles(props.projectId, dirPath)
      setDirContents((prev) => {
        const next = new Map(prev)
        next.set(dirPath, entries)
        return next
      })
    } catch (err) {
      console.error(`FileTree: failed to load ${dirPath}`, err)
      // Store empty array so we don't retry infinitely on error
      setDirContents((prev) => {
        const next = new Map(prev)
        next.set(dirPath, [])
        return next
      })
    } finally {
      setLoadingDirs((prev) => {
        const next = new Set(prev)
        next.delete(dirPath)
        return next
      })
    }
  }

  function toggleDir(dirPath: string): void {
    const wasExpanded = expandedDirs().has(dirPath)
    if (wasExpanded) {
      setExpandedDirs((prev) => {
        const next = new Set(prev)
        next.delete(dirPath)
        return next
      })
    } else {
      setExpandedDirs((prev) => new Set([...prev, dirPath]))
      // Fetch contents if not already loaded
      if (!dirContents().has(dirPath)) {
        loadDir(dirPath)
      }
    }
  }

  onMount(async () => {
    try {
      const entries = await apiClient.listFiles(props.projectId, ".")
      setDirContents(new Map([[".", entries]]))
      setExpandedDirs(new Set(["."])) // root is expanded by default
    } catch (err) {
      console.error("FileTree: failed to load root", err)
      setRootError(err instanceof Error ? err.message : "Failed to load files")
    } finally {
      setRootLoading(false)
    }
  })

  const rootEntries = () => sortEntries(dirContents().get(".") ?? [])

  return (
    <div class="file-tree" role="tree" aria-label="Project files">
      <Show when={!rootLoading()} fallback={
        <div class="file-tree-loading">...</div>
      }>
        <Show when={rootError() === null} fallback={
          <div class="file-tree-loading">{rootError()}</div>
        }>
          <For each={rootEntries()}>
            {(entry) => (
              <FileTreeNode
                entry={entry}
                depth={0}
                projectId={props.projectId}
                selectedPath={props.selectedPath}
                onSelect={props.onSelect}
                dirContents={dirContents}
                loadingDirs={loadingDirs}
                expandedDirs={expandedDirs}
                onToggleDir={toggleDir}
              />
            )}
          </For>
        </Show>
      </Show>
    </div>
  )
}
