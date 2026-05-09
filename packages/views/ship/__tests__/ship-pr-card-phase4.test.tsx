import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "../../locales";
import type { PullRequest } from "@multica/core/types";
import { ShipPRCard } from "../components/ship-pr-card";

// Phase 4 — linkage rendering on the PR card.
//
// Source icons + linked-issue chip + open-conversation-channel button are
// all derived from PullRequest fields (no extra queries), so these tests
// stay in `packages/views/` per CLAUDE.md "Where to write tests".

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

function I18nWrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return (
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={RESOURCES}>
        {children}
      </I18nProvider>
    </QueryClientProvider>
  );
}

function makePR(overrides: Partial<PullRequest> = {}): PullRequest {
  return {
    id: "pr-1",
    workspace_id: "ws-1",
    project_id: "p-1",
    repo_url: "https://github.com/acme/app",
    number: 1234,
    title: "Refactor auth",
    state: "open",
    is_draft: false,
    author_login: "alice",
    author_avatar_url: null,
    base_ref: "main",
    head_ref: "feat/x",
    head_sha: "deadbee",
    html_url: "https://github.com/acme/app/pull/1234",
    body: null,
    ci_status: "",
    review_decision: "",
    mergeable: "MERGEABLE",
    additions: 0,
    deletions: 0,
    changed_files: 0,
    labels: [],
    pr_created_at: "2026-05-01T00:00:00Z",
    pr_updated_at: "2026-05-08T10:00:00Z",
    pr_merged_at: null,
    pr_closed_at: null,
    fetched_at: "2026-05-09T00:00:00Z",
    ...overrides,
  };
}

describe("ShipPRCard — Phase 4 source icon", () => {
  it("renders Multica agent icon when source=multica_agent", () => {
    render(<ShipPRCard pr={makePR({ source: "multica_agent" })} />, {
      wrapper: I18nWrapper,
    });
    expect(screen.getByLabelText(/Multica agent/i)).toBeInTheDocument();
  });

  it("renders Multica issue icon when source=multica_human", () => {
    render(<ShipPRCard pr={makePR({ source: "multica_human" })} />, {
      wrapper: I18nWrapper,
    });
    expect(screen.getByLabelText(/Multica issue/i)).toBeInTheDocument();
  });

  it("renders external tool icon when source=external_tool", () => {
    render(<ShipPRCard pr={makePR({ source: "external_tool" })} />, {
      wrapper: I18nWrapper,
    });
    expect(screen.getByLabelText(/External tool/i)).toBeInTheDocument();
  });

  it("renders external contributor icon when source=external_contributor", () => {
    render(<ShipPRCard pr={makePR({ source: "external_contributor" })} />, {
      wrapper: I18nWrapper,
    });
    expect(screen.getByLabelText(/External contributor/i)).toBeInTheDocument();
  });

  it("falls back to external contributor icon for unknown source values", () => {
    // Per CLAUDE.md "Enum drift downgrades, not crashes" — an unknown
    // string from a future server release must not crash the UI.
    render(<ShipPRCard pr={makePR({ source: "future_value_42" })} />, {
      wrapper: I18nWrapper,
    });
    // The default branch returns the external_contributor icon.
    expect(screen.getByLabelText(/External contributor/i)).toBeInTheDocument();
  });
});

describe("ShipPRCard — Phase 4 linked-issue chip", () => {
  it("renders the linked-issue chip when originating_issue_id is set", () => {
    render(
      <ShipPRCard pr={makePR({ originating_issue_id: "issue-uuid-99" })} />,
      { wrapper: I18nWrapper },
    );
    expect(screen.getByTestId("linked-issue-chip")).toBeInTheDocument();
  });

  it("does NOT render the linked-issue chip when originating_issue_id is null", () => {
    render(<ShipPRCard pr={makePR({ originating_issue_id: null })} />, {
      wrapper: I18nWrapper,
    });
    expect(screen.queryByTestId("linked-issue-chip")).not.toBeInTheDocument();
  });

  it("invokes onOpenIssue when the chip is clicked, suppressing default", () => {
    const onOpenIssue = vi.fn();
    render(
      <ShipPRCard
        pr={makePR({ originating_issue_id: "issue-uuid-7" })}
        onOpenIssue={onOpenIssue}
      />,
      { wrapper: I18nWrapper },
    );
    fireEvent.click(screen.getByTestId("linked-issue-chip"));
    expect(onOpenIssue).toHaveBeenCalledWith("issue-uuid-7");
  });
});

describe("ShipPRCard — Phase 4 conversation channel link", () => {
  it("renders the link when conversation_channel_id is set AND callback provided", () => {
    render(
      <ShipPRCard
        pr={makePR({ conversation_channel_id: "ch-1" })}
        onOpenConversationChannel={vi.fn()}
      />,
      { wrapper: I18nWrapper },
    );
    expect(screen.getByTestId("open-conversation-channel")).toBeInTheDocument();
  });

  it("does NOT render the link when no callback is provided", () => {
    render(
      <ShipPRCard pr={makePR({ conversation_channel_id: "ch-1" })} />,
      { wrapper: I18nWrapper },
    );
    expect(screen.queryByTestId("open-conversation-channel")).not.toBeInTheDocument();
  });

  it("invokes the callback with the channel id when clicked", () => {
    const onOpen = vi.fn();
    render(
      <ShipPRCard
        pr={makePR({ conversation_channel_id: "ch-7" })}
        onOpenConversationChannel={onOpen}
      />,
      { wrapper: I18nWrapper },
    );
    fireEvent.click(screen.getByTestId("open-conversation-channel"));
    expect(onOpen).toHaveBeenCalledWith("ch-7");
  });
});
