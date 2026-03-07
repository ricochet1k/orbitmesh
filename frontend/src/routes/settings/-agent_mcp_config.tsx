import { createResource, For, Show } from 'solid-js'
import { apiClient } from '../../api/client'
import type { AgentMCPRef, MCPServerEntryResponse, MCPToolInfo } from '../../types/api'

interface AgentMCPServerConfigProps {
  server: MCPServerEntryResponse
  ref: AgentMCPRef
  onChange: (updatedRef: AgentMCPRef) => void
}

export function AgentMCPServerConfig(props: AgentMCPServerConfigProps) {
  const [capabilities] = createResource(
    () => props.server.id,
    apiClient.getMCPServerCapabilities,
  )

  const isAllowed = (toolName: string) => {
    if (props.ref.disallowed_tools?.includes(toolName)) return false
    if (props.ref.allowed_tools && props.ref.allowed_tools.length > 0) {
      return props.ref.allowed_tools.includes(toolName)
    }
    return true // Default allowed if neither list restricts it
  }

  const toggleTool = (toolName: string, checked: boolean) => {
    const allowed = [...(props.ref.allowed_tools ?? [])]
    const disallowed = [...(props.ref.disallowed_tools ?? [])]

    if (checked) {
      const dIndex = disallowed.indexOf(toolName)
      if (dIndex >= 0) disallowed.splice(dIndex, 1)

      if (allowed.length > 0 && !allowed.includes(toolName)) {
        allowed.push(toolName)
      }
    } else {
      if (allowed.length > 0) {
        const aIndex = allowed.indexOf(toolName)
        if (aIndex >= 0) allowed.splice(aIndex, 1)
      } else {
        if (!disallowed.includes(toolName)) disallowed.push(toolName)
      }
    }

    props.onChange({
      ...props.ref,
      allowed_tools: allowed.length > 0 ? allowed : undefined,
      disallowed_tools: disallowed.length > 0 ? disallowed : undefined,
    })
  }

  return (
    <div style="margin-left: 1.5rem; margin-top: 0.5rem; padding-left: 0.5rem; border-left: 2px solid var(--border-color); font-size: 0.9em;">
      <Show when={capabilities.loading}>
        <p class="muted">Loading tools...</p>
      </Show>
      <Show when={capabilities.error}>
        <p class="error-message">Failed to load tools: {capabilities.error.message}</p>
      </Show>
      <Show when={capabilities()}>
        <div style="display: flex; flex-direction: column; gap: 0.5rem;">
          <For each={capabilities()!.tools}>
            {(tool: MCPToolInfo) => (
              <label style="display: flex; align-items: start; gap: 0.5rem; cursor: pointer;">
                <input
                  type="checkbox"
                  style="margin-top: 0.2rem;"
                  checked={isAllowed(tool.name)}
                  onChange={(e) => toggleTool(tool.name, e.currentTarget.checked)}
                />
                <div style="display: flex; flex-direction: column;">
                  <span style="font-family: monospace; font-weight: 500;">{tool.name}</span>
                  <Show when={tool.description}>
                    <span class="muted" style="font-size: 0.85em; margin-top: 0.1rem;">{tool.description}</span>
                  </Show>
                </div>
              </label>
            )}
          </For>
        </div>
      </Show>
    </div>
  )
}
