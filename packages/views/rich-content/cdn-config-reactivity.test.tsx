/**
 * Late CDN config must reprocess already-rendered content (MUL-4922).
 *
 * The CDN domain is fetched asynchronously after auth. File-card detection
 * happens in the markdown preprocess step, which used to read the domain once,
 * imperatively, at render time — so content that rendered before the config
 * landed kept its legacy CDN links as plain anchors permanently. Nothing
 * re-ran the transform, because the memo only depended on `content`.
 *
 * This drives the real store: render with an empty domain, then publish the
 * config the way AuthInitializer does, and assert the block upgrades.
 */
import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, render } from "@testing-library/react";
import { configStore } from "@multica/core/config";

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

import { RichContent } from "./rich-content";

const CDN_DOMAIN = "multica-static.example.com";
const FILE_URL = `https://${CDN_DOMAIN}/workspaces/w1/files/report.pdf`;
// Legacy file-card syntax is a link ON ITS OWN LINE (FILE_LINK_LINE is
// anchored); an inline link is deliberately not a card.
const CONTENT = `Attached:\n\n[report.pdf](${FILE_URL})`;

beforeEach(() => {
  configStore.setState({ cdnDomain: "", cdnSigned: false });
});

describe("RichContent CDN config reactivity", () => {
  it("upgrades a legacy CDN link to a file card when the config arrives late", () => {
    const { container } = render(<RichContent content={CONTENT} />);

    // Config has not landed: the CDN host is unknown, so this is still a link.
    expect(container.querySelector("[data-testid='file-card']")).toBeNull();
    expect(container.querySelector(`a[href="${FILE_URL}"]`)).not.toBeNull();

    // AuthInitializer publishes the config after its background fetch resolves.
    act(() => {
      configStore.getState().setCdnConfig({ cdnDomain: CDN_DOMAIN });
    });

    const card = container.querySelector("[data-testid='file-card']");
    expect(card).not.toBeNull();
    expect(card?.getAttribute("data-url")).toBe(FILE_URL);
  });

  it("renders the file card immediately when the config is already present", () => {
    configStore.setState({ cdnDomain: CDN_DOMAIN });

    const { container } = render(<RichContent content={CONTENT} />);

    expect(container.querySelector("[data-testid='file-card']")).not.toBeNull();
  });

  it("leaves non-CDN links alone when the config arrives", () => {
    // Same shape as CONTENT — only the hostname differs — so the assertion is
    // about CDN matching, not about link placement.
    const external = "Attached:\n\n[the spec](https://example.com/spec.pdf)";
    const { container } = render(<RichContent content={external} />);

    act(() => {
      configStore.getState().setCdnConfig({ cdnDomain: CDN_DOMAIN });
    });

    expect(container.querySelector("[data-testid='file-card']")).toBeNull();
    expect(
      container.querySelector('a[href="https://example.com/spec.pdf"]'),
    ).not.toBeNull();
  });
});
