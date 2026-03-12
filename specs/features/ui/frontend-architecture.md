# Frontend Architecture

## 1. Summary
The OrbitMesh frontend is a single-page application (SPA) built to provide a robust, highly responsive, and real-time user interface for managing autonomous agents, executing complex developer workflows, and visualizing high-density data. This document outlines the foundational architecture of the frontend, which leverages SolidJS for fine-grained reactivity, Vite as the build tool, and @tanstack/solid-router for type-safe routing. It serves as the definitive technical guide for the frontend's structural design, API integration patterns, and rendering strategies.

## 2. Motivation
OrbitMesh operates in an environment characterized by high-frequency streaming data (e.g., live terminal feeds, real-time agent execution events) and complex visualizations (e.g., CodeFlow node graphs). Traditional virtual DOM-based frameworks can struggle with the overhead of rendering such rapid, dense updates. A reactive, signal-based architecture is necessary to ensure the UI remains performant, predictable, and maintainable without sacrificing developer velocity. Establishing a strict architectural foundation ensures consistency, security, and scalability as the platform's feature set grows.

## 3. Scope
* **In Scope**:
    * Core technology stack selection and rationale (SolidJS, Vite, TanStack Router).
    * Project folder structure and module organization.
    * State management patterns (Signals, Stores).
    * API client architecture, including REST interactions, CSRF protection, and WebSocket feed integration.
    * High-performance rendering strategies for high-frequency updates and large canvas visualizations (e.g., integration patterns for WebGL/sigma.js).
    * Testing philosophy and tooling setup (Vitest, Playwright).
* **Out of Scope**:
    * Detailed implementations of specific UI features (e.g., Agent Session Viewer, CodeFlow Explorer, Management Interfaces). These have their own dedicated specifications (see `SPEC.md`).
    * Backend API implementation details or database schema designs.
    * Detailed visual design guidelines or CSS utility framework choices, except where they impact overarching architecture.

## 4. Requirements & User Experience (UX)
* **Real-time Responsiveness**: The UI must reflect high-frequency backend events (e.g., terminal output, session state changes) with minimal latency and no UI blocking.
* **Type Safety**: End-to-end type safety from the routing layer down to API responses to catch errors at compile time.
* **Resilience**: The frontend must gracefully handle WebSocket disconnections, API timeouts, and unexpected payload formats, providing clear feedback to the user and attempting recovery where appropriate.
* **Security**: All mutations and sensitive data requests must seamlessly integrate with the backend's security model, including mandatory CSRF headers and proper authentication handling.
* **Developer Experience (DX)**: The architecture must support rapid iteration with fast Hot Module Replacement (HMR) and strict linting rules to enforce best practices (e.g., avoiding generic object types, preventing `innerHTML` usage).

## 5. System Design & Architecture

### 5.1 Technology Stack
* **Framework**: SolidJS (fine-grained reactivity, no Virtual DOM).
* **Build Tool**: Vite (fast HMR, optimized production builds).
* **Routing**: `@tanstack/solid-router` (type-safe, file-based routing).
* **Package Manager**: pnpm (fast, deterministic dependency management).
* **Language**: TypeScript (strict mode enabled).

### 5.2 Folder Structure
The `frontend/` directory is organized to separate concerns and scale with complex features:

```text
frontend/
├── src/
│   ├── api/           # API client generators, WebSocket managers, and fetch wrappers.
│   ├── components/    # Reusable, domain-agnostic UI components (buttons, modals).
│   ├── features/      # Domain-specific modules (e.g., sessions, codeflow).
│   ├── routes/        # File-based routing tree managed by TanStack Router.
│   ├── store/         # Global state management (if necessary beyond local signals).
│   ├── utils/         # Helper functions, type definitions, and formatters.
│   └── App.tsx        # Root component and router provider.
├── tests/
│   ├── unit/          # Vitest unit tests.
│   └── ui/            # Playwright E2E tests.
├── vite.config.ts     # Vite build configuration.
└── tsconfig.json      # TypeScript configuration.
```

### 5.3 State Management
* **Local State**: Managed using SolidJS `createSignal` and `createStore` for fine-grained, localized reactivity within components or feature boundaries.
* **Derived State**: Computed using `createMemo` to avoid redundant calculations, particularly for data derived from high-frequency feeds.
* **Global State**: Kept to a minimum. Global concerns (like current user session or theme) are managed via SolidJS Context providers wrapping the application tree.

### 5.4 Routing (TanStack Router)
* Utilizes a file-based routing convention within the `src/routes/` directory.
* Files and utilities intended to be ignored by the route tree must be prefixed with a dash (e.g., `-_shared.tsx`).
* Route definitions strictly define their expected search parameters and loader data requirements to ensure type-safe navigation.

### 5.5 API and WebSocket Integration
* **REST API Client**: All outgoing fetch requests must include CSRF headers to prevent 403 Forbidden errors from the Go backend. The client architecture abstracts this requirement to ensure developers do not accidentally omit it.
* **WebSocket Management**: A dedicated WebSocket manager handles connecting to the backend's real-time feeds (e.g., PTY activity, session events). It manages connection lifecycle, automatic reconnection with exponential backoff, and dispatches events to reactive signals.

### 5.6 Rendering Strategies for High-Frequency Data
* **Terminal/Event Feeds**: Instead of re-rendering massive lists on every tick, SolidJS's `<For>` and `<Index>` components are used strictly for list rendering. For extreme high-frequency data (like raw PTY streams), the architecture dictates using a custom lightweight terminal renderer rather than heavy third-party emulators like xterm.js. The architecture supports buffering and batching these DOM updates seamlessly with SolidJS's fine-grained reactivity.
* **Canvas Visualizations**: For complex node graphs (CodeFlow), the architecture dictates offloading rendering to WebGL via libraries like `sigma.js` and `graphology`. SolidJS is responsible for managing the *data state* and *canvas lifecycle*, but not the frame-by-frame rendering of the graph nodes to avoid reactive overhead on thousands of elements.

## 6. Security & Privacy
* **CSRF Protection**: All mutating API requests (POST, PUT, DELETE) and authenticated data fetches are wrapped by an API client that automatically injects the required CSRF tokens negotiated with the backend.
* **XSS Prevention**: Strict adherence to the `eslint-plugin-solid` rules. The use of `innerHTML` is strictly prohibited to prevent Cross-Site Scripting vulnerabilities, especially when rendering agent outputs or logs.
* **Authentication State**: The frontend reacts to unauthorized (401) or forbidden (403) responses by gracefully terminating active WebSocket connections, clearing sensitive local state, and redirecting to the appropriate authentication flow.

## 7. Testing Plan
* **Unit and Integration Testing**: Executed using Vitest (`pnpm run test`). Tests focus on utility functions, API client logic, state management behavior, and complex component rendering logic.
* **End-to-End (E2E) Testing**: Executed using Playwright (`frontend/tests/ui/`). Tests validate critical user journeys, ensuring that routing, API integration, and rendering work cohesively in a real browser environment.
* **Strict Linting**: ESLint enforces strict SolidJS patterns (e.g., avoiding `Array.map` in JSX, forbidding generic `{}` object types). CI pipelines will fail if linting rules are violated.

## 8. Rollout & Deployment
* **Build Process**: The SPA is compiled into static assets (HTML, CSS, JS) using Vite (`pnpm run build`).
* **Environment Configuration**: API endpoints and environment-specific behaviors are injected via Vite's `import.meta.env` mechanism during the build process or provided dynamically by the hosting environment.
* **Serving**: The static assets are designed to be served by any standard web server or CDN, with the backend API path typically proxying to the Go execution engine.

## 9. Alternatives Considered
N/A - As per project constraints, the focus is strictly on the technical design and requirements for the selected architecture.

## 10. Implementation Plan
* [ ] Initialize Vite project with SolidJS and TypeScript template.
* [ ] Configure pnpm, strict ESLint rules (`eslint-plugin-solid`), and TypeScript compiler options.
* [ ] Set up `@tanstack/solid-router` with basic route tree structure.
* [ ] Implement core API fetch wrapper with mandatory CSRF header injection.
* [ ] Design and implement the WebSocket connection manager for real-time feeds.
* [ ] Configure Vitest and Playwright testing environments.
* [ ] Document canvas integration patterns (sigma.js/graphology) for future CodeFlow features.

## 11. Open Questions
* *Are there specific bandwidth or latency constraints we need to simulate for the WebSocket connection manager's reconnection logic?*
* *Will the static assets be served directly by the Go backend in production, or via a separate CDN?*
