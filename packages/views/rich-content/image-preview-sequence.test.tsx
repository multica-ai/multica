import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Attachment } from "@multica/core/types";
import { configStore } from "@multica/core/config";

vi.mock("../issues/hooks", () => ({
  useResolveIssueIdentifier: () => null,
}));

vi.mock("../i18n", async () => {
  const editor = (await import("../locales/en/editor.json")).default;
  return {
    useT: () => ({
      t: (select: (bundle: typeof editor) => string, params?: Record<string, string | number>) => {
        const value = select(editor);
        return params ? value.replace(/\{\{(\w+)\}\}/g, (_, key: string) => String(params[key])) : value;
      },
    }),
    useTimeAgo: () => "just now",
  };
});

vi.mock("@multica/core/api", () => ({
  api: { getBaseUrl: () => "", getAttachmentTextContent: vi.fn() },
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

vi.mock("../editor/use-download-attachment", () => ({
  useDownloadAttachment: () => vi.fn(),
}));

import { RichContent } from "./rich-content";
import { collectPreviewSequence, PreviewSequenceProvider } from "../editor/preview-sequence-context";

const ID = "11111111-1111-4111-8111-111111111111";
const attachment: Attachment = {
  id: ID,
  workspace_id: "ws",
  issue_id: null,
  comment_id: null,
  chat_session_id: null,
  chat_message_id: null,
  uploader_type: "member",
  uploader_id: "u",
  filename: "reference.png",
  content_type: "image/png",
  size_bytes: 10,
  url: "https://cdn.example/reference.png",
  download_url: "https://cdn.example/reference.png",
  markdown_url: `/api/attachments/${ID}/download`,
  created_at: "2026-11-06T00:00:00Z",
};

beforeEach(() => {
  configStore.setState({ cdnDomain: "", cdnSigned: false });
});

describe("RichContent image preview sequence", () => {
  it.each([
    ["reference attachment", "![Reference][shot]", `[shot]: /api/attachments/${ID}/download`, [attachment], attachment.markdown_url, "Reference"],
    ["external reference", "![Reference][shot]", "[shot]: https://cdn.example/chart.png", [], "https://cdn.example/chart.png", "Reference"],
    ["indented file-card image", "    !file[photo.png](https://cdn.example/photo.png)", "", [], "https://cdn.example/photo.png", "photo.png"],
    ["angle destination", "![Reference](</chart v2.png>)", "", [], "/chart%20v2.png", "Reference"],
    ["escaped destination", String.raw`![Reference](/chart\(v2\).png)`, "", [], "/chart(v2).png", "Reference"],
    ["parenthesized URL", "![Reference](/chart(v2).png)", "", [], "/chart(v2).png", "Reference"],
  ] as const)("opens the %s in the shared viewer and pages through rendered images", (_name, image, definition, attachments, expectedSrc, imageAlt) => {
    const content = [
      "![First](/first.png)",
      image,
      "![Last](/last.png)",
      String.raw`\![Literal](/literal.png)`,
      "    ![Indented](/indented.png)",
      "```md\n![Example](/example.png)\n```",
      definition,
    ].join("\n\n");
    const records = [...attachments];
    const items = collectPreviewSequence([{ content, attachments: records, standalone: false }]);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
    render(
      <QueryClientProvider client={client}>
        <PreviewSequenceProvider items={items}>
          <RichContent content={content} attachments={records} />
        </PreviewSequenceProvider>
      </QueryClientProvider>,
    );
    expect(screen.getAllByRole("img")).toHaveLength(3);
    const reference = screen.getByRole("img", { name: imageAlt });
    expect(reference).toHaveAttribute("src", expectedSrc);
    fireEvent.click(reference);
    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveAttribute("aria-label", attachments.length ? "reference.png" : imageAlt);
    expect(within(dialog).getByText("2 / 3")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Next" }));
    expect(within(dialog).getByText("3 / 3")).toBeInTheDocument();
    expect(within(dialog).getByRole("img", { name: "Last" })).toHaveAttribute("src", "/last.png");
    expect(within(dialog).getByRole("button", { name: "Next" })).toBeDisabled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Previous" }));
    expect(within(dialog).getByText("2 / 3")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Previous" }));
    expect(within(dialog).getByText("1 / 3")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Previous" })).toBeDisabled();
  });
});
