import { createResource, For, Show } from "solid-js";
import { apiClient } from "../api/client";
import AgentGraph from "../graph/AgentGraph";

export default function Dashboard() {
  const [sessions] = createResource(apiClient.listSessions);

  return (
    <div class="dashboard">
      <header>
        <h1>OrbitMesh Dashboard</h1>
      </header>
      
      <main>
        <section class="sessions-list">
          <h2>Active Sessions</h2>
          <Show when={!sessions.loading} fallback={<p>Loading sessions...</p>}>
            <table>
              <thead>
                <tr>
                  <th>ID</th>
                  <th>Provider</th>
                  <th>State</th>
                  <th>Task</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                <For each={sessions()?.sessions}>
                  {(session) => (
                    <tr>
                      <td>{session.id.substring(0, 8)}...</td>
                      <td>{session.provider_type}</td>
                      <td>
                        <span class={`state-badge ${session.state}`}>
                          {session.state}
                        </span>
                      </td>
                      <td>{session.current_task || "None"}</td>
                      <td>
                        <button onClick={() => alert("Inspect " + session.id)}>
                          Inspect
                        </button>
                      </td>
                    </tr>
                  )}
                </For>
              </tbody>
            </table>
          </Show>
        </section>

        <section class="graph-view">
          <h2>System Graph</h2>
          <div id="graph-container">
            <AgentGraph />
          </div>
        </section>
      </main>
    </div>
  );
}
