import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { setAgentGuiI18nTestLocale } from "../../i18n/testUtils";
import { AgentSessionChrome } from "./AgentSessionChrome";
import type { AgentGUISessionChrome } from "./model/agentGuiNodeTypes";

describe("AgentSessionChrome", () => {
  beforeEach(() => {
    setAgentGuiI18nTestLocale("en");
  });

  it("renders auth, approval, and recovery sections", () => {
    const onSubmitApprovalOption = vi.fn();
    const onRetryActivation = vi.fn();

    render(
      <AgentSessionChrome
        source={{ chrome: chromeState(), isRespondingApproval: false }}
        onSubmitApprovalOption={onSubmitApprovalOption}
        onRetryActivation={onRetryActivation}
        onContinueInNewConversation={vi.fn()}
        labels={{
          approvalRequired: "Approval required",
          authRequired: "Authentication required",
          activatingSession: "Connecting session...",
          retryActivation: "Retry",
          continueInNewConversation: "Continue in new session"
        }}
      />
    );

    expect(screen.queryByText("Authentication required")).toBeNull();
    expect(
      screen.getByText("Please sign in to continue this session.")
    ).toBeTruthy();
    expect(screen.getByText("Approval required")).toBeTruthy();
    expect(
      screen.getByText("Waiting for permission to run the command")
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Yes, proceed" }));
    expect(onSubmitApprovalOption).toHaveBeenCalledWith({
      agentSessionId: "session-1",
      optionId: "allow_once",
      requestId: "request-1",
      turnId: "turn-1"
    });

    const retryButtons = screen.getAllByRole("button", { name: "Retry" });
    expect(retryButtons).toHaveLength(1);
    for (const retryButton of retryButtons) {
      expect(retryButton.className).toContain("h-7");
    }
    fireEvent.click(retryButtons[0]!);
    expect(onRetryActivation).toHaveBeenCalledTimes(1);
  });

  it("runs the auth login action from auth failures", () => {
    const onAuthLogin = vi.fn();
    render(
      <AgentSessionChrome
        source={{
          chrome: {
            auth: {
              message: "Please sign in to continue this session."
            },
            approval: null,
            recovery: null,
            rawState: null
          },
          isRespondingApproval: false
        }}
        onSubmitApprovalOption={vi.fn()}
        onAuthLogin={onAuthLogin}
        onRetryActivation={vi.fn()}
        onContinueInNewConversation={vi.fn()}
        labels={{
          approvalRequired: "Approval required",
          authLogin: "Sign in",
          authRequired: "Authentication required",
          activatingSession: "Connecting session...",
          retryActivation: "Retry",
          continueInNewConversation: "Continue in new conversation"
        }}
      />
    );

    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

    expect(onAuthLogin).toHaveBeenCalledTimes(1);
  });

  it("explains why approval actions are disabled on hover", async () => {
    const reason = "The shared Agent owner is offline";
    render(
      <AgentSessionChrome
        source={{
          approvalDisabledReason: reason,
          chrome: chromeState(),
          isRespondingApproval: true
        }}
        onSubmitApprovalOption={vi.fn()}
        onRetryActivation={vi.fn()}
        onContinueInNewConversation={vi.fn()}
        labels={{
          approvalRequired: "Approval required",
          authRequired: "Authentication required",
          activatingSession: "Connecting session...",
          retryActivation: "Retry",
          continueInNewConversation: "Continue in new session"
        }}
      />
    );

    const disabledGroup = screen.getByRole("group", { name: reason });
    expect(screen.getByRole("button", { name: "Yes, proceed" })).toBeDisabled();

    fireEvent.pointerMove(disabledGroup);

    expect(await screen.findByRole("tooltip")).toHaveTextContent(reason);
  });

  it("offers login without manual retry for authentication failures", () => {
    const onAuthLogin = vi.fn();
    const onRetryActivation = vi.fn();
    render(
      <AgentSessionChrome
        source={{
          chrome: {
            auth: {
              message: "Please sign in before starting a new conversation."
            },
            approval: null,
            recovery: null,
            rawState: null
          },
          isRespondingApproval: false
        }}
        onSubmitApprovalOption={vi.fn()}
        onAuthLogin={onAuthLogin}
        onRetryActivation={onRetryActivation}
        onContinueInNewConversation={vi.fn()}
        labels={{
          approvalRequired: "Approval required",
          authLogin: "Sign in",
          authRequired: "Authentication required",
          activatingSession: "Connecting session...",
          retryActivation: "Retry",
          continueInNewConversation: "Continue in new conversation"
        }}
      />
    );

    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(onAuthLogin).toHaveBeenCalledTimes(1);
    expect(onRetryActivation).not.toHaveBeenCalled();
  });

  it("shows full auth chrome messages with a native title without expandable layout state", () => {
    const onRetryActivation = vi.fn();
    const message =
      "Codex ACP requires authentication in the runtime VM. Sync the Codex host credentials, then start a new session.";

    render(
      <AgentSessionChrome
        source={{
          chrome: {
            auth: {
              message
            },
            approval: null,
            recovery: null,
            rawState: null
          },
          isRespondingApproval: false
        }}
        onSubmitApprovalOption={vi.fn()}
        onRetryActivation={onRetryActivation}
        onContinueInNewConversation={vi.fn()}
        labels={{
          approvalRequired: "Approval required",
          authRequired: "Authentication required",
          activatingSession: "Connecting session...",
          retryActivation: "Retry",
          continueInNewConversation: "Continue in new session"
        }}
      />
    );

    const messageElement = screen.getByText(message);
    const warningChrome = messageElement.closest("section");
    expect(warningChrome).not.toBeNull();
    expect(warningChrome).not.toHaveAttribute("data-expandable");
    expect(warningChrome).not.toHaveAttribute("data-expanded");
    expect(messageElement).toHaveAttribute("title", message);
    expect(
      screen.queryByTestId("agent-session-chrome-auth-expand-cue")
    ).toBeNull();

    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
    expect(onRetryActivation).not.toHaveBeenCalled();
    expect(warningChrome).not.toHaveAttribute("data-expanded");
  });

  it("renders a continue-in-new-conversation action for non-local recovery failures", () => {
    const onContinueInNewConversation = vi.fn();

    const { container } = render(
      <AgentSessionChrome
        source={{
          chrome: {
            auth: null,
            approval: null,
            recovery: {
              kind: "resume-unavailable",
              message: "This session is not recoverable on this machine.",
              followupAction: "continue-in-new-conversation"
            },
            rawState: null
          },
          isRespondingApproval: false
        }}
        onSubmitApprovalOption={vi.fn()}
        onRetryActivation={vi.fn()}
        onContinueInNewConversation={onContinueInNewConversation}
        labels={{
          approvalRequired: "Approval required",
          authRequired: "Authentication required",
          activatingSession: "Connecting session...",
          retryActivation: "Retry",
          continueInNewConversation: "Continue in new session"
        }}
      />
    );

    const continueButton = screen.getByRole("button", {
      name: "Continue in new session"
    });
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
    expect(
      container.querySelector(".agent-gui-chrome__message-slot")
    ).toBeTruthy();
    expect(
      continueButton.closest(".agent-gui-chrome__inline-actions")
    ).toBeTruthy();
    expect(continueButton.closest(".agent-gui-chrome__card")).toHaveAttribute(
      "data-has-inline-actions",
      "true"
    );
    expect(continueButton).toHaveAttribute("data-slot", "button");
    expect(continueButton).toHaveAttribute("data-variant", "ghost");
    expect(continueButton).toHaveAttribute("data-size", "sm");
    expect(continueButton.className).toContain(
      "agent-gui-chrome__success-ghost-button"
    );
    expect(
      continueButton.closest(".agent-gui-chrome__card")?.className
    ).toContain("agent-gui-chrome__card--success");
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByRole("status")).toBe(
      continueButton.closest(".agent-gui-chrome__card")
    );

    fireEvent.click(continueButton);

    expect(onContinueInNewConversation).toHaveBeenCalledTimes(1);
  });

  it("renders revoked sharing as a terminal notice without a retry action", () => {
    render(
      <AgentSessionChrome
        source={{
          chrome: {
            auth: null,
            approval: null,
            recovery: {
              kind: "agent-sharing-revoked",
              message: "riceballmama stopped sharing this agent",
              canRetry: false
            },
            rawState: null
          },
          isRespondingApproval: false
        }}
        onSubmitApprovalOption={vi.fn()}
        onRetryActivation={vi.fn()}
        onContinueInNewConversation={vi.fn()}
        labels={{
          approvalRequired: "Approval required",
          authRequired: "Authentication required",
          activatingSession: "Connecting session...",
          retryActivation: "Retry",
          continueInNewConversation: "Continue in new session"
        }}
      />
    );

    expect(
      screen.getByText("riceballmama stopped sharing this agent")
    ).toBeTruthy();
    expect(screen.getByRole("alert")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  });
});

function chromeState(): AgentGUISessionChrome {
  return {
    auth: {
      message: "Please sign in to continue this session."
    },
    approval: {
      kind: "approval",
      id: "approval:call-1",
      agentSessionId: "session-1",
      turnId: "turn-1",
      requestId: "request-1",
      callId: "call-1",
      title: "Waiting for permission to run the command",
      status: "waiting_approval",
      toolName: "Bash",
      input: null,
      options: [{ id: "allow_once", label: "Allow once", kind: "allow_once" }],
      output: null,
      occurredAtUnixMs: 1
    },
    recovery: {
      kind: "failed",
      message: "Connection dropped while restoring the session."
    },
    rawState: null
  };
}
