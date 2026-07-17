import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { GitLabIssueBadge } from "./gitlab-issue-badge";

const mockIssueGitLabIssueOptions = vi.hoisted(() => vi.fn());
const mockUseLinkGitLabIssue = vi.hoisted(() => vi.fn());
const mockUseUnlinkGitLabIssue = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/gitlab", () => ({
  issueGitLabIssueOptions: mockIssueGitLabIssueOptions,
  useLinkGitLabIssue: mockUseLinkGitLabIssue,
  useUnlinkGitLabIssue: mockUseUnlinkGitLabIssue,
}));

vi.mock("../../i18n", () => ({
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  useT: () => ({
    t: (fn: (keys: any) => string) =>
      fn({
        gitlab_issue: {
          title: "GitLab Issue",
          assignee_label: "GitLab",
          unlink_button: "Unlink",
          link_placeholder: "group/project#123",
          link_button: "Link",
          link_error: "Invalid format",
        },
      }),
  }),
}));

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

const noopMutation = { mutate: vi.fn(), isPending: false };

describe("GitLabIssueBadge", () => {
  beforeEach(() => {
    mockUseLinkGitLabIssue.mockReturnValue(noopMutation);
    mockUseUnlinkGitLabIssue.mockReturnValue(noopMutation);
  });

  it("renders the badge with project path and issue number", async () => {
    mockIssueGitLabIssueOptions.mockReturnValue({
      queryKey: ["gitlab", "issue", "abc"],
      queryFn: async () => ({
        gl_issue_iid: 42,
        project_path: "group/project",
        url: "https://gitlab.example.com/group/project/-/issues/42",
        gl_assignee_username: "alice",
      }),
      enabled: true,
    });

    render(<GitLabIssueBadge issueId="abc" />, { wrapper });
    const link = await screen.findByRole("link");
    expect(link).toHaveTextContent("group/project#42");
    expect(link).toHaveAttribute("href", "https://gitlab.example.com/group/project/-/issues/42");
  });

  it("renders the GitLab assignee row", async () => {
    mockIssueGitLabIssueOptions.mockReturnValue({
      queryKey: ["gitlab", "issue", "abc"],
      queryFn: async () => ({
        gl_issue_iid: 42,
        project_path: "group/project",
        url: "https://gitlab.example.com/group/project/-/issues/42",
        gl_assignee_username: "alice",
      }),
      enabled: true,
    });

    render(<GitLabIssueBadge issueId="abc" />, { wrapper });
    expect(await screen.findByText("GitLab: alice")).toBeInTheDocument();
  });

  it("renders link form when no linked issue", async () => {
    mockIssueGitLabIssueOptions.mockReturnValue({
      queryKey: ["gitlab", "issue", "xyz"],
      queryFn: async () => null,
      enabled: true,
    });

    render(<GitLabIssueBadge issueId="xyz" />, { wrapper });
    await new Promise((resolve) => setTimeout(resolve, 10));
    expect(screen.getByPlaceholderText("group/project#123")).toBeInTheDocument();
  });
});
