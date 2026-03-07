import type { ProviderConfigRequest, ProviderTestRequest } from '../../../types/api'
import type { ProviderType, ProviderEdit } from './-_shared'

function parseTools(raw: string): string[] {
  return raw.split(',').map((s) => s.trim()).filter(Boolean)
}

/** Build a ProviderTestRequest from the current store state. */
export function buildTestRequest(provider: ProviderEdit): ProviderTestRequest {
  const t = provider.type as ProviderType
  const req: ProviderTestRequest = { type: t }

  // Env (filter blank keys)
  const env: Record<string, string> = {}
  for (const [k, v] of Object.entries(provider.env ?? {})) {
    if (k.trim()) env[k.trim()] = v
  }
  if (Object.keys(env).length > 0) req.env = env

  req.custom = {}

  // PTY: command
  if (t === 'pty') {
    const parts = provider.command.trim().split(/\s+/)
    if (parts[0]) req.command = parts
  }

  // ACP
  if (t === 'acp') {
    const cmd = String(provider.scratch['acp_command'] ?? '').trim()
    if (cmd) req.custom['acp_command'] = cmd
  }

  // Model (claude, claude-ws, adk, openai, codex)
  if (t === 'claude' || t === 'claude-ws' || t === 'adk' || t === 'openai' || t === 'codex') {
    const model = String(provider.scratch['model'] ?? '').trim()
    if (model) req.custom['model'] = model
  }

  // OpenAI base URL
  if (t === 'openai') {
    const baseURL = String(provider.scratch['base_url'] ?? '').trim()
    if (baseURL) req.custom['base_url'] = baseURL
  }

  if (t === 'codex') {
    const effort = String(provider.scratch['effort'] ?? '').trim()
    if (effort) req.custom['effort'] = effort

    const summary = String(provider.scratch['summary'] ?? '').trim()
    if (summary) req.custom['summary'] = summary

    const approvalPolicy = String(provider.scratch['approval_policy'] ?? '').trim()
    if (approvalPolicy) req.custom['approval_policy'] = approvalPolicy

    const sandboxMode = String(provider.scratch['sandbox_mode'] ?? '').trim()
    if (sandboxMode) req.custom['sandbox_mode'] = sandboxMode

    const codexCommand = String(provider.scratch['codex_command'] ?? '').trim()
    if (codexCommand) req.custom['codex_command'] = codexCommand

    const codexArgs = String(provider.scratch['codex_args'] ?? '').trim()
    if (codexArgs) req.custom['codex_args'] = codexArgs.split(/\s+/)
  }

  // ADK Vertex AI
  if (t === 'adk') {
    if (provider.scratch['use_vertex_ai']) {
      req.custom['use_vertex_ai'] = true
      const proj = String(provider.scratch['vertex_project_id'] ?? '').trim()
      if (proj) req.custom['vertex_project_id'] = proj
      const loc = String(provider.scratch['vertex_location'] ?? '').trim()
      if (loc) req.custom['vertex_location'] = loc
    }
  }

  if (Object.keys(req.custom).length === 0) delete req.custom
  return req
}

export function buildConfigRequest(provider: ProviderEdit): ProviderConfigRequest {
  const t = provider.type as ProviderType

  if (!provider.name.trim()) throw new Error('Name is required')

  const config: ProviderConfigRequest = {
    name: provider.name.trim(),
    type: t,
    is_active: provider.is_active ?? false,
    custom: {},
  }

  // --- Env (filter out blank keys, inject API keys from sub-forms) ---
  const env: Record<string, string> = {}
  for (const [k, v] of Object.entries(provider.env ?? {})) {
    if (k.trim()) env[k.trim()] = v
  }
  if (Object.keys(env).length > 0) config.env = env

  // --- PTY: command (required, split on whitespace) ---
  if (t === 'pty') {
    const parts = provider.command.trim().split(/\s+/)
    if (!parts[0]) throw new Error('Command is required for PTY provider')
    config.command = parts
  }

  // --- ACP: command + optional args (stored in scratch) ---
  if (t === 'acp') {
    const cmd = String(provider.scratch['acp_command'] ?? '').trim()
    if (!cmd) throw new Error('ACP command is required')
    config.custom!['acp_command'] = cmd
    const rawArgs = String(provider.scratch['acp_args'] ?? '').trim()
    if (rawArgs) config.custom!['acp_args'] = rawArgs.split(/\s+/)
  }

  // --- Model (claude, claude-ws, adk, openai, codex) ---
  if (t === 'claude' || t === 'claude-ws' || t === 'adk' || t === 'openai' || t === 'codex') {
    const model = String(provider.scratch['model'] ?? '').trim()
    if (model) config.custom!['model'] = model
  }

  // --- OpenAI base URL ---
  if (t === 'openai') {
    const baseURL = String(provider.scratch['base_url'] ?? '').trim()
    if (baseURL) config.custom!['base_url'] = baseURL
  }

  // --- Claude shared fields ---
  if (t === 'claude' || t === 'claude-ws') {
    const pm = String(provider.scratch['permission_mode'] ?? '').trim()
    if (pm) config.custom!['permission_mode'] = pm

    const budget = parseFloat(String(provider.scratch['max_budget_usd'] ?? ''))
    if (!isNaN(budget) && budget > 0) config.custom!['max_budget_usd'] = budget

    const allowed = parseTools(String(provider.scratch['allowed_tools'] ?? ''))
    if (allowed.length > 0) config.custom!['allowed_tools'] = allowed

    const disallowed = parseTools(String(provider.scratch['disallowed_tools'] ?? ''))
    if (disallowed.length > 0) config.custom!['disallowed_tools'] = disallowed

    if (provider.scratch['dangerously_skip_permissions']) {
      config.custom!['dangerously_skip_permissions'] = true
    }
  }

  // --- claude-ws extras ---
  if (t === 'claude-ws') {
    const turns = parseInt(String(provider.scratch['max_turns'] ?? ''), 10)
    if (!isNaN(turns) && turns > 0) config.custom!['max_turns'] = turns

    const resume = String(provider.scratch['resume_session_id'] ?? '').trim()
    if (resume) config.custom!['resume_session_id'] = resume
  }

  // --- ADK extras ---
  if (t === 'adk') {
    if (provider.scratch['use_vertex_ai']) {
      config.custom!['use_vertex_ai'] = true
      const proj = String(provider.scratch['vertex_project_id'] ?? '').trim()
      if (proj) config.custom!['vertex_project_id'] = proj
      const loc = String(provider.scratch['vertex_location'] ?? '').trim()
      if (loc) config.custom!['vertex_location'] = loc
    }
  }

  // --- codex app-server extras ---
  if (t === 'codex') {
    const effort = String(provider.scratch['effort'] ?? '').trim()
    if (effort) config.custom!['effort'] = effort

    const summary = String(provider.scratch['summary'] ?? '').trim()
    if (summary) config.custom!['summary'] = summary

    const approvalPolicy = String(provider.scratch['approval_policy'] ?? '').trim()
    if (approvalPolicy) config.custom!['approval_policy'] = approvalPolicy

    const sandboxMode = String(provider.scratch['sandbox_mode'] ?? '').trim()
    if (sandboxMode) config.custom!['sandbox_mode'] = sandboxMode

    const codexCommand = String(provider.scratch['codex_command'] ?? '').trim()
    if (codexCommand) config.custom!['codex_command'] = codexCommand

    const codexArgs = String(provider.scratch['codex_args'] ?? '').trim()
    if (codexArgs) config.custom!['codex_args'] = codexArgs.split(/\s+/)
  }

  // Drop empty custom map
  if (Object.keys(config.custom!).length === 0) delete config.custom

  return config
}
