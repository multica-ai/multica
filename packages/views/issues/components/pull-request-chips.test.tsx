import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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

function renderChips(props: { enabled: boolean; linked?: boolean }, onRowClick = vi.fn()) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <div onClick={onRowClick}>
          <Chips {...props} />
        </div>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("issue pull request chips", () => {
  beforeEach(() => {
    apiMock.listIssuePullRequestSummaries.mockReset();
    // jsdom has no layout; only count capping is exercised here.
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(1000);
    vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(60);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("does not request PRs while the display option is off", () => {
    renderChips({ enabled: false });
    expect(apiMock.listIssuePullRequestSummaries).not.toHaveBeenCalled();
    expect(screen.queryByTestId("issue-pull-request-chip")).not.toBeInTheDocument();
  });

  it("shows linked PRs for this issue with states and overflow without opening the row", async () => {
    apiMock.listIssuePullRequestSummaries.mockResolvedValue({
      pull_requests: [
        summary({
          id: "pr-3",
          number: 3,
          state: "merged",
          html_url: "https://github.com/acme/widget/pull/3",
        }),
        summary({ id: "pr-2", number: 2, state: "draft", repo_name: "api" }),
        summary({ id: "pr-1", number: 1 }),
        summary({ id: "pr-9", issue_id: "issue-2", number: 9 }),
      ],
    });
    const onRowClick = vi.fn();
    renderChips({ enabled: true, linked: true }, onRowClick);

    const chips = await screen.findAllByTestId("issue-pull-request-chip");
    expect(chips.map((chip) => chip.textContent)).toEqual(["Mergedwidget#3", "Draftapi#2"]);
    const mergedLink = screen.getByRole("link", { name: /Merged/ });
    expect(mergedLink).toHaveAttribute("href", "https://github.com/acme/widget/pull/3");
    expect(mergedLink).toHaveAttribute("target", "_blank");
    fireEvent.click(mergedLink);
    expect(onRowClick).not.toHaveBeenCalled();
    expect(screen.getByText("+1")).toBeInTheDocument();
    expect(screen.queryByText(/#9/)).not.toBeInTheDocument();
  });
});
