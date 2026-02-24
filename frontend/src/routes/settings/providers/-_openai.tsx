/**
 * OpenAI provider fields.
 *
 * Authentication: OPENAI_API_KEY stored in `env`.
 * Model is stored in `custom.model` via the shared scratch mechanism.
 */
import type { SetStoreFunction } from 'solid-js/store'
import { TextField } from './-_shared'
import type { ProviderEdit } from './-_shared'

interface Props {
  provider: ProviderEdit
  setProvider: SetStoreFunction<ProviderEdit>
}

export function OpenAiFields(props: Props) {
  const apiKey  = () => String(props.provider.env?.['OPENAI_API_KEY'] ?? '')
  const model   = () => String(props.provider.scratch['model'] ?? '')
  const baseURL = () => String(props.provider.scratch['base_url'] ?? '')

  return (
    <>
      <TextField
        id="openai-api-key"
        label="OpenAI API Key"
        type="password"
        value={apiKey()}
        onInput={(v) =>
          props.setProvider('env', (env) => ({ ...env, OPENAI_API_KEY: v }))
        }
        placeholder="sk-..."
        hint="Stored as the OPENAI_API_KEY environment variable when sessions start."
      />

      <TextField
        id="openai-base-url"
        label="Base URL"
        value={baseURL()}
        onInput={(v) => props.setProvider('scratch', 'base_url', v)}
        placeholder="e.g. http://localhost:11434/v1"
        hint="Optional. Override to point at an OpenAI-compatible server (Ollama, vLLM, LM Studio, etc.). Leave blank to use the OpenAI API."
      />

      <TextField
        id="openai-model"
        label="Model"
        value={model()}
        onInput={(v) => props.setProvider('scratch', 'model', v)}
        placeholder="e.g. gpt-4o"
        hint="Optional model override. Leave blank to use gpt-4o."
      />
    </>
  )
}
