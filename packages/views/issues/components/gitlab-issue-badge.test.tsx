import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { GitLabIssueBadge } from "./gitlab-issue-badge";

const mockIssueGitLabIssueOptions = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/gitlab", () => ({
  issueGitLabIssueOptions: mockIssueGitLabIssueOptions,
}));

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (fn: (keys: { gitlab_issue: { title: string; assignee_label: string } }) => string) =>
      fn({ gitlab_issue: { title: "GitLab Issue", assignee_label: "GitLab" } }),
  }),
}));

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

describe("GitLabIssueBadge", () => {
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

  it("renders nothing when no linked issue", async () => {
    mockIssueGitLabIssueOptions.mockReturnValue({
      queryKey: ["gitlab", "issue", "xyz"],
      queryFn: async () => null,
      enabled: true,
    });

    const { container } = render(<GitLabIssueBadge issueId="xyz" />, { wrapper });
    // Wait a tick for query to settle
    await new Promise((resolve) => setTimeout(resolve, 10));
    expect(container.innerHTML).toBe("");
  });
});
