/**
 * A link can name a file from another workspace the reader belongs to. The
 * link opener resolves it by attachment, and every request the viewer makes
 * afterwards — text body, stream re-sign, Download — must target the
 * workspace that owns it, not the current route's, or the server answers 404.
 *
 * Real viewer, real download hook, real link opener; only the API is mocked,
 * and it answers the way the server does: an id-keyed request scoped to the
 * wrong workspace is not found.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Attachment, Workspace } from "@multica/core/types";
import { workspaceKeys } from "@multica/core/workspace/queries";

const CURRENT_WS = "aaaaaaaa-0000-4000-8000-000000000000";
const OWNER_WS = "bbbbbbbb-0000-4000-8000-000000000000";
const ID = "01a13179-08aa-7898-ae86-3a1994d2147e";
const FORCED_URL = "https://cdn.example/x?response-content-disposition=attachment";

const { api, openInNewTab } = vi.hoisted(() => ({
  api: {
    getBaseUrl: () => "",
    getAttachmentForDownload: vi.fn(),
    getAttachment: vi.fn(),
    getAttachmentTextContent: vi.fn(),
    getAttachmentBlob: vi.fn(),
  },
  openInNewTab: vi.fn(),
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspaceSlug: () => "alpha",
}));

vi.mock("../navigation", () => ({
  useAppOrigin: () => "https://app.example",
  useNavigation: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/alpha/issues/ALP-1",
    searchParams: new URLSearchParams(),
    hash: "",
    openInNewTab,
    getShareableUrl: (p: string) => `https://app.example${p}`,
  }),
}));

// Every string renders as its key path, so the test can find controls by key.
function keyPath(path: string[] = []): unknown {
  return new Proxy(
    {},
    {
      get: (_t, k) =>
        k === Symbol.toPrimitive ? () => path.join(".") : keyPath([...path, String(k)]),
    },
  );
}
vi.mock("../i18n", () => ({
  useT: () => ({ t: (sel: (s: unknown) => unknown) => String(sel(keyPath())) }),
}));

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

vi.mock("./readonly-content", () => ({
  ReadonlyContent: ({ content }: { content: string }) => (
    <div data-testid="readonly-content">{content}</div>
  ),
}));

import { useOpenAttachmentLink } from "./use-open-attachment-link";

function record(overrides: Partial<Attachment>): Attachment {
  return {
    id: ID,
    workspace_id: OWNER_WS,
    issue_id: null,
    comment_id: null,
    uploader_type: "member",
    uploader_id: "u",
    filename: "notes.md",
    url: `https://cdn.example/workspaces/${OWNER_WS}/${ID}.md`,
    download_url: `https://cdn.example/workspaces/${OWNER_WS}/${ID}.md?Signature=s`,
    attachment_download_url: FORCED_URL,
    content_type: "text/markdown",
    size_bytes: 12,
    created_at: "2026-11-18T00:00:00Z",
    ...overrides,
  } as Attachment;
}

function notFound(): Promise<never> {
  return Promise.reject(Object.assign(new Error("attachment not found"), { status: 404 }));
}

function ownerScoped(scope: unknown): boolean {
  return (scope as { workspaceId?: string } | undefined)?.workspaceId === OWNER_WS;
}

function LinkOpener({ href }: { href: string }) {
  const link = useOpenAttachmentLink();
  return (
    <>
      <button type="button" onClick={() => link.open(href)}>
        open link
      </button>
      {link.modal}
    </>
  );
}

function renderOpener(href: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData<Workspace[]>(workspaceKeys.list(), [
    { id: CURRENT_WS, slug: "alpha" } as Workspace,
    { id: OWNER_WS, slug: "bravo" } as Workspace,
  ]);
  return render(
    <QueryClientProvider client={client}>
      <LinkOpener href={href} />
    </QueryClientProvider>,
  );
}

let clickedDownloads: string[] = [];

beforeEach(() => {
  clickedDownloads = [];
  vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
    if (this.hasAttribute("download")) clickedDownloads.push(this.href);
  });
});

afterEach(() => {
  vi.restoreAllMocks();
  api.getAttachmentForDownload.mockReset();
  api.getAttachment.mockReset();
  api.getAttachmentTextContent.mockReset();
  openInNewTab.mockReset();
});

describe("a link to another workspace's attachment", () => {
  it("reads the text body and downloads from the owning workspace", async () => {
    const att = record({});
    api.getAttachmentForDownload.mockResolvedValue(att);
    api.getAttachmentTextContent.mockImplementation((_id: string, scope?: unknown) =>
      ownerScoped(scope) ? Promise.resolve({ text: "# Notes", originalContentType: "" }) : notFound(),
    );
    api.getAttachment.mockImplementation((_id: string, scope?: unknown) =>
      ownerScoped(scope) ? Promise.resolve(att) : notFound(),
    );
    renderOpener(`/api/attachments/${ID}/download`);

    fireEvent.click(screen.getByText("open link"));

    expect(await screen.findByTestId("readonly-content")).toHaveTextContent("# Notes");
    expect(api.getAttachmentTextContent).toHaveBeenCalledWith(ID, { workspaceId: OWNER_WS });

    fireEvent.click(screen.getAllByRole("button", { name: /download/i })[0]!);

    await waitFor(() => expect(clickedDownloads).toEqual([FORCED_URL]));
    expect(api.getAttachment).toHaveBeenCalledWith(ID, { workspaceId: OWNER_WS });
  });

  it("re-signs a video's stream against the owning workspace", async () => {
    const signed = `https://cdn.example/workspaces/${OWNER_WS}/${ID}.mp4?Signature=fresh`;
    const att = record({ filename: "clip.mp4", content_type: "video/mp4", download_url: signed });
    api.getAttachmentForDownload.mockResolvedValue(att);
    api.getAttachment.mockImplementation((_id: string, scope?: unknown) =>
      ownerScoped(scope) ? Promise.resolve(att) : notFound(),
    );
    renderOpener(`/api/attachments/${ID}/download`);

    fireEvent.click(screen.getByText("open link"));

    await waitFor(() => expect(document.querySelector("video")?.getAttribute("src")).toBe(signed));
    expect(api.getAttachment).toHaveBeenCalledWith(ID, { workspaceId: OWNER_WS });
  });

  it("opens the full-page preview under the owning workspace", async () => {
    api.getAttachmentForDownload.mockResolvedValue(record({ filename: "clip.mp4", content_type: "video/mp4" }));
    api.getAttachment.mockImplementation(() => notFound());
    renderOpener(`/api/attachments/${ID}/download`);

    fireEvent.click(screen.getByText("open link"));
    const openTab = await screen.findByRole("button", { name: /open_in_new_tab/ });
    await act(async () => {
      fireEvent.click(openTab);
    });

    expect(openInNewTab).toHaveBeenCalledWith(
      expect.stringMatching(new RegExp(`^/bravo/attachments/${ID}/preview`)),
      "clip.mp4",
      { activate: true },
    );
  });
});
