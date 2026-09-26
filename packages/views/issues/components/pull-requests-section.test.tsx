import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { ApiError } from "@multica/core/api";
import type { PRAutoComplete } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

let mockAutoComplete: PRAutoComplete | null = null;
vi.mock("@multica/core/github/queries", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/github/queries")>(
    "@multica/core/github/queries",
  );
  return {
    ...actual,
    issuePullRequestsOptions: (issueId: string) => ({
      queryKey: ["github", "pull-requests", issueId],
      queryFn: async () => ({ pull_requests: [], auto_complete: mockAutoComplete }),
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

import { PullRequestsSection } from "./pull-requests-section";

function renderSection() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <PullRequestsSection issueId="issue-1" identifier="MUL-1" open onOpenChange={() => {}} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

const decision: PRAutoComplete = {
  state: "none",
  pull_request_ids: [],
  issue_disabled: false,
  workspace_enabled: true,
};

describe("PullRequestsSection (MUL-7429)", () => {
  beforeEach(() => {
    mockAutoComplete = decision;
    mockWorkspaceSettings = {};
    apiMock.linkIssuePullRequest.mockReset();
    apiMock.setIssuePRAutoComplete.mockReset();
    navigatePush.mockReset();
  });

  it("links a pasted PR URL", async () => {
    apiMock.linkIssuePullRequest.mockResolvedValue({ pull_requests: [], auto_complete: decision });
    renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "Link pull request" }));
    fireEvent.change(await screen.findByRole("textbox", { name: "Link pull request" }), {
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

  it("accepts a link pasted without a scheme", async () => {
    apiMock.linkIssuePullRequest.mockResolvedValue({ pull_requests: [], auto_complete: decision });
    renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "Link pull request" }));
    fireEvent.change(await screen.findByRole("textbox", { name: "Link pull request" }), {
      target: { value: "github.com/acme/widget/pull/41/files" },
    });
    fireEvent.submit(screen.getByRole("textbox", { name: "Link pull request" }).closest("form")!);
    await waitFor(() =>
      expect(apiMock.linkIssuePullRequest).toHaveBeenCalledWith("issue-1", { url: "github.com/acme/widget/pull/41/files" }),
    );
  });

  it("explains a PR Multica has not received", async () => {
    apiMock.linkIssuePullRequest.mockRejectedValue(new ApiError("not found", 404, "Not Found"));
    renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "Link pull request" }));
    fireEvent.change(await screen.findByRole("textbox", { name: "Link pull request" }), {
      target: { value: "https://github.com/acme/widget/pull/999" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Link" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Multica hasn’t received this PR yet. Check that its repository is connected.",
    );
  });

  it("keeps this issue's status on merge from the section menu", async () => {
    apiMock.setIssuePRAutoComplete.mockResolvedValue({ pull_requests: [], auto_complete: { ...decision, issue_disabled: true } });
    renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "Pull request automation" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Keep status when PRs merge" }));
    await waitFor(() => expect(apiMock.setIssuePRAutoComplete).toHaveBeenCalledWith("issue-1", true));
  });

  it("opens the GitHub setting from the section menu", async () => {
    renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "Pull request automation" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "PR merge settings" }));
    expect(navigatePush).toHaveBeenCalledWith("/acme/settings?tab=integrations&integration=github");
  });

  it("offers only the settings link while the workspace leaves status alone", async () => {
    mockAutoComplete = { ...decision, workspace_enabled: false };
    renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "Pull request automation" }));
    expect(await screen.findByRole("menuitem", { name: "PR merge settings" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Keep status when PRs merge" })).toBeNull();
  });

  it("drops the auto-link hint when the workspace does not auto-link PRs", async () => {
    mockWorkspaceSettings = { github_auto_link_prs_enabled: false };
    renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "Link pull request" }));
    await screen.findByRole("textbox", { name: "Link pull request" });
    expect(screen.queryByText("PRs with MUL-1 in the title or branch name link automatically.")).toBeNull();
  });

  it("hides the actions on a backend without auto-complete", async () => {
    mockAutoComplete = null;
    renderSection();
    await screen.findByText("No linked pull requests.");
    expect(screen.queryByRole("button", { name: "Link pull request" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Pull request automation" })).toBeNull();
  });
});
