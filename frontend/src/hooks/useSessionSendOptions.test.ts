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
    expect(hook.buildSendOptions()).toEqual({ model: "my-custom-model" })

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
