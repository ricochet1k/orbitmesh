import type { ProviderConfigResponse, ProviderUsageInsight, UsageStats } from "../types/api"

type RecordLike = Record<string, unknown>

export interface ModelDiscoverySummary {
  status: string
  supported: boolean
  reason?: string
  source?: string
}

export interface ProviderModelSummary {
  currentModel?: string
  availableModels: string[]
  discovery: ModelDiscoverySummary
}

export interface UsageLimitSummary {
  status: string
  supported: boolean
  reason?: string
  used?: number
  limit?: number
  remaining?: number
  resetAt?: string
}

function asRecord(value: unknown): RecordLike | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null
  return value as RecordLike
}

function asNumber(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) return value
  if (typeof value === "string") {
    const parsed = Number(value)
    if (Number.isFinite(parsed)) return parsed
  }
  return undefined
}

function asString(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined
  const trimmed = value.trim()
  return trimmed.length > 0 ? trimmed : undefined
}

function byScopeData(stats: UsageStats | undefined, scope: string): RecordLike | null {
  return asRecord(stats?.by_scope?.[scope]?.data)
}

export function findProviderInsight(
  provider: ProviderConfigResponse,
  insights: ProviderUsageInsight[],
): ProviderUsageInsight | undefined {
  const idKey = provider.id ? `id:${provider.id}` : ""
  if (idKey) {
    const direct = insights.find((insight) => insight.provider_key === idKey)
    if (direct) return direct
    const byId = insights.find((insight) => insight.provider_id === provider.id)
    if (byId) return byId
  }

  const typeKey = provider.type ? `type:${provider.type}` : ""
  if (typeKey) {
    const byTypeKey = insights.find((insight) => insight.provider_key === typeKey)
    if (byTypeKey) return byTypeKey
  }

  return insights.find((insight) => insight.provider_type === provider.type)
}

export function extractProviderModelSummary(insight?: ProviderUsageInsight): ProviderModelSummary {
  const modelsData = byScopeData(insight?.usage, "models") ?? byScopeData(insight?.usage, "provider")
  const currentModel = asString(modelsData?.["current_model"])
  const available = Array.isArray(modelsData?.["available_models"])
    ? modelsData?.["available_models"]
    : []

  const availableModels = available
    .map((entry) => {
      if (typeof entry === "string") return entry.trim()
      if (entry && typeof entry === "object") {
        const id = asString((entry as RecordLike)["id"])
        const label = asString((entry as RecordLike)["label"])
        return id ?? label ?? ""
      }
      return ""
    })
    .filter((model): model is string => model.length > 0)

  const discoveryNode = asRecord(modelsData?.["discovery"])
  const status = asString(discoveryNode?.["status"]) ?? "unknown"
  const supported = typeof discoveryNode?.["supported"] === "boolean"
    ? discoveryNode["supported"] as boolean
    : status === "ok" || status === "partial"

  return {
    currentModel,
    availableModels,
    discovery: {
      status,
      supported,
      reason: asString(discoveryNode?.["reason"]),
      source: asString(discoveryNode?.["source"]),
    },
  }
}

function chooseCapabilityNode(insight?: ProviderUsageInsight): RecordLike | null {
  const capabilities = byScopeData(insight?.usage, "capabilities")
  if (capabilities) {
    return (
      asRecord(capabilities["provider_usage_limits"]) ??
      asRecord(capabilities["account_quota_progress"]) ??
      asRecord(capabilities["usage_limits"]) ??
      capabilities
    )
  }

  for (const scope of ["account", "global", "provider"]) {
    const scoped = byScopeData(insight?.usage, scope)
    if (scoped) return scoped
  }

  return null
}

export function extractUsageLimitSummary(insight?: ProviderUsageInsight): UsageLimitSummary {
  const capability = chooseCapabilityNode(insight)
  if (!capability) {
    return {
      status: "unknown",
      supported: false,
      reason: "No usage limit data was reported by this provider.",
    }
  }

  const status = asString(capability["status"]) ?? "unknown"
  const supported = typeof capability["supported"] === "boolean"
    ? capability["supported"] as boolean
    : status === "ok" || status === "partial"

  const used = asNumber(capability["used"]) ?? asNumber(capability["usage"]) ?? asNumber(capability["consumed"])
  const limit = asNumber(capability["limit"]) ?? asNumber(capability["max"]) ?? asNumber(capability["quota"])
  const remaining = asNumber(capability["remaining"]) ?? asNumber(capability["left"]) ?? asNumber(capability["available"])
  const resetAt =
    asString(capability["reset_at"]) ??
    asString(capability["resets_at"]) ??
    asString(capability["next_reset_at"]) ??
    asString(capability["reset"])

  return {
    status,
    supported,
    reason: asString(capability["reason"]),
    used,
    limit,
    remaining,
    resetAt,
  }
}

export function modelOptionsFromInsight(insight?: ProviderUsageInsight): string[] {
  const modelSummary = extractProviderModelSummary(insight)
  const deduped = new Set<string>()
  if (modelSummary.currentModel) deduped.add(modelSummary.currentModel)
  modelSummary.availableModels.forEach((model) => deduped.add(model))
  return Array.from(deduped)
}
