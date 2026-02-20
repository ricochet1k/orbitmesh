/**
 * Shared types, store shape, and small field components used by all provider
 * sub-forms.  Nothing here renders a full form — just primitives.
 */
import { type JSX } from 'solid-js'
import type { ProviderConfigResponse } from '../../../types/api'

// ---------------------------------------------------------------------------
// Provider type registry
// ---------------------------------------------------------------------------

export const PROVIDER_TYPES = [
  { value: 'claude-ws', label: 'Claude (WebSocket SDK)' },
  { value: 'claude',    label: 'Claude (stream-json)' },
  { value: 'acp',       label: 'ACP (generic agent)' },
  { value: 'adk',       label: 'ADK (Gemini / Google)' },
  { value: 'pty',       label: 'PTY (terminal)' },
] as const

export type ProviderType = (typeof PROVIDER_TYPES)[number]['value']

// ---------------------------------------------------------------------------
// Store shape
// ---------------------------------------------------------------------------

/**
 * The mutable edit state for a provider.  `command` is stored as a single
 * space-separated string and split on save; `api_key` is intentionally absent
 * at the top level — each provider sub-form places it in `env` or `custom`.
 */
export type ProviderEdit = Omit<ProviderConfigResponse, 'command' | 'api_key'> & {
  command: string
  /** Scratch area for provider-specific fields that map to `custom`. */
  scratch: Record<string, string | boolean | number>
}

export function initialProviderEdit(base?: ProviderConfigResponse): ProviderEdit {
  if (!base) {
    return {
      id: '',
      name: '',
      type: '',
      command: '',
      custom: {},
      env: {},
      is_active: true,
      scratch: {},
    }
  }

  const scratch: Record<string, string | boolean | number> = {}
  const custom = base.custom ?? {}

  // Lift well-known custom keys into scratch so sub-forms can bind to them
  // directly without reaching into the opaque `custom` map.
  const liftString = (k: string) => { if (k in custom) scratch[k] = String(custom[k]) }
  const liftBool   = (k: string) => { if (k in custom) scratch[k] = Boolean(custom[k]) }
  const liftNum    = (k: string) => { if (k in custom) scratch[k] = Number(custom[k]) }
  const liftArray  = (k: string) => {
    if (k in custom) scratch[k] = (custom[k] as string[]).join(', ')
  }

  liftString('model')
  liftString('permission_mode')
  liftNum('max_budget_usd')
  liftArray('allowed_tools')
  liftArray('disallowed_tools')
  liftBool('dangerously_skip_permissions')
  liftNum('max_turns')
  liftString('resume_session_id')
  liftBool('use_vertex_ai')
  liftString('vertex_project_id')
  liftString('vertex_location')
  liftString('acp_command')
  liftString('acp_args')

  // API keys were historically stored on api_key; lift them into env scratch
  // so the env editor can display/edit them.
  if (base.api_key) {
    const envKey = envKeyName(base.type as ProviderType)
    if (envKey) {
      scratch[`env_apikey`] = base.api_key
    }
  }

  return {
    id: base.id,
    name: base.name,
    type: base.type,
    command: base.command?.join(' ') ?? '',
    custom: { ...custom },
    env: { ...(base.env ?? {}) },
    is_active: base.is_active,
    scratch,
  }
}

/** Returns the conventional environment-variable name for the API key of a
 *  provider type, or null for types that don't use an API key. */
export function envKeyName(type: ProviderType | string): string | null {
  switch (type) {
    case 'claude':
    case 'claude-ws':
      return 'ANTHROPIC_API_KEY'
    case 'adk':
      return 'GOOGLE_API_KEY'
    default:
      return null
  }
}

// ---------------------------------------------------------------------------
// Small reusable field components
// ---------------------------------------------------------------------------

interface FormGroupProps {
  id?: string
  label: string
  hint?: string
  danger?: boolean
  children: JSX.Element
}

export function FormGroup(props: FormGroupProps) {
  return (
    <div class={`form-group${props.danger ? ' form-group--danger' : ''}`}>
      <label for={props.id}>{props.label}</label>
      {props.children}
      {props.hint && (
        <p class={`form-hint${props.danger ? ' form-hint--danger' : ''}`}>{props.hint}</p>
      )}
    </div>
  )
}

interface TextFieldProps {
  id: string
  label: string
  value: string
  onInput: (v: string) => void
  placeholder?: string
  hint?: string
  required?: boolean
  type?: string
}

export function TextField(props: TextFieldProps) {
  return (
    <FormGroup id={props.id} label={props.label} hint={props.hint}>
      <input
        id={props.id}
        type={props.type ?? 'text'}
        value={props.value}
        onInput={(e) => props.onInput(e.currentTarget.value)}
        placeholder={props.placeholder}
        required={props.required}
      />
    </FormGroup>
  )
}

interface CheckboxFieldProps {
  id: string
  label: string
  checked: boolean
  onChange: (v: boolean) => void
  hint?: string
  danger?: boolean
}

export function CheckboxField(props: CheckboxFieldProps) {
  return (
    <div class={`form-group${props.danger ? ' form-group--danger' : ''}`}>
      <label class="checkbox-label">
        <input
          id={props.id}
          type="checkbox"
          checked={props.checked}
          onChange={(e) => props.onChange(e.currentTarget.checked)}
        />
        <span>{props.label}</span>
      </label>
      {props.hint && (
        <p class={`form-hint${props.danger ? ' form-hint--danger' : ''}`}>{props.hint}</p>
      )}
    </div>
  )
}
