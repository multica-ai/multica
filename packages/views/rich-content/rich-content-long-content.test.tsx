/**
 * Long bodies (#11547): a pasted log or JSON export must not freeze the
 * surface that shows it. Past the Markdown limit the body is plain text and
 * never parsed; below it, the patched GFM autolink-literal extension keeps a
 * long paragraph that opens with an unclosed `[` linear, and still links what
 * it linked before.
 */
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import ReactMarkdown from "react-markdown";
vi.mock("../issues/hooks", () => ({
  useResolveIssueIdentifier: () => null,
}));

vi.mock("../i18n", async () => {
  const editor = (await import("../locales/en/editor.json")).default;
  return {
    useT: () => ({
      t: (select: (bundle: typeof editor) => string) => select(editor),
    }),
    useTimeAgo: () => "just now",
  };
});

vi.mock("@multica/core/api", () => ({
  api: { getAttachmentTextContent: vi.fn() },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/acme/issues/${id}`,
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
  useWorkspaceSlug: () => "acme",
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: vi.fn(), openInNewTab: vi.fn() }),
  useOptionalNavigation: () => ({ push: vi.fn(), openInNewTab: vi.fn() }),
  resolveClickIntent: () => "push",
  useAppOrigin: () => null,
  AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => (
    <a href={href}>{children}</a>
  ),
}));

vi.mock("../issues/components/issue-mention-card", () => ({
  IssueMentionCard: ({ issueId }: { issueId: string }) => <span>{issueId}</span>,
}));

vi.mock("../projects/components/project-chip", () => ({
  ProjectChip: ({ projectId }: { projectId: string }) => <span>{projectId}</span>,
}));

vi.mock("../editor/link-hover-card", () => ({
  useLinkHover: () => ({}),
  LinkHoverCard: () => null,
}));

// Surface what the file-card renderer received, so the assertion is about the
// emitted attachment rather than incidental chrome.
vi.mock("../editor/attachment", () => ({
  Attachment: ({ attachment }: { attachment: { url: string; filename: string } }) => (
    <span data-testid="file-card" data-url={attachment.url}>
      {attachment.filename}
    </span>
  ),
}));

vi.mock("react-markdown", async (importOriginal) => {
  const actual = await importOriginal<typeof import("react-markdown")>();
  return { ...actual, default: vi.fn(actual.default) };
});

const downloadBlob = vi.hoisted(() => vi.fn());
vi.mock("../editor/utils/mermaid-export", () => ({ downloadBlob }));

import { RichContent } from "./rich-content";
import { MAX_MARKDOWN_CHARS } from "./markdown-limit";

const parses = () => vi.mocked(ReactMarkdown).mock.calls.length;

/** A JSON export on one line: one paragraph that opens with an unclosed `[`. */
function jsonExport(chars: number): string {
  const rows: string[] = [];
  let length = 2;
  for (let i = 0; length < chars; i++) {
    const row = JSON.stringify({ id: i, title: `Row ${i}: when the page loads, see https://example.com/${i} or write to ops@example.com`, labels: ["perf", `area-${i % 9}`] });
    rows.push(row);
    length += row.length + 1;
  }
  return `[${rows.join(",")}]`;
}

describe("RichContent past the Markdown limit", () => {
  const huge = `# Not a heading\n\n**not bold** ${"x".repeat(MAX_MARKDOWN_CHARS)}`;

  it("shows the raw text without parsing it", () => {
    const before = parses();
    const { container } = render(<RichContent content={huge} />);

    expect(parses()).toBe(before);
    expect(screen.getByTestId("rich-content-plain-text").textContent).toBe(huge);
    expect(container.querySelector("h1, strong")).toBeNull();
    expect(screen.getByText("Too long to format, shown as plain text.")).toBeTruthy();
  });

  it("downloads the full text", () => {
    render(<RichContent content={huge} />);
    fireEvent.click(screen.getByRole("button", { name: "Download as .txt" }));

    expect(downloadBlob).toHaveBeenCalledTimes(1);
    const [blob, filename] = downloadBlob.mock.calls[0]!;
    expect((blob as Blob).size).toBe(huge.length);
    expect(filename).toBe("content.txt");
  });

  it("formats content at the limit as Markdown", () => {
    const content = `**bold** ${"x".repeat(MAX_MARKDOWN_CHARS - 9)}`;
    const { container } = render(<RichContent content={content} />);

    expect(screen.queryByTestId("rich-content-plain-text")).toBeNull();
    expect(container.querySelector("strong")?.textContent).toBe("bold");
  });
});

describe("GFM autolink literals in a long paragraph", () => {
  it("formats a JSON export below the limit without rescanning the paragraph", () => {
    // Rendering the export's thousands of links takes seconds on a busy runner
    // either way, so time it against the same text without the opening `[`.
    // Unpatched, the unclosed `[` makes it about ten times slower (quadratic);
    // patched, the two take about as long.
    const content = jsonExport(150_000);
    const time = (markdown: string) => {
      const start = performance.now();
      render(<RichContent content={markdown} />);
      return performance.now() - start;
    };
    const withoutBracket = time(` ${content.slice(1)}`);
    const withBracket = time(content);

    expect(withBracket).toBeLessThan(withoutBracket * 3);
  }, 60_000);

  it("still links a URL that follows a label which became a link", () => {
    const { container } = render(<RichContent content="[(b)](https://e.com)https://example.com/a" />);
    const hrefs = [...container.querySelectorAll("a")].map((a) => a.getAttribute("href"));

    expect(hrefs).toEqual(["https://e.com", "https://example.com/a"]);
  });
});
