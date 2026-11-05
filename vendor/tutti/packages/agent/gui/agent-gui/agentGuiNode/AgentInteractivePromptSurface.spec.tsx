import {
  act,
  fireEvent,
  render,
  screen,
  waitFor
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { setAgentGuiI18nTestLocale } from "../../i18n/testUtils";
import { AgentInteractivePromptSurface } from "./AgentInteractivePromptSurface";

const labels = {
  approvalLead: "Waiting for your approval",
  fileChangeApprovalLead: "Codex requests authorization to edit files",
  planLead: "Ready to implement. How should permissions work?",
  planModes: [
    {
      id: "acceptEdits",
      label: "Accept edits",
      description: "Auto-approve file edits"
    },
    {
      id: "default",
      label: "Ask for approval",
      description: "Prompt before each tool"
    },
    {
      id: "bypassPermissions",
      label: "Allow all",
      description: "Do not prompt for tools"
    }
  ],
  stayInPlan: "Keep planning",
  sendFeedback: "Send feedback and keep planning",
  feedbackPlaceholder: "Give feedback to refine the plan...",
  previousQuestion: "Back",
  nextQuestion: "Next",
  submitAnswers: "Submit answers",
  answerPlaceholder: "Add details for the agent...",
  waitingForAnswer: "Waiting for your answer...",
  returnToConversation: "Back to conversation",
  continueAnswering: "Continue answering",
  planImplementationLead: "Implement this plan?",
  planImplementationConfirm: "Implement plan",
  planImplementationFeedbackPlaceholder: "Adjust the plan instead...",
  planImplementationSend: "Send adjustments",
  planImplementationSkip: "Stay in Plan Mode"
};

describe("AgentInteractivePromptSurface", () => {
  beforeEach(() => {
    setAgentGuiI18nTestLocale("en");
  });

  it("submits approval options from the shared prompt surface", () => {
    const onSubmit = vi.fn();
    const prompt = {
      kind: "approval" as const,
      id: "approval:request-approval",
      agentSessionId: "session-1",
      turnId: "turn-1",
      requestId: "request-approval",
      callId: "request-approval",
      title: "Run command",
      status: "waiting_approval" as const,
      toolName: "Bash",
      input: {
        command: "pnpm test --run renderer",
        description: "Verify the renderer parity fixes."
      },
      options: [
        {
          id: "allow_once",
          label: "Allow once",
          kind: "allow_once" as const,
          description: "Run this tool a single time."
        }
      ],
      output: null,
      occurredAtUnixMs: 1
    };
    render(
      <AgentInteractivePromptSurface
        prompt={prompt}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    expect(screen.getByText("Waiting for your approval")).toBeTruthy();
    expect(screen.getByText("pnpm test --run renderer")).toBeTruthy();
    expect(screen.getByText("Verify the renderer parity fixes.")).toBeTruthy();
    expect(screen.getByText("Run this tool a single time.")).toBeTruthy();
    expect(
      screen
        .getByText("pnpm test --run renderer")
        .closest(".agent-gui-conversation__interactive-option-display")
    ).toBeTruthy();
    expect(
      screen
        .getByText("pnpm test --run renderer")
        .closest(".agent-gui-conversation__interactive-option-button")
    ).toBeNull();
    expect(
      screen.getByTestId("agent-interaction-request-approval")
    ).toHaveAttribute("data-agent-interaction-kind", "approval");
    expect(
      screen.getByTestId("agent-approval-request-approval-approve")
    ).toHaveAttribute("data-agent-approval-option-id", "allow_once");

    fireEvent.click(
      screen.getByRole("button", {
        name: "Yes, proceed Run this tool a single time."
      })
    );
    expect(onSubmit).toHaveBeenCalledWith({
      agentSessionId: "session-1",
      requestId: "request-approval",
      optionId: "allow_once",
      turnId: "turn-1"
    });
  });

  it("shows the disabled reason when an approval control is hovered", async () => {
    const reason = "The shared Agent owner is offline";
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "approval",
          id: "approval:request-disabled",
          turnId: "turn-1",
          requestId: "request-disabled",
          callId: "request-disabled",
          title: "Run command",
          status: "waiting_approval",
          toolName: "Bash",
          input: { command: "printf offline" },
          options: [
            {
              id: "allow_once",
              label: "Allow once",
              kind: "allow_once"
            }
          ],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        isInteractionDisabled
        interactionDisabledReason={reason}
        onSubmit={vi.fn()}
        labels={labels}
      />
    );

    const disabledGroup = screen.getByRole("group", { name: reason });
    expect(screen.getByRole("button", { name: "Yes, proceed" })).toBeDisabled();
    expect(disabledGroup.firstElementChild).toHaveClass("pointer-events-none");

    fireEvent.pointerMove(disabledGroup);

    expect(await screen.findByRole("tooltip")).toHaveTextContent(reason);
  });

  it("shows approval reasons and file-change paths below the title", () => {
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "approval",
          id: "approval:request-file-change",
          turnId: "turn-1",
          requestId: "request-file-change",
          callId: "request-file-change",
          approvalPurpose: "edit-files",
          title: "Apply file changes",
          status: "waiting_approval",
          toolName: "Approval",
          input: {
            reason: "Allow these file changes?",
            grantRoot: "/workspace/session",
            changes: [
              { path: "/workspace/session/app.js" },
              { path: "/workspace/session/styles.css" }
            ]
          },
          options: [
            {
              id: "allow_once",
              label: "Allow once",
              kind: "allow_once"
            }
          ],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        onSubmit={vi.fn()}
        labels={labels}
      />
    );

    const lead = screen
      .getByText("Codex requests authorization to edit files")
      .closest(".agent-gui-conversation__interactive-prompt-lead-content");
    expect(lead).toHaveTextContent("Allow these file changes?");
    expect(lead).toHaveTextContent("/workspace/session");
    expect(screen.getAllByText("/workspace/session")).toHaveLength(1);
    expect(screen.queryByText("Summary")).toBeNull();
    expect(screen.queryByText("Path")).toBeNull();
    expect(screen.getByText("Files")).toBeTruthy();
    expect(screen.getByText("app.js, styles.css")).toBeTruthy();
  });

  it("does not infer an edit-files title from file details", () => {
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "approval",
          id: "approval:request-generic",
          turnId: "turn-1",
          requestId: "request-generic",
          callId: "request-generic",
          title: "Approval",
          status: "waiting_approval",
          toolName: "Approval",
          input: {
            changes: [{ path: "/workspace/session/app.js" }]
          },
          options: [{ id: "allow_once", label: "Allow", kind: "allow_once" }],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        onSubmit={vi.fn()}
        labels={labels}
      />
    );

    expect(screen.getByText("Waiting for your approval")).toBeTruthy();
    expect(
      screen.queryByText("Codex requests authorization to edit files")
    ).toBeNull();
  });

  it("clears approval option loading when external submission settles", async () => {
    const onSubmit = vi.fn();
    const prompt = {
      kind: "approval" as const,
      id: "approval:request-approval",
      turnId: "turn-1",
      requestId: "request-approval",
      callId: "request-approval",
      title: "Run command",
      status: "waiting_approval" as const,
      toolName: "Bash",
      input: null,
      options: [
        {
          id: "allow_once",
          label: "Allow once",
          kind: "allow_once" as const,
          description: "Run this tool a single time."
        }
      ],
      output: null,
      occurredAtUnixMs: 1
    };
    const { rerender } = render(
      <AgentInteractivePromptSurface
        prompt={prompt}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    fireEvent.click(
      screen.getByRole("button", {
        name: "Yes, proceed Run this tool a single time."
      })
    );
    expect(screen.getByTestId("agent-interactive-option-spinner")).toBeTruthy();

    rerender(
      <AgentInteractivePromptSurface
        prompt={prompt}
        isSubmitting={true}
        onSubmit={onSubmit}
        labels={labels}
      />
    );
    expect(screen.getByTestId("agent-interactive-option-spinner")).toBeTruthy();

    rerender(
      <AgentInteractivePromptSurface
        prompt={prompt}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    await waitFor(() => {
      expect(
        screen.queryByTestId("agent-interactive-option-spinner")
      ).toBeNull();
    });
    expect(
      screen.getByRole("button", {
        name: "Yes, proceed Run this tool a single time."
      })
    ).not.toBeDisabled();
  });

  it("submits abort approval options with optional feedback", () => {
    const onSubmit = vi.fn();
    setAgentGuiI18nTestLocale("zh-CN");
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "approval",
          id: "approval:request-approval",
          turnId: "turn-1",
          requestId: "request-approval",
          callId: "request-approval",
          title: "Run command",
          status: "waiting_approval",
          toolName: "Bash",
          input: null,
          options: [
            {
              id: "approved",
              label: "Yes, proceed",
              kind: "allow_once"
            },
            {
              id: "abort",
              label: "Deny and stop the turn",
              kind: "reject_always"
            }
          ],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    expect(
      screen.queryByPlaceholderText(labels.feedbackPlaceholder)
    ).toBeNull();
    expect(
      screen.getByTestId("agent-approval-request-approval-feedback")
    ).toHaveAttribute("data-agent-approval-option-id", "abort");
    fireEvent.click(
      screen.getByRole("button", {
        name: "拒绝，然后发送新的指令"
      })
    );
    expect(onSubmit).not.toHaveBeenCalled();
    expect(
      screen.queryByRole("button", {
        name: "拒绝，然后发送新的指令"
      })
    ).toBeNull();
    expect(screen.getByPlaceholderText(labels.feedbackPlaceholder)).toBe(
      document.activeElement
    );
    expect(
      screen
        .getByPlaceholderText(labels.feedbackPlaceholder)
        .closest(".agent-gui-conversation__interactive-feedback-composer")
        ?.tagName
    ).toBe("DIV");
    expect(
      screen
        .getByRole("button", { name: labels.sendFeedback })
        .closest(".agent-gui-conversation__interactive-feedback-composer")
    ).toBe(
      screen
        .getByPlaceholderText(labels.feedbackPlaceholder)
        .closest(".agent-gui-conversation__interactive-feedback-composer")
    );
    expect(
      screen.getByRole("button", { name: labels.sendFeedback })
    ).toBeDisabled();
    expect(
      screen.getByTestId("agent-approval-request-approval-feedback-input")
    ).toBe(screen.getByPlaceholderText(labels.feedbackPlaceholder));
    expect(
      screen.getByTestId("agent-approval-request-approval-feedback-submit")
    ).toBe(screen.getByRole("button", { name: labels.sendFeedback }));

    const feedback = screen.getByPlaceholderText(labels.feedbackPlaceholder);
    fireEvent.change(feedback, {
      target: { value: "Please split the work into smaller steps." }
    });
    fireEvent.keyDown(feedback, { key: "Enter", code: "Enter" });

    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "request-approval",
      action: "deny",
      optionId: "abort",
      payload: { denyMessage: "Please split the work into smaller steps." }
    });
  });

  it("turns Claude Code reject approval options into feedback composers", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "approval",
          id: "approval:request-approval",
          turnId: "turn-1",
          requestId: "request-approval",
          callId: "request-approval",
          title: "Run command",
          status: "waiting_approval",
          toolName: "Bash",
          input: null,
          options: [
            {
              id: "allow_always",
              label:
                "Always Allow Bash(curl -i --max-time 5 https://example.com)",
              kind: "allow_always"
            },
            {
              id: "allow",
              label: "Allow",
              kind: "allow_once"
            },
            {
              id: "reject",
              label: "Reject",
              kind: "reject_once"
            }
          ],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    fireEvent.click(
      screen.getByRole("button", { name: "No, then send new instructions" })
    );

    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Reject" })).toBeNull();
    expect(screen.getByPlaceholderText(labels.feedbackPlaceholder)).toBe(
      document.activeElement
    );

    fireEvent.change(screen.getByPlaceholderText(labels.feedbackPlaceholder), {
      target: { value: "Explain why this needs approval instead." }
    });
    fireEvent.click(screen.getByRole("button", { name: labels.sendFeedback }));

    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "request-approval",
      action: "deny",
      optionId: "reject",
      payload: { denyMessage: "Explain why this needs approval instead." }
    });
  });

  it("prefers a non-terminal reject option for feedback over abort", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "approval",
          id: "approval:request-approval",
          turnId: "turn-1",
          requestId: "request-approval",
          callId: "request-approval",
          title: "Run command",
          status: "waiting_approval",
          toolName: "Bash",
          input: null,
          options: [
            {
              id: "allow_once",
              label: "Allow once",
              kind: "allow_once"
            },
            {
              id: "reject",
              label: "No, continue without running",
              kind: "reject_once"
            },
            {
              id: "abort",
              label: "Deny and stop the turn",
              kind: "reject_always"
            }
          ],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    expect(
      screen.getByRole("button", { name: "No, then send new instructions" })
    ).toBeTruthy();
    expect(
      screen.getByTestId("agent-approval-request-approval-feedback")
    ).toHaveAttribute("data-agent-approval-option-id", "reject");
    expect(
      screen.getByTestId("agent-approval-request-approval-deny")
    ).toHaveAttribute("data-agent-approval-option-id", "abort");
    expect(
      screen.getByRole("button", {
        name: "Deny and stop the current response"
      })
    ).toBe(screen.getByTestId("agent-approval-request-approval-deny"));

    fireEvent.click(
      screen.getByRole("button", { name: "No, then send new instructions" })
    );
    fireEvent.change(screen.getByPlaceholderText(labels.feedbackPlaceholder), {
      target: { value: "Use another command" }
    });
    fireEvent.click(screen.getByRole("button", { name: labels.sendFeedback }));
    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "request-approval",
      action: "deny",
      optionId: "reject",
      payload: { denyMessage: "Use another command" }
    });
  });

  it("uses localized feedback labels instead of provider reject copy", () => {
    setAgentGuiI18nTestLocale("zh-CN");
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "approval",
          id: "approval:request-approval",
          turnId: "turn-1",
          requestId: "request-approval",
          callId: "request-approval",
          title: "Run command",
          status: "waiting_approval",
          toolName: "Bash",
          input: null,
          options: [
            {
              id: "reject",
              label: "Reject",
              kind: "reject_once"
            }
          ],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        onSubmit={vi.fn()}
        labels={labels}
      />
    );

    expect(
      screen.getByRole("button", { name: "拒绝，然后发送新的指令" })
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Reject" })).toBeNull();
  });

  it("does not infer feedback behavior from localized option labels", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "approval",
          id: "approval:request-approval",
          turnId: "turn-1",
          requestId: "request-approval",
          callId: "request-approval",
          title: "Run command",
          status: "waiting_approval",
          toolName: "Bash",
          input: null,
          options: [
            {
              id: "custom_choice",
              label: "No, and tell Codex what to do differently",
              kind: "custom"
            }
          ],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    fireEvent.click(
      screen.getByRole("button", {
        name: "No, and tell Codex what to do differently"
      })
    );

    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "request-approval",
      optionId: "custom_choice"
    });
    expect(
      screen.queryByPlaceholderText(labels.feedbackPlaceholder)
    ).toBeNull();
  });

  it("does not submit empty feedback with Enter", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "approval",
          id: "approval:request-approval",
          turnId: "turn-1",
          requestId: "request-approval",
          callId: "request-approval",
          title: "Run command",
          status: "waiting_approval",
          toolName: "Bash",
          input: null,
          options: [
            {
              id: "approved",
              label: "Yes, proceed",
              kind: "allow_once"
            },
            {
              id: "abort",
              label: "No, and tell Codex what to do differently",
              kind: "reject_once"
            }
          ],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    fireEvent.click(
      screen.getByRole("button", {
        name: "No, then send new instructions"
      })
    );
    const feedback = screen.getByPlaceholderText(labels.feedbackPlaceholder);
    feedback.focus();
    fireEvent.keyDown(feedback, {
      key: "Enter",
      code: "Enter"
    });

    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("hides approval shortcut hints and ignores shortcut submission when shortcuts are disabled", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        keyboardShortcuts={false}
        prompt={{
          kind: "approval",
          id: "approval:request-approval",
          turnId: "turn-1",
          requestId: "request-approval",
          callId: "request-approval",
          title: "Run command",
          status: "waiting_approval",
          toolName: "Bash",
          input: null,
          options: [
            {
              id: "approved",
              label: "Yes, proceed",
              kind: "allow_once"
            },
            {
              id: "abort",
              label: "No, and tell Codex what to do differently",
              kind: "reject_once"
            }
          ],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    expect(screen.queryByText("Enter")).toBeNull();
    expect(screen.queryByText(/Enter$/)).toBeNull();

    fireEvent.keyDown(window, {
      key: "Enter",
      code: "Enter"
    });

    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("renders approval lead copy without a trailing sentence period", () => {
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "approval",
          id: "approval:request-approval",
          turnId: "turn-1",
          requestId: "request-approval",
          callId: "request-approval",
          title: "Run command",
          status: "waiting_approval",
          toolName: "Bash",
          input: null,
          options: [],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        onSubmit={vi.fn()}
        labels={{ ...labels, approvalLead: "Codex needs your choice." }}
      />
    );

    expect(screen.getByText("Codex needs your choice")).toBeTruthy();
    expect(screen.queryByText("Codex needs your choice.")).toBeNull();
  });

  it("applies edge glow to the prompt card only when requested", () => {
    const prompt = {
      kind: "approval" as const,
      id: "approval:request-approval",
      turnId: "turn-1",
      requestId: "request-approval",
      callId: "request-approval",
      title: "Run command",
      status: "waiting_approval" as const,
      toolName: "Bash",
      input: null,
      options: [
        {
          id: "allow_once",
          label: "Allow once",
          kind: "allow_once" as const,
          description: ""
        }
      ],
      output: null,
      occurredAtUnixMs: 1
    };
    const { container, rerender } = render(
      <AgentInteractivePromptSurface
        prompt={prompt}
        isSubmitting={false}
        onSubmit={vi.fn()}
        labels={labels}
      />
    );

    expect(
      container.querySelector(
        ".agent-gui-conversation__interactive-prompt-card"
      )
    ).not.toHaveClass("agent-gui-edge-glow");

    rerender(
      <AgentInteractivePromptSurface
        prompt={prompt}
        edgeGlow={true}
        isSubmitting={false}
        onSubmit={vi.fn()}
        labels={labels}
      />
    );

    expect(
      container.querySelector(
        ".agent-gui-conversation__interactive-prompt-card"
      )
    ).toHaveClass("agent-gui-edge-glow");
  });

  it("shows command details from nested approval tool calls", () => {
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "approval",
          id: "approval:request-approval",
          turnId: "turn-1",
          requestId: "request-approval",
          callId: "request-approval",
          title: "Run command",
          status: "waiting_approval",
          toolName: "Approval",
          input: {
            requestId: "request-approval",
            toolCall: {
              input: {
                command: ["touch", "/workspace/project/approval-check.txt"],
                description: "Create a temporary approval check file."
              }
            }
          },
          options: [
            {
              id: "allow_once",
              label: "Yes",
              kind: "allow_once",
              description: ""
            }
          ],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        onSubmit={vi.fn()}
        labels={{ ...labels, approvalLead: "Codex needs your choice." }}
      />
    );

    expect(
      screen.getByText("touch /workspace/project/approval-check.txt")
    ).toBeTruthy();
    expect(
      screen.getByText("Create a temporary approval check file.")
    ).toBeTruthy();
    expect(screen.queryByText("requestId")).toBeNull();
  });

  it("clamps long command details and exposes the full command in a tooltip after a hover delay", async () => {
    vi.useFakeTimers();
    const longCommand = [
      "node - <<'NODE'",
      "const fs = require('fs');",
      "const content = `Desktop summary with many files, folders, and generated notes`;",
      "fs.writeFileSync('/workspace/readme.md', content.repeat(24));",
      "console.log('/workspace/readme.md');",
      "NODE"
    ].join(" ");

    try {
      const { container } = render(
        <AgentInteractivePromptSurface
          prompt={{
            kind: "approval",
            id: "approval:request-approval",
            turnId: "turn-1",
            requestId: "request-approval",
            callId: "request-approval",
            title: "Run command",
            status: "waiting_approval",
            toolName: "Bash",
            input: {
              command: longCommand
            },
            options: [
              {
                id: "allow_once",
                label: "Yes",
                kind: "allow_once",
                description: ""
              }
            ],
            output: null,
            occurredAtUnixMs: 1
          }}
          isSubmitting={false}
          onSubmit={vi.fn()}
          labels={{ ...labels, approvalLead: "Codex needs your choice." }}
        />
      );

      const commandDetail = container.querySelector(
        '[data-agent-interactive-command-detail="true"]'
      );
      expect(commandDetail).toHaveTextContent(longCommand);
      expect(commandDetail).toHaveClass(
        "agent-gui-conversation__interactive-option-command-description"
      );

      fireEvent.pointerMove(commandDetail as HTMLElement, {
        pointerType: "mouse"
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(999);
      });

      expect(screen.queryByRole("tooltip")).toBeNull();

      await act(async () => {
        await vi.advanceTimersByTimeAsync(1);
      });

      expect(screen.getByRole("tooltip")).toHaveTextContent(longCommand);
    } finally {
      vi.useRealTimers();
    }
  });

  it("renders structured details instead of approval titles", () => {
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "approval",
          id: "approval:request-approval",
          turnId: "turn-1",
          requestId: "4601155f-fdab-47b8-9b1a-a569e7a79df3",
          callId: "request-approval",
          title: "requestId: 4601155f-fdab-47b8-9b1a-a569e7a79df3",
          status: "waiting_approval",
          toolName: "Approval",
          input: {
            requestId: "4601155f-fdab-47b8-9b1a-a569e7a79df3",
            toolCall: {
              input: {
                command: "date"
              }
            }
          },
          options: [
            {
              id: "allow_once",
              label: "Yes",
              kind: "allow_once",
              description: ""
            }
          ],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        onSubmit={vi.fn()}
        labels={{ ...labels, approvalLead: "Codex needs your choice." }}
      />
    );

    expect(screen.getByText("Command")).toBeTruthy();
    expect(screen.getByText("date")).toBeTruthy();
    expect(screen.queryByText(/requestId:/)).toBeNull();
    expect(
      screen.queryByText(/4601155f-fdab-47b8-9b1a-a569e7a79df3/)
    ).toBeNull();
  });

  it("does not render approval titles without structured details", () => {
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "approval",
          id: "approval:2",
          turnId: "turn-1",
          requestId: "2",
          callId: "approval-2",
          title: "requestId: 2",
          status: "waiting_approval",
          toolName: "Approval",
          input: {},
          options: [
            {
              id: "allow_once",
              label: "Yes",
              kind: "allow_once",
              description: ""
            }
          ],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        onSubmit={vi.fn()}
        labels={{ ...labels, approvalLead: "Claude Code needs your choice." }}
      />
    );

    expect(screen.getByText("Claude Code needs your choice")).toBeTruthy();
    expect(screen.queryByText(/requestId:/)).toBeNull();
  });

  it("does not render approval metadata titles", () => {
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "approval",
          id: "approval:level-2",
          turnId: "turn-1",
          requestId: "level-2",
          callId: "approval-level-2",
          title: "level: 2",
          status: "waiting_approval",
          toolName: "Approval",
          input: {},
          options: [
            {
              id: "allow_once",
              label: "Yes",
              kind: "allow_once",
              description: ""
            }
          ],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        onSubmit={vi.fn()}
        labels={{ ...labels, approvalLead: "Claude Code needs your choice." }}
      />
    );

    expect(screen.getByText("Claude Code needs your choice")).toBeTruthy();
    expect(screen.queryByText(/level:/)).toBeNull();
  });

  it("uses structured approval details instead of repeating the prompt title", () => {
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "approval",
          id: "approval:request-approval",
          turnId: "turn-1",
          requestId: "request-approval",
          callId: "request-approval",
          title: "/workspace/project/index.html",
          status: "waiting_approval",
          toolName: "Read",
          input: {
            path: "/workspace/project/index.html"
          },
          options: [
            {
              id: "allow_once",
              label: "Yes",
              kind: "allow_once",
              description: ""
            }
          ],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        onSubmit={vi.fn()}
        labels={{ ...labels, approvalLead: "Codex needs your choice." }}
      />
    );

    expect(screen.getByText("/workspace/project/index.html")).toBeTruthy();
    expect(screen.getByText("Path")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Yes, proceed" })).toBeTruthy();
  });

  it("shows explicit MCP server, tool, and argument details for approval prompts", () => {
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "approval",
          id: "approval:request-approval",
          turnId: "turn-1",
          requestId: "request-approval",
          callId: "request-approval",
          title: "Approval",
          status: "waiting_approval",
          toolName: "Approval",
          input: {
            requestId: "request-approval",
            arguments: {
              url: "file:///workspace/index.html"
            },
            server: "playwright",
            tool: "browser_navigate",
            request: {
              _meta: {
                tool_params_display: [
                  {
                    display_name: "url",
                    name: "url",
                    value: "file:///workspace/index.html"
                  }
                ]
              },
              message:
                'Allow the playwright MCP server to run tool "browser_navigate"?'
            }
          },
          options: [
            {
              id: "allow_once",
              label: "Yes",
              kind: "allow_once",
              description: ""
            }
          ],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        onSubmit={vi.fn()}
        labels={{ ...labels, approvalLead: "Codex needs your choice." }}
      />
    );

    expect(screen.getByText("MCP")).toBeTruthy();
    expect(screen.getByText("playwright / browser_navigate")).toBeTruthy();
    expect(screen.getByText("url: file:///workspace/index.html")).toBeTruthy();
    expect(screen.queryByText("Navigate to a URL")).toBeNull();
  });

  it("localizes known approval option labels in the active UI language", () => {
    setAgentGuiI18nTestLocale("zh-CN");

    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "approval",
          id: "approval:request-approval",
          turnId: "turn-1",
          requestId: "request-approval",
          callId: "request-approval",
          title: "Run command",
          status: "waiting_approval",
          toolName: "Bash",
          input: null,
          options: [
            {
              id: "allow_once",
              label: "Approve",
              kind: "allow_once"
            },
            {
              id: "approved_for_session",
              label: "Approve for session",
              kind: "allow_always"
            },
            {
              id: "allow_always",
              label:
                "Yes, and don't ask again for commands that start with `curl -I https://example.com`",
              kind: "allow_always"
            },
            {
              id: "always_allow_bash",
              label: "Always Allow Bash(chmod +x ./bootstrap.sh)",
              kind: "allow_always"
            },
            {
              id: "bypassPermissions",
              label: "Yes, and bypass permissions",
              kind: "allow_always"
            },
            {
              id: "auto",
              label: 'Yes, and use "auto" mode',
              kind: "allow_always"
            },
            {
              id: "acceptEdits",
              label: "Yes, and auto-accept edits",
              kind: "allow_always"
            },
            {
              id: "default",
              label: "Yes, and manually approve edits",
              kind: "allow_once"
            },
            {
              id: "reject",
              label: "No, and tell Codex what to do differently",
              kind: "reject_once"
            },
            {
              id: "reject_always",
              label: "No, and don't ask again",
              kind: "reject_always"
            },
            {
              id: "custom",
              label: "Custom provider option",
              kind: "custom"
            }
          ],
          output: null,
          occurredAtUnixMs: 1
        }}
        isSubmitting={false}
        onSubmit={vi.fn()}
        labels={labels}
      />
    );

    expect(screen.getByRole("button", { name: "允许执行" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "本次会话允许" })).toBeTruthy();
    expect(
      screen.getByRole("button", {
        name: "允许，并且不再询问以 `curl -I https://example.com` 开头的命令"
      })
    ).toBeTruthy();
    expect(
      screen.getByText("允许，并且不再询问以下列内容开头的命令")
    ).toBeTruthy();
    expect(
      screen
        .getByText("curl -I https://example.com")
        .closest("[data-agent-interactive-command-prefix-option='true']")
    ).toBeTruthy();
    expect(
      screen.getByRole("button", {
        name: "始终允许 Bash(chmod +x ./bootstrap.sh)"
      })
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "允许，并绕过权限" })
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "允许，并使用自动模式" })
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "允许，并自动接受编辑" })
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "允许，并手动确认编辑" })
    ).toBeTruthy();
    expect(
      screen.getByRole("button", {
        name: "拒绝，然后发送新的指令"
      })
    ).toBeTruthy();
    expect(
      screen.getByRole("button", {
        name: "拒绝，并且不再询问"
      })
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Custom provider option" })
    ).toBeTruthy();
  });

  it("uses a single continue-planning action for exit-plan prompts", () => {
    const onSubmit = vi.fn();
    const { unmount } = render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "exit-plan",
          requestId: "request-plan",
          title: "Exit plan mode",
          options: []
        }}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    fireEvent.click(
      screen.getByRole("button", {
        name: "Accept edits Auto-approve file edits"
      })
    );
    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "request-plan",
      action: "allow",
      optionId: "acceptEdits"
    });

    unmount();

    const secondPrompt = render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "exit-plan",
          requestId: "request-plan-feedback",
          title: "Exit plan mode",
          options: []
        }}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );
    expect(
      screen.getByRole("button", { name: labels.stayInPlan })
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: labels.sendFeedback })
    ).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: labels.stayInPlan }));
    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "request-plan-feedback",
      action: "deny"
    });

    secondPrompt.unmount();

    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "exit-plan",
          requestId: "request-plan-feedback-2",
          title: "Exit plan mode",
          options: []
        }}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );
    fireEvent.change(screen.getByPlaceholderText(labels.feedbackPlaceholder), {
      target: { value: "Please split the work into smaller steps." }
    });
    expect(
      screen.queryByRole("button", { name: labels.stayInPlan })
    ).toBeNull();
    expect(
      screen.getByRole("button", { name: labels.sendFeedback })
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: labels.sendFeedback }));
    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "request-plan-feedback-2",
      action: "deny",
      payload: { denyMessage: "Please split the work into smaller steps." }
    });
  });

  it("hides the Back button for a single-question ask-user prompt", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "ask-user",
          requestId: "request-single",
          title: "One question",
          questions: [
            {
              id: "scope",
              header: "Scope",
              question: "Which scope should we use?",
              options: [
                {
                  id: "small",
                  label: "Small",
                  description: "Minimal change"
                },
                {
                  id: "large",
                  label: "Large",
                  description: "Broader cleanup"
                }
              ],
              multiSelect: true
            }
          ]
        }}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    // Only one question, so there is nowhere to go back to.
    expect(
      screen.queryByRole("button", { name: labels.previousQuestion })
    ).toBeNull();
    expect(
      screen.getByRole("button", { name: labels.submitAnswers })
    ).toBeTruthy();
  });

  it("returns to the conversation without submitting and restores the selected answer", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        prompt={{
          agentSessionId: "session-return-to-conversation",
          kind: "ask-user",
          requestId: "request-return-to-conversation",
          title: "One question",
          turnId: "turn-return-to-conversation",
          questions: [
            {
              id: "scope",
              header: "Scope",
              question: "Which scope should we use?",
              options: [
                {
                  id: "small",
                  label: "Small",
                  description: "Minimal change"
                }
              ],
              multiSelect: true
            }
          ]
        }}
        conversationReturn={{
          continueAnswering: labels.continueAnswering,
          returnToConversation: labels.returnToConversation
        }}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    fireEvent.click(screen.getByRole("button", { name: /Small/ }));
    expect(screen.getByRole("button", { name: /Small/ })).toHaveAttribute(
      "aria-pressed",
      "true"
    );

    fireEvent.click(
      screen.getByRole("button", { name: labels.returnToConversation })
    );

    expect(onSubmit).not.toHaveBeenCalled();
    expect(
      screen.getByTestId(
        "agent-question-request-return-to-conversation-collapsed"
      )
    ).toBeTruthy();
    expect(screen.queryByPlaceholderText(labels.answerPlaceholder)).toBeNull();

    fireEvent.click(
      screen.getByRole("button", { name: labels.continueAnswering })
    );

    expect(screen.getByRole("button", { name: /Small/ })).toHaveAttribute(
      "aria-pressed",
      "true"
    );

    fireEvent.click(screen.getByRole("button", { name: labels.submitAnswers }));
    expect(onSubmit).toHaveBeenCalledWith({
      agentSessionId: "session-return-to-conversation",
      requestId: "request-return-to-conversation",
      action: "submit",
      payload: {
        answers: ["Small"],
        answersByQuestionId: {
          scope: ["Small"]
        }
      },
      turnId: "turn-return-to-conversation"
    });
  });

  it("fails closed when conversation return lacks an exact interaction tuple", () => {
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "ask-user",
          requestId: "request-missing-tuple",
          title: "One question",
          questions: [
            {
              id: "scope",
              header: "Scope",
              question: "Which scope?",
              options: [{ label: "Small", description: "Focused change" }],
              multiSelect: false
            }
          ]
        }}
        conversationReturn={{
          continueAnswering: labels.continueAnswering,
          returnToConversation: labels.returnToConversation
        }}
        isSubmitting={false}
        onSubmit={vi.fn()}
        labels={labels}
      />
    );

    expect(
      screen.queryByRole("button", { name: labels.returnToConversation })
    ).toBeNull();
    expect(screen.getByPlaceholderText(labels.answerPlaceholder)).toBeTruthy();
  });

  it("fails closed when a fixed-choice prompt cannot be answered by Composer text", () => {
    render(
      <AgentInteractivePromptSurface
        prompt={{
          agentSessionId: "session-fixed-choice",
          kind: "ask-user",
          requestId: "request-fixed-choice",
          title: "One question",
          turnId: "turn-fixed-choice",
          questions: [
            {
              allowFreeText: false,
              id: "scope",
              header: "Scope",
              question: "Which scope?",
              options: [{ label: "Small", description: "Focused change" }],
              multiSelect: false
            }
          ]
        }}
        conversationReturn={{
          continueAnswering: labels.continueAnswering,
          returnToConversation: labels.returnToConversation
        }}
        isSubmitting={false}
        onSubmit={vi.fn()}
        labels={labels}
      />
    );

    expect(
      screen.queryByRole("button", { name: labels.returnToConversation })
    ).toBeNull();
    expect(screen.getByRole("button", { name: /Small/ })).toBeTruthy();
  });

  it("resets presentation state for the exact interaction when request ids repeat", () => {
    const onSubmit = vi.fn();
    const firstPrompt = {
      agentSessionId: "session-1",
      kind: "ask-user" as const,
      requestId: "request-reused",
      title: "First question",
      turnId: "turn-1",
      questions: [
        {
          id: "first",
          header: "First",
          question: "First turn question?",
          options: [],
          multiSelect: false
        }
      ]
    };
    const { rerender } = render(
      <AgentInteractivePromptSurface
        prompt={firstPrompt}
        conversationReturn={{
          continueAnswering: labels.continueAnswering,
          returnToConversation: labels.returnToConversation
        }}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    fireEvent.click(
      screen.getByRole("button", { name: labels.returnToConversation })
    );
    expect(
      screen.getByTestId("agent-question-request-reused-collapsed")
    ).toBeTruthy();

    rerender(
      <AgentInteractivePromptSurface
        prompt={{
          ...firstPrompt,
          title: "Second question",
          turnId: "turn-2",
          questions: [
            {
              ...firstPrompt.questions[0]!,
              id: "second",
              header: "Second",
              question: "Second turn question?"
            }
          ]
        }}
        conversationReturn={{
          continueAnswering: labels.continueAnswering,
          returnToConversation: labels.returnToConversation
        }}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    expect(
      screen.queryByTestId("agent-question-request-reused-collapsed")
    ).toBeNull();
    expect(screen.getByText("Second turn question?")).toBeTruthy();
  });

  it("can return to the conversation when provider interaction controls are disabled", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        prompt={{
          agentSessionId: "session-disabled-return",
          kind: "ask-user",
          requestId: "request-disabled-return",
          title: "One question",
          turnId: "turn-disabled-return",
          questions: [
            {
              id: "scope",
              header: "Scope",
              question: "Which scope should we use?",
              options: [{ id: "small", label: "Small", description: "" }],
              multiSelect: false
            }
          ]
        }}
        conversationReturn={{
          continueAnswering: labels.continueAnswering,
          returnToConversation: labels.returnToConversation
        }}
        isSubmitting={false}
        isInteractionDisabled
        interactionDisabledReason="Provider connection is unavailable"
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    const returnToConversation = screen.getByRole("button", {
      name: labels.returnToConversation
    });
    expect(returnToConversation).toBeEnabled();
    expect(returnToConversation.closest('[aria-disabled="true"]')).toBeNull();
    expect(screen.getByRole("button", { name: /Small/ })).toBeDisabled();
    fireEvent.click(returnToConversation);

    expect(onSubmit).not.toHaveBeenCalled();
    const collapsed = screen.getByTestId(
      "agent-question-request-disabled-return-collapsed"
    );
    expect(collapsed).toBeTruthy();
    const continueAnswering = screen.getByRole("button", {
      name: labels.continueAnswering
    });
    expect(continueAnswering.closest('[aria-disabled="true"]')).toBeNull();
  });

  it("collects answers for ask-user prompts before submission", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "ask-user",
          requestId: "request-ask",
          title: "Questions for you",
          questions: [
            {
              id: "scope",
              header: "Scope",
              question: "Which scope should we use?",
              options: [
                {
                  id: "small",
                  label: "Small",
                  description: "Minimal change"
                },
                {
                  id: "large",
                  label: "Large",
                  description: "Broader cleanup"
                }
              ],
              multiSelect: false
            },
            {
              id: "details",
              header: "Details",
              question: "Anything else to include?",
              options: [],
              multiSelect: false
            }
          ]
        }}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    const smallOption = screen.getByRole("button", {
      name: "Small Minimal change"
    });
    const largeOption = screen.getByRole("button", {
      name: "Large Broader cleanup"
    });

    expect(
      screen.queryByRole("button", { name: labels.returnToConversation })
    ).toBeNull();
    expect(screen.getByPlaceholderText(labels.answerPlaceholder)).toBeTruthy();

    expect(
      screen.getByTestId("agent-question-request-ask-scope")
    ).toHaveAttribute("data-agent-interaction-id", "request-ask");
    expect(
      screen.getByTestId("agent-question-request-ask-scope-option-small")
    ).toBe(smallOption);
    expect(
      screen.getByTestId("agent-question-request-ask-scope-option-large")
    ).toBe(largeOption);

    expect(smallOption.getAttribute("aria-pressed")).toBe("false");
    fireEvent.click(smallOption);
    expect(smallOption.getAttribute("aria-pressed")).toBe("true");
    expect(largeOption.getAttribute("aria-pressed")).toBe("false");
    expect(onSubmit).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: labels.nextQuestion }));
    expect(
      screen.getByTestId("agent-question-request-ask-details-custom-answer")
    ).toBeTruthy();
    fireEvent.change(screen.getByPlaceholderText(labels.answerPlaceholder), {
      target: { value: "Include API docs parity." }
    });
    expect(
      screen.getByTestId("agent-question-request-ask-details-submit")
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: labels.submitAnswers }));

    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "request-ask",
      action: "submit",
      payload: {
        answers: ["Small", "Include API docs parity."],
        answersByQuestionId: {
          scope: "Small",
          details: "Include API docs parity."
        }
      }
    });
    expect(screen.queryByText("Questions for you")).toBeNull();
  });

  it("dispatches implement / skip / feedback for a plan-implementation prompt", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "plan-implementation",
          requestId: "plan-turn-1",
          title: "Session 1"
        }}
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    expect(screen.getByText(labels.planImplementationLead)).toBeTruthy();

    fireEvent.click(screen.getByTestId("agent-plan-implementation-implement"));
    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "plan-turn-1",
      action: "implement"
    });

    // Empty continue button skips (stays in plan mode).
    fireEvent.click(screen.getByTestId("agent-plan-implementation-continue"));
    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "plan-turn-1",
      action: "skip",
      payload: undefined
    });

    // Typing then continuing sends the adjustment back as feedback.
    fireEvent.change(screen.getByTestId("agent-plan-implementation-feedback"), {
      target: { value: "focus on tests first" }
    });
    fireEvent.click(screen.getByTestId("agent-plan-implementation-continue"));
    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "plan-turn-1",
      action: "feedback",
      payload: { text: "focus on tests first" }
    });
  });

  it("submits a single-select ask-user answer in one click when compact", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "ask-user",
          requestId: "ask-req-1",
          title: "Plan topic",
          questions: [
            {
              id: "plan-kind",
              header: "Plan topic",
              question: "Which kind of plan?",
              options: [
                { label: "Health check", description: "Audit the repo" },
                { label: "Feature plan", description: "Needs a name" }
              ],
              multiSelect: false,
              answer: null
            }
          ]
        }}
        variant="compact"
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    // One click submits the chosen option directly — no separate submit step,
    // no free-text box (that rich flow lives in the conversation).
    fireEvent.click(screen.getByRole("button", { name: /Health check/ }));
    expect(onSubmit).toHaveBeenCalledOnce();
    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "ask-req-1",
      action: "submit",
      payload: {
        answers: ["Health check"],
        answersByQuestionId: { "plan-kind": "Health check" }
      }
    });
    expect(screen.queryByPlaceholderText(labels.answerPlaceholder)).toBeNull();
    expect(
      screen.queryByRole("button", { name: labels.submitAnswers })
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: labels.returnToConversation })
    ).toBeNull();
  });

  it("supports selecting and removing compact multi-select answers", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "ask-user",
          requestId: "ask-req-multi",
          title: "Plan topic",
          questions: [
            {
              id: "areas",
              header: "Areas",
              question: "Which areas need review?",
              options: [
                { label: "Backend", description: "API surface" },
                { label: "Frontend", description: "UI surface" }
              ],
              multiSelect: true,
              answer: null
            }
          ]
        }}
        variant="compact"
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    const backend = screen.getByRole("button", { name: /Backend/ });
    const frontend = screen.getByRole("button", { name: /Frontend/ });
    const submit = screen.getByRole("button", { name: labels.submitAnswers });

    expect(backend).toHaveAttribute("aria-pressed", "false");
    expect(submit).toBeDisabled();
    fireEvent.click(backend);
    fireEvent.click(frontend);
    fireEvent.click(backend);
    expect(backend).toHaveAttribute("aria-pressed", "false");
    expect(frontend).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(submit);

    expect(onSubmit).toHaveBeenCalledOnce();
    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "ask-req-multi",
      action: "submit",
      payload: {
        answers: ["Frontend"],
        answersByQuestionId: { areas: ["Frontend"] }
      }
    });
  });

  it("keeps compact multi-question drafts while navigating and submits only when complete", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "ask-user",
          requestId: "ask-req-multi-question",
          title: "Rollout plan",
          questions: [
            {
              id: "scope",
              header: "Scope",
              question: "Which scope should we use?",
              options: [
                { label: "Small", description: "Minimal change" },
                { label: "Large", description: "Broader cleanup" }
              ],
              multiSelect: false,
              answer: null
            },
            {
              id: "details",
              header: "Details",
              question: "Anything else to include?",
              options: [],
              multiSelect: false,
              answer: null
            }
          ]
        }}
        variant="compact"
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    const previous = screen.getByRole("button", {
      name: labels.previousQuestion
    });
    const next = screen.getByRole("button", { name: labels.nextQuestion });
    expect(previous).toBeDisabled();
    expect(next).toBeDisabled();

    const small = screen.getByRole("button", { name: /Small/ });
    fireEvent.click(small);
    fireEvent.click(small);
    expect(
      screen.getByRole("button", { name: labels.nextQuestion })
    ).toBeDisabled();
    fireEvent.click(small);
    fireEvent.click(next);
    expect(onSubmit).not.toHaveBeenCalled();
    const submit = screen.getByRole("button", { name: labels.submitAnswers });
    expect(submit).toBeDisabled();
    fireEvent.change(screen.getByPlaceholderText(labels.answerPlaceholder), {
      target: { value: "Keep the compatibility path" }
    });
    fireEvent.click(
      screen.getByRole("button", { name: labels.previousQuestion })
    );
    expect(screen.getByRole("button", { name: /Small/ })).toHaveAttribute(
      "aria-pressed",
      "true"
    );
    fireEvent.click(screen.getByRole("button", { name: labels.nextQuestion }));
    expect(screen.getByPlaceholderText(labels.answerPlaceholder)).toHaveValue(
      "Keep the compatibility path"
    );
    fireEvent.click(screen.getByRole("button", { name: labels.submitAnswers }));

    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "ask-req-multi-question",
      action: "submit",
      payload: {
        answers: ["Small", "Keep the compatibility path"],
        answersByQuestionId: {
          scope: "Small",
          details: "Keep the compatibility path"
        }
      }
    });
  });

  it("hides free text for option-only ask-user questions", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "ask-user",
          requestId: "ask-option-only",
          title: "Choose one",
          questions: [
            {
              id: "question-1",
              header: "Choice",
              question: "Which option?",
              options: [
                { label: "First", description: "Use the first option" },
                { label: "Second", description: "Use the second option" }
              ],
              multiSelect: false,
              allowFreeText: false
            }
          ]
        }}
        variant="full"
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    expect(
      screen.queryByPlaceholderText(labels.answerPlaceholder)
    ).not.toBeInTheDocument();
    const submit = screen.getByRole("button", { name: labels.submitAnswers });
    expect(submit).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: /First/ }));
    fireEvent.click(submit);

    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "ask-option-only",
      action: "submit",
      payload: {
        answers: ["First"],
        answersByQuestionId: { "question-1": "First" }
      }
    });
  });

  it("trims pure-text answers and rejects whitespace-only input", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "ask-user",
          requestId: "ask-text",
          title: "Details",
          questions: [
            {
              id: "details",
              header: "Details",
              question: "What should the agent know?",
              options: [],
              multiSelect: false
            }
          ]
        }}
        variant="compact"
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    const textarea = screen.getByPlaceholderText(labels.answerPlaceholder);
    const submit = screen.getByRole("button", { name: labels.submitAnswers });
    expect(submit).toBeDisabled();
    fireEvent.change(textarea, { target: { value: "   " } });
    expect(submit).toBeDisabled();
    fireEvent.change(textarea, { target: { value: "  Include tests  " } });
    fireEvent.click(submit);

    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "ask-text",
      action: "submit",
      payload: {
        answers: ["Include tests"],
        answersByQuestionId: { details: "Include tests" }
      }
    });
  });

  it("supports prototype-like question ids as own payload keys", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "ask-user",
          requestId: "ask-prototype-ids",
          title: "Prototype-like ids",
          questions: [
            {
              id: "__proto__",
              header: "First",
              question: "What is the first answer?",
              options: [],
              multiSelect: false
            },
            {
              id: "constructor",
              header: "Second",
              question: "What is the second answer?",
              options: [],
              multiSelect: false
            }
          ]
        }}
        variant="compact"
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    const textarea = screen.getByPlaceholderText(labels.answerPlaceholder);
    fireEvent.change(textarea, { target: { value: "First answer" } });
    fireEvent.click(screen.getByRole("button", { name: labels.nextQuestion }));
    fireEvent.change(textarea, { target: { value: "Second answer" } });
    fireEvent.click(screen.getByRole("button", { name: labels.submitAnswers }));

    expect(onSubmit).toHaveBeenCalledOnce();
    const payload = onSubmit.mock.calls[0]?.[0]?.payload;
    expect(payload?.answers).toEqual(["First answer", "Second answer"]);
    expect(Object.hasOwn(payload?.answersByQuestionId ?? {}, "__proto__")).toBe(
      true
    );
    expect(
      Object.hasOwn(payload?.answersByQuestionId ?? {}, "constructor")
    ).toBe(true);
    expect(payload?.answersByQuestionId?.["__proto__"]).toBe("First answer");
    expect(payload?.answersByQuestionId?.constructor).toBe("Second answer");
  });

  it("uses custom text instead of a single choice and appends it to multiple choices", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "ask-user",
          requestId: "ask-custom-answers",
          title: "Custom answers",
          questions: [
            {
              id: "scope",
              header: "Scope",
              question: "Which scope?",
              options: [{ label: "Small", description: "Minimal" }],
              multiSelect: false
            },
            {
              id: "areas",
              header: "Areas",
              question: "Which areas?",
              options: [{ label: "Renderer", description: "UI" }],
              multiSelect: true
            }
          ]
        }}
        variant="compact"
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    fireEvent.click(screen.getByRole("button", { name: /Small/ }));
    fireEvent.change(screen.getByPlaceholderText(labels.answerPlaceholder), {
      target: { value: "  Full workspace  " }
    });
    fireEvent.click(screen.getByRole("button", { name: labels.nextQuestion }));
    fireEvent.click(screen.getByRole("button", { name: /Renderer/ }));
    fireEvent.change(screen.getByPlaceholderText(labels.answerPlaceholder), {
      target: { value: "  Documentation  " }
    });
    fireEvent.click(screen.getByRole("button", { name: labels.submitAnswers }));

    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "ask-custom-answers",
      action: "submit",
      payload: {
        answers: ["Full workspace", "Renderer, Documentation"],
        answersByQuestionId: {
          scope: "Full workspace",
          areas: ["Renderer", "Documentation"]
        }
      }
    });
  });

  it("resets question position and drafts when requestId changes", () => {
    const firstPrompt = {
      kind: "ask-user" as const,
      requestId: "ask-first",
      title: "First request",
      questions: [
        {
          id: "scope",
          header: "Scope",
          question: "Which scope?",
          options: [{ label: "Small", description: "Minimal" }],
          multiSelect: false
        },
        {
          id: "details",
          header: "Details",
          question: "What else?",
          options: [],
          multiSelect: false
        }
      ]
    };
    const { rerender } = render(
      <AgentInteractivePromptSurface
        prompt={firstPrompt}
        variant="compact"
        isSubmitting={false}
        onSubmit={vi.fn()}
        labels={labels}
      />
    );
    fireEvent.click(screen.getByRole("button", { name: /Small/ }));
    fireEvent.click(screen.getByRole("button", { name: labels.nextQuestion }));
    fireEvent.change(screen.getByPlaceholderText(labels.answerPlaceholder), {
      target: { value: "Old draft" }
    });

    rerender(
      <AgentInteractivePromptSurface
        prompt={{
          ...firstPrompt,
          requestId: "ask-second",
          title: "Second request"
        }}
        variant="compact"
        isSubmitting={false}
        onSubmit={vi.fn()}
        labels={labels}
      />
    );

    expect(screen.getByText("1/2")).toBeTruthy();
    expect(screen.getByRole("button", { name: /Small/ })).toHaveAttribute(
      "aria-pressed",
      "false"
    );
    expect(screen.getByPlaceholderText(labels.answerPlaceholder)).toHaveValue(
      ""
    );
    expect(
      screen.getByRole("button", { name: labels.nextQuestion })
    ).toBeDisabled();
  });

  it("disables answer writes and navigation while submitting", () => {
    const onSubmit = vi.fn();
    const { rerender } = render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "ask-user",
          requestId: "ask-submitting",
          title: "Submitting",
          questions: [
            {
              id: "scope",
              header: "Scope",
              question: "Which scope?",
              options: [{ label: "Small", description: "Minimal" }],
              multiSelect: false
            },
            {
              id: "details",
              header: "Details",
              question: "What else?",
              options: [],
              multiSelect: false
            }
          ]
        }}
        variant="compact"
        isSubmitting
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    expect(screen.getByRole("button", { name: /Small/ })).toBeDisabled();
    expect(
      screen.getByPlaceholderText(labels.answerPlaceholder)
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: labels.previousQuestion })
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: labels.nextQuestion })
    ).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: /Small/ }));
    fireEvent.change(screen.getByPlaceholderText(labels.answerPlaceholder), {
      target: { value: "Blocked" }
    });
    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getByPlaceholderText(labels.answerPlaceholder)).toHaveValue(
      ""
    );

    rerender(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "ask-user",
          requestId: "ask-submitting-last",
          title: "Submitting final answer",
          questions: [
            {
              id: "areas",
              header: "Areas",
              question: "Which areas?",
              options: [{ label: "Renderer", description: "UI" }],
              multiSelect: true
            }
          ]
        }}
        variant="compact"
        isSubmitting
        onSubmit={onSubmit}
        labels={labels}
      />
    );
    expect(
      screen.getByRole("button", { name: labels.submitAnswers })
    ).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: labels.submitAnswers }));
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("submits identical payloads from full and compact answer flows", () => {
    const prompt = {
      kind: "ask-user" as const,
      requestId: "ask-parity",
      title: "Parity",
      questions: [
        {
          id: "scope",
          header: "Scope",
          question: "Which scope?",
          options: [{ label: "Small", description: "Minimal" }],
          multiSelect: false
        },
        {
          id: "details",
          header: "Details",
          question: "What else?",
          options: [],
          multiSelect: false
        }
      ]
    };
    const submitWithVariant = (variant: "full" | "compact") => {
      const onSubmit = vi.fn();
      const view = render(
        <AgentInteractivePromptSurface
          prompt={prompt}
          variant={variant}
          isSubmitting={false}
          onSubmit={onSubmit}
          labels={labels}
        />
      );
      fireEvent.click(screen.getByRole("button", { name: /Small/ }));
      fireEvent.click(
        screen.getByRole("button", { name: labels.nextQuestion })
      );
      fireEvent.change(screen.getByPlaceholderText(labels.answerPlaceholder), {
        target: { value: "Same details" }
      });
      fireEvent.click(
        screen.getByRole("button", { name: labels.submitAnswers })
      );
      const submitted = onSubmit.mock.calls[0]?.[0];
      view.unmount();
      return submitted;
    };

    expect(submitWithVariant("compact")).toEqual(submitWithVariant("full"));
  });

  it("offers only the implement decision for a compact plan-implementation prompt", () => {
    const onSubmit = vi.fn();
    render(
      <AgentInteractivePromptSurface
        prompt={{
          kind: "plan-implementation",
          requestId: "plan-turn-1",
          title: "Session 1"
        }}
        variant="compact"
        isSubmitting={false}
        onSubmit={onSubmit}
        labels={labels}
      />
    );

    // Compact (message-center deck): only the primary "implement" action is
    // shown; refining / staying in plan is deferred to the conversation.
    fireEvent.click(screen.getByTestId("agent-plan-implementation-implement"));
    expect(onSubmit).toHaveBeenCalledWith({
      requestId: "plan-turn-1",
      action: "implement"
    });
    expect(
      screen.queryByTestId("agent-plan-implementation-feedback")
    ).toBeNull();
    expect(
      screen.queryByTestId("agent-plan-implementation-continue")
    ).toBeNull();
  });
});
