import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "../../locales";
import type { PullRequest } from "@multica/core/types";
import { ShipPRCard } from "../components/ship-pr-card";

// Phase 3 added a chip row inside ShipPRCard whose mutation hooks call
// useWorkspaceId() and useQueryClient(). Mock the workspace id and supply
// a real QueryClient so the card mounts cleanly.
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
    title: "Memory KB UI",
    state: "open",
    is_draft: false,
    author_login: "alice",
    author_avatar_url: null,
    base_ref: "main",
    head_ref: "feat/x",
    head_sha: "deadbee",
    html_url: "https://github.com/acme/app/pull/1234",
    body: null,
    ci_status: "success",
    review_decision: "",
    mergeable: "MERGEABLE",
    additions: 611,
    deletions: 20,
    changed_files: 25,
    labels: [],
    pr_created_at: "2026-05-01T00:00:00Z",
    pr_updated_at: "2026-05-08T10:00:00Z",
    pr_merged_at: null,
    pr_closed_at: null,
    fetched_at: "2026-05-09T00:00:00Z",
    ...overrides,
  };
}

describe("ShipPRCard", () => {
  it("renders title, PR number, author, and diff stats", () => {
    render(<ShipPRCard pr={makePR()} />, { wrapper: I18nWrapper });
    expect(screen.getByText("Memory KB UI")).toBeInTheDocument();
    expect(screen.getByText("#1234")).toBeInTheDocument();
    expect(screen.getByText("alice")).toBeInTheDocument();
    // The interpolated stats string carries unicode minus from the locale
    // file (`+611 −20`) — match by both segments.
    expect(screen.getByText(/\+611/)).toBeInTheDocument();
    expect(screen.getByText(/25 files/)).toBeInTheDocument();
    // CI passing pill is rendered for ci_status === "success".
    expect(screen.getByText(/CI passing/i)).toBeInTheDocument();
  });

  it("shows a server-classified high-risk badge with reasons", () => {
    // Phase 5 — risk derivation reads `risk_level` / `risk_reasons` off
    // the PR row instead of scanning the title.
    render(
      <ShipPRCard
        pr={makePR({
          risk_level: "high",
          risk_reasons: ["migration file: 083_x.up.sql"],
        })}
      />,
      { wrapper: I18nWrapper },
    );
    expect(screen.getByTestId("risk-badge")).toBeInTheDocument();
    expect(screen.getByText(/High risk/i)).toBeInTheDocument();
  });

  it("renders the conflict warning when mergeable is CONFLICTING", () => {
    render(<ShipPRCard pr={makePR({ mergeable: "CONFLICTING" })} />, {
      wrapper: I18nWrapper,
    });
    expect(screen.getByText(/Merge conflicts/i)).toBeInTheDocument();
  });

  it("renders the Draft pill when is_draft is true", () => {
    render(<ShipPRCard pr={makePR({ is_draft: true })} />, {
      wrapper: I18nWrapper,
    });
    expect(screen.getByText(/Draft/i)).toBeInTheDocument();
  });

  it("renders a 'View diff' link pointing at /files in a new tab", () => {
    // Phase 6.5 — the deep-link goes to the GitHub Files tab so a
    // reviewer lands on the unified diff rather than the conversation.
    render(<ShipPRCard pr={makePR()} />, { wrapper: I18nWrapper });
    const link = screen.getByTestId("ship-card-view-diff");
    expect(link).toHaveAttribute(
      "href",
      "https://github.com/acme/app/pull/1234/files",
    );
    expect(link).toHaveAttribute("target", "_blank");
    // rel="noopener" prevents the new-tab GitHub page from grabbing
    // window.opener; defensive even when target=_blank already
    // protects most browsers.
    expect(link.getAttribute("rel")).toMatch(/noopener/);
  });
});
