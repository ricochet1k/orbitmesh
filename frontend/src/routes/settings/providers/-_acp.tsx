/**
 * ACP (generic agent) provider fields.
 *
 * ACP providers need a command to launch the agent binary and optional
 * space-separated args.  API authentication is handled by the agent process
 * itself; pass secrets via environment variables if needed.
 */
import type { SetStoreFunction } from 'solid-js/store'
import { TextField } from './-_shared'
import type { ProviderEdit } from './-_shared'

interface Props {
  provider: ProviderEdit
  setProvider: SetStoreFunction<ProviderEdit>
}

export function AcpFields(props: Props) {
  const acp_command = () => String(props.provider.scratch['acp_command'] ?? '')
  const acp_args    = () => String(props.provider.scratch['acp_args'] ?? '')

  return (
    <>
      <TextField
        id="acp-command"
        label="ACP Command"
        value={acp_command()}
        onInput={(v) => props.setProvider('scratch', 'acp_command', v)}
        placeholder="e.g. gemini"
        hint="The executable to launch (ACP-compatible agent binary)."
        required
      />
      <TextField
        id="acp-args"
        label="ACP Arguments"
        value={acp_args()}
        onInput={(v) => props.setProvider('scratch', 'acp_args', v)}
        placeholder="e.g. --experimental-acp"
        hint="Space-separated arguments passed to the ACP command."
      />
    </>
  )
}
