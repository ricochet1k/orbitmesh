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

    expect(onSend).toHaveBeenCalledWith("test message");
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
});
