/**
 * Cross-surface sanitize contract (MUL-4922).
 *
 * The two product-level Markdown chains — Chat (ui/markdown/Markdown.tsx) and
 * Issue/Comment (views/editor/readonly-content.tsx) — used to carry a verbatim
 * fork of the sanitize schema and urlTransform each, and had already drifted:
 * readonly whitelisted <mark>, chat did not. Both now import the single
 * canonical base from @multica/ui/markdown.
 *
 * This suite runs one set of security fixtures through BOTH surfaces and
 * asserts the same outcome. It is the mechanism that stops a third fork from
 * growing back: a schema change that lands on only one chain fails here.
 */
import { describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import {
  Markdown as MarkdownBase,
  markdownSanitizeSchema,
} from "@multica/ui/markdown";

vi.mock("@multica/core/config", () => ({
  useConfigStore: (selector: (state: { cdnDomain: string }) => unknown) =>
    selector({ cdnDomain: "" }),
  configStore: { getState: () => ({ cdnDomain: "" }) },
}));

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
    issueDetail: (id: string) => `/test/issues/${id}`,
    projectDetail: (id: string) => `/test/projects/${id}`,
  }),
  useWorkspaceSlug: () => "test",
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: vi.fn(), openInNewTab: vi.fn() }),
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

vi.mock("../editor/utils/link-handler", () => ({
  openLink: vi.fn(),
  isMentionHref: (href?: string) => Boolean(href?.startsWith("mention://")),
}));

// Expose the sanitized url/filename that survived the schema, so image
// fixtures can be asserted identically on both surfaces (readonly routes
// images through <Attachment>, the ui base renders a plain <img>).
vi.mock("../editor/attachment", () => ({
  Attachment: ({
    attachment,
  }: {
    attachment: { url: string; filename: string };
  }) => <img src={attachment.url} alt={attachment.filename} />,
}));

import { ReadonlyContent } from "../editor/readonly-content";

const PNG_1X1 =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";

const SURFACES: ReadonlyArray<{
  name: string;
  render: (markdown: string) => HTMLElement;
}> = [
  {
    name: "Chat (ui/markdown)",
    render: (markdown) => render(<MarkdownBase>{markdown}</MarkdownBase>).container,
  },
  {
    name: "Issue/Comment (ReadonlyContent)",
    render: (markdown) => render(<ReadonlyContent content={markdown} />).container,
  },
];

describe.each(SURFACES)("sanitize contract — $name", ({ render: renderSurface }) => {
  it("strips <script>", () => {
    const container = renderSurface("hi\n\n<script>alert(1)</script>");

    expect(container.querySelector("script")).toBeNull();
    expect(container.textContent).not.toContain("alert(1)");
  });

  it("strips event-handler attributes from raw HTML", () => {
    const container = renderSurface('<img src="x" onerror="alert(1)">');

    expect(container.querySelector("[onerror]")).toBeNull();
    expect(container.innerHTML).not.toContain("onerror");
  });

  it("neutralizes javascript: hrefs", () => {
    const container = renderSurface("[click](javascript:alert(1))");

    const href = container.querySelector("a")?.getAttribute("href") ?? "";
    expect(href.toLowerCase()).not.toContain("javascript:");
  });

  it("preserves an inline data:image/* src", () => {
    const container = renderSurface(`![demo](${PNG_1X1})`);

    expect(container.querySelector("img")).toHaveAttribute("src", PNG_1X1);
  });

  it("blanks a non-image data: URI", () => {
    const container = renderSurface("![x](data:text/html,<script>alert(1)</script>)");

    expect(container.querySelector("img")?.getAttribute("src") ?? "").toBe("");
  });

  it("keeps http(s) image src intact", () => {
    const container = renderSurface("![cat](https://cdn.example.com/cat.png)");

    expect(container.querySelector("img")).toHaveAttribute(
      "src",
      "https://cdn.example.com/cat.png",
    );
  });

  // The drift this sweep closes: <mark> was whitelisted on readonly only, so
  // the same content highlighted in a comment and rendered in chat disagreed.
  it("allows <mark> (==highlight== lowering target)", () => {
    const container = renderSurface("<mark>hi</mark>");

    expect(container.querySelector("mark")?.textContent).toBe("hi");
  });

  it("allows the slash:// protocol", () => {
    const container = renderSurface("[/deploy](slash://skill/abc-123)");

    expect(container.querySelector(".slash-command")?.textContent).toBe("/deploy");
  });
});

// Code-block *rendering* is deliberately not asserted cross-surface yet: chat
// highlights with Shiki and readonly with lowlight, so the emitted class tokens
// still differ. Converging them is the RichCodeBlock phase of MUL-4922; until
// the highlight engine is picked, only the schema-level allow-list is shared.
describe("canonical sanitize schema", () => {
  it("permits language-/math-/hljs class tokens on <code>", () => {
    const codeAttrs = markdownSanitizeSchema.attributes?.code ?? [];
    const patterns = codeAttrs
      .filter((attr): attr is [string, RegExp] => Array.isArray(attr))
      .filter(([name]) => name === "className")
      .map(([, pattern]) => pattern.source);

    expect(patterns).toEqual(
      expect.arrayContaining(["^language-", "^math-", "^hljs"]),
    );
  });
});
