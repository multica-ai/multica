/**
 * A comment remounts whenever it scrolls back into a virtualized timeline or
 * its issue reopens. Settled content is parsed once and its rendered tree
 * reused; streaming content is never cached (MUL-9538).
 */
import { describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
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

import { RichContent } from "./rich-content";

const parses = () => vi.mocked(ReactMarkdown).mock.calls.length;

describe("RichContent render cache", () => {
  it("parses settled content once across remounts", () => {
    const content = "Cached **once**\n\n| a | b |\n|---|---|\n| 1 | 2 |";
    const before = parses();
    const first = render(<RichContent content={content} />);
    const html = first.container.innerHTML;
    first.unmount();
    const second = render(<RichContent content={content} />);

    expect(parses() - before).toBe(1);
    expect(second.container.innerHTML).toBe(html);
    expect(second.container.querySelector("strong")?.textContent).toBe("once");
  });

  it("parses streaming content on every mount", () => {
    const content = "Still **arriving**";
    const before = parses();
    render(<RichContent content={content} phase="streaming" />).unmount();
    render(<RichContent content={content} phase="streaming" />);

    expect(parses() - before).toBe(2);
  });
});
