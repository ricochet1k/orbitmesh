import { For, createRoot, createSignal, onCleanup, onMount } from "solid-js"

type ProbeOptions = {
  animationNames?: string[]
  remountThresholdMs?: number
}

type ProbeRuntime = {
  element: HTMLElement
  options: Required<ProbeOptions>
  animationCounts: Map<string, number>
}

type RecentUnmount = {
  when: number
  routePath: string
}

type ActiveWindow = {
  source: string
  startAt: number
  endAt: number
}

type UiStabilityAnomaly = {
  kind: "probe_remount" | "animation_replay" | "debug_trace"
  probe_id: string
  route_path: string
  source: string
  message: string
  animation_name?: string
  metadata?: Record<string, any>
  detected_at: string
}

type ToastMessage = {
  id: number
  text: string
}

const DEFAULT_OPTIONS: Required<ProbeOptions> = {
  animationNames: ["rise", "fadeIn"],
  remountThresholdMs: 1500,
}

const probeRegistry = new Map<string, ProbeRuntime>()
const recentUnmounts = new Map<string, RecentUnmount>()
const dedupeTimestamps = new Map<string, number>()

let listenersInstalled = false
let fetchPatched = false
let nativeFetch: typeof fetch | null = null
let activeWindow: ActiveWindow | null = null
let toastCounter = 0
let panelObserverInstalled = false

const recentPanelRemovals = new Map<string, RecentUnmount>()
const UI_TRACE_STORAGE_KEY = "orbitmesh.uiTrace"

const [toastMessages, setToastMessages] = createRoot(() => createSignal<ToastMessage[]>([]))

const nowMs = () => Date.now()

const currentRoutePath = () => {
  if (typeof window === "undefined") return "unknown"
  return `${window.location.pathname}${window.location.search}`
}

const isUiTraceEnabled = () => {
  if (typeof window === "undefined") return false
  try {
    return window.localStorage.getItem(UI_TRACE_STORAGE_KEY) === "1"
  } catch {
    return false
  }
}

const readCookie = (name: string) => {
  if (typeof document === "undefined") return ""
  const parts = document.cookie.split(";").map((part) => part.trim())
  const hit = parts.find((part) => part.startsWith(`${name}=`))
  if (!hit) return ""
  return decodeURIComponent(hit.split("=")[1] || "")
}

const describeElement = (target: EventTarget | null) => {
  if (!(target instanceof Element)) return "unknown"
  const id = target.id ? `#${target.id}` : ""
  const className =
    target.className && typeof target.className === "string"
      ? `.${target.className.trim().replace(/\s+/g, ".")}`
      : ""
  return `${target.tagName.toLowerCase()}${id}${className}`
}

const panelSignature = (element: Element) => {
  const explicit = element.getAttribute("data-ui-stability-id")
  if (explicit) return explicit
  const classNames = element.className
  const classes = typeof classNames === "string" ? classNames.split(/\s+/).filter(Boolean) : []
  const stableClasses = classes.filter((name) =>
    ["ds-panel", "placeholder-panel", "session-panel", "dashboard-panel"].includes(name),
  )
  if (stableClasses.length > 0) {
    return `${element.tagName.toLowerCase()}.${stableClasses.join(".")}`
  }
  return `${element.tagName.toLowerCase()}.panel`
}

const emitPanelRemount = (signature: string, previous: RecentUnmount) => {
  const routePath = currentRoutePath()
  const deltaMs = nowMs() - previous.when
  if (previous.routePath !== routePath || deltaMs > 1500) {
    return
  }

  emitAnomaly({
    kind: "probe_remount",
    probe_id: `auto:${signature}`,
    route_path: routePath,
    source: currentWindowSource(),
    message: `panel "${signature}" remounted in ${deltaMs}ms on the same route`,
    metadata: { remount_ms: deltaMs, auto_detected: true },
    detected_at: new Date().toISOString(),
  })
}

const trackPanelNode = (node: Node, removed: boolean) => {
  if (!(node instanceof Element)) return
  const panelNodes = node.matches(".ds-panel")
    ? [node, ...Array.from(node.querySelectorAll(".ds-panel"))]
    : Array.from(node.querySelectorAll(".ds-panel"))
  if (panelNodes.length === 0) return

  const routePath = currentRoutePath()
  const timestamp = nowMs()
  for (const panel of panelNodes) {
    const signature = panelSignature(panel)
    if (removed) {
      recentPanelRemovals.set(signature, { when: timestamp, routePath })
      continue
    }
    const previous = recentPanelRemovals.get(signature)
    if (previous) {
      emitPanelRemount(signature, previous)
    }
  }
}

const openWindow = (source: string, ttlMs: number) => {
  const startAt = nowMs()
  activeWindow = { source, startAt, endAt: startAt + ttlMs }
}

const refreshWindow = (source: string, ttlMs: number) => {
  const current = activeWindow
  const now = nowMs()
  if (!current || now > current.endAt) {
    openWindow(source, ttlMs)
    return
  }
  current.source = source
  current.endAt = Math.max(current.endAt, now + ttlMs)
}

const currentWindowSource = () => {
  const current = activeWindow
  if (!current) return "passive"
  if (nowMs() > current.endAt) {
    activeWindow = null
    return "passive"
  }
  return current.source
}

const pushToast = (text: string) => {
  const id = ++toastCounter
  setToastMessages((current) => [...current, { id, text }].slice(-4))
  window.setTimeout(() => {
    setToastMessages((current) => current.filter((item) => item.id !== id))
  }, 7000)
}

const shouldEmit = (key: string) => {
  const now = nowMs()
  const previous = dedupeTimestamps.get(key)
  if (previous !== undefined && now - previous < 10_000) {
    return false
  }
  dedupeTimestamps.set(key, now)
  return true
}

const sendAnomaly = async (payload: UiStabilityAnomaly) => {
  const token = readCookie("orbitmesh-csrf-token")
  try {
    await fetch("/api/v1/frontend/anomalies", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        ...(token ? { "X-CSRF-Token": token } : {}),
      },
      body: JSON.stringify(payload),
      keepalive: true,
    })
  } catch {
    // Best-effort telemetry: failures should not impact UX.
  }
}

const emitAnomaly = (payload: UiStabilityAnomaly) => {
  const dedupeKey = [
    payload.kind,
    payload.probe_id,
    payload.route_path,
    payload.animation_name ?? "",
    payload.source,
  ].join("|")
  if (!shouldEmit(dedupeKey)) return

  if (payload.source !== "passive") {
    pushToast(`UI stability warning: ${payload.message}`)
  }
  console.warn("[ui-stability]", payload)
  void sendAnomaly(payload)
}

const reportRemount = (probeId: string, previous: RecentUnmount, options: Required<ProbeOptions>) => {
  const routePath = currentRoutePath()
  const deltaMs = nowMs() - previous.when
  if (previous.routePath !== routePath || deltaMs > options.remountThresholdMs) {
    return
  }

  emitAnomaly({
    kind: "probe_remount",
    probe_id: probeId,
    route_path: routePath,
    source: currentWindowSource(),
    message: `probe "${probeId}" remounted in ${deltaMs}ms on the same route`,
    metadata: {
      remount_ms: deltaMs,
      threshold_ms: options.remountThresholdMs,
    },
    detected_at: new Date().toISOString(),
  })
}

const registerProbe = (probeId: string, element: HTMLElement, options: ProbeOptions = {}) => {
  const merged: Required<ProbeOptions> = {
    animationNames: options.animationNames ?? DEFAULT_OPTIONS.animationNames,
    remountThresholdMs: options.remountThresholdMs ?? DEFAULT_OPTIONS.remountThresholdMs,
  }
  const previous = recentUnmounts.get(probeId)
  if (previous) {
    reportRemount(probeId, previous, merged)
  }

  probeRegistry.set(probeId, {
    element,
    options: merged,
    animationCounts: new Map<string, number>(),
  })

  return () => {
    const entry = probeRegistry.get(probeId)
    if (entry?.element === element) {
      probeRegistry.delete(probeId)
      recentUnmounts.set(probeId, { when: nowMs(), routePath: currentRoutePath() })
    }
  }
}

const installFetchMonitor = () => {
  if (fetchPatched || typeof window === "undefined" || typeof window.fetch !== "function") {
    return
  }
  fetchPatched = true
  nativeFetch = window.fetch.bind(window)
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = input instanceof Request ? input : null
    const method = (init?.method ?? request?.method ?? "GET").toUpperCase()
    const url = String(request?.url ?? input)
    const isApi = url.includes("/api/")
    const shouldTrack = isApi && method !== "GET" && method !== "HEAD" && method !== "OPTIONS"
    if (shouldTrack) {
      refreshWindow(`fetch:${method}:${url}`, 2500)
    }

    try {
      return await (nativeFetch as typeof fetch)(input, init)
    } finally {
      if (shouldTrack) {
        refreshWindow(`fetch:${method}:${url}`, 1200)
      }
    }
  }
}

const handleAnimationStart = (event: AnimationEvent) => {
  const target = event.target
  if (!(target instanceof Element)) return
  const routePath = currentRoutePath()

  probeRegistry.forEach((probe, probeId) => {
    if (!probe.element.contains(target) && probe.element !== target) return
    if (!probe.options.animationNames.includes(event.animationName)) return

    const key = `${routePath}|${event.animationName}`
    const count = (probe.animationCounts.get(key) ?? 0) + 1
    probe.animationCounts.set(key, count)
    if (count <= 1) return

    emitAnomaly({
      kind: "animation_replay",
      probe_id: probeId,
      route_path: routePath,
      source: currentWindowSource(),
      animation_name: event.animationName,
      message: `animation "${event.animationName}" replayed ${count}x for probe "${probeId}"`,
      metadata: {
        count,
      },
      detected_at: new Date().toISOString(),
    })
  })
}

const installInteractionListeners = () => {
  if (listenersInstalled || typeof document === "undefined") {
    return
  }
  listenersInstalled = true

  document.addEventListener(
    "click",
    (event) => {
      const source = describeElement(event.target)
      refreshWindow(`click:${source}`, 1200)
    },
    true,
  )

  document.addEventListener(
    "submit",
    (event) => {
      const source = describeElement(event.target)
      refreshWindow(`submit:${source}`, 1800)
    },
    true,
  )

  document.addEventListener("animationstart", handleAnimationStart, true)
}

const installPanelObserver = () => {
  if (panelObserverInstalled || typeof document === "undefined" || !document.documentElement) {
    return
  }
  panelObserverInstalled = true

  const observer = new MutationObserver((records) => {
    for (const record of records) {
      record.removedNodes.forEach((node) => trackPanelNode(node, true))
      record.addedNodes.forEach((node) => trackPanelNode(node, false))
    }
  })
  observer.observe(document.documentElement, { childList: true, subtree: true })
}

const installUiStabilityRuntime = () => {
  installFetchMonitor()
  installInteractionListeners()
  installPanelObserver()
}

export function useUiStabilityProbe(probeId: string, options: ProbeOptions = {}) {
  let detach: (() => void) | null = null

  const setProbeRef = (element: HTMLElement | undefined) => {
    detach?.()
    detach = null
    if (!element) return
    detach = registerProbe(probeId, element, options)
  }

  onCleanup(() => {
    detach?.()
    detach = null
  })

  return setProbeRef
}

export function UiStabilityRuntime() {
  onMount(() => {
    installUiStabilityRuntime()
  })

  return (
    <div class="ui-stability-toast-stack" aria-live="assertive" aria-atomic="false">
      <For each={toastMessages()}>
        {(toast) => <div class="ui-stability-toast">{toast.text}</div>}
      </For>
    </div>
  )
}

export function reportUiStabilityTrace(probeId: string, message: string, metadata?: Record<string, any>) {
  if (!isUiTraceEnabled()) return
  emitAnomaly({
    kind: "debug_trace",
    probe_id: probeId,
    route_path: currentRoutePath(),
    source: currentWindowSource(),
    message,
    metadata,
    detected_at: new Date().toISOString(),
  })
}
