import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { ApiError } from "@multica/core/api";
import type { GitHubPullRequest, PRAutoComplete } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

let queryFails = false;
let mockPullRequests: GitHubPullRequest[] = [];
let mockAutoComplete: PRAutoComplete | null = null;
vi.mock("@multica/core/github/queries", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/github/queries")>(
    "@multica/core/github/queries",
  );
  return {
    ...actual,
    issuePullRequestsOptions: (issueId: string) => ({
      queryKey: ["github", "pull-requests", issueId],
      queryFn: async () => {
        if (queryFails) throw new Error("Offline");
        return { pull_requests: mockPullRequests, auto_complete: mockAutoComplete };
      },
      enabled: !!issueId,
    }),
  };
});
const apiMock = vi.hoisted(() => ({
  linkIssuePullRequest: vi.fn(),
  setIssuePRAutoComplete: vi.fn(),
}));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: apiMock,
}));
const toastError = vi.hoisted(() => vi.fn());
vi.mock("sonner", async (importOriginal) => {
  const actual = await importOriginal<typeof import("sonner")>();
  return { ...actual, toast: Object.assign(vi.fn(), actual.toast, { error: toastError }) };
});
const navigatePush = vi.hoisted(() => vi.fn());
vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: navigatePush }),
  AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
let mockWorkspaceSettings: Record<string, unknown> = {};
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ settings: () => "/acme/settings" }),
  useCurrentWorkspace: () => ({ id: "ws-1", slug: "acme", settings: mockWorkspaceSettings }),
}));

import { IssueSidebarAddMenu, IssueSidebarAddMenuProvider } from "./issue-sidebar-add-menu";
import { PullRequestsSection } from "./pull-requests-section";

function renderSection(cachedDecision?: PRAutoComplete) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  if (cachedDecision) {
    qc.setQueryData(["github", "pull-requests", "issue-1"], { pull_requests: [], auto_complete: cachedDecision });
  }
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <IssueSidebarAddMenuProvider>
          <IssueSidebarAddMenu />
          <div data-testid="section">
            <PullRequestsSection issueId="issue-1" identifier="MUL-1" open onOpenChange={() => {}} />
          </div>
        </IssueSidebarAddMenuProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

/** The sidebar's "+" menu, where an issue with no PR offers the section's actions. */
async function openAddMenu() {
  fireEvent.click(await screen.findByRole("button", { name: "Add" }));
}

/** With no PR linked, linking starts from the "+" menu and continues in a dialog. */
async function openLinkForm() {
  await openAddMenu();
  fireEvent.click(await screen.findByRole("menuitem", { name: "Link pull request" }));
  return screen.findByRole("textbox", { name: "Link pull request" });
}

const linkedPR = {
  id: "pr-7", provider: "github", workspace_id: "ws-1", repo_owner: "acme", repo_name: "widget", number: 7,
  title: "Add widget", state: "open", html_url: "https://github.com/acme/widget/pull/7", branch: "mul-1",
} as GitHubPullRequest;

const decision: PRAutoComplete = {
  state: "none",
  pull_request_ids: [],
  issue_disabled: false,
  workspace_enabled: true,
};

describe("PullRequestsSection (MUL-7429)", () => {
  beforeEach(() => {
    queryFails = false;
    mockPullRequests = [];
    mockAutoComplete = decision;
    mockWorkspaceSettings = {};
    apiMock.linkIssuePullRequest.mockReset();
    apiMock.setIssuePRAutoComplete.mockReset();
    navigatePush.mockReset();
    toastError.mockReset();
  });

  it("renders nothing while no PR is linked and offers the section's actions from the + menu", async () => {
    renderSection();
    await openAddMenu();
    expect(await screen.findByRole("menuitem", { name: "Link pull request" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Keep status when PRs merge" })).toBeInTheDocument();
    expect(screen.getByTestId("section")).toBeEmptyDOMElement();
    expect(screen.queryByText(enIssues.detail.pull_requests_empty_title)).not.toBeInTheDocument();
  });

  it("opens the + menu on a click the moment it appears", async () => {
    renderSection();
    await new Promise<void>((resolve) => {
      const observer = new MutationObserver(() => {
        const add = document.querySelector<HTMLElement>('button[aria-label="Add"]');
        if (!add) return;
        observer.disconnect();
        fireEvent.click(add);
        resolve();
      });
      observer.observe(document.body, { subtree: true, childList: true });
    });
    expect(await screen.findByRole("menuitem", { name: "Keep status when PRs merge" })).toBeInTheDocument();
  });

  it("shows the section header with its actions once a PR is linked, and leaves the + menu", async () => {
    mockPullRequests = [linkedPR];
    renderSection();
    expect(await screen.findByRole("button", { name: /^Pull requests/ })).toBeVisible();
    expect(screen.getByRole("button", { name: "Link pull request" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Pull request automation" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Add" })).toBeNull();
  });

  it("keeps a retry entry when loading fails with no cached PRs", async () => {
    queryFails = true;
    renderSection();
    expect(await screen.findByRole("button", { name: "Could not load. Retry" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Link pull request" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add" })).toBeNull();
  });

  it("retains linking after a failed refresh when cached data confirms backend support", async () => {
    queryFails = true;
    renderSection(decision);
    expect(await screen.findByRole("button", { name: "Could not load. Retry" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Link pull request" })).toBeVisible();
  });

  it("links a pasted PR URL", async () => {
    apiMock.linkIssuePullRequest.mockResolvedValue({ pull_requests: [], auto_complete: decision });
    renderSection();
    fireEvent.change(await openLinkForm(), {
      target: { value: "https://github.com/acme/widget/pull/41" },
    });
    expect(screen.getByText("PRs with MUL-1 in the title or branch name link automatically.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Link" }));
    await waitFor(() =>
      expect(apiMock.linkIssuePullRequest).toHaveBeenCalledWith("issue-1", {
        url: "https://github.com/acme/widget/pull/41",
      }),
    );
  });

  it("links from the section header once a PR is linked", async () => {
    mockPullRequests = [linkedPR];
    apiMock.linkIssuePullRequest.mockResolvedValue({ pull_requests: [linkedPR], auto_complete: decision });
    renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "Link pull request" }));
    fireEvent.change(await screen.findByRole("textbox", { name: "Link pull request" }), {
      target: { value: "https://github.com/acme/widget/pull/42" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Link" }));
    await waitFor(() =>
      expect(apiMock.linkIssuePullRequest).toHaveBeenCalledWith("issue-1", { url: "https://github.com/acme/widget/pull/42" }),
    );
  });

  it("accepts a link pasted without a scheme", async () => {
    apiMock.linkIssuePullRequest.mockResolvedValue({ pull_requests: [], auto_complete: decision });
    renderSection();
    const input = await openLinkForm();
    fireEvent.change(input, {
      target: { value: "github.com/acme/widget/pull/41/files" },
    });
    fireEvent.submit(input.closest("form")!);
    await waitFor(() =>
      expect(apiMock.linkIssuePullRequest).toHaveBeenCalledWith("issue-1", { url: "github.com/acme/widget/pull/41/files" }),
    );
  });

  it("explains a PR Multica has not received", async () => {
    apiMock.linkIssuePullRequest.mockRejectedValue(new ApiError("not found", 404, "Not Found"));
    renderSection();
    fireEvent.change(await openLinkForm(), {
      target: { value: "https://github.com/acme/widget/pull/999" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Link" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Multica hasn’t received this PR yet. Check that its repository is connected.",
    );
  });

  // A PR that is already merged when it is linked moves the issue inside the
  // link request, so keeping the status must be settable before any PR.
  it("keeps this issue's status on merge before the first PR is linked", async () => {
    apiMock.setIssuePRAutoComplete.mockResolvedValue({ pull_requests: [], auto_complete: { ...decision, issue_disabled: true } });
    renderSection();
    await openAddMenu();
    fireEvent.click(await screen.findByRole("menuitem", { name: "Keep status when PRs merge" }));
    await waitFor(() => expect(apiMock.setIssuePRAutoComplete).toHaveBeenCalledWith("issue-1", true));
  });

  it("keeps this issue's status on merge from the section menu", async () => {
    mockPullRequests = [linkedPR];
    apiMock.setIssuePRAutoComplete.mockResolvedValue({ pull_requests: [], auto_complete: { ...decision, issue_disabled: true } });
    renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "Pull request automation" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Keep status when PRs merge" }));
    await waitFor(() => expect(apiMock.setIssuePRAutoComplete).toHaveBeenCalledWith("issue-1", true));
  });

  // The menu closes, unmounting the item, before the request fails; the
  // failure must still be reported, or a merged PR linked next would move the
  // issue the reader meant to keep.
  it("reports a failed save of keep-status chosen from the + menu after the menu closed", async () => {
    apiMock.setIssuePRAutoComplete.mockRejectedValue(new Error("Offline"));
    renderSection();
    await openAddMenu();
    fireEvent.click(await screen.findByRole("menuitem", { name: "Keep status when PRs merge" }));
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
    await waitFor(() => expect(toastError).toHaveBeenCalledWith(enIssues.pr_automation.update_failed));
  });

  it("reports a failed save of keep-status chosen from the section menu after the menu closed", async () => {
    mockPullRequests = [linkedPR];
    apiMock.setIssuePRAutoComplete.mockRejectedValue(new Error("Offline"));
    renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "Pull request automation" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Keep status when PRs merge" }));
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
    await waitFor(() => expect(toastError).toHaveBeenCalledWith(enIssues.pr_automation.update_failed));
  });

  it("opens the GitHub setting from the + menu", async () => {
    renderSection();
    await openAddMenu();
    fireEvent.click(await screen.findByRole("menuitem", { name: "PR merge settings" }));
    expect(navigatePush).toHaveBeenCalledWith("/acme/settings?tab=code");
  });

  it("offers only the settings link while the workspace leaves status alone", async () => {
    mockAutoComplete = { ...decision, workspace_enabled: false };
    renderSection();
    await openAddMenu();
    expect(await screen.findByRole("menuitem", { name: "PR merge settings" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Keep status when PRs merge" })).toBeNull();
  });

  it("drops the auto-link hint when the workspace does not auto-link PRs", async () => {
    mockWorkspaceSettings = { github_auto_link_prs_enabled: false };
    renderSection();
    await openLinkForm();
    expect(screen.queryByText("PRs with MUL-1 in the title or branch name link automatically.")).toBeNull();
  });

  it("offers nothing on a backend without auto-complete", async () => {
    mockAutoComplete = null;
    renderSection();
    await waitFor(() => expect(screen.queryByText("Pull requests")).not.toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Link pull request" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Pull request automation" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Add" })).toBeNull();
  });
});
