import { createResource, For, Show } from "solid-js";
import { apiClient } from "../api/client";
import AgentGraph from "../graph/AgentGraph";

export default function Dashboard() {
  const [sessions] = createResource(apiClient.listSessions);
  const [permissions] = createResource(apiClient.getPermissions);

  const guardrails = () => permissions()?.guardrails ?? [];

  return (
    <div class="dashboard">
      <header>
        <h1>OrbitMesh Dashboard</h1>
        <p class="dashboard-subtitle">Guarded management, visible operations</p>
      </header>
      
      <main>
        <div class="primary-column">
          <section class="guardrails-panel">
            <div class="guardrails-header">
              <div>
                <p class="guardrails-label">Management guardrails</p>
                <h2>Permission health</h2>
              </div>
              <span class="guardrail-pill">Protected</span>
            </div>

            <Show
              when={!permissions.loading}
              fallback={<p class="guardrails-loading">Loading guardrail policy...</p>}
            >
              <p class="guardrails-role">
                Role: <strong>{permissions()?.role}</strong>
              </p>
              <Show
                when={guardrails().length > 0}
                fallback={<p class="guardrails-loading">Guardrail policy unavailable.</p>}
              >
                <div class="guardrail-grid">
                  <For each={guardrails()}>
                    {(item) => (
                      <article class="guardrail-card" classList={{ active: item.allowed }}>
                        <header>
                          <h3>{item.title}</h3>
                          <span>{item.allowed ? "Allowed" : "Restricted"}</span>
                        </header>
                        <p>{item.detail}</p>
                      </article>
                    )}
                  </For>
                </div>
              </Show>
              <p class="guardrails-note">
                {permissions()?.requires_owner_approval_for_role_changes
                  ? "Role escalations now require explicit owner confirmation before they can be saved."
                  : "Role changes follow an automatic review process."}
              </p>
            </Show>
          </section>

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
                          <Show
                            when={!permissions.loading}
                            fallback={<span class="muted-action">Guardrail pending</span>}
                          >
                            <Show
                              when={permissions()?.can_inspect_sessions}
                              fallback={<span class="muted-action">Inspect locked</span>}
                            >
                              <button onClick={() => alert("Inspect " + session.id)}>
                                Inspect
                              </button>
                            </Show>
                          </Show>
                        </td>
                      </tr>
                    )}
                  </For>
                </tbody>
              </table>
            </Show>
          </section>
        </div>

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
