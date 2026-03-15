# CodeFlow Incremental Analysis and Update System

## 1. Summary

The incremental analysis system enables the CodeFlow analysis engine to respond to file changes in near-real-time by selectively re-parsing only modified files, invalidating and recomputing affected derived facts across all three analysis layers, and emitting structured events that drive live UI updates. Rather than re-scanning the entire codebase on every change, the system tracks fine-grained dependencies between base facts (Layer 1), enrichment-derived facts (Layer 2), and findings (Layer 3), recomputing only what is necessary. This transforms CodeFlow from a batch-mode scanner into a continuous, reactive analysis engine.

The system builds on the existing `scan_epoch` and `producer` property conventions and the `retirePriorEpochFacts` retirement mechanism already implemented in `store.go`, extending them with per-file scoping, per-producer epoch counters, dependency tracking for enrichment invalidation, and crash recovery.

## 2. Motivation

A full re-scan of a large codebase (10,000+ files) takes tens of seconds. During active development, files change every few seconds. Without incremental analysis, the graph database either becomes stale (if the developer must manually trigger re-scans) or the system wastes significant CPU re-analyzing unchanged files. This creates a poor developer experience in the CodeFlow Explorer, where the live query SSE system is designed to push updates in real-time but has no incremental data source to drive it.

Incremental analysis is also a prerequisite for watch mode, which is listed as a core deliverable in the implementation plan (Phase 12) and referenced in the backend design spec's file watcher component.

Without this feature, the CodeFlow Explorer's "live" promise is hollow: the SSE endpoint can push updates, but has nothing to push unless the entire scan pipeline runs again.

## 3. Scope

* **In Scope**:
  * File change detection via `fsnotify` with debouncing, batching, and filtering
  * Git-aware change detection for branch switches
  * Incremental Layer 1 fact extraction for changed files only
  * Dependency tracking and invalidation for Layer 2 enrichment rules
  * Tag propagation invalidation with `tag_source` tracking
  * Finding stability: fingerprint-based matching, status preservation, stale retirement
  * Monotonic per-producer epoch counter with completion markers
  * Notification events for downstream consumers (live query SSE, frontend)
  * Read-write locking for consistency during updates
  * Crash recovery via incomplete epoch detection
  * Performance budgets and benchmarking strategy

* **Out of Scope**:
  * Distributed or multi-process analysis (the system assumes a single process with an embedded goraphdb)
  * Cross-repository incremental analysis
  * Incremental tree-sitter parsing (we re-parse entire changed files; tree-sitter's own incremental parsing API is a future optimization)
  * Real-time collaboration or conflict resolution between multiple concurrent editors
  * Historical snapshots or time-travel queries over past epochs

## 4. Requirements & User Experience (UX)

### 4.1 File Change Detection

**UC-1: Single file edit.** A developer saves a Go file. Within 2 seconds, the CodeFlow Explorer graph reflects the updated functions, call edges, and any new or retired findings for that file.

**UC-2: Batch save.** An IDE reformats 15 files simultaneously. The system debounces these into a single incremental update batch rather than triggering 15 independent update cycles.

**UC-3: Branch switch.** A developer runs `git checkout feature-branch`. The system detects all files that differ between the previous HEAD and the new HEAD and triggers an incremental update covering all of them.

**UC-4: File creation/deletion.** A developer creates a new file or deletes an existing one. New files are picked up and analyzed; deleted files have all their facts removed from the graph.

**UC-5: Rename detection.** A developer renames `handler.go` to `api_handler.go`. The system detects this (via git or content hash) and preserves node identity where possible, avoiding spurious finding churn.

### 4.2 Functional Requirements

* **FR-1**: Only files matching configured language extensions (`.go`, `.js`, `.ts`, `.tsx`, `.jsx`) are processed. All others are ignored.
* **FR-2**: Directories matching ignore patterns (`node_modules`, `vendor`, `.git`, `build`, `dist`, `__pycache__`, `.codeflow-mvp.goraphdb`) are excluded from watching and scanning.
* **FR-3**: Ignore patterns are configurable via `codeflow.yaml` with sensible defaults.
* **FR-4**: After an incremental update, findings that no longer apply are marked `status: "retired"` rather than deleted, preserving audit history.
* **FR-5**: Findings with user-set statuses (`"acknowledged"`, `"suppressed"`, `"false_positive"`) retain those statuses across incremental updates unless the finding itself is retired.
* **FR-6**: A full re-scan can be triggered manually, which retires all facts from all previous epochs and rebuilds from scratch.
* **FR-7**: The system must recover gracefully from crashes during incremental updates without leaving the graph in an inconsistent state.

## 5. System Design & Architecture

### 5.1 High-Level Data Flow

```
┌─────────────────┐     ┌──────────────┐     ┌──────────────────┐
│   File Watcher   │────>│  Change      │────>│  Incremental     │
│   (fsnotify)     │     │  Aggregator  │     │  Update Engine   │
└─────────────────┘     └──────────────┘     └──────┬───────────┘
                                                     │
                         ┌───────────────────────────┼───────────────────────┐
                         │                           │                       │
                         v                           v                       v
                  ┌──────────────┐          ┌────────────────┐      ┌────────────────┐
                  │  Layer 1:    │          │  Layer 2:      │      │  Layer 3:      │
                  │  Fact        │────────> │  Enrichment    │────> │  Analysis      │
                  │  Extraction  │          │  Invalidation  │      │  Re-evaluation │
                  └──────────────┘          │  & Recompute   │      └────────┬───────┘
                                            └────────────────┘               │
                                                                             v
                                                                    ┌────────────────┐
                                                                    │  Notification  │
                                                                    │  Emitter       │
                                                                    └────────────────┘
                                                                             │
                                                                             v
                                                                    ┌────────────────┐
                                                                    │  Live Query    │
                                                                    │  SSE System    │
                                                                    └────────────────┘
```

### 5.2 File Change Detection

#### 5.2.1 fsnotify Watcher

The file watcher uses `fsnotify` to monitor the project directory recursively. It filters events through a two-stage pipeline:

```
fsnotify events → Extension Filter → Ignore Pattern Filter → Debounce Buffer → Batch Emit
```

**Configuration:**

```yaml
# codeflow.yaml
watch:
  debounce_ms: 500          # Default 500ms, configurable
  extensions:
    - .go
    - .js
    - .ts
    - .tsx
    - .jsx
  ignore:
    - node_modules
    - vendor
    - .git
    - build
    - dist
    - "*.test.js"            # Glob patterns supported
    - "**/*_generated.go"
```

**Debounce mechanism:** When a file event arrives, the watcher starts (or resets) a debounce timer. When the timer fires, all accumulated file paths are emitted as a single `ChangeBatch`:

```go
type ChangeType int

const (
    ChangeModified ChangeType = iota
    ChangeCreated
    ChangeDeleted
    ChangeRenamed
)

type FileChange struct {
    Path       string
    ChangeType ChangeType
    OldPath    string  // Non-empty only for ChangeRenamed
}

type ChangeBatch struct {
    Changes   []FileChange
    Trigger   string  // "fsnotify", "git-switch", "manual-rescan"
    Timestamp time.Time
}
```

#### 5.2.2 Git-Aware Branch Switch Detection

When the watcher detects a change to `.git/HEAD` (which `fsnotify` will surface), it triggers git-aware diff detection:

```go
func detectBranchSwitch(projectDir string, previousHEAD string) ([]FileChange, error) {
    // 1. Read current HEAD
    currentHEAD := gitResolveHEAD(projectDir)
    if currentHEAD == previousHEAD {
        return nil, nil
    }

    // 2. Diff the two commits
    // git diff --name-status <previousHEAD> <currentHEAD>
    diffOutput := gitDiffNameStatus(projectDir, previousHEAD, currentHEAD)

    // 3. Parse into FileChange entries
    changes := parseDiffNameStatus(diffOutput)

    // 4. Update stored HEAD reference
    storePreviousHEAD(projectDir, currentHEAD)

    return changes, nil
}
```

The system stores the last-known HEAD commit hash in memory (and persists it as a metadata property in goraphdb) so that it can compute the diff even if multiple commits happened between checks.

#### 5.2.3 Rename Detection

File renames are detected through two mechanisms, tried in order:

1. **Git detection**: If the project is a git repository, `git diff --name-status -M` detects renames with similarity scoring. Files with >50% similarity are treated as renames.
2. **Content hash fallback**: If a file is deleted and a new file is created in the same batch with an identical or near-identical content hash (SHA-256 of the file contents), treat it as a rename.

When a rename is detected, the system updates the `path` and `file_id` properties on the existing `File` node and all child nodes, rather than deleting and recreating them. This preserves node identity and avoids spurious finding churn.

### 5.3 Scan Epoch Management

#### 5.3.1 Epoch Counter

The epoch is a monotonically increasing `uint64` counter, NOT a timestamp. This avoids clock skew issues and guarantees strict ordering.

```go
type EpochManager struct {
    mu       sync.Mutex
    counters map[string]uint64  // producer -> current epoch
}

func (em *EpochManager) Next(producer string) uint64 {
    em.mu.Lock()
    defer em.mu.Unlock()
    em.counters[producer]++
    return em.counters[producer]
}
```

On startup, the `EpochManager` reads the highest epoch for each producer from goraphdb metadata nodes and initializes from there.

#### 5.3.2 Per-Producer Scoping

Each producer maintains its own epoch counter independently:

| Producer | Description | Epoch Scope |
|----------|-------------|-------------|
| `codeflow-mvp` | Layer 1 fact extraction | Per-file: retirement scoped to changed files |
| `enrichment/<rule-id>` | Layer 2 enrichment rules | Per-rule: retirement scoped to affected derived facts |
| `analysis/<rule-id>` | Layer 3 analysis findings | Per-rule: retirement scoped to affected findings |

This means that a Layer 1 epoch bump for file `auth.go` does not interfere with Layer 1 epochs for file `handler.go`, and Layer 2 enrichment epochs are completely independent of Layer 1 epochs.

#### 5.3.3 Epoch Completion Marker

Every epoch writes a completion marker when finished successfully:

```go
// Stored as a metadata node in goraphdb
type ScanEpochMarker struct {
    ID        string   // "epoch:<producer>:<epoch>"
    Producer  string
    Epoch     uint64
    Status    string   // "in_progress" | "complete" | "failed"
    Files     []string // File IDs touched in this epoch (stored as comma-separated string due to goraphdb array filter limitation)
    StartedAt int64    // Unix timestamp
    Duration  int64    // Milliseconds
}
```

Note: Because goraphdb cannot filter on array properties in Cypher queries, the `Files` list is stored as a comma-delimited string property and parsed in application code when needed. The marker node's primary purpose is crash recovery (Section 5.10), not querying.

#### 5.3.4 Retirement: Extension of `retirePriorEpochFacts`

The existing `retirePriorEpochFacts` function in `store.go` already implements the core retirement logic:

1. For each node label, find nodes where `producer` matches AND `scan_epoch` differs from the current epoch AND the node belongs to a touched file (via `file_id` or `id` for File nodes).
2. Delete stale edges connected to those nodes.
3. Delete the stale nodes.

The incremental system extends this in two ways:

**Extension 1: Numeric epoch comparison.** The current implementation compares `scan_epoch` as a string (RFC3339 timestamp). The incremental system switches to `uint64` epoch counters, enabling `<` comparison instead of `!=` comparison. This is important because multiple incremental updates may run, and we want to retire facts from ALL previous epochs, not just the immediately prior one.

```go
// Current (string equality):
if node.GetString("scan_epoch") != currentEpoch { ... }

// New (numeric comparison):
if node.GetUint64("scan_epoch") < currentEpoch { ... }
```

**Extension 2: Per-producer file scoping.** The `touchedFiles` parameter is already file-scoped. The incremental system continues this pattern: each incremental update passes only the set of changed files, so retirement only affects facts within those files for that producer.

**Full re-scan behavior:** When a full re-scan is triggered, the `touchedFiles` set contains ALL files in the project. This causes retirement of all prior-epoch facts across the entire codebase, equivalent to starting fresh.

### 5.4 Incremental Fact Extraction (Layer 1)

When a `ChangeBatch` arrives, the Layer 1 pipeline executes:

```
For each changed file in batch:
    1. Allocate new epoch E = EpochManager.Next("codeflow-mvp")
    2. If file was deleted:
         a. Find all nodes with file_id = <file_id> AND producer = "codeflow-mvp"
         b. Delete their edges, then the nodes themselves
         c. Delete the File node
         d. Skip to next file
    3. If file was renamed:
         a. Update File node path property
         b. Update file_id on all child nodes
         c. Continue to step 4 (re-extract to catch any content changes)
    4. Re-parse the file with tree-sitter
    5. Extract all facts (functions, calls, spawns, API handlers, API requests)
    6. Upsert facts into goraphdb with scan_epoch = E, producer = "codeflow-mvp"
    7. Call retirePriorEpochFacts(db, {file_id}, E, "codeflow-mvp")
       to remove facts from this file that were not re-emitted

Collect set of changed file IDs for Layer 2 invalidation
```

This is structurally identical to the existing `PersistExtraction` flow in `store.go`, but scoped to a subset of files rather than the full codebase. The key difference is that the `touchedFiles` map passed to `retirePriorEpochFacts` contains only the changed files, not all files.

### 5.5 Dependency Invalidation for Enrichment (Layer 2)

This is the most architecturally significant component. When base facts change, the system must determine which enrichment-derived facts are stale and need recomputation.

#### 5.5.1 Dependency Index Design

The system maintains two in-memory indices, rebuilt on startup from goraphdb metadata:

**Forward Dependency Index** — For each enrichment rule execution, which base nodes did it read?

```go
// ForwardDeps maps (rule_id, execution_key) -> set of base node IDs read
type ForwardDeps map[string]map[string]StringSet
```

**Reverse Dependency Index** — For each base node, which enrichment rule executions depend on it?

```go
// ReverseDeps maps base_node_id -> set of (rule_id, execution_key)
type ReverseDeps map[string][]RuleExecution

type RuleExecution struct {
    RuleID       string
    ExecutionKey string  // Unique key for this rule execution instance
}
```

These indices are populated during enrichment rule execution. When an enrichment rule runs, it reports which base nodes it accessed:

```go
type EnrichmentContext struct {
    RuleID       string
    ExecutionKey string
    ReadNodes    StringSet  // Populated during rule execution
}

func (ctx *EnrichmentContext) RecordRead(nodeID string) {
    ctx.ReadNodes.Add(nodeID)
}
```

After rule execution, the indices are updated:

```go
func (idx *DependencyIndex) Update(ctx EnrichmentContext, producedNodeIDs StringSet) {
    // Clear old forward deps for this execution
    delete(idx.Forward[ctx.RuleID], ctx.ExecutionKey)

    // Set new forward deps
    idx.Forward[ctx.RuleID][ctx.ExecutionKey] = ctx.ReadNodes

    // Update reverse deps
    for nodeID := range ctx.ReadNodes {
        idx.Reverse[nodeID] = append(idx.Reverse[nodeID], RuleExecution{
            RuleID:       ctx.RuleID,
            ExecutionKey: ctx.ExecutionKey,
        })
    }
}
```

#### 5.5.2 Invalidation Algorithm

When Layer 1 re-extracts facts for a set of changed files, it produces a set of changed base node IDs (nodes that were created, modified, or deleted). The invalidation algorithm:

```
Input: changedBaseNodeIDs (set of node IDs whose facts changed in Layer 1)

1. affectedRuleExecutions = {}
   For each nodeID in changedBaseNodeIDs:
       For each (ruleID, execKey) in ReverseDeps[nodeID]:
           affectedRuleExecutions.Add((ruleID, execKey))

2. For each (ruleID, execKey) in affectedRuleExecutions:
       a. Find all nodes/edges with producer = "enrichment/<ruleID>"
          that were produced by this execution (tracked via execution_key property)
       b. Delete those derived nodes and edges
       c. Re-run the enrichment rule

3. Update dependency indices with results from step 2

4. If step 2 produced new or changed derived nodes:
       Recursively check if any OTHER enrichment rules depend on those derived nodes
       (enrichment rules can depend on other enrichment rules' output)
       Add those to affectedRuleExecutions and repeat from step 2
       Cap recursion at 10 iterations to prevent infinite loops
```

#### 5.5.3 Granularity: Per-File

The recommended starting granularity is **per-file**:

- When any fact in file F changes, invalidate ALL enrichment rule executions that read ANY base node from file F.
- This is coarser than per-function or per-node invalidation, but significantly simpler to implement and reason about.
- Most code changes affect an entire file's worth of facts anyway (reformatting, import changes, etc.).
- Per-function granularity can be added later by changing the reverse dependency index key from `file_id` to `function_id`.

**Why not per-node from the start?** Per-node invalidation requires tracking every individual node read during enrichment rule execution and matching those against the specific nodes that changed. This is precise but expensive in bookkeeping. Given that a typical file contains 5-20 functions and 20-100 call sites, the over-invalidation cost of per-file granularity is modest: we re-run a few extra enrichment rule executions that turn out to produce identical results. The epoch-based retirement mechanism handles this gracefully, since identical re-derived facts simply get their epoch bumped.

#### 5.5.4 Persistence of Dependency Indices

The indices are stored as metadata nodes in goraphdb:

```
(:DependencyRecord {
    id:             "dep:<rule_id>:<execution_key>",
    rule_id:        "taint_propagation",
    execution_key:  "file:auth.go",
    read_node_ids:  "func:auth.go:Login,func:auth.go:Validate,call:auth.go:Login:3",
    produced_nodes: "taint:auth.go:Login:sink",
    scan_epoch:     42
})
```

The `read_node_ids` and `produced_nodes` are stored as comma-delimited strings (workaround for goraphdb's lack of array property filtering). They are parsed into sets in application code on startup.

### 5.6 Tag Propagation Invalidation

The tag propagation engine propagates tags (e.g., `READ`, `WRITE`, `TAINT_SOURCE`) bottom-up through the call graph, halting at configured boundaries.

#### 5.6.1 Tag Source Tracking

Every propagated tag carries a `tag_source` property identifying which leaf node originated it:

```
(:Function {
    id:         "pkg/auth::Validate",
    effects:    "READ,WRITE",          // Comma-delimited (goraphdb limitation)
    tag_sources: "READ:pkg/db::Query,WRITE:pkg/db::Execute"
})
```

When a leaf function `pkg/db::Query` is tagged `READ` by a Layer 1 rule, and this tag propagates upward to `pkg/auth::Validate` (which calls `pkg/db::Query`), the `tag_sources` property records that the `READ` tag on `Validate` originated from `pkg/db::Query`.

#### 5.6.2 Invalidation When Tags Change

When Layer 1 re-extracts facts for a file and a function's directly-assigned tags change:

```
Input: changedTagNodeID (a leaf node whose tags changed)

1. Find all nodes N where tag_sources contains changedTagNodeID
   (Application-level string search on the tag_sources property,
    since goraphdb cannot filter arrays in Cypher)

2. For each such node N:
       a. Remove the stale tags that originated from changedTagNodeID
       b. Re-propagate tags from changedTagNodeID upward through CALLS edges
       c. Halt at boundary nodes (as configured)
       d. Update tag_sources on all affected nodes

3. If any node's effective tag set changed as a result:
       Add that node's ID to the Layer 2 changedBaseNodeIDs set
       (triggers further enrichment invalidation)
```

#### 5.6.3 Propagation Boundaries as Blast Radius Limits

Boundary nodes (HTTP handlers, test functions, main functions) stop tag propagation. This inherently limits the invalidation blast radius: a tag change in a deeply nested utility function only propagates upward until it hits a boundary. In a well-structured codebase, this typically limits propagation to 3-5 levels of the call graph.

### 5.7 Finding Stability (Layer 3)

#### 5.7.1 Fingerprint-Based Matching

Each finding has a structural fingerprint computed as:

```go
func computeFingerprint(ruleID string, fileID string, functionID string, factID string, calleeExpr string) string {
    components := []string{ruleID, fileID, functionID, factID, calleeExpr}
    joined := strings.Join(components, "|")
    hash := sha256.Sum256([]byte(joined))
    return hex.EncodeToString(hash[:16])  // 128-bit fingerprint
}
```

The fingerprint is based on structural identity (rule + location), NOT on line numbers or file content. This means a finding survives:
- Line number changes due to edits above the finding
- Whitespace/formatting changes
- Comment changes

But a finding is considered "new" if:
- The rule that generated it changes
- The function it applies to is renamed
- The call expression it refers to changes

#### 5.7.2 Status Lifecycle

```
                    ┌──────────────────────────┐
                    │                          │
              ┌─────v──────┐           ┌───────┴──────┐
  New finding │   "open"   │──user───> │"acknowledged"│
              └─────┬──────┘  action   └───────┬──────┘
                    │                          │
                    │         ┌────────────────┘
                    │         │
                    v         v
              ┌──────────────────┐
  No longer   │   "retired"      │  (finding no longer produced by analysis)
  produced    └──────────────────┘

              ┌──────────────────┐
  User action │  "suppressed"    │  (user explicitly suppressed)
              └──────────────────┘

              ┌──────────────────┐
  User action │ "false_positive" │  (user marked as false positive)
              └──────────────────┘
```

**Incremental update rules:**

1. **Finding still produced, same fingerprint**: Bump `scan_epoch`. Do NOT change `status`. If the user set it to `"acknowledged"` or `"suppressed"`, that status is preserved.
2. **Finding no longer produced**: Set `status = "retired"`, update `retired_at_epoch`. Do NOT delete the node.
3. **New finding (fingerprint not seen before)**: Create with `status = "open"`.
4. **Previously retired finding re-appears**: Set `status = "open"`, clear `retired_at_epoch`. This handles the case where a developer reverts a change that previously fixed a finding.

#### 5.7.3 Implementation

After Layer 3 analysis rules run for the affected scope:

```go
func reconcileFindings(db *graphdb.DB, newFindings []Finding, affectedFileIDs StringSet, epoch uint64) error {
    // 1. Index new findings by fingerprint
    newByFingerprint := map[string]Finding{}
    for _, f := range newFindings {
        newByFingerprint[f.Fingerprint] = f
    }

    // 2. Find existing findings in affected files
    existingFindings := findFindingsByFileIDs(db, affectedFileIDs)

    // 3. Reconcile
    for _, existing := range existingFindings {
        if _, stillExists := newByFingerprint[existing.Fingerprint]; stillExists {
            // Finding persists: bump epoch, preserve status
            updateNodeEpoch(db, existing.ID, epoch)
            delete(newByFingerprint, existing.Fingerprint)
        } else {
            // Finding retired
            setNodeProperty(db, existing.ID, "status", "retired")
            setNodeProperty(db, existing.ID, "retired_at_epoch", epoch)
        }
    }

    // 4. Create truly new findings
    for _, f := range newByFingerprint {
        createFindingNode(db, f, epoch, "open")
    }

    return nil
}
```

### 5.8 Notification System

After an incremental update completes, the system emits structured events through an internal event bus. These events are consumed by the live query SSE system described in the backend design spec.

#### 5.8.1 Event Types

```go
type FactsUpdatedEvent struct {
    Type       string   `json:"type"`       // "facts_updated"
    Files      []string `json:"files"`      // File IDs that changed
    Added      int      `json:"added"`      // New nodes created
    Removed    int      `json:"removed"`    // Stale nodes retired
    Modified   int      `json:"modified"`   // Existing nodes updated
    Epoch      uint64   `json:"epoch"`
    DurationMs int64    `json:"duration_ms"`
}

type EnrichmentUpdatedEvent struct {
    Type         string   `json:"type"`          // "enrichment_updated"
    Rules        []string `json:"rules"`         // Rule IDs that re-ran
    NodesAdded   int      `json:"nodes_added"`
    NodesRemoved int      `json:"nodes_removed"`
    EdgesAdded   int      `json:"edges_added"`
    EdgesRemoved int      `json:"edges_removed"`
    Epoch        uint64   `json:"epoch"`
    DurationMs   int64    `json:"duration_ms"`
}

type FindingsUpdatedEvent struct {
    Type     string            `json:"type"`     // "findings_updated"
    New      []FindingSummary  `json:"new"`      // Newly created findings
    Retired  []FindingSummary  `json:"retired"`  // Newly retired findings
    Epoch    uint64            `json:"epoch"`
    DurationMs int64           `json:"duration_ms"`
}

type FindingSummary struct {
    ID          string `json:"id"`
    RuleID      string `json:"rule_id"`
    Severity    string `json:"severity"`
    FileID      string `json:"file_id"`
    Fingerprint string `json:"fingerprint"`
}
```

#### 5.8.2 Event Bus Integration

Events are published to a Go channel-based broadcaster:

```go
type EventBus struct {
    mu          sync.RWMutex
    subscribers map[string][]chan Event
}

func (eb *EventBus) Publish(event Event) {
    eb.mu.RLock()
    defer eb.mu.RUnlock()
    for _, ch := range eb.subscribers[event.Type()] {
        select {
        case ch <- event:
        default:
            // Drop if subscriber is slow; they'll get the next one
        }
    }
}
```

The live query SSE system subscribes to these events. When it receives a `FactsUpdatedEvent`, it checks whether any of the changed node labels match the labels referenced in active live queries. If so, it re-evaluates the query and pushes updated results to the SSE stream.

Events carry enough information for the frontend to make a local decision about whether to re-fetch: the `Files` and `Rules` lists allow the frontend to check if its current view is affected.

### 5.9 Consistency Model

#### 5.9.1 Read-Write Lock

During an incremental update, the graph database is in a transitional state. To prevent queries from seeing partially-updated data, the system uses a read-write lock:

```go
type GraphLock struct {
    mu sync.RWMutex
}

func (gl *GraphLock) WithWriteLock(fn func() error) error {
    gl.mu.Lock()
    defer gl.mu.Unlock()
    return fn()
}

func (gl *GraphLock) WithReadLock(fn func() error) error {
    gl.mu.RLock()
    defer gl.mu.RUnlock()
    return fn()
}
```

**Write lock scope:** The write lock is held per-batch, NOT per-file. An incremental update acquires the write lock once, processes all files in the batch through all three layers, then releases the lock. This ensures that readers always see a consistent state: either the pre-update state or the fully-updated state, never a partial mix.

**Read lock scope:** All Cypher queries from the `POST /api/v1/codeflow/query` endpoint acquire a read lock. Multiple concurrent reads are allowed. Reads block during a write, and writes block until all active reads complete.

**Why not snapshot isolation?** goraphdb is an embedded, file-based database without built-in snapshot isolation or MVCC. Implementing snapshot isolation would require either (a) a copy-on-write mechanism in goraphdb itself or (b) maintaining two copies of the database. Both are complex. A simple read-write lock is appropriate for a single-process embedded database where write batches complete in under 10 seconds.

#### 5.9.2 Live Query Buffering

The notification emitter buffers events during the write lock and flushes them atomically after the lock releases:

```go
func (engine *IncrementalEngine) RunUpdate(batch ChangeBatch) error {
    var events []Event

    err := engine.graphLock.WithWriteLock(func() error {
        // Layer 1, 2, 3 processing...
        events = append(events, factsEvent, enrichmentEvent, findingsEvent)
        return nil
    })
    if err != nil {
        return err
    }

    // Events emitted AFTER write lock releases
    // Readers can now see the updated data
    for _, event := range events {
        engine.eventBus.Publish(event)
    }
    return nil
}
```

This guarantees that when a live query subscriber receives an event and re-runs its query, the query will see the data that triggered the event.

### 5.10 Error Recovery

#### 5.10.1 Crash Detection

On startup, the system scans for incomplete epochs:

```go
func detectIncompleteEpochs(db *graphdb.DB) ([]ScanEpochMarker, error) {
    // Find all ScanEpochMarker nodes where status = "in_progress"
    markers := findMarkersByStatus(db, "in_progress")
    return markers, nil
}
```

#### 5.10.2 Recovery Procedure

For each incomplete epoch:

```
1. Read the marker to find (producer, epoch, files)
2. Delete all nodes/edges with scan_epoch = <epoch> AND producer = <producer>
   (These are the partially-written facts from the crashed update)
3. Mark the epoch marker as status = "failed"
4. For each file listed in the marker:
       Add it to a recovery re-scan queue
5. Trigger a standard incremental update for the recovery queue
```

This is safe because:
- The partially-written facts are cleanly identifiable by their epoch and producer.
- Deleting them restores the graph to the state before the crashed update.
- Re-scanning the affected files regenerates the correct facts.

#### 5.10.3 Write Ordering for Crash Safety

To minimize the window of inconsistency:

1. Write the `ScanEpochMarker` with `status: "in_progress"` FIRST
2. Write all new facts
3. Retire stale facts
4. Update the `ScanEpochMarker` to `status: "complete"` LAST

If a crash occurs between steps 1 and 4, recovery detects the incomplete marker. If a crash occurs before step 1, no partial data was written. If a crash occurs after step 4, the update is complete and no recovery is needed.

### 5.11 Full Incremental Update Sequence (Pseudo-code)

```go
func (engine *IncrementalEngine) ProcessBatch(batch ChangeBatch) error {
    epoch := engine.epochs.Next("codeflow-mvp")

    // Write epoch start marker
    writeEpochMarker(engine.db, "codeflow-mvp", epoch, batch.FileIDs(), "in_progress")

    var factsEvent FactsUpdatedEvent
    var enrichmentEvent EnrichmentUpdatedEvent
    var findingsEvent FindingsUpdatedEvent

    err := engine.graphLock.WithWriteLock(func() error {
        // ── Layer 1: Fact Extraction ──
        changedNodeIDs := StringSet{}

        for _, change := range batch.Changes {
            switch change.ChangeType {
            case ChangeDeleted:
                removed := deleteAllFactsForFile(engine.db, change.Path)
                factsEvent.Removed += removed
                continue

            case ChangeRenamed:
                updateFilePaths(engine.db, change.OldPath, change.Path)
                // Fall through to re-extract
            }

            // Parse and extract
            facts := engine.parser.ExtractFacts(change.Path)

            // Upsert with new epoch
            upserted := persistFacts(engine.db, facts, epoch, "codeflow-mvp")
            changedNodeIDs.AddAll(upserted.ModifiedNodeIDs)

            // Retire stale facts for this file
            retired := retirePriorEpochFacts(engine.db,
                map[string]struct{}{facts.FileID: {}}, epoch, "codeflow-mvp")
            changedNodeIDs.AddAll(retired)

            factsEvent.Files = append(factsEvent.Files, facts.FileID)
            factsEvent.Added += upserted.Added
            factsEvent.Modified += upserted.Modified
            factsEvent.Removed += len(retired)
        }

        // ── Layer 2: Enrichment Invalidation ──
        affectedRules := engine.depIndex.FindAffectedRules(changedNodeIDs)

        for _, ruleExec := range affectedRules {
            // Delete derived facts from previous execution
            removed := deleteDerivedFacts(engine.db, ruleExec)
            enrichmentEvent.NodesRemoved += removed

            // Re-run enrichment rule
            ctx := NewEnrichmentContext(ruleExec.RuleID, ruleExec.ExecutionKey)
            produced := engine.enrichment.RunRule(engine.db, ctx)

            // Update dependency index
            engine.depIndex.Update(ctx, produced.NodeIDs)

            enrichmentEvent.Rules = append(enrichmentEvent.Rules, ruleExec.RuleID)
            enrichmentEvent.NodesAdded += produced.NodesAdded
            enrichmentEvent.EdgesAdded += produced.EdgesAdded
        }

        // ── Tag Propagation Re-evaluation ──
        tagChangedNodes := findNodesWithChangedTags(engine.db, changedNodeIDs)
        for _, nodeID := range tagChangedNodes {
            repropagateTagsFrom(engine.db, nodeID, engine.config.Boundaries)
        }

        // ── Layer 3: Finding Re-evaluation ──
        affectedFileIDs := batch.FileIDSet()
        // Also include files whose enrichment-derived facts changed
        for _, ruleExec := range affectedRules {
            affectedFileIDs.AddAll(ruleExec.AffectedFileIDs)
        }

        newFindings := engine.analysis.RunForFiles(engine.db, affectedFileIDs)
        findingsEvent = reconcileFindings(engine.db, newFindings, affectedFileIDs, epoch)

        return nil
    })

    if err != nil {
        writeEpochMarker(engine.db, "codeflow-mvp", epoch, batch.FileIDs(), "failed")
        return err
    }

    // Mark epoch complete
    writeEpochMarker(engine.db, "codeflow-mvp", epoch, batch.FileIDs(), "complete")

    // Emit events (outside write lock)
    factsEvent.Epoch = epoch
    enrichmentEvent.Epoch = epoch
    findingsEvent.Epoch = epoch
    engine.eventBus.Publish(factsEvent)
    engine.eventBus.Publish(enrichmentEvent)
    engine.eventBus.Publish(findingsEvent)

    return nil
}
```

## 6. Security & Privacy

* **File System Access**: The file watcher is sandboxed to the configured project directory. Symlinks pointing outside the project root are not followed.
* **Ignore Patterns**: The `.git` directory is always excluded from watching, preventing accidental exposure of git internal data.
* **No New Authentication Surface**: The incremental update system is entirely server-side. No new API endpoints are introduced; updates flow through the existing event bus and SSE system, which are already behind the session authentication middleware.
* **Resource Exhaustion**: The debounce mechanism prevents a burst of file changes (e.g., a `git checkout` touching thousands of files) from spawning thousands of concurrent parse operations. Changes are batched and processed sequentially within the write lock.
* **Epoch Marker Cleanup**: Failed epoch markers are cleaned up during recovery. A background job (hourly) deletes epoch markers older than 24 hours with `status: "complete"` to prevent metadata node accumulation.

## 7. Testing Plan

### 7.1 Unit Tests

* **Debounce logic**: Emit 100 file events within 200ms, verify they coalesce into a single batch. Emit events 1 second apart, verify they produce separate batches.
* **Extension filter**: Verify `.go`, `.ts` files pass; `.png`, `.lock` files are rejected.
* **Ignore pattern matching**: Verify `node_modules/foo.js` is ignored; `src/foo.js` is not.
* **Epoch counter**: Verify monotonic increment, persistence across simulated restarts, per-producer independence.
* **Dependency index**: Build a mock index, mutate a base node, verify correct rule executions are identified for invalidation.
* **Finding reconciliation**: Create mock existing findings with various statuses (`"open"`, `"acknowledged"`, `"suppressed"`), run reconciliation with a new finding set, verify status preservation and correct retirement.
* **Rename detection**: Mock a git diff with rename status, verify `FileChange` output.

### 7.2 Integration Tests

* **Single file change end-to-end**: Set up a small Go project in a temp directory, run a full scan, modify one file, trigger incremental update, verify only that file's facts are re-extracted and all other files' facts are untouched.
* **File deletion**: Delete a file, verify all its nodes and edges are removed.
* **Enrichment invalidation**: Set up a project where function A calls function B. Run a full scan. Modify function B's tags. Verify that enrichment rules affecting A are re-run.
* **Finding stability**: Run a full scan that produces 5 findings. Acknowledge one. Modify a file that retires one finding and creates one new finding. Verify: 3 findings unchanged, 1 finding still acknowledged, 1 finding retired, 1 finding new with status "open".
* **Crash recovery**: Write an in-progress epoch marker, then simulate recovery by calling the startup recovery procedure. Verify the partial data is cleaned up and affected files are re-scanned.

### 7.3 Performance Tests

* **Single file latency**: Benchmark incremental update for a single file change in a 1,000-file project. Target: <2 seconds.
* **Batch latency**: Benchmark incremental update for 10 simultaneous file changes. Target: <10 seconds.
* **Full re-scan baseline**: Benchmark full re-scan of a 10,000-file project. Target: <60 seconds.
* **Enrichment scaling**: Measure enrichment re-computation time as a function of affected rules (not total rules). Verify linear scaling.

### 7.4 Edge Cases

* **Rapid successive changes**: File changes faster than the debounce window. Verify no updates are dropped.
* **Change during update**: A file changes while an incremental update is in progress. Verify the change is queued and processed after the current update completes.
* **Empty batch**: A batch where all changes are to ignored files. Verify no work is done.
* **Circular enrichment dependencies**: Two enrichment rules that depend on each other's output. Verify the recursion cap (10 iterations) prevents infinite loops.

## 8. Rollout & Deployment

### 8.1 Database Migrations

* **Epoch property type change**: Existing `scan_epoch` properties are RFC3339 strings. The migration converts them to `uint64` values. This is a one-time migration triggered on first startup with the new version.
* **New metadata nodes**: `ScanEpochMarker` and `DependencyRecord` are new node types with their own label and unique constraint on `id`. Created on first startup.
* **Index creation**: Add index on `scan_epoch` (numeric) for each node label to speed up retirement queries.

### 8.2 Feature Flags

```yaml
# codeflow.yaml
watch:
  enabled: true     # Master switch for file watching (default: false)
  debounce_ms: 500
```

When `watch.enabled` is `false`, the system operates in batch mode (current behavior). This is the default to prevent surprises for existing users.

### 8.3 Rollout Strategy

1. **Phase A**: Ship with `watch.enabled: false` by default. The incremental engine exists but is only triggered by manual re-scan commands. This validates the epoch, retirement, and recovery logic in production without introducing the file watcher.
2. **Phase B**: Enable file watching in development environments. Monitor for performance regressions and correctness issues.
3. **Phase C**: Enable by default for all environments.

### 8.4 Monitoring

* **Metric**: `codeflow_incremental_update_duration_seconds` (histogram, labeled by trigger type)
* **Metric**: `codeflow_incremental_files_processed_total` (counter)
* **Metric**: `codeflow_incremental_facts_retired_total` (counter, labeled by producer)
* **Metric**: `codeflow_incremental_enrichment_invalidations_total` (counter, labeled by rule_id)
* **Metric**: `codeflow_incremental_findings_new_total` / `codeflow_incremental_findings_retired_total` (counters)
* **Log**: Warn-level log if an incremental update exceeds 10 seconds
* **Log**: Error-level log if crash recovery is triggered on startup

## 9. Alternatives Considered

### 9.1 Per-Node Dependency Tracking (rejected for v1)

Track dependencies at individual node granularity rather than per-file. This would minimize over-invalidation but requires instrumenting every graph read operation in enrichment rules and maintaining a much larger reverse dependency index. The bookkeeping overhead is not justified until we have evidence that per-file granularity causes unacceptable performance in practice.

### 9.2 Content Hash-Based Change Detection (rejected)

Skip re-extraction if the file's content hash matches the stored hash, even when the filesystem reports a change. This would save parsing time for "phantom" writes (e.g., `touch` without modification). Rejected because (a) computing a SHA-256 of every changed file adds its own overhead, (b) the parser is already fast (tree-sitter parses a typical Go file in <5ms), and (c) this adds complexity to the critical path. However, this optimization may be revisited for very large files.

### 9.3 Snapshot Isolation via DB Cloning (rejected)

Maintain two copies of the goraphdb database: a "stable" copy for reads and a "working" copy for writes. After a write batch completes, atomically swap the database file. This provides true snapshot isolation but doubles storage and adds significant complexity for file-based database management. The simple read-write lock is sufficient for the single-process architecture.

### 9.4 Polling Instead of fsnotify (rejected)

Poll the filesystem periodically (e.g., every 2 seconds) instead of using `fsnotify`. Simpler and more portable, but adds unnecessary latency and CPU overhead for large directory trees. `fsnotify` is well-tested on Linux, macOS, and Windows and is already a dependency (referenced in the backend design spec).

### 9.5 Eventual Consistency Without Locking (rejected)

Allow readers to see partially-updated data during writes and rely on the live query system to eventually push a corrected view. This avoids any write-blocking of reads but can surface confusing intermediate states in the UI (e.g., a finding appears briefly for a function that no longer exists). The write lock duration is bounded (typically <10 seconds), so the read-blocking cost is minimal and the consistency guarantee is worth it.

## 10. Implementation Plan

* [ ] Implement monotonic `EpochManager` with per-producer counters and goraphdb persistence
* [ ] Migrate `scan_epoch` from RFC3339 string to `uint64` in `store.go` and update `retirePriorEpochFacts`
* [ ] Add `ScanEpochMarker` metadata node creation and management
* [ ] Implement crash recovery: detect incomplete epochs on startup, clean up, re-queue affected files
* [ ] Build `fsnotify`-based file watcher with debouncing, extension filtering, and ignore patterns
* [ ] Implement git-aware branch switch detection via `.git/HEAD` monitoring and `git diff --name-status`
* [ ] Implement rename detection (git-based and content hash fallback)
* [ ] Build `ChangeBatch` aggregation pipeline connecting watcher to incremental engine
* [ ] Implement incremental Layer 1 fact extraction (re-parse changed files only, retire stale facts per-file)
* [ ] Build forward and reverse dependency index data structures with goraphdb persistence
* [ ] Implement Layer 2 enrichment invalidation algorithm (identify affected rules, delete derived facts, re-run)
* [ ] Implement tag propagation invalidation with `tag_source` tracking and bounded re-propagation
* [ ] Implement finding reconciliation (fingerprint matching, status preservation, retirement)
* [ ] Implement `GraphLock` read-write locking around all DB access
* [ ] Build notification event types and event bus with channel-based broadcasting
* [ ] Wire events into the existing live query SSE system
* [ ] Add configuration schema for `watch` section in `codeflow.yaml`
* [ ] Write unit tests for debounce, filtering, epoch, dependency index, finding reconciliation
* [ ] Write integration tests for end-to-end incremental update, deletion, enrichment invalidation, crash recovery
* [ ] Write performance benchmarks (single file, batch, full re-scan, enrichment scaling)
* [ ] Add metrics and logging instrumentation
* [ ] Implement `watch.enabled` feature flag with phased rollout

## 11. Open Questions

1. **Enrichment rule execution tracking granularity**: Should enrichment rules self-report their read set (requires instrumenting the rule execution context), or should the system infer read sets by diffing the DB state before and after rule execution? Self-reporting is more precise but couples rule implementations to the dependency tracking system.

2. **Epoch marker garbage collection**: How aggressively should completed epoch markers be cleaned up? Keeping them indefinitely provides audit history but accumulates metadata nodes. The spec proposes 24-hour retention, but this may need tuning based on observed volumes.

3. **Concurrent incremental updates**: If a new batch of file changes arrives while a previous incremental update is still in progress (inside the write lock), should the system (a) queue the new batch and process it after the current one finishes, or (b) merge the new batch into the current one? Option (a) is simpler and is the current recommendation, but option (b) would reduce total update latency for rapid successive changes.

4. **goraphdb lock contention under load**: The read-write lock prevents concurrent reads during writes. If the write lock is held for >5 seconds on a large batch, interactive Cypher queries will block. Should we introduce a query timeout that returns stale-but-fast results when the lock is contended, or is blocking acceptable given the write lock duration budget?

5. **Tag propagation fixed-point**: When re-propagating tags after a change, should the system run to a fixed point (keep propagating until no node's tag set changes), or is a single bottom-up pass sufficient? A single pass is sufficient if the call graph is acyclic, but recursive functions could require multiple iterations. The spec recommends a single pass with a cap of 3 iterations for recursive cycles.

6. **Dependency index memory footprint**: For very large codebases (100,000+ nodes), the in-memory dependency index may become large. Should the index be lazily loaded (only for recently-active files) or always fully materialized? Lazy loading adds complexity but could significantly reduce memory for repositories where most files rarely change.
