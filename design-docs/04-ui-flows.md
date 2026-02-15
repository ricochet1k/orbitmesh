# UI Flows & Information Architecture

**Designer Task:** Ttwbges - Admin/IDE UX Discovery  
**Purpose:** Unblock visual language task with lightweight, pragmatic design guidance.

---

## 1. Information Architecture (IA) Map

### Route Hierarchy & Navigation Model

```
OrbitMesh (Root)
│
├─ Dashboard (/)
│  └─ System graph visualization
│     └─ Task/commit nodes → drill into details
│
├─ Tasks (/tasks)
│  ├─ Task tree + hierarchical view
│  ├─ Filter/search by status, role, priority
│  └─ Select task → Inspect detail panel
│      └─ Primary action: Start Agent
│
├─ Sessions (/sessions)
│  ├─ Active & recent sessions list
│  ├─ Filter by status (running, paused, stopped)
│  └─ Select session → Session Viewer (/sessions/:id)
│      ├─ Live transcript stream
│      ├─ PTY terminal emulation
│      ├─ Session controls (pause/resume/stop)
│      └─ Historical replay & export
│
└─ Settings (/settings)
   ├─ User preferences
   ├─ API keys/tokens
   ├─ View settings (graph theme, transcript columns)
   └─ Admin controls (if permitted)
```

### Primary Actions by Route

| Route | Primary Action | Secondary Actions | Destination |
|-------|---|---|---|
| **Dashboard** | View system state | Inspect session, drill into task/commit | /sessions/:id, /tasks?task=:id |
| **Tasks** | Create/assign task | Edit details, filter by role/status | Task detail, /sessions/:id |
| **Sessions** | Start new session | Pause/resume/stop, inspect, export | /sessions/:id |
| **Session Viewer** | View live output | Pause/resume/stop, send input, replay | — |
| **Settings** | Update preferences | Manage API keys, view logs | — |

---

## 2. Key User Flow: "Select → Inspect → Start → View"

This is the primary happy path for operating agents in OrbitMesh.

```
┌─────────────────────────────────────────────────────────────────────┐
│ USER FLOW: SELECT TASK → INSPECT → START AGENT → VIEW SESSION       │
└─────────────────────────────────────────────────────────────────────┘

1. BROWSE TASKS
   Navigate to /tasks
   └─> TaskTreeView loads task hierarchy from API
   └─> User sees: tree structure, status badges, role assignments
   └─> User can: search, filter by role/status/priority

2. SELECT & INSPECT TASK
   Click on task in tree
   └─> Detail panel opens (right sidebar or modal)
   └─> Display: full task description, assigned role, todos, history
   └─> Show: estimated time, dependencies, permissions
   └─> Key action highlighted: "Start Agent" button

3. CHECK PERMISSIONS (if restricted)
   Is "start_agent" permission allowed?
   ├─ YES: Show enabled button, proceed
   └─ NO: Show disabled button with "Request access" link
      └─> Opens request modal (email/notification to reviewer)
      └─> Show helper text: "This action requires approval"

4. START AGENT / CREATE SESSION
   Click "Start Agent"
   └─> POST /api/v1/sessions with task ID
   └─> Session created, receives ID
   └─> Auto-navigate to /sessions/:id
   └─> Show: "Starting agent, please wait..."

5. VIEW LIVE SESSION
   SessionViewer mounts with active SSE stream
   ├─ Top bar: Session ID, status, elapsed time, created timestamp
   ├─ Main content: Live transcript with auto-scroll
   ├─ Right panel: System info, metadata, links back to task
   ├─ Bottom: Terminal emulation (PTY) if active
   └─ Controls: Pause/Resume/Stop buttons, Send input field
   
6. MONITOR & CONTROL
   Watch agent execution in real-time
   ├─ Output streams in chunks (messages, tool calls, results)
   ├─ User can: pause for review, send follow-up prompts, stop
   ├─ Automatic status updates every 100ms (SSE tick)
   └─ Session completes or user stops it

7. REVIEW RESULTS / REPLAY
   After session ends:
   ├─ Can replay from start (scrub timeline)
   ├─ Export as JSON or Markdown
   ├─ Link back to task to mark complete
   └─ Navigate back to /tasks or /sessions
```

---

## 3. Sidebar Navigation Model

### Visual Structure

```
┌─────────────────┐
│  OrbitMesh      │  ← Brand/logo
├─────────────────┤
│ ⊞ Dashboard  ●  │  ← Icon + label + active indicator
│ ≡ Tasks          │
│ ◻ Sessions       │
│ ⚙ Settings       │  ← Currently selected
├─────────────────┤
│ [bottom section] │  ← Status/user info (optional)
└─────────────────┘

Content Area
──────────────────────────────────────────
Top bar with breadcrumbs + section title
──────────────────────────────────────────
Main content (doesn't scroll with nav)
──────────────────────────────────────────
```

### Sidebar Behavior

**Permanent Elements:**
- Brand (OrbitMesh logo/text)
- Navigation items (Dashboard, Tasks, Sessions, Settings)
- Always visible on desktop
- Can be collapsed on mobile to icon-only view

**Responsive Behavior:**
- **Desktop (> 768px):** Sidebar 200px fixed, shows labels + icons
- **Tablet/Mobile (≤ 768px):** Sidebar becomes drawer (slide-out) or collapses to 60px icon-only
- **Collapse state:** Only icons visible, tooltip on hover
- **Drawer state:** Full-width overlay on mobile, click outside or select to dismiss

**Active/Inactive States:**
- Active nav item: background highlight + left border indicator
- Inactive: muted icon + label
- Hover: subtle background change
- All items remain clickable

### Key UX Principles

1. **Always accessible:** Navigation never hidden or deeply nested
2. **Current location always visible:** Active state clearly marked
3. **Fast switching:** Avoid animations that slow down nav clicks
4. **Space efficient:** Collapse to icons on small screens
5. **Keyboard accessible:** Tab through nav items, Enter to navigate

---

## 4. State Management & UI States

### 4.1 Loading States

**Dashboard Loading:**
```
┌─────────────────────────────┐
│ Dashboard                   │
├─────────────────────────────┤
│                             │
│  ▯▯▯▯▯▯▯ (skeleton)        │  ← Session list placeholder
│  ▯▯▯▯▯▯▯                   │
│  ▯▯▯▯▯▯▯                   │
│                             │
│           [GRAPH LOADING]   │
│                             │  ← Large D3 graph area
│                             │
└─────────────────────────────┘

Guidance:
- Show skeleton placeholders for session list
- Dim graph area with loading spinner (centered)
- Display "Loading system state..." message
- Estimated load time: 1–2 seconds
```

**Tasks View Loading:**
```
┌──────────────────────────────┐
│ Tasks                        │
├──────────────────────────────┤
│ Search [_________] Filter ▼  │
├──────────────────────────────┤
│ ▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯            │  ← Tree items
│ ▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯            │
│   ▯▯▯▯▯▯▯ (subtask)        │
│ ▯▯▯▯▯▯▯▯▯▯▯▯▯▯▯            │
└──────────────────────────────┘

Guidance:
- Show hierarchy skeleton with indentation
- Each skeleton line ~20px tall
- Include checkbox placeholders
- Avoid full-width skeletons; use 80% width for natural feel
```

**Session Viewer Loading:**
```
┌─────────────────────────────────────────┐
│ Session [ID: abc123...] | Starting...   │
├─────────────────────────────────────────┤
│                                         │
│  ⟳ Connecting to session stream...     │  ← Center message
│  (Estimated 3–5 seconds)               │
│                                         │
│                                         │
│  [Status bar at bottom: connecting]    │
└─────────────────────────────────────────┘

Guidance:
- Show spinner + message
- Don't load transcript until stream ready
- Show estimated connection time
- Cancel button visible (navigate back)
```

### 4.2 Empty States

**Dashboard (No Active Sessions):**
```
┌─────────────────────────────┐
│ Dashboard                   │
├─────────────────────────────┤
│                             │
│   🚀 No active sessions     │  ← Icon + message
│                             │
│  Get started by creating a  │
│  task and starting an agent │
│                             │
│  [ Create Task ]  [ Docs ]  │  ← CTA buttons
│                             │
└─────────────────────────────┘

Guidance:
- Large icon (80px) for visual emphasis
- Clear, friendly message (1–2 sentences)
- 1–2 CTAs: primary (Create Task) + secondary (Docs)
- Suggestions: "Try the tutorial task" or "Inspect a past session"
```

**Sessions List (No Sessions):**
```
┌──────────────────────────────┐
│ Sessions                     │
├──────────────────────────────┤
│ Search [_________] Filter ▼  │
├──────────────────────────────┤
│                              │
│   📭 No sessions yet         │
│                              │
│   Create a new session to    │
│   start an agent task        │
│                              │
│   [ New Session ]  [ Tasks ] │
│                              │
└──────────────────────────────┘

Guidance:
- Show in center of list area
- Icon + headline + 1 CTA
- Secondary link to Tasks view
```

**Task Detail (No Todos):**
```
┌──────────────────────────────┐
│ Task: Implement Auth         │
├──────────────────────────────┤
│                              │
│ 📋 No todos defined          │
│                              │
│ Create todos to break down   │
│ this task                    │
│                              │
│ [ Add Todo ]                 │
│                              │
└──────────────────────────────┘
```

**Search Results (No Match):**
```
┌──────────────────────────────┐
│ Tasks (search: "database")   │
├──────────────────────────────┤
│ Search [database____] Clear  │
├──────────────────────────────┤
│                              │
│   🔍 No tasks found          │
│                              │
│   Try adjusting your filters │
│   or search term             │
│                              │
│   [ Reset Filters ]          │
│                              │
└──────────────────────────────┘
```

### 4.3 Error States

**Backend Unavailable:**
```
┌─────────────────────────────────────────┐
│ ⚠️  Backend Connection Lost             │
├─────────────────────────────────────────┤
│                                         │
│  The server is not responding. Check:  │
│  • Server is running                   │
│  • Network connection is stable        │
│  • No firewall blocking requests       │
│                                         │
│  Last successful ping: 2 min ago       │
│  [ Retry ]  [ Check Logs ]             │
│                                         │
│  Auto-retry in 10 seconds...          │
│                                         │
└─────────────────────────────────────────┘

Guidance:
- Full-width banner at top or center modal
- Tone: amber/orange (cautionary, not critical)
- Show last known good state
- Provide actionable help (logs, retry)
- Auto-retry with backoff (2s, 5s, 10s, 30s)
```

**Session Failed:**
```
┌─────────────────────────────────────────┐
│ Session [abc123] | ❌ Failed            │
├─────────────────────────────────────────┤
│                                         │
│  Agent execution failed                │
│                                         │
│  Error: "Failed to load MCP server"   │
│  Code: AGENT_EXEC_ERROR_001           │
│  Time: 2:34 PM                        │
│                                         │
│  [Retry]  [View Logs]  [Report]       │
│                                         │
│  Transcript captured below (read-only) │
│  ─────────────────────────────────     │
│  > Initializing agent...               │
│  > Loading MCP server config...        │
│  ! Error: server not found             │
│                                         │
└─────────────────────────────────────────┘

Guidance:
- Show error at top with badge
- Display error code + human-readable message
- Include timestamp
- Allow logging/debugging actions
- Show partial transcript if available
- Don't lose user data
```

**Stream Disconnected (During Session):**
```
┌─────────────────────────────────────────┐
│ Session [abc123] | ⚠️  Stream Lost      │
├─────────────────────────────────────────┤
│ [Previous transcript above]             │
│ ──────────────────────────────────────  │
│                                         │
│ ❌ Stream disconnected at 2:47 PM      │
│                                         │
│ [ Reconnect ]  [ View Latest ]        │
│                                         │
│ Agent may still be running. Attempting │
│ automatic reconnection...               │
│                                         │
└─────────────────────────────────────────┘

Guidance:
- Show inline, not modal
- Preserve transcript history (don't clear)
- Offer manual reconnect + auto-retry
- Indicate agent status if known
- Show countdown to next retry attempt
```

**Permission Denied:**
```
┌─────────────────────────────────────────┐
│ Task: Critical System Change            │
├─────────────────────────────────────────┤
│                                         │
│ [ Start Agent ]  ← Button is DISABLED  │
│                                         │
│ 🔒 This action requires approval       │
│                                         │
│ Permission: start_agent_on_prod        │
│ Requires: security_lead approval       │
│                                         │
│ [ Request Access ]                     │
│                                         │
└─────────────────────────────────────────┘

Guidance:
- Disable button visually (grayed out)
- Show lock icon + explanation
- Identify permission + required role
- "Request Access" opens modal to notify reviewer
- Don't hide the option (keep it visible!)
```

**Form Validation Error:**
```
┌──────────────────────────────┐
│ Create New Task              │
├──────────────────────────────┤
│                              │
│ Title: [_______________]     │
│        ❌ Title is required  │
│                              │
│ Description: [____________   │
│              ____________]   │
│                              │
│ Role: [developer________▼]   │
│       ❌ Select a role      │
│                              │
│ [ Submit ]  [ Cancel ]      │
│                              │
└──────────────────────────────┘

Guidance:
- Show inline error below field
- Use red/pink text (accessible contrast)
- Include icon (❌ or ℹ️ for helpful errors)
- Don't disable form (user can correct)
- Show error as soon as field loses focus
- Clear on correction
```

---

## 5. Placeholder Content & Implementation Guidance

### 5.1 Data Loading Patterns

**Skeleton Loaders:**
```
• List items: Use 3–5 skeleton rows to set expectations
• Graph: Single large gray box (match graph height)
• Details panel: 2–3 skeleton lines for headers, 1 for content
• Timing: Show skeleton for ≥500ms (avoid flashing)
```

**Progressive Disclosure:**
```
• Load viewport-first content (session list before graph)
• Lazy-load related data (task detail after list is visible)
• Stream transcript lines as they arrive (don't wait for full buffer)
```

### 5.2 Form Guidance

**Task Creation / Assignment:**
- Title (required): Short, descriptive name
- Description (optional): Full context, links, examples
- Assigned Role (required): Dropdown of roles (architect, developer, etc.)
- Priority (required): Critical | High | Normal | Low
- Estimated Time (optional): Hours/minutes
- Tags (optional): Multi-select for categorization
- Template (optional): Use predefined task template

**Error Handling:**
- Validate on blur (not keystroke)
- Show error inline below field
- Disable submit until all required fields valid
- Show success toast after submit (2–3 seconds)

### 5.3 List & Table Patterns

**Task Tree:**
```
Column Layout:
┌────────────────┬──────────┬──────────┬──────────┐
│ Task Name      │ Role     │ Status   │ Actions  │
├────────────────┼──────────┼──────────┼──────────┤
│ Epic Roadmap   │          │          │          │
│ ├─ Auth        │ dev      │ In Prog  │ ▼ menu   │
│ ├─ API         │ arch     │ Blocked  │ ▼ menu   │
│ └─ Testing     │ tester   │ Pending  │ ▼ menu   │
│ Feature Build  │          │          │          │
└────────────────┴──────────┴──────────┴──────────┘

Interaction:
- Checkbox on left for multi-select
- Click row to expand details
- Chevron/arrow indicates expandable
- Hover row for subtle background
- Right-click or menu icon for actions
```

**Sessions List:**
```
┌──────────┬──────────┬──────────┬──────────┬──────────┐
│ Session  │ Task     │ Status   │ Created  │ Actions  │
├──────────┼──────────┼──────────┼──────────┼──────────┤
│ abc123   │ Auth     │ ▶ Running│ 2:30 PM  │ Inspect  │
│ def456   │ API      │ ✓ Done   │ 1:15 PM  │ Replay   │
│ ghi789   │ Testing  │ ⚠ Paused │ 11:00 AM │ Resume   │
└──────────┴──────────┴──────────┴──────────┴──────────┘

Status Badges:
- Running (blue): ▶ Active
- Paused (orange): ⚠ Paused
- Done (green): ✓ Complete
- Failed (red): ✗ Failed
- Pending (gray): ◯ Queued
```

### 5.4 Status Badges & Icons

**Semantic Color & Icon Usage:**
| Status | Color | Icon | Usage |
|--------|-------|------|-------|
| Running | Blue | ▶ | Active session or in-progress task |
| Success | Green | ✓ | Completed, approved |
| Warning | Orange | ⚠ | Paused, pending, needs attention |
| Error | Red | ✗ | Failed, blocked |
| Info | Gray | ℹ | Neutral info, queued |
| Locked | Purple | 🔒 | Guardrail active, access denied |

**Label Examples:**
- "Running (3m 42s elapsed)"
- "Paused (1 message)"
- "Done (15 todos completed)"
- "Failed: MCP server error"

---

## 6. Accessibility & Mobile Considerations

### 6.1 Keyboard Navigation

**Tab Order:**
1. Skip-to-content link (always first)
2. Sidebar nav items
3. Search/filter inputs
4. List items (or "New" button if empty)
5. Action buttons (Inspect, Start Agent, etc.)
6. Modals (trap focus within)

**Keyboard Shortcuts (Optional):**
- `K` or `Cmd+K`: Global search
- `N`: New task
- `?`: Help menu
- `Esc`: Close modal/panel

### 6.2 Mobile Layout

**Stack Order (mobile):**
```
[Header with nav toggle]
[Sidebar - drawer (hidden by default)]
[Search/filter bar]
[Content (full width)]
[Bottom action bar (sticky)]
```

**Touch-Friendly Sizing:**
- Buttons/toggles: ≥44px × 44px
- Tap targets: 8–12px padding
- Form inputs: ≥44px tall
- List items: ≥56px tall

**Responsive Breakpoints:**
| Device | Width | Sidebar | Layout |
|--------|-------|---------|--------|
| Mobile | < 480px | Drawer | Single column |
| Tablet | 480–768px | Collapsed icon | Single/two column |
| Desktop | > 768px | Full width | Two/three column |

---

## 7. Typography & Visual Hierarchy

### Font Scale (Recommended)

```
Display:    24–32px  (page titles, large headers)
Headline:   18–20px  (section headers, task names)
Body:       14–16px  (regular text, descriptions)
Small:      12–13px  (metadata, timestamps, labels)
Monospace:  11–13px  (code blocks, terminal output)
```

### Color Scheme (Light Mode Recommended)

```
Primary:     #0066FF (actions, links, active state)
Success:     #16A34A (completed, allowed)
Warning:     #F59E0B (paused, attention needed)
Error:       #DC2626 (failed, blocked)
Background:  #FFFFFF (main)
Surface:     #F3F4F6 (sidebar, cards)
Border:      #E5E7EB (dividers)
Text:        #111827 (body)
Text Muted:  #6B7280 (secondary)
```

---

## 8. Implementation Checklist for Developers

Use this checklist to implement the flows above:

### Routing & Navigation
- [ ] Sidebar navigation with active state indicator
- [ ] Responsive sidebar (collapse on mobile)
- [ ] Breadcrumb navigation in header
- [ ] Proper `aria-current="page"` on active nav item
- [ ] History API integration (back/forward work correctly)

### Loading & Empty States
- [ ] Skeleton loaders for Dashboard, Tasks, Sessions
- [ ] Empty state illustrations + CTAs for each view
- [ ] Search/filter empty state messaging
- [ ] Loading spinner with status message

### Error Handling
- [ ] Backend unavailable → show banner with retry
- [ ] Session failed → preserve transcript, show error detail
- [ ] Stream disconnect → offer reconnect + auto-retry
- [ ] Guardrail block → show disabled button + "Request Access" CTA
- [ ] Form validation → inline field errors, clear on correction

### User Flows
- [ ] "Select task → Inspect → Start → View session" path is unblocked
- [ ] Task tree expand/collapse with proper visual cues
- [ ] Session detail panel opens from task or sessions list
- [ ] Start Agent button sends correct API request + navigates to session
- [ ] Session viewer streams updates from SSE + renders correctly

### Accessibility
- [ ] Skip-to-content link present
- [ ] All images have alt text
- [ ] Form inputs have associated labels
- [ ] Color not sole means of conveying info (use icons/text too)
- [ ] Buttons/links have sufficient color contrast (≥4.5:1)
- [ ] Keyboard navigation works (Tab, Enter, Esc)
- [ ] Focus visible (not removed, good contrast)

### Mobile
- [ ] Sidebar drawer on mobile (test at 480px width)
- [ ] Touch-friendly buttons (44×44px minimum)
- [ ] Proper viewport meta tag (viewport-fit=cover for notch)
- [ ] List items stack vertically
- [ ] Action buttons always accessible (sticky footer if needed)

---

## 9. Next Steps

This lightweight guide provides:
1. **IA Map** - Routes, hierarchy, primary actions
2. **User Flow** - Select → Inspect → Start → View journey
3. **Sidebar Model** - Fixed permanent nav + responsive collapse
4. **State Visuals** - Loading, empty, and error states with ASCII wireframes
5. **Implementation Guidance** - Forms, lists, badges, colors, a11y

**For Visual Language Task:**
- Use these ASCII diagrams as wireframe reference
- Define final color palette (Light mode recommended)
- Create high-fidelity mockups for each state
- Document icon set (16px for lists, 24px for headers)
- Test at mobile breakpoints (480px, 768px, 1024px)

**For Developer Implementation:**
- This doc is spec-ready; use checklist above
- Follow accessibility guidelines (WCAG 2.1 AA target)
- Test keyboard nav + screen readers before deploy
- Implement error states first (hardest to retrofit)
- Skeleton loaders improve perceived performance significantly

---

**Document Version:** 1.0  
**Last Updated:** Feb 2025  
**Maintainer:** Designer Role  
**Status:** Ready for implementation
