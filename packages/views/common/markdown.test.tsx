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

// FIR-2372 — mermaid/html render the same components comments use; stub them so
// the test asserts routing (block → the right component) without booting the
// real mermaid engine / preview iframe.
vi.mock("../editor/mermaid-diagram", () => ({
  MermaidDiagram: ({ chart }: { chart: string }) => (
    <div data-testid="mermaid-diagram">{chart}</div>
  ),
}));

vi.mock("../editor/html-block-preview", () => ({
  HtmlBlockPreview: ({ html }: { html: string }) => (
    <div data-testid="html-preview">{html}</div>
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

  it("renders fenced mermaid blocks as a diagram (chat parity with comments)", () => {
    render(<Markdown>{"```mermaid\nflowchart TD\n  A --> B\n```"}</Markdown>);

    expect(screen.getByTestId("mermaid-diagram")).toHaveTextContent("flowchart TD");
    expect(document.querySelector(".shiki")).toBeNull();
  });

  it("renders fenced html blocks as a preview (chat parity with comments)", () => {
    render(<Markdown>{"```html\n<b>hi</b>\n```"}</Markdown>);

    expect(screen.getByTestId("html-preview")).toHaveTextContent("<b>hi</b>");
  });

  it("leaves ordinary code blocks as highlighted code", () => {
    render(<Markdown>{"```sh\nuv run pytest -q\n```"}</Markdown>);

    expect(screen.queryByTestId("mermaid-diagram")).toBeNull();
    expect(screen.queryByTestId("html-preview")).toBeNull();
    expect(screen.getByText("uv run pytest -q")).toBeInTheDocument();
  });
});
