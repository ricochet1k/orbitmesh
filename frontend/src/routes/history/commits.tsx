import { createFileRoute } from '@tanstack/solid-router'
import { createResource, createSignal, createEffect, createMemo, Show, For } from 'solid-js'
import { apiClient } from '../../api/client'
import AgentGraph from '../../graph/AgentGraph'
import { buildCommitGraph } from '../../graph/graphData'
import type { CommitDetail } from '../../types/api'

export const Route = createFileRoute('/history/commits')({
  component: CommitHistoryView,
})

interface DiffRow {
  kind: "file" | "hunk" | "line"
  left: string
  right: string
  className?: string
}

function CommitHistoryView() {
  const [commitList] = createResource(() => apiClient.listCommits(40))
  const [selectedSha, setSelectedSha] = createSignal(getInitialCommitSha())
  const [commitDetail] = createResource(
    () => (selectedSha() ? selectedSha() : null),
    (sha) => apiClient.getCommit(sha)
  )

  const commits = () => commitList()?.commits ?? []

  createEffect(() => {
    if (selectedSha()) return
    const first = commits()[0]?.sha
    if (first) setSelectedSha(first)
  })

  const selectCommit = (sha: string) => {
    setSelectedSha(sha)
    updateCommitQuery(sha)
  }

  const graphData = createMemo(() => buildCommitGraph(commits()))
  const diffRows = createMemo(() => parseDiff(commitDetail()?.commit))

  return (
    <div class="commit-history-view">
      <header class="view-header">
        <div>
          <p class="eyebrow">Source history</p>
          <h1>Git Commit Viewer</h1>
          <p class="dashboard-subtitle">
            Track recent commits, inspect author context, and review diffs side-by-side.
          </p>
        </div>
        <div class="header-meta">
          <div class="meta-card">
            <p>Commits loaded</p>
            <strong>{commits().length}</strong>
          </div>
          <div class="meta-card">
            <p>Latest author</p>
            <strong>{commits()[0]?.author ?? "-"}</strong>
          </div>
          <div class="meta-card">
            <p>Latest commit</p>
            <strong>{commits()[0]?.sha?.slice(0, 7) ?? "-"}</strong>
          </div>
        </div>
      </header>

      <main class="commit-layout">
        <section class="commit-list-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">History</p>
              <h2>Commit List</h2>
            </div>
            <span class="panel-pill">Live</span>
          </div>
          <Show when={!commitList.loading} fallback={<p class="muted">Loading commits...</p>}>
            <div class="commit-list">
              <For each={commits()}>
                {(commit) => (
                  <button
                    type="button"
                    class={`commit-card ${selectedSha() === commit.sha ? "active" : ""}`}
                    onClick={() => selectCommit(commit.sha)}
                  >
                    <div>
                      <h3>{commit.message}</h3>
                      <p>{commit.author} · {formatTimestamp(commit.timestamp)}</p>
                    </div>
                    <span>{commit.sha.slice(0, 7)}</span>
                  </button>
                )}
              </For>
              <Show when={commits().length === 0}>
                <p class="muted">No commits available.</p>
              </Show>
            </div>
          </Show>
        </section>

        <section class="commit-detail-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Diff viewer</p>
              <h2>Commit Detail</h2>
            </div>
            <span class="panel-pill neutral">Side-by-side</span>
          </div>
          <Show when={commitDetail.loading}>
            <p class="muted">Loading diff...</p>
          </Show>
          <Show when={!commitDetail.loading && commitDetail()?.commit}>
            {(detail) => (
              <div class="commit-detail">
                <div class="commit-meta">
                  <div>
                    <p class="commit-title">{detail().message}</p>
                    <p class="commit-sub">
                      {detail().author} · {formatTimestamp(detail().timestamp)} · {detail().sha.slice(0, 7)}
                    </p>
                  </div>
                  <div class="commit-files">
                    <For each={detail().files ?? []}>{(file) => <span>{file}</span>}</For>
                  </div>
                </div>
                <div class="diff-grid">
                  <div class="diff-header">Before</div>
                  <div class="diff-header">After</div>
                  <For each={diffRows()}>
                    {(row) => (
                      <div class={`diff-row ${row.className ?? ""} ${row.kind}`}>
                        {row.kind === "line" ? (
                          <>
                            <div>{row.left}</div>
                            <div>{row.right}</div>
                          </>
                        ) : (
                          <div class="diff-span">{row.left || row.right}</div>
                        )}
                      </div>
                    )}
                  </For>
                </div>
              </div>
            )}
          </Show>
        </section>

        <section class="graph-view compact">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Commit topology</p>
              <h2>Graph Sync</h2>
            </div>
            <span class="panel-pill neutral">Linked</span>
          </div>
          <div id="graph-container">
            <AgentGraph
              nodes={graphData().nodes}
              links={graphData().links}
              selectedId={selectedSha()}
              onSelect={(node) => node.type === "commit" && selectCommit(node.id)}
            />
          </div>
        </section>
      </main>
    </div>
  )
}

function parseDiff(commit?: CommitDetail): DiffRow[] {
  if (!commit?.diff) return []
  const rows: DiffRow[] = []
  const lines = commit.diff.split("\n")

  lines.forEach((line) => {
    if (line.startsWith("diff --git ")) {
      rows.push({ kind: "file", left: line.replace("diff --git ", ""), right: "" })
      return
    }
    if (line.startsWith("@@")) {
      rows.push({ kind: "hunk", left: line, right: line, className: "diff-hunk" })
      return
    }
    if (line.startsWith("+")) {
      if (line.startsWith("+++")) {
        rows.push({ kind: "file", left: line, right: "" })
      } else {
        rows.push({ kind: "line", left: "", right: line.slice(1), className: "diff-add" })
      }
      return
    }
    if (line.startsWith("-")) {
      if (line.startsWith("---")) {
        rows.push({ kind: "file", left: line, right: "" })
      } else {
        rows.push({ kind: "line", left: line.slice(1), right: "", className: "diff-remove" })
      }
      return
    }
    if (line.startsWith(" ")) {
      rows.push({ kind: "line", left: line.slice(1), right: line.slice(1), className: "diff-context" })
      return
    }

    rows.push({ kind: "line", left: line, right: line, className: "diff-meta" })
  })

  return rows
}

function updateCommitQuery(sha: string) {
  const url = new URL(window.location.href)
  url.searchParams.set("commit", sha)
  history.replaceState({}, "", url)
}

function getInitialCommitSha(): string {
  const params = new URLSearchParams(window.location.search)
  return params.get("commit") ?? ""
}

function formatTimestamp(raw: string): string {
  if (!raw) return ""
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) return raw
  return date.toLocaleString()
}

