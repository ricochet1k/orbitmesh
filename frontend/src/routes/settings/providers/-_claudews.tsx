/**
 * Claude (WebSocket SDK) provider fields.
 *
 * Extends the shared Claude fields with claude-ws-specific options:
 * max_turns and resume_session_id.
 */
import type { SetStoreFunction } from 'solid-js/store'
import { TextField } from './-_shared'
import type { ProviderEdit } from './-_shared'
import { ClaudeSharedFields } from './-_claude'

interface Props {
  provider: ProviderEdit
  setProvider: SetStoreFunction<ProviderEdit>
}

export function ClaudeWsFields(props: Props) {
  const maxTurns       = () => String(props.provider.scratch['max_turns'] ?? '')
  const resumeSession  = () => String(props.provider.scratch['resume_session_id'] ?? '')

  return (
    <>
      <ClaudeSharedFields {...props} />

      <TextField
        id="claudews-max-turns"
        label="Max Turns"
        type="number"
        value={maxTurns()}
        onInput={(v) => props.setProvider('scratch', 'max_turns', v)}
        placeholder="e.g. 20"
        hint="Maximum agentic turns per session. Leave blank for no limit."
      />

      <TextField
        id="claudews-resume-session"
        label="Resume Session ID"
        value={resumeSession()}
        onInput={(v) => props.setProvider('scratch', 'resume_session_id', v)}
        placeholder="e.g. abc123"
        hint="Resume a previous Claude session by its session ID."
      />
    </>
  )
}
