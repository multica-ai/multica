import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { AgentComposerProps } from "../AgentComposer";
import type { AgentGUIViewLabels } from "../AgentGUINodeView";
import { AgentGUIEmptyHomePane } from "./AgentGUIEmptyState";

vi.mock("../AgentComposer", () => ({
  AgentComposer: () => <div data-testid="agent-composer" />
}));

vi.mock("./AgentGUIEmptyHeroCarouselStage", () => ({
  AgentGUIEmptyHeroCarouselStage: ({
    children
  }: {
    children: React.ReactNode;
  }) => <>{children}</>
}));

vi.mock("./AgentTargetSetupGate.tsx", () => ({
  AgentTargetSetupGate: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  )
}));

describe("AgentGUIEmptyHeroPane notices", () => {
  it("does not render an unresolved target label in the Home title", () => {
    render(
      <AgentGUIEmptyHomePane
        isActive
        isVisible
        provider="unknown"
        providerReadinessGate={null}
        showAllProviders={false}
        agentTargets={[]}
        selectedAgentTarget={{
          disabled: true,
          label: "unknown",
          provider: "unknown",
          ref: { kind: "loading", provider: "unknown" },
          targetId: "__loading__"
        }}
        labels={
          {
            empty: "需要 Agent 帮你做些什么？",
            emptyForProvider: () => "需要 Agent 帮你做些什么？",
            emptyProvider: "Agent",
            emptyProviderForProvider: () => "Agent",
            providerSwitchLabel: "选择 Agent",
            sharedAgentOwnerSeparator: "的"
          } as unknown as AgentGUIViewLabels
        }
        noticeSource={{ chrome: null, isRespondingApproval: false }}
        onSubmitApprovalOption={vi.fn()}
        onRetryActivation={vi.fn()}
        onContinueInNewConversation={vi.fn()}
        chromeLabels={{} as never}
        composerProps={{} as AgentComposerProps}
        suggestions={[]}
        onSelectSuggestion={vi.fn()}
      />
    );

    expect(
      screen.getByRole("heading", {
        name: "需要 Agent 帮你做些什么？"
      })
    ).toBeInTheDocument();
    expect(screen.queryByText(/unknown/i)).not.toBeInTheDocument();
  });

  it("reuses the Home status chrome for revoked sharing above the disabled composer", () => {
    render(
      <AgentGUIEmptyHomePane
        isActive
        isVisible
        provider="codex"
        providerReadinessGate={null}
        showAllProviders={false}
        agentTargets={[]}
        selectedAgentTarget={null}
        labels={
          {
            empty: "What can Agent help with?",
            emptyForProvider: () => "What can Agent help with?",
            emptyProvider: "Agent",
            emptyProviderForProvider: () => "Agent",
            providerSwitchLabel: "Select Agent",
            sharedAgentOwnerSeparator: "'s"
          } as unknown as AgentGUIViewLabels
        }
        noticeSource={{
          chrome: {
            auth: null,
            approval: null,
            recovery: {
              kind: "agent-sharing-revoked",
              message: "Jackson stopped sharing this agent",
              canRetry: false
            },
            rawState: null
          },
          isRespondingApproval: false
        }}
        onSubmitApprovalOption={vi.fn()}
        onRetryActivation={vi.fn()}
        onContinueInNewConversation={vi.fn()}
        chromeLabels={{} as never}
        composerProps={{} as AgentComposerProps}
        suggestions={[]}
        onSelectSuggestion={vi.fn()}
      />
    );

    const status = screen.getByRole("alert");
    const composer = screen.getByTestId("agent-composer");
    expect(status).toHaveTextContent("Jackson stopped sharing this agent");
    expect(
      status.compareDocumentPosition(composer) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
  });
});
