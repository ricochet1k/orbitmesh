# OrbitMesh User Journeys & UI Flows

This document outlines the core developer journeys within the OrbitMesh platform. It details how users interact with the system, how they navigate between different views, and the critical role that real-time websocket updates play in the overall user experience.

## Target Audience
The primary users of OrbitMesh are developers who need to manage, monitor, and debug autonomous agent executions. The interface must provide both a high-level overview of system health and granular, real-time insights into individual tasks, terminal outputs, and code execution flows.

---

## 1. Journey: Starting and Monitoring a New Agent Session

### 1.1 Triggering a Session
**Goal**: A developer wants to initiate a new task for an AI agent to solve.
* **Entry Point**: The user starts at the main dashboard or a dedicated "New Session" view.
* **Action**: They input a prompt or select a pre-defined task template. They may configure specific provider settings (e.g., Claude, Codex) or select the target project workspace.
* **Feedback**: Upon submission, the UI immediately transitions to the active **Agent Session Viewer** for that newly created session.

### 1.2 Real-Time Monitoring
**Goal**: The developer needs to ensure the agent is making progress and hasn't encountered errors or infinite loops.
* **View**: Agent Session Viewer.
* **Real-time Elements**:
    * **Activity Feed**: A live feed of events streams in via websockets, detailing each step the agent takes (e.g., "Reading file X", "Executing command Y").
    * **Terminal PTY Viewer**: If the agent is running shell commands, a live terminal output is displayed. This is driven by the real-time PTY websocket feed, ensuring the developer sees output *exactly* as it is generated, without polling delays.
    * **Status Indicators**: Dynamic badges indicate the current state (e.g., *Running*, *Waiting for User Input*, *Completed*, *Failed*).

### 1.3 Intervention & Interaction
**Goal**: The agent is blocked and requires human confirmation or specific input.
* **Trigger**: A "Waiting for User Input" event is received over the websocket.
* **Action**: The UI highlights the required action (e.g., approving a command execution, answering a clarifying question). The developer provides the input directly within the Session Viewer.
* **Continuation**: The input is sent to the backend, the agent resumes processing, and the real-time feeds immediately reflect the continuation.

---

## 2. Journey: Debugging a Complex Task Hierarchy

### 2.1 Exploring Task Trees
**Goal**: A developer wants to understand how a high-level goal was decomposed into smaller sub-tasks.
* **Entry Point**: Navigating to the **Tasks** or **History** view.
* **View**: Task Tree Viewer.
* **Interaction**: The user sees a hierarchical representation of tasks. They can expand nodes to drill down into specific sub-tasks.
* **Status Visibility**: Each node in the tree displays its status. Real-time updates push status changes up the tree (e.g., a sub-task failure immediately marks the parent node as "At Risk" or "Failed").

### 2.2 Deep Dive into Code Execution
**Goal**: A specific sub-task failed or produced unexpected results, and the developer needs to understand the exact code execution path.
* **Transition**: From a specific task node or session event, the user clicks "View in CodeFlow".
* **View**: **CodeFlow Explorer** (Canvas Interface).
* **Interaction**:
    * The user is presented with a WebGL-based node graph (powered by sigma.js) representing the structural elements (functions, variables) and execution flow.
    * They can pan, zoom, and select specific nodes to see the exact data flowing through them at runtime.
    * **Real-time Aspect**: If the session is currently active, the CodeFlow graph updates dynamically as the static analyzer processes new files or as execution events are emitted, showing the "live pulse" of the agent's work.

---

## 3. Journey: Reviewing Historical Executions

### 3.1 Session History & Filtering
**Goal**: A developer needs to audit past agent performance or review a specific execution from last week.
* **Entry Point**: The **History / Sessions** view.
* **Interaction**: The user utilizes filters (by date, status, provider, project) to find specific sessions.
* **View**: A paginated list of past sessions.

### 3.2 Examining Transcripts
**Goal**: Understanding the exact prompt and response exchange for a historical session.
* **Action**: Selecting a session opens the historical **Agent Session Viewer**.
* **View**: The transcript is loaded.
* **UX Consideration**: For very long sessions, the transcript is paginated or lazily loaded to ensure UI responsiveness. The user can scroll back through the exact sequence of events, LLM responses, and tool calls.

---

## 4. Journey: Environment and Settings Management

### 4.1 Configuring Providers
**Goal**: A developer needs to add a new API key or switch the default LLM provider.
* **Entry Point**: The **Settings** view.
* **Interaction**: The user navigates to the "Providers" section. They can input credentials, configure global parameters (like temperature or default models), and test connections.

### 4.2 Managing Project Workspaces
**Goal**: The platform needs to operate on a new repository or codebase.
* **Entry Point**: A "Projects" or "Workspaces" management view.
* **Interaction**: The user defines the path to the project, sets environment variables specific to that workspace, and configures any pre-requisite setup commands the agent should run.

---

## Conclusion
The success of the OrbitMesh UI hinges on its ability to present complex, asynchronous, and deeply hierarchical data in a comprehensible manner. The heavy reliance on real-time websocket updates is not just a technical feature; it is the core mechanism that keeps the developer in sync with the autonomous agent, building trust and enabling rapid intervention when necessary.
