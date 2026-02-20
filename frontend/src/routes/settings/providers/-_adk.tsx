/**
 * ADK (Gemini / Google AI) provider fields.
 *
 * Authentication:
 * - Default: GOOGLE_API_KEY environment variable (stored in `env`).
 * - Vertex AI: uses Google Cloud application default credentials; no API key
 *   needed, but project ID and location are required.
 *
 * The API key field is rendered here as part of the env block rather than as
 * a top-level `api_key` property.
 */
import { Show } from 'solid-js'
import type { SetStoreFunction } from 'solid-js/store'
import { TextField, CheckboxField } from './-_shared'
import type { ProviderEdit } from './-_shared'

interface Props {
  provider: ProviderEdit
  setProvider: SetStoreFunction<ProviderEdit>
}

export function AdkFields(props: Props) {
  const model          = () => String(props.provider.scratch['model'] ?? '')
  const apiKey         = () => String(props.provider.env?.['GOOGLE_API_KEY'] ?? '')
  const useVertexAI    = () => Boolean(props.provider.scratch['use_vertex_ai'])
  const vertexProject  = () => String(props.provider.scratch['vertex_project_id'] ?? '')
  const vertexLocation = () => String(props.provider.scratch['vertex_location'] ?? '')

  return (
    <>
      <TextField
        id="adk-model"
        label="Model"
        value={model()}
        onInput={(v) => props.setProvider('scratch', 'model', v)}
        placeholder="e.g. gemini-2.5-flash"
        hint="Optional model override. Leave blank to use the provider default (gemini-2.5-flash)."
      />

      <CheckboxField
        id="adk-use-vertex"
        label="Use Vertex AI (OAuth)"
        checked={useVertexAI()}
        onChange={(v) => props.setProvider('scratch', 'use_vertex_ai', v)}
        hint="Uses Google Cloud application default credentials instead of an API key."
      />

      <Show when={!useVertexAI()}>
        <TextField
          id="adk-api-key"
          label="Google API Key"
          type="password"
          value={apiKey()}
          onInput={(v) =>
            props.setProvider('env', (env) => ({ ...env, GOOGLE_API_KEY: v }))
          }
          placeholder="AIza..."
          hint="Stored as the GOOGLE_API_KEY environment variable when sessions start."
        />
      </Show>

      <Show when={useVertexAI()}>
        <TextField
          id="adk-vertex-project"
          label="Vertex Project ID"
          value={vertexProject()}
          onInput={(v) => props.setProvider('scratch', 'vertex_project_id', v)}
          placeholder="e.g. my-gcp-project"
        />
        <TextField
          id="adk-vertex-location"
          label="Vertex Location"
          value={vertexLocation()}
          onInput={(v) => props.setProvider('scratch', 'vertex_location', v)}
          placeholder="e.g. us-central1"
        />
      </Show>
    </>
  )
}
