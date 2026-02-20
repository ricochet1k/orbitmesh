/**
 * PTY provider fields.
 *
 * The PTY provider requires only a `command` (the executable + args to spawn
 * as a pseudo-terminal).  No API key, no model, no special auth.
 */
import type { SetStoreFunction } from 'solid-js/store'
import { TextField } from './-_shared'
import type { ProviderEdit } from './-_shared'

interface Props {
  provider: ProviderEdit
  setProvider: SetStoreFunction<ProviderEdit>
}

export function PtyFields(props: Props) {
  return (
    <TextField
      id="provider-command"
      label="Command"
      value={props.provider.command}
      onInput={(v) => props.setProvider('command', v)}
      placeholder="e.g. claude"
      hint="Space-separated command and arguments (e.g. claude --verbose)"
      required
    />
  )
}
