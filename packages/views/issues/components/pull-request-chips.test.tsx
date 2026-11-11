import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { IssuePullRequestSummary } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

const apiMock = vi.hoisted(() => ({
  listIssuePullRequestSummaries: vi.fn(),
}));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: apiMock,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

import { IssuePullRequestChips, useIssuePullRequestSummaries } from "./pull-request-chips";

function summary(overrides: Partial<IssuePullRequestSummary>): IssuePullRequestSummary {
  return {
    issue_id: "issue-1",
    id: "pr-1",
    provider: "github",
    repo_owner: "acme",
    repo_name: "widget",
    number: 1,
    title: "Fix the widget",
    state: "open",
    html_url: "https://github.com/acme/widget/pull/1",
    ...overrides,
  };
}

function Chips({ enabled, linked = false }: { enabled: boolean; linked?: boolean }) {
  const pullRequests = useIssuePullRequestSummaries("issue-1", enabled);
  return <IssuePullRequestChips pullRequests={pullRequests} max={2} linked={linked} />;
}

function renderChips(props: { enabled: boolean; linked?: boolean }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <Chips {...props} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("issue pull request chips", () => {
  beforeEach(() => {
    apiMock.listIssuePullRequestSummaries.mockReset();
  });

  it("does not request PRs while the display option is off", () => {
    renderChips({ enabled: false });
    expect(apiMock.listIssuePullRequestSummaries).not.toHaveBeenCalled();
    expect(screen.queryByTestId("issue-pull-request-chip")).not.toBeInTheDocument();
  });

  it("shows this issue's PRs with their state, capped with an overflow count", async () => {
    apiMock.listIssuePullRequestSummaries.mockResolvedValue({
      pull_requests: [
        summary({ id: "pr-3", number: 3, state: "merged" }),
        summary({ id: "pr-2", number: 2, state: "draft" }),
        summary({ id: "pr-1", number: 1 }),
        summary({ id: "pr-9", issue_id: "issue-2", number: 9 }),
      ],
    });
    renderChips({ enabled: true });

    const chips = await screen.findAllByTestId("issue-pull-request-chip");
    expect(chips.map((chip) => chip.textContent)).toEqual(["Merged#3", "Draft#2"]);
    expect(chips[0]).toHaveAttribute("title", "acme/widget#3 · Merged\nFix the widget");
    expect(screen.getByText("+1")).toBeInTheDocument();
    expect(screen.queryByText(/#9/)).not.toBeInTheDocument();
  });

  it("names the repo only when the issue's PRs span repos", async () => {
    apiMock.listIssuePullRequestSummaries.mockResolvedValue({
      pull_requests: [
        summary({ id: "pr-1", repo_name: "api" }),
        summary({ id: "pr-2", repo_name: "web", number: 2 }),
      ],
    });
    renderChips({ enabled: true });

    const chips = await screen.findAllByTestId("issue-pull-request-chip");
    expect(chips.map((chip) => chip.textContent)).toEqual(["Openapi#1", "Openweb#2"]);
  });

  it("opens the PR from a linked chip without triggering the row's own click", async () => {
    apiMock.listIssuePullRequestSummaries.mockResolvedValue({
      pull_requests: [summary({})],
    });
    const onRowClick = vi.fn();
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <I18nProvider resources={TEST_RESOURCES} locale="en">
          <div onClick={onRowClick}>
            <Chips enabled linked />
          </div>
        </I18nProvider>
      </QueryClientProvider>,
    );

    const chip = await screen.findByRole("link");
    expect(chip).toHaveAttribute("href", "https://github.com/acme/widget/pull/1");
    expect(chip).toHaveAttribute("target", "_blank");
    fireEvent.click(chip);
    expect(onRowClick).not.toHaveBeenCalled();
  });
});
