# Frontend UI Audit and Remediation Plan (2026-03-26)

## Executive assessment
The frontend has strong functional breadth and decent route/component separation, but suffers from inconsistent information architecture and page-level noise. Key operational views often present too many controls at once, while some routes are clearly less mature than others.

## Observed strengths
- Shared shell and DS primitives exist and are reusable.
- Route organization is broadly coherent.
- Dashboard widgets are already componentized, enabling controlled re-prioritization.

## Core issues

### 1) Information density and noisy defaults
- Dashboard renders many widgets at once, including highly detailed ones, increasing scan cost.
- Task page combines hierarchy exploration, agent launching, graph view, and context actions in one surface.
- Session surfaces combine summary, filtering, and creation complexity in ways that dilute primary actions.

### 2) Continuity gaps between pages
- Some routes are DS-structured and production-like, while others remain placeholder/utility-like.
- Utility-heavy settings pages diverge in visual rigor (e.g., inline style islands and inconsistent panel structure).

### 3) Design-system migration is incomplete
- Mixed use of DS classes, older class families, and per-file custom patterns causes visible inconsistency.

---

## Detailed remediation plan

### Phase 0 — Spec and alignment (complete)
- [x] Write down audit findings.
- [x] Create feature spec for UI flow consistency and noise reduction.
- [x] Link the new spec from `SPEC.md`.

### Phase 1 — Quick continuity wins (in progress)
- [x] Dashboard: introduce concise default mode and move secondary widgets behind an explicit advanced toggle.
- [x] About page: replace placeholder output with DS-consistent scaffold.
- [x] Add/update route-level UI tests for concise dashboard/task behavior.

### Phase 2 — IA restructuring (planned)
- [x] Task route: separate exploration from action control with progressive disclosure and tighter primary-action framing.
- [x] Sessions index: rebalance toward “find/start session quickly” as dominant workflow.
- [x] Extractors page: reduce “all-controls-at-once” exposure with expandable advanced sections.
- [x] MCP page: reduce “all-controls-at-once” exposure with expandable advanced sections.

### Phase 3 — Systemization and hardening (planned)
- [x] Introduce shared page template helpers (header/actions/metrics/body slots).
- [x] Remove ad hoc inline styling in settings utilities.
- [x] Normalize naming and class usage to DS-first conventions.
- [x] Expand navigation/responsive tests to guard continuity regressions.

---

## Success criteria
1. Every major route has one clearly dominant action path.
2. Advanced controls remain accessible but are not default-noise.
3. Page structure remains consistent across Dashboard, Tasks, Sessions, Terminals, and Settings.
4. Core UX continuity is test-protected.
