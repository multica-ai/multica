import type { ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Markdown as MarkdownBase } from "@multica/ui/markdown";
import { Markdown } from "./markdown";

const { resolveMock } = vi.hoisted(() => ({ resolveMock: vi.fn() }));

vi.mock("../issues/hooks", () => ({
  useResolveIssueIdentifier: (identifier: string) => resolveMock(identifier),
}));

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
});

describe("Markdown bare issue identifier autolink", () => {
  afterEach(() => {
    resolveMock.mockReset();
  });

  it("renders a resolved bare identifier as an issue chip", () => {
    resolveMock.mockImplementation((id: string) =>
      id === "MUL-1" ? { id: "issue-1", identifier: "MUL-1" } : null,
    );

    render(<Markdown>{"Related to MUL-1 today"}</Markdown>);

    expect(screen.getByTestId("issue-mention-card")).toHaveTextContent("issue-1");
    expect(resolveMock).toHaveBeenCalledWith("MUL-1");
  });

  it("leaves an unresolved bare identifier as plain text", () => {
    resolveMock.mockReturnValue(null);

    render(<Markdown>{"Related to MUL-999 today"}</Markdown>);

    expect(screen.queryByTestId("issue-mention-card")).toBeNull();
    expect(screen.getByText(/Related to/)).toHaveTextContent("MUL-999");
  });

  it("does not autolink identifiers inside inline code", () => {
    resolveMock.mockReturnValue(null);

    render(<Markdown>{"use `MUL-1` here"}</Markdown>);

    expect(resolveMock).not.toHaveBeenCalled();
    expect(screen.queryByTestId("issue-mention-card")).toBeNull();
  });

  it("routes a canonical UUID mention straight to the chip (no identifier resolve)", () => {
    render(
      <Markdown>
        {"[MUL-2](mention://issue/00000000-0000-0000-0000-000000000002)"}
      </Markdown>,
    );

    expect(screen.getByTestId("issue-mention-card")).toHaveTextContent(
      "00000000-0000-0000-0000-000000000002",
    );
    expect(resolveMock).not.toHaveBeenCalled();
  });
});

// The base renderer uses a plain <img>; exercising it here (instead of the
// views wrapper, which swaps in <Attachment>) lets us assert the sanitized
// `src` directly. Covers the two gates that used to blank data-URI images:
// rehype-sanitize's protocols.src and react-markdown's urlTransform.
describe("Markdown inline data-URI images", () => {
  const PNG_1X1 =
    "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";

  it("preserves the src of an inline data:image/png image", () => {
    render(<MarkdownBase>{`![demo](${PNG_1X1})`}</MarkdownBase>);

    const img = screen.getByAltText("demo");
    expect(img.tagName).toBe("IMG");
    expect(img).toHaveAttribute("src", PNG_1X1);
  });

  it("keeps regular http(s) images working", () => {
    render(<MarkdownBase>{"![cat](https://cdn.example.com/cat.png)"}</MarkdownBase>);

    expect(screen.getByAltText("cat")).toHaveAttribute(
      "src",
      "https://cdn.example.com/cat.png",
    );
  });

  it("strips non-image data URIs (data:text/html)", () => {
    render(
      <MarkdownBase>{"![x](data:text/html,<script>alert(1)</script>)"}</MarkdownBase>,
    );

    const img = screen.getByAltText("x");
    expect(img.getAttribute("src") ?? "").toBe("");
  });
});
