import type { ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Markdown } from "./markdown";

vi.mock("@multica/core/config", () => ({
  useConfigStore: (selector: (state: { cdnDomain: string }) => unknown) =>
    selector({ cdnDomain: "" }),
}));

vi.mock("../issues/components/issue-mention-card", () => ({
  IssueMentionCard: ({ issueId }: { issueId: string }) => (
    <span data-testid="issue-mention-card">{issueId}</span>
  ),
}));

vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useWorkspaceSlug: () => "acme",
    useRequiredWorkspaceSlug: () => "acme",
    useWorkspacePaths: () => ({
      ...actual.paths.workspace("acme"),
      projectDetail: (projectId: string) => `/projects/${projectId}`,
    }),
  };
});

vi.mock("../navigation", () => ({
  AppLink: ({
    href,
    children,
    className,
  }: {
    href: string;
    children: ReactNode;
    className?: string;
  }) => (
    <a href={href} className={className}>
      {children}
    </a>
  ),
}));

vi.mock("../projects/components/project-chip", () => ({
  ProjectChip: ({ projectId }: { projectId: string }) => (
    <span data-testid="project-chip">{projectId}</span>
  ),
}));

const ligatureClasses = [
  "[font-variant-ligatures:none]",
  "[font-feature-settings:'liga'_0]",
];

describe("Markdown", () => {
  it("disables ligatures inside raw code tags", () => {
    render(<Markdown>{"<code>uv run --extra dev pytest -q</code>"}</Markdown>);

    expect(screen.getByText("uv run --extra dev pytest -q")).toHaveClass(...ligatureClasses);
  });

  it("disables ligatures inside fenced code blocks", () => {
    render(<Markdown>{"```sh\nuv run --extra dev pytest -q\n```"}</Markdown>);

    expect(screen.getByText("uv run --extra dev pytest -q")).toHaveClass(...ligatureClasses);
  });

  it("disables ligatures in terminal-mode code", () => {
    render(<Markdown mode="terminal">{"<code>uv run --extra dev pytest -q</code>"}</Markdown>);

    expect(screen.getByText("uv run --extra dev pytest -q")).toHaveClass(...ligatureClasses);
  });

  it("renders slash skill links as slash command pills", () => {
    const { container } = render(
      <Markdown>[/deploy](slash://skill/abc-123)</Markdown>,
    );

    const pill = container.querySelector(".slash-command");
    expect(pill).not.toBeNull();
    expect(pill?.textContent).toBe("/deploy");
  });

  it("renders project mention links as project chips", () => {
    render(<Markdown>{"[Roadmap](mention://project/project-123)"}</Markdown>);

    expect(screen.getByTestId("project-chip")).toHaveTextContent("project-123");
    expect(screen.getByRole("link")).toHaveAttribute("href", "/projects/project-123");
  });

  it("renders standalone review status words as badges", () => {
    const { container } = render(
      <Markdown>{"Preview QA: PASS\n\nPR checks: FAIL"}</Markdown>,
    );

    const badges = Array.from(container.querySelectorAll(".review-status-badge"));
    expect(badges.map((badge) => badge.textContent)).toEqual(["PASS", "FAIL"]);
    expect(badges[0]).toHaveClass("review-status-badge-pass");
    expect(badges[1]).toHaveClass("review-status-badge-fail");
  });
});
