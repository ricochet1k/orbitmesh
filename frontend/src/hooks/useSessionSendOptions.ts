import { createEffect, createMemo, createSignal } from "solid-js"
import type { Accessor } from "solid-js"

import type {
  AgentConfigResponse,
  ProviderConfigResponse,
  SessionResponse,
} from "../types/api"

const DOCK_UI_ALLOWED_TOOLS = [
  "list_ui_components",
  "dispatch_ui_action",
  "multi_edit_ui",
] as const

type SendOptions = {
  providerId?: string
  providerType?: string
  agentId?: string
  model?: string
  allowedTools?: string[]
}

type SessionLike = Pick<SessionResponse, "id" | "provider_type" | "preferred_provider_id" | "agent_id">

export function useSessionSendOptions(params: {
  session: Accessor<SessionLike | null | undefined>
  providers: Accessor<ProviderConfigResponse[]>
  agents: Accessor<AgentConfigResponse[]>
  includeDockUITools?: boolean
}) {
  const [selectedProviderId, setSelectedProviderId] = createSignal<string | null>(null)
  const [selectedAgentId, setSelectedAgentId] = createSignal<string | null>(null)
  const [selectedModel, setSelectedModel] = createSignal("")

  const [initialProviderId, setInitialProviderId] = createSignal<string | null>(null)
  const [initialAgentId, setInitialAgentId] = createSignal<string | null>(null)
  const [modelDirty, setModelDirty] = createSignal(false)

  const providerList = createMemo(() => params.providers() ?? [])
  const agentList = createMemo(() => params.agents() ?? [])

  const selectedProvider = createMemo(() => {
    const providers = providerList()
    const providerId = selectedProviderId()
    if (!providerId) return providers[0] ?? null
    return providers.find((provider) => provider.id === providerId) ?? providers[0] ?? null
  })

  const selectedAgent = createMemo(() => {
    const agentId = selectedAgentId()
    if (!agentId) return null
    return agentList().find((agent) => agent.id === agentId) ?? null
  })

  const selectedAgentDefaultModel = createMemo(() => {
    const value = selectedAgent()?.custom?.["model"]
    return typeof value === "string" ? value : ""
  })

  const selectedProviderDefaultModel = createMemo(() => {
    const value = selectedProvider()?.custom?.["model"]
    return typeof value === "string" ? value : ""
  })

  const modelOptions = createMemo(() => {
    const set = new Set<string>()
    const fromAgent = selectedAgentDefaultModel().trim()
    if (fromAgent) set.add(fromAgent)
    const fromProvider = selectedProviderDefaultModel().trim()
    if (fromProvider) set.add(fromProvider)
    providerList().forEach((provider) => {
      const value = provider.custom?.["model"]
      if (typeof value === "string" && value.trim()) set.add(value.trim())
    })
    if (selectedModel().trim()) set.add(selectedModel().trim())
    return Array.from(set)
  })

  const reset = () => {
    setSelectedProviderId(null)
    setSelectedAgentId(null)
    setSelectedModel("")
    setInitialProviderId(null)
    setInitialAgentId(null)
    setModelDirty(false)
  }

  createEffect(() => {
    params.session()?.id
    reset()
  })

  createEffect(() => {
    if (selectedProviderId() !== null) return
    const sess = params.session()
    const providers = providerList()
    const preferred = sess?.preferred_provider_id?.trim()
    const type = sess?.provider_type
    const matchedByType = type
      ? providers.find((provider) => provider.type === type)?.id
      : undefined
    const first = providers[0]?.id
    const resolved = preferred || matchedByType || first || null
    if (resolved) {
      setSelectedProviderId(resolved)
      setInitialProviderId(resolved)
    }
  })

  createEffect(() => {
    if (selectedAgentId() !== null) return
    const resolved = params.session()?.agent_id?.trim() || null
    if (resolved) setSelectedAgentId(resolved)
    setInitialAgentId(resolved)
  })

  createEffect(() => {
    if (selectedModel().trim()) return
    const fromAgent = selectedAgentDefaultModel().trim()
    if (fromAgent) {
      setSelectedModel(fromAgent)
      return
    }
    const fromProvider = selectedProviderDefaultModel().trim()
    if (fromProvider) {
      setSelectedModel(fromProvider)
    }
  })

  const updateModel = (value: string) => {
    setSelectedModel(value)
    setModelDirty(true)
  }

  const buildSendOptions = (): SendOptions | undefined => {
    const providerId = selectedProvider()?.id
    const changedProviderId = providerId && providerId !== initialProviderId() ? providerId : undefined
    const agentId = selectedAgentId() ?? undefined
    const changedAgentId = agentId && agentId !== initialAgentId() ? agentId : undefined
    const model = modelDirty() ? (selectedModel().trim() || undefined) : undefined

    const options: SendOptions = {}
    if (changedProviderId) {
      options.providerId = changedProviderId
      options.providerType = selectedProvider()?.type
    }
    if (changedAgentId) options.agentId = changedAgentId
    if (model) options.model = model
    if (params.includeDockUITools) {
      options.allowedTools = [...DOCK_UI_ALLOWED_TOOLS]
    }

    return Object.keys(options).length > 0 ? options : undefined
  }

  return {
    providerList,
    agentList,
    selectedProvider,
    selectedProviderId,
    setSelectedProviderId,
    selectedAgentId,
    setSelectedAgentId,
    selectedModel,
    setSelectedModel: updateModel,
    modelOptions,
    buildSendOptions,
  }
}
