import { describe, it, expect, beforeEach, vi } from "vitest";
import { createSignal, createResource } from "solid-js";
import { useSessionTranscript } from "./useSessionTranscript";
import type { SessionStatusResponse, PermissionsResponse } from "../types/api";

describe("useSessionTranscript", () => {
  let mockSession: SessionStatusResponse;
  let mockPermissions: PermissionsResponse;

  beforeEach(() => {
    mockSession = {
      id: "test-session",
      state: "running",
      provider_type: "pty",
      updated_at: new Date().toISOString(),
      output: null,
      error_message: null,
      current_task: null,
    };

    mockPermissions = {
      role: "developer",
      can_inspect_sessions: false,
      can_manage_roles: false,
      can_manage_templates: false,
      can_initiate_bulk_actions: false,
      requires_owner_approval_for_role_changes: false,
    };
  });

  it("appends error events as error type messages to transcript", async () => {
    const [sessionId] = createSignal("test-session");
    // Create resources that resolve immediately with mock data
    const [sessionResource] = createResource(
      () => "test-session",
      () => Promise.resolve(mockSession)
    );
    const [permissionsResource] = createResource(
      () => "perms",
      () => Promise.resolve(mockPermissions)
    );

    const {
      messages,
      handleEvent,
    } = useSessionTranscript({
      sessionId,
      session: sessionResource,
      permissions: permissionsResource,
      refetchSession: () => {},
      onStatusChange: () => {},
    });

    // Wait a tick for effects to run
    await new Promise(resolve => setTimeout(resolve, 0));

    const initialCount = messages().length;

    // Create a mock error event
    const errorEvent = new MessageEvent("message", {
      data: JSON.stringify({
        type: "error",
        timestamp: "2026-02-18T20:00:00Z",
        session_id: "test-session",
        data: {
          message: "Test error message",
        },
      }),
    });

    handleEvent("error", errorEvent);

    const msgs = messages();
    // Find the error message (may have initialization message)
    const errorMsg = msgs.find(m => m.content === "Test error message");
    expect(errorMsg).toBeDefined();
    expect(errorMsg?.type).toBe("error");
    expect(errorMsg?.timestamp).toBe("2026-02-18T20:00:00Z");
  });

  it("uses 'Unknown error' when error message is missing", async () => {
    const [sessionId] = createSignal("test-session");
    const [sessionResource] = createResource(
      () => "test-session",
      () => Promise.resolve(mockSession)
    );
    const [permissionsResource] = createResource(
      () => "perms",
      () => Promise.resolve(mockPermissions)
    );

    const { messages, handleEvent } = useSessionTranscript({
      sessionId,
      session: sessionResource,
      permissions: permissionsResource,
      refetchSession: () => {},
    });

    await new Promise(resolve => setTimeout(resolve, 0));

    const errorEvent = new MessageEvent("message", {
      data: JSON.stringify({
        type: "error",
        timestamp: "2026-02-18T20:00:00Z",
        session_id: "test-session",
        data: {},
      }),
    });

    handleEvent("error", errorEvent);

    const msgs = messages();
    const errorMsg = msgs.find(m => m.type === "error" && m.content === "Unknown error");
    expect(errorMsg).toBeDefined();
  });

  it("handles malformed error event gracefully", async () => {
    const [sessionId] = createSignal("test-session");
    const [sessionResource] = createResource(
      () => "test-session",
      () => Promise.resolve(mockSession)
    );
    const [permissionsResource] = createResource(
      () => "perms",
      () => Promise.resolve(mockPermissions)
    );

    const { messages, handleEvent } = useSessionTranscript({
      sessionId,
      session: sessionResource,
      permissions: permissionsResource,
      refetchSession: () => {},
    });

    await new Promise(resolve => setTimeout(resolve, 0));

    const invalidEvent = new MessageEvent("message", {
      data: "not valid json",
    });

    handleEvent("message", invalidEvent);

    const msgs = messages();
    const parseError = msgs.find(m => m.content === "Failed to parse stream event payload.");
    expect(parseError).toBeDefined();
    expect(parseError?.type).toBe("error");
  });

  it("error messages are visually distinct with error type", async () => {
    const [sessionId] = createSignal("test-session");
    const [sessionResource] = createResource(
      () => "test-session",
      () => Promise.resolve(mockSession)
    );
    const [permissionsResource] = createResource(
      () => "perms",
      () => Promise.resolve(mockPermissions)
    );

    const { messages, handleEvent } = useSessionTranscript({
      sessionId,
      session: sessionResource,
      permissions: permissionsResource,
      refetchSession: () => {},
    });

    await new Promise(resolve => setTimeout(resolve, 0));

    const errorEvent = new MessageEvent("message", {
      data: JSON.stringify({
        type: "error",
        timestamp: "2026-02-18T20:00:00Z",
        session_id: "test-session",
        data: {
          message: "Provider connection failed",
        },
      }),
    });

    handleEvent("error", errorEvent);

    const msgs = messages();
    const errorMsg = msgs.find(m => m.content === "Provider connection failed");

    // Error messages should have type "error" so they render with distinct styling
    // CSS rule: .transcript-item.error { border-color: rgba(185, 28, 28, 0.25); }
    // CSS rule: .transcript-type-error { background: rgba(185,28,28,0.2); color: #b91c1c; }
    expect(errorMsg?.type).toBe("error");
  });
});
