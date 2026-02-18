import { render, screen, waitFor } from "@solidjs/testing-library";
import { describe, expect, it, vi } from "vitest";
import SessionComposer from "./SessionComposer";

describe("SessionComposer", () => {
  it("shows 'Send a message to start…' placeholder in idle state", async () => {
    const onSend = vi.fn();
    render(() => (
      <SessionComposer
        sessionState={() => "idle"}
        canSend={() => true}
        isRunning={() => false}
        pendingAction={() => null}
        onSend={onSend}
      />
    ));

    const input = screen.getByTestId("session-composer-input") as HTMLTextAreaElement;
    expect(input.placeholder).toBe("Send a message to start…");
  });

  it("shows 'Message queued until current run completes' placeholder in running state", async () => {
    const onSend = vi.fn();
    render(() => (
      <SessionComposer
        sessionState={() => "running"}
        canSend={() => false}
        isRunning={() => true}
        pendingAction={() => null}
        onSend={onSend}
      />
    ));

    const input = screen.getByTestId("session-composer-input") as HTMLTextAreaElement;
    expect(input.placeholder).toBe("Message queued until current run completes");
  });

  it("shows suspend-resolved placeholder in suspended state", async () => {
    const onSend = vi.fn();
    render(() => (
      <SessionComposer
        sessionState={() => "suspended"}
        canSend={() => true}
        isRunning={() => false}
        pendingAction={() => null}
        onSend={onSend}
      />
    ));

    const input = screen.getByTestId("session-composer-input") as HTMLTextAreaElement;
    expect(input.placeholder).toBe("Send a message (will be delivered after suspension resolves)…");
  });

  it("respects custom placeholder prop over state-based placeholder", async () => {
    const onSend = vi.fn();
    const customPlaceholder = "Custom placeholder text";
    render(() => (
      <SessionComposer
        sessionState={() => "idle"}
        canSend={() => true}
        isRunning={() => false}
        pendingAction={() => null}
        onSend={onSend}
        placeholder={customPlaceholder}
      />
    ));

    const input = screen.getByTestId("session-composer-input") as HTMLTextAreaElement;
    expect(input.placeholder).toBe(customPlaceholder);
  });

  it("calls onSend when message is entered and sent", async () => {
    const onSend = vi.fn();
    render(() => (
      <SessionComposer
        sessionState={() => "idle"}
        canSend={() => true}
        isRunning={() => false}
        pendingAction={() => null}
        onSend={onSend}
      />
    ));

    const input = screen.getByTestId("session-composer-input") as HTMLTextAreaElement;
    const sendButton = screen.getByTestId("session-composer-send") as HTMLButtonElement;

    // Set input value
    input.value = "test message";
    input.dispatchEvent(new Event("input", { bubbles: true }));

    // Send button should now be enabled
    expect(sendButton.disabled).toBe(false);

    // Click send button
    sendButton.click();

    expect(onSend).toHaveBeenCalledWith("test message", undefined);
  });

  it("disables send button when canSend is false", async () => {
    const onSend = vi.fn();
    render(() => (
      <SessionComposer
        sessionState={() => "running"}
        canSend={() => false}
        isRunning={() => true}
        pendingAction={() => null}
        onSend={onSend}
      />
    ));

    const sendButton = screen.getByTestId("session-composer-send") as HTMLButtonElement;
    expect(sendButton.disabled).toBe(true);
  });

  it("shows Interrupt button only when running and handler provided", () => {
    const onSend = vi.fn();
    const onInterrupt = vi.fn();

    render(() => (
      <SessionComposer
        sessionState={() => "running"}
        canSend={() => true}
        isRunning={() => true}
        pendingAction={() => null}
        onSend={onSend}
        onInterrupt={onInterrupt}
      />
    ));

    const interruptButton = screen.getByTestId("session-composer-interrupt");
    expect(interruptButton).toBeTruthy();
  });

  it("does not show Interrupt button when not running", async () => {
    const onSend = vi.fn();
    const onInterrupt = vi.fn();

    render(() => (
      <SessionComposer
        sessionState={() => "idle"}
        canSend={() => true}
        isRunning={() => false}
        pendingAction={() => null}
        onSend={onSend}
        onInterrupt={onInterrupt}
      />
    ));

    const interruptButton = screen.queryByTestId("session-composer-interrupt");
    expect(interruptButton).toBeNull();
  });



  it("disables controls when pendingAction is set", async () => {
    const onSend = vi.fn();
    render(() => (
      <SessionComposer
        sessionState={() => "idle"}
        canSend={() => true}
        isRunning={() => false}
        pendingAction={() => "send"}
        onSend={onSend}
      />
    ));

    const input = screen.getByTestId("session-composer-input") as HTMLTextAreaElement;
    const sendButton = screen.getByTestId("session-composer-send") as HTMLButtonElement;

    expect(input.disabled).toBe(true);
    expect(sendButton.disabled).toBe(true);
  });

  it("displays error message when provided", async () => {
    const onSend = vi.fn();
    render(() => (
      <SessionComposer
        sessionState={() => "idle"}
        canSend={() => true}
        isRunning={() => false}
        pendingAction={() => null}
        onSend={onSend}
        error={() => "Connection failed"}
      />
    ));

    const errorDiv = screen.getByText("Connection failed");
    expect(errorDiv).toBeTruthy();
  });

  it("shows provider selector when providers are available", async () => {
    const onSend = vi.fn();
    const mockProviders = [
      { id: "provider-1", name: "Claude", type: "anthropic", is_active: true },
      { id: "provider-2", name: "OpenAI", type: "openai", is_active: true },
    ];

    render(() => (
      <SessionComposer
        sessionState={() => "idle"}
        canSend={() => true}
        isRunning={() => false}
        pendingAction={() => null}
        onSend={onSend}
        providers={() => mockProviders}
      />
    ));

    const selector = screen.getByTestId("session-composer-provider-selector");
    expect(selector).toBeTruthy();

    const options = selector.querySelectorAll("option");
    expect(options.length).toBe(3); // Default + 2 providers
    expect(options[0].textContent).toBe("Default Provider");
    expect(options[1].textContent).toBe("Claude");
    expect(options[2].textContent).toBe("OpenAI");
  });

  it("includes selected provider ID in send payload when different from default", async () => {
    const onSend = vi.fn();
    const mockProviders = [
      { id: "provider-1", name: "Claude", type: "anthropic", is_active: true },
      { id: "provider-2", name: "OpenAI", type: "openai", is_active: true },
    ];

    render(() => (
      <SessionComposer
        sessionState={() => "idle"}
        canSend={() => true}
        isRunning={() => false}
        pendingAction={() => null}
        onSend={onSend}
        providers={() => mockProviders}
        defaultProviderId="provider-1"
      />
    ));

    const input = screen.getByTestId("session-composer-input") as HTMLTextAreaElement;
    const selector = screen.getByTestId("session-composer-provider-selector") as HTMLSelectElement;
    const sendButton = screen.getByTestId("session-composer-send") as HTMLButtonElement;

    // Set input value
    input.value = "test message";
    input.dispatchEvent(new Event("input", { bubbles: true }));

    // Select different provider
    selector.value = "provider-2";
    selector.dispatchEvent(new Event("change", { bubbles: true }));

    // Click send button
    sendButton.click();

    await waitFor(() => {
      expect(onSend).toHaveBeenCalledWith("test message", "provider-2");
    });
  });

  it("omits provider ID when selection matches default", async () => {
    const onSend = vi.fn();
    const mockProviders = [
      { id: "provider-1", name: "Claude", type: "anthropic", is_active: true },
    ];

    render(() => (
      <SessionComposer
        sessionState={() => "idle"}
        canSend={() => true}
        isRunning={() => false}
        pendingAction={() => null}
        onSend={onSend}
        providers={() => mockProviders}
        defaultProviderId="provider-1"
      />
    ));

    const input = screen.getByTestId("session-composer-input") as HTMLTextAreaElement;
    const sendButton = screen.getByTestId("session-composer-send") as HTMLButtonElement;

    // Set input value
    input.value = "test message";
    input.dispatchEvent(new Event("input", { bubbles: true }));

    // Send with default selection (should not include provider ID)
    sendButton.click();

    await waitFor(() => {
      expect(onSend).toHaveBeenCalledWith("test message", undefined);
    });
  });

  it("persists last-used provider to localStorage", async () => {
    const onSend = vi.fn();
    const mockProviders = [
      { id: "provider-1", name: "Claude", type: "anthropic", is_active: true },
      { id: "provider-2", name: "OpenAI", type: "openai", is_active: true },
    ];

    localStorage.clear();

    render(() => (
      <SessionComposer
        sessionState={() => "idle"}
        canSend={() => true}
        isRunning={() => false}
        pendingAction={() => null}
        onSend={onSend}
        providers={() => mockProviders}
      />
    ));

    const input = screen.getByTestId("session-composer-input") as HTMLTextAreaElement;
    const selector = screen.getByTestId("session-composer-provider-selector") as HTMLSelectElement;
    const sendButton = screen.getByTestId("session-composer-send") as HTMLButtonElement;

    // Select provider
    selector.value = "provider-2";
    selector.dispatchEvent(new Event("change", { bubbles: true }));

    // Set input and send
    input.value = "test message";
    input.dispatchEvent(new Event("input", { bubbles: true }));
    sendButton.click();

    await waitFor(() => {
      expect(localStorage.getItem("lastUsedProviderId")).toBe("provider-2");
    });
  });
});
