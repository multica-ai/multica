import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { ReactElement, ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Attachment as AttachmentRecord } from "@multica/core/types";

const {
  getAttachmentTextContentMock,
  downloadMock,
  openExternalMock,
  openByUrlMock,
} = vi.hoisted(() => ({
  getAttachmentTextContentMock: vi.fn(),
  downloadMock: vi.fn(),
  openExternalMock: vi.fn(),
  openByUrlMock: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: { getAttachmentTextContent: getAttachmentTextContentMock },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock("./use-download-attachment", () => ({
  useDownloadAttachment: () => downloadMock,
}));

vi.mock("../platform", () => ({
  openExternal: openExternalMock,
}));

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (sel: (s: Record<string, Record<string, string>>) => string) =>
      sel({
        image: {
          view: "View",
          download: "Download",
          copy_link: "Copy link",
          copy_link_failed: "Copy failed",
          link_copied: "Link copied",
          delete: "Delete",
        },
        attachment: {
          preview: "Preview",
          preview_loading: "Loading preview…",
          preview_failed: "Couldn't load preview",
          preview_unsupported: "This file type can't be previewed.",
          preview_too_large: "File is too large to preview.",
          open_in_new_tab: "Open in new tab",
          close: "Close",
        },
        file_card: { uploading: "Uploading {{filename}}" },
      }),
  }),
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/issues",
    searchParams: new URLSearchParams(),
    openInNewTab: vi.fn(),
    getShareableUrl: (p: string) => `https://app.example${p}`,
  }),
}));

vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useWorkspaceSlug: () => "acme",
    useWorkspacePaths: () => actual.paths.workspace("acme"),
  };
});

// Resolver mock — feeds the test-scoped attachments[] into the
// useAttachmentDownloadResolver hook the component reads.
const resolverState: { attachments: AttachmentRecord[] } = { attachments: [] };
vi.mock("./attachment-download-context", () => ({
  useAttachmentDownloadResolver: () => ({
    resolveAttachmentId: (url: string) =>
      resolverState.attachments.find((a) => a.url === url)?.id,
    resolveAttachment: (url: string) =>
      resolverState.attachments.find((a) => a.url === url),
    openByUrl: openByUrlMock,
  }),
  AttachmentDownloadProvider: ({ children }: { children: ReactNode }) =>
    <>{children}</>,
}));

import { Attachment } from "./attachment";

function makeRecord(overrides: Partial<AttachmentRecord> = {}): AttachmentRecord {
  return {
    id: "att-1",
    workspace_id: "ws-1",
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "u-1",
    filename: "shot.png",
    url: "https://cdn.example.test/att-1.png",
    download_url: "https://cdn.example.test/att-1.png?Signature=s",
    content_type: "image/png",
    size_bytes: 1024,
    created_at: "2026-05-13T00:00:00Z",
    ...overrides,
  };
}

function renderWithQuery(ui: ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  resolverState.attachments = [];
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("Attachment — image dispatch", () => {
  it("record image renders <img> with hover toolbar (View/Download/Copy)", () => {
    const att = makeRecord();
    renderWithQuery(<Attachment attachment={{ kind: "record", attachment: att }} />);
    const img = document.querySelector("img");
    expect(img).toBeTruthy();
    expect(img?.getAttribute("src")).toBe(att.url);
    expect(img?.getAttribute("alt")).toBe("shot.png");
    expect(screen.getByTitle("View")).toBeTruthy();
    expect(screen.getByTitle("Download")).toBeTruthy();
    expect(screen.getByTitle("Copy link")).toBeTruthy();
    // Trash only shows in editable mode.
    expect(screen.queryByTitle("Delete")).toBeNull();
  });

  it("editable image shows Trash button and wires onDelete", () => {
    const att = makeRecord();
    const onDelete = vi.fn();
    renderWithQuery(
      <Attachment
        attachment={{ kind: "record", attachment: att }}
        editable
        onDelete={onDelete}
      />,
    );
    const trash = screen.getByTitle("Delete");
    fireEvent.click(trash);
    expect(onDelete).toHaveBeenCalled();
  });

  it("url-only image resolves to a record via context and uses its id for download", () => {
    const att = makeRecord({
      filename: "from-resolver.png",
      url: "https://cdn.example.test/from-resolver.png",
    });
    resolverState.attachments = [att];
    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: att.url,
          filename: "from-resolver.png",
        }}
      />,
    );
    const img = document.querySelector("img");
    expect(img?.getAttribute("src")).toBe(att.url);
    fireEvent.click(screen.getByTitle("Download"));
    expect(downloadMock).toHaveBeenCalledWith("att-1");
  });

  it("forceKind=image renders as image even when filename is empty (markdown ![](url) regression)", () => {
    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: "https://external.example/no-ext-here",
          filename: "",
          forceKind: "image",
        }}
      />,
    );
    // Without forceKind the empty filename would fall through to AttachmentCard.
    // With forceKind="image" it must render as an <img>.
    expect(document.querySelector("img")).toBeTruthy();
    expect(screen.queryByText("Uploading")).toBeNull();
  });

  it("external image (no resolver match) renders <img> and falls back to openByUrl on Download", () => {
    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: "https://external.example/foo.png",
          filename: "foo.png",
        }}
      />,
    );
    const img = document.querySelector("img");
    expect(img?.getAttribute("src")).toBe("https://external.example/foo.png");
    fireEvent.click(screen.getByTitle("Download"));
    expect(openByUrlMock).toHaveBeenCalledWith("https://external.example/foo.png");
    expect(downloadMock).not.toHaveBeenCalled();
  });

  it("uploading image renders no toolbar (loader state)", () => {
    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: "blob://local",
          filename: "in-flight.png",
          uploading: true,
        }}
      />,
    );
    expect(screen.queryByTitle("View")).toBeNull();
    expect(screen.queryByTitle("Download")).toBeNull();
  });
});

describe("Attachment — html dispatch", () => {
  it("record html with attachmentId renders HtmlAttachmentPreview (no file-card chrome)", () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>chart</p>",
      originalContentType: "text/html",
    });
    const att = makeRecord({
      filename: "report.html",
      content_type: "text/html",
      url: "https://cdn.example.test/report.html",
    });
    renderWithQuery(<Attachment attachment={{ kind: "record", attachment: att }} />);
    // HtmlAttachmentPreview hides the filename row.
    expect(screen.queryByText("report.html")).toBeNull();
    expect(screen.getByTitle("Preview")).toBeTruthy();
    expect(screen.getByTitle("Download")).toBeTruthy();
  });

  it("url-only html (no resolver match) falls back to AttachmentCard chrome", () => {
    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: "https://external.example/report.html",
          filename: "report.html",
          contentType: "text/html",
        }}
      />,
    );
    // Without an attachment id the /content proxy is unreachable, so we
    // show the chrome instead of the iframe.
    expect(screen.getByText("report.html")).toBeTruthy();
    expect(document.querySelector("iframe")).toBeNull();
  });
});

describe("Attachment — file-card dispatch", () => {
  it("record pdf renders the file-card chrome (filename + Preview/Download)", () => {
    const att = makeRecord({
      filename: "manual.pdf",
      content_type: "application/pdf",
    });
    renderWithQuery(<Attachment attachment={{ kind: "record", attachment: att }} />);
    expect(screen.getByText("manual.pdf")).toBeTruthy();
    expect(document.querySelector("iframe")).toBeNull();
    expect(document.querySelector("img")).toBeNull();
  });

  it("uploading file-card surfaces the uploading template, no Preview/Download", () => {
    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: "blob://local",
          filename: "in-flight.zip",
          uploading: true,
        }}
      />,
    );
    expect(screen.getByText("Uploading {{filename}}")).toBeTruthy();
    // Preview/Download chrome is hidden while uploading.
    expect(screen.queryByTitle("Preview")).toBeNull();
    expect(screen.queryByTitle("Download")).toBeNull();
  });
});
