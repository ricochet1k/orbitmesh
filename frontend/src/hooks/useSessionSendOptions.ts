import { createEffect, createMemo, createSignal } from "solid-js"
import type { Accessor } from "solid-js"

import type {
  AgentConfigResponse,
  ProviderConfigResponse,
  ProviderUsageInsight,
  SessionResponse,
} from "../types/api"
import type { SessionStreamIntel } from "./useSessionData"
import { findProviderInsight, modelOptionsFromInsight, extractProviderModelSummary } from "../utils/providerInsights"

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
  providerInsights?: Accessor<ProviderUsageInsight[]>
  sessionIntel?: Accessor<SessionStreamIntel | null | undefined>
  includeDockUITools?: boolean
}) {
  const [selectedProviderId, setSelectedProviderId] = createSignal<string | null>(null)
  const [selectedAgentId, setSelectedAgentId] = createSignal<string | null>(null)
  const [selectedModel, setSelectedModel] = createSignal("")
  const [lastProviderId, setLastProviderId] = createSignal<string | null>(null)

  const [initialAgentId, setInitialAgentId] = createSignal<string | null>(null)
  const [modelDirty, setModelDirty] = createSignal(false)

  const providerList = createMemo(() => params.providers() ?? [])
  const agentList = createMemo(() => params.agents() ?? [])
  const providerInsights = createMemo(() => params.providerInsights?.() ?? [])
  const streamIntel = createMemo(() => params.sessionIntel?.() ?? null)

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

  const selectedProviderInsight = createMemo(() => {
    const provider = selectedProvider()
    if (!provider) return undefined
    return findProviderInsight(provider, providerInsights())
  })

  const selectedProviderInsightModel = createMemo(() => {
    return extractProviderModelSummary(selectedProviderInsight()).currentModel ?? ""
  })

  const streamIntelModel = createMemo(() => streamIntel()?.model?.trim() ?? "")

  const selectedProviderDefaultModel = createMemo(() => {
    const value = selectedProvider()?.custom?.["model"]
    return typeof value === "string" ? value : ""
  })

  const modelOptions = createMemo(() => {
    const set = new Set<string>()
    modelOptionsFromInsight(selectedProviderInsight()).forEach((model) => set.add(model))
    const fromStream = streamIntelModel()
    if (fromStream) set.add(fromStream)
    const fromAgent = selectedAgentDefaultModel().trim()
    if (fromAgent) set.add(fromAgent)
    const fromProvider = selectedProviderDefaultModel().trim()
    if (fromProvider) set.add(fromProvider)
    if (modelDirty() && selectedModel().trim()) set.add(selectedModel().trim())
    return Array.from(set).sort((a, b) => a.localeCompare(b))
  })

  const providerKnownModels = createMemo(() => {
    const set = new Set<string>()
    modelOptionsFromInsight(selectedProviderInsight()).forEach((model) => set.add(model))
    const fromProvider = selectedProviderDefaultModel().trim()
    if (fromProvider) set.add(fromProvider)
    return Array.from(set).sort((a, b) => a.localeCompare(b))
  })

  const reset = () => {
    setSelectedProviderId(null)
    setSelectedAgentId(null)
    setSelectedModel("")
    setLastProviderId(null)
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
    }
  })

  createEffect(() => {
    const providerId = selectedProvider()?.id ?? null
    if (!providerId) return

    const previousProviderId = lastProviderId()
    if (previousProviderId === null) {
      setLastProviderId(providerId)
      return
    }
    if (previousProviderId === providerId) return

    setLastProviderId(providerId)

    const current = selectedModel().trim()
    if (!current) return

    const known = providerKnownModels()
    if (known.length === 0) return
    if (known.includes(current)) return

    setModelDirty(false)
    setSelectedModel(known[0])
  })

  createEffect(() => {
    if (selectedAgentId() !== null) return
    const resolved = params.session()?.agent_id?.trim() || null
    if (resolved) setSelectedAgentId(resolved)
    setInitialAgentId(resolved)
  })

  createEffect(() => {
    if (modelDirty()) return
    const fromAgent = selectedAgentDefaultModel().trim()
    if (fromAgent) {
      setSelectedModel(fromAgent)
      return
    }
    const fromStream = streamIntelModel().trim()
    if (fromStream) {
      setSelectedModel(fromStream)
      return
    }
    const fromInsight = selectedProviderInsightModel().trim()
    if (fromInsight) {
      setSelectedModel(fromInsight)
      return
    }
    const fromProvider = selectedProviderDefaultModel().trim()
    if (fromProvider) {
      setSelectedModel(fromProvider)
      return
    }
    setSelectedModel("")
  })

  const updateModel = (value: string) => {
    setSelectedModel(value)
    setModelDirty(true)
  }

  const buildSendOptions = (): SendOptions | undefined => {
    const providerId = selectedProvider()?.id
    const providerType = selectedProvider()?.type
    const agentId = selectedAgentId() ?? undefined
    const changedAgentId = agentId && agentId !== initialAgentId() ? agentId : undefined
    const model = modelDirty() ? (selectedModel().trim() || undefined) : undefined

    const options: SendOptions = {}
    if (providerId) {
      options.providerId = providerId
      options.providerType = providerType
    }
    if (changedAgentId) options.agentId = changedAgentId
    if (model) options.model = model
    if (params.includeDockUITools) {
      options.allowedTools = [...DOCK_UI_ALLOWED_TOOLS]
    }

    return Object.keys(options).length > 0 ? options : undefined
  }

  const sessionOptionHints = createMemo(() => ({
    model: streamIntel()?.model?.trim() || undefined,
    permissionMode: streamIntel()?.permissionMode?.trim() || undefined,
    tools: streamIntel()?.tools ?? [],
  }))

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
    sessionOptionHints,
  }
}
