# UI Flows: Information Architecture, Visual Continuity, and Noise Reduction

## 1. Summary
OrbitMesh's frontend currently offers broad operational power, but the user experience is uneven across pages: several views are overloaded with controls and telemetry, while others are placeholders or rely on ad hoc presentation patterns. This spec defines a structured remediation program to improve information architecture, visual continuity, and interaction clarity without reducing core capability.

The goal is to make each major page legible at first glance, establish predictable page structure, and move advanced/low-frequency controls behind progressive disclosure while preserving access for expert users.

## 2. Motivation
The current UI has three recurring user costs:

1. High cognitive load on high-traffic pages (Dashboard, Tasks, Sessions).
2. Inconsistent page maturity and interaction quality between routes.
3. Mixed design-system usage that leads to visual discontinuity.

If unaddressed, this slows onboarding, increases operator mistakes, and raises maintenance cost as every new page invents its own structure.

## 3. Scope
* **In Scope**:
  * Standardize top-level page anatomy (header, metrics, primary actions, body zones).
  * Reduce dashboard noise through prioritization and progressive disclosure.
  * Rebalance Task and Session screens to prioritize primary workflows.
  * Bring outlier pages (e.g., placeholder pages and utility-heavy forms) into visual alignment.
  * Create/maintain frontend UX audit and remediation checklist.
* **Out of Scope**:
  * Backend API contract changes.
  * Provider protocol changes.
  * Major feature additions unrelated to UX continuity.
  * Replacing SolidJS/router/design token stack.

## 4. Requirements & User Experience (UX)
### User stories
1. As an operator, I can identify the primary action for each page in under 3 seconds.
2. As a frequent user, I can still access advanced controls without losing efficiency.
3. As a new user, page layout and terminology feel consistent across routes.

### Functional requirements
1. Every major route uses a common page template with:
   * page title/subtitle,
   * optional metrics strip,
   * max two primary top-level actions,
   * body split into clearly labeled panels.
2. Dashboard defaults to a concise mode; advanced widgets are opt-in.
3. Placeholder pages must either be removed from navigation or upgraded to DS-consistent scaffolds.
4. Utility-heavy settings pages must avoid ad hoc inline style islands in primary flows.

## 5. System Design & Architecture
The remediation uses existing frontend layers:

* Route components in `frontend/src/routes/**` become thinner orchestration containers.
* Reusable presentation components are added under `frontend/src/components/**` for page scaffolds.
* Existing DS classes in `frontend/src/styles/system.css` remain canonical; route-specific style divergence is reduced.

Data flow remains unchanged; this is primarily a composition and presentation refactor with selective state/view toggles.

Performance impacts are expected to be neutral or positive by reducing default-rendered content density on overview pages.

## 6. Security & Privacy
No new auth/authz behavior is introduced. Existing permissions and server-driven gating remain unchanged. UX changes must not hide security-relevant state; instead, they may move low-priority panels behind explicit “Advanced” controls.

## 7. Testing Plan
1. Unit tests for new page-mode toggles and visibility conditions.
2. Route smoke tests for continuity of core controls and headings.
3. Existing navigation and dashboard UI tests updated for concise/advanced behavior.
4. Manual responsive checks for page header + panel layout on mobile/tablet/desktop breakpoints.

Edge cases:
* No data states,
* loading/error states,
* narrow viewport with expanded advanced sections.

## 8. Rollout & Deployment
No DB migrations. No feature flag required for initial pass.

Rollout strategy:
1. Merge in phases by route family (overview -> worklists -> settings outliers).
2. Validate each phase with UI tests and targeted manual checks.
3. Track post-merge regressions with existing UI test suites and bug intake.

## 9. Alternatives Considered
1. **Full redesign rewrite**: rejected due to high risk and delayed value.
2. **Do nothing / incremental ad hoc tweaks**: rejected because it perpetuates inconsistency.
3. **Strict visual-only CSS pass without IA changes**: rejected; noise is structural, not only stylistic.

## 10. Implementation Plan
* [x] Create written UX audit and remediation roadmap.
* [x] Add this UI Flows feature spec and link it from `SPEC.md`.
* [x] Dashboard: implement concise default mode with opt-in advanced widgets.
* [x] Upgrade placeholder-level About page to DS-consistent informational page.
* [ ] Task view: split “control” concerns from “exploration” concerns with progressive disclosure.
* [ ] Session index: prioritize creation and filtering above secondary metadata.
* [ ] Settings outliers: remove inline style islands and align with DS panel primitives.
* [ ] Add/adjust tests to lock in concise-vs-advanced page behavior.

## 11. Open Questions
1. Should concise/advanced preference persist per user (local storage) or remain per-session?
2. Which dashboard widgets are mandatory in concise mode for first-release UX baseline?
3. Should utility pages (Extractors/MCP servers) become “Advanced” nav destinations by default?
