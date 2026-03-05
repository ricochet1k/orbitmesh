import { createRoot } from "solid-js"
import { describe, expect, it } from "vitest"
import { useSessionSendOptions } from "./useSessionSendOptions"

describe("useSessionSendOptions", () => {
  it("prefers provider insight models for defaults and options", async () => {
    let dispose!: () => void
    let hook!: ReturnType<typeof useSessionSendOptions>

    createRoot((d) => {
      dispose = d
      hook = useSessionSendOptions({
        session: () => ({
          id: "session-1",
          provider_type: "openai",
          preferred_provider_id: "prov-1",
          agent_id: "",
        }),
        providers: () => ([
          {
            id: "prov-1",
            name: "OpenAI",
            type: "openai",
            custom: { model: "gpt-4o" },
            is_active: true,
          },
        ]),
        agents: () => [],
        providerInsights: () => ([
          {
            provider_key: "id:prov-1",
            provider_id: "prov-1",
            provider_type: "openai",
            usage: {
              by_scope: {
                models: {
                  scope: "models",
                  data: {
                    current_model: "gpt-4.1",
                    available_models: [{ id: "gpt-4.1" }, { id: "gpt-4.1-mini" }],
                    discovery: { supported: true, status: "ok" },
                  },
                },
              },
            },
          },
        ]),
      })
    })

    await Promise.resolve()

    expect(hook.selectedModel()).toBe("gpt-4.1")
    expect(hook.modelOptions()).toEqual(["gpt-4.1", "gpt-4.1-mini", "gpt-4o"])

    dispose()
  })

  it("does not mix model defaults from other providers", async () => {
    let dispose!: () => void
    let hook!: ReturnType<typeof useSessionSendOptions>

    createRoot((d) => {
      dispose = d
      hook = useSessionSendOptions({
        session: () => ({
          id: "session-1",
          provider_type: "openai",
          preferred_provider_id: "prov-1",
          agent_id: "",
        }),
        providers: () => ([
          {
            id: "prov-1",
            name: "OpenAI Primary",
            type: "openai",
            custom: { model: "gpt-4o" },
            is_active: true,
          },
          {
            id: "prov-2",
            name: "OpenAI Secondary",
            type: "openai",
            custom: { model: "gpt-4.1-nano" },
            is_active: true,
          },
        ]),
        agents: () => [],
        providerInsights: () => ([
          {
            provider_key: "id:prov-1",
            provider_id: "prov-1",
            provider_type: "openai",
            usage: {
              by_scope: {
                models: {
                  scope: "models",
                  data: {
                    current_model: "gpt-4.1",
                    available_models: [{ id: "gpt-4.1" }, { id: "gpt-4.1-mini" }],
                    discovery: { supported: true, status: "ok" },
                  },
                },
              },
            },
          },
        ]),
      })
    })

    await Promise.resolve()

    expect(hook.modelOptions()).toEqual(["gpt-4.1", "gpt-4.1-mini", "gpt-4o"])
    expect(hook.modelOptions()).not.toContain("gpt-4.1-nano")

    dispose()
  })

  it("recomputes model defaults when provider changes without carrying stale model", async () => {
    let dispose!: () => void
    let hook!: ReturnType<typeof useSessionSendOptions>

    createRoot((d) => {
      dispose = d
      hook = useSessionSendOptions({
        session: () => ({
          id: "session-1",
          provider_type: "openai",
          preferred_provider_id: "prov-1",
          agent_id: "",
        }),
        providers: () => ([
          {
            id: "prov-1",
            name: "OpenAI Primary",
            type: "openai",
            custom: { model: "gpt-4o" },
            is_active: true,
          },
          {
            id: "prov-2",
            name: "OpenAI Secondary",
            type: "openai",
            custom: { model: "gpt-4.1-nano" },
            is_active: true,
          },
        ]),
        agents: () => [],
      })
    })

    await Promise.resolve()
    expect(hook.selectedModel()).toBe("gpt-4o")

    hook.setSelectedProviderId("prov-2")
    await Promise.resolve()

    expect(hook.selectedModel()).toBe("gpt-4.1-nano")
    expect(hook.modelOptions()).toEqual(["gpt-4.1-nano"])

    dispose()
  })

  it("switches to a known model when selected model is invalid for new provider", async () => {
    let dispose!: () => void
    let hook!: ReturnType<typeof useSessionSendOptions>

    createRoot((d) => {
      dispose = d
      hook = useSessionSendOptions({
        session: () => ({
          id: "session-1",
          provider_type: "openai",
          preferred_provider_id: "prov-1",
          agent_id: "",
        }),
        providers: () => ([
          {
            id: "prov-1",
            name: "OpenAI Primary",
            type: "openai",
            custom: {},
            is_active: true,
          },
          {
            id: "prov-2",
            name: "OpenAI Secondary",
            type: "openai",
            custom: {},
            is_active: true,
          },
        ]),
        agents: () => [],
        providerInsights: () => ([
          {
            provider_key: "id:prov-1",
            provider_id: "prov-1",
            provider_type: "openai",
            usage: {
              by_scope: {
                models: {
                  scope: "models",
                  data: {
                    current_model: "gpt-4.1",
                    available_models: [{ id: "gpt-4.1" }],
                    discovery: { supported: true, status: "ok" },
                  },
                },
              },
            },
          },
          {
            provider_key: "id:prov-2",
            provider_id: "prov-2",
            provider_type: "openai",
            usage: {
              by_scope: {
                models: {
                  scope: "models",
                  data: {
                    current_model: "gpt-4.1-mini",
                    available_models: [{ id: "gpt-4.1-mini" }, { id: "gpt-4.1-nano" }],
                    discovery: { supported: true, status: "ok" },
                  },
                },
              },
            },
          },
        ]),
      })
    })

    await Promise.resolve()
    hook.setSelectedModel("gpt-5")
    expect(hook.selectedModel()).toBe("gpt-5")

    hook.setSelectedProviderId("prov-2")
    await Promise.resolve()

    expect(hook.selectedModel()).toBe("gpt-4.1-mini")
    expect(hook.modelOptions()).toContain("gpt-4.1-mini")
    expect(hook.modelOptions()).not.toContain("gpt-5")

    dispose()
  })

  it("keeps freeform model override in send options", () => {
    let dispose!: () => void
    let hook!: ReturnType<typeof useSessionSendOptions>

    createRoot((d) => {
      dispose = d
      hook = useSessionSendOptions({
        session: () => ({
          id: "session-1",
          provider_type: "openai",
          preferred_provider_id: "prov-1",
          agent_id: "",
        }),
        providers: () => ([
          {
            id: "prov-1",
            name: "OpenAI",
            type: "openai",
            custom: {},
            is_active: true,
          },
        ]),
        agents: () => [],
      })
    })

    hook.setSelectedModel("my-custom-model")
    expect(hook.buildSendOptions()).toEqual({
      providerId: "prov-1",
      providerType: "openai",
      model: "my-custom-model",
    })

    dispose()
  })

  it("uses stream intel model hints when available", async () => {
    let dispose!: () => void
    let hook!: ReturnType<typeof useSessionSendOptions>

    createRoot((d) => {
      dispose = d
      hook = useSessionSendOptions({
        session: () => ({
          id: "session-1",
          provider_type: "claudews",
          preferred_provider_id: "prov-1",
          agent_id: "",
        }),
        providers: () => ([
          {
            id: "prov-1",
            name: "Claude",
            type: "claudews",
            custom: {},
            is_active: true,
          },
        ]),
        agents: () => [],
        sessionIntel: () => ({
          model: "claude-sonnet-4-5",
          permissionMode: "acceptEdits",
          tools: ["bash", "edit"],
          mcpServers: [],
        }),
      })
    })

    await Promise.resolve()

    expect(hook.selectedModel()).toBe("claude-sonnet-4-5")
    expect(hook.modelOptions()).toContain("claude-sonnet-4-5")
    expect(hook.sessionOptionHints()).toEqual({
      model: "claude-sonnet-4-5",
      permissionMode: "acceptEdits",
      tools: ["bash", "edit"],
    })

    dispose()
  })
})
