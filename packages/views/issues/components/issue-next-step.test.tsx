import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { githubKeys } from "@multica/core/github";
import type { GitHubPullRequest, GitHubPullRequestState } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { IssueNextStep, type IssueNextStepReply } from "./issue-next-step";

const githubSettings = vi.hoisted(() => ({ enabled: true, prSidebar: true, coAuthor: true, autoLinkPRs: true }));
vi.mock("@multica/core/github", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/github")>()),
  useGitHubSettings: () => githubSettings,
}));

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

const pr = (number: number, state: GitHubPullRequestState) =>
  ({ id: `pr-${number}`, number, state, title: `Fix ${number}`, html_url: `https://github.com/o/r/pull/${number}` }) as GitHubPullRequest;

function renderNextStep(
  status: string,
  { prs = [] as GitHubPullRequest[] | null, reply = null as IssueNextStepReply | null } = {},
) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  if (prs) client.setQueryData(githubKeys.pullRequests("issue-1"), { pull_requests: prs, auto_complete: null });
  const result = render(
    <I18nProvider resources={TEST_RESOURCES} locale="en">
      <QueryClientProvider client={client}>
        <IssueNextStep issueId="issue-1" status={status} reply={reply} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { ...result, client };
}

describe("IssueNextStep", () => {
  beforeEach(() => {
    Object.assign(githubSettings, { enabled: true, prSidebar: true });
  });

  it("asks for a review of the open PR while the issue is in review", () => {
    renderNextStep("in_review", { prs: [pr(1, "merged"), pr(2, "open")] });
    expect(screen.getByText("Ready for review")).toBeInTheDocument();
    expect(screen.getByText("#2 Fix 2")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Review PR" })).toHaveAttribute("href", "https://github.com/o/r/pull/2");
  });

  it("stays quiet in review when no PR is open", () => {
    const { container } = renderNextStep("in_review", { prs: [pr(1, "draft"), pr(2, "merged")] });
    expect(container).toBeEmptyDOMElement();
  });

  it.each([
    ["the GitHub integration", { enabled: false, prSidebar: false }],
    ["the PR sidebar", { enabled: true, prSidebar: false }],
  ])("neither asks for nor fetches a review with %s turned off", (_, settings) => {
    Object.assign(githubSettings, settings);
    const { container } = renderNextStep("in_review", { prs: [pr(2, "open")] });
    expect(container).toBeEmptyDOMElement();

    const { client } = renderNextStep("in_review", { prs: null });
    expect(client.getQueryCache().find({ queryKey: githubKeys.pullRequests("issue-1") })?.isDisabled()).toBe(true);
  });

  it("offers to reply when a blocked issue is waiting on an agent", () => {
    const onReply = vi.fn();
    renderNextStep("blocked", { reply: { authorName: "Lambda", onReply } });
    expect(screen.getByText("Lambda is waiting for a reply")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Reply" }));
    expect(onReply).toHaveBeenCalledOnce();
  });

  it.each([
    ["blocked", null],
    ["todo", { authorName: "Lambda", onReply: () => {} }],
  ])("renders nothing for %s without a rule that applies", (status, reply) => {
    const { container } = renderNextStep(status, { prs: [pr(1, "open")], reply });
    expect(container).toBeEmptyDOMElement();
  });
});
