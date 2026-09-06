import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { ReactElement, ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Attachment as AttachmentRecord } from "@multica/core/types";

const {
  getAttachmentTextContentMock,
  getAttachmentMock,
  getAttachmentBlobMock,
  getBaseUrlMock,
  downloadMock,
  openExternalMock,
  openByUrlMock,
} = vi.hoisted(() => ({
  getAttachmentTextContentMock: vi.fn(),
  getAttachmentMock: vi.fn(),
  getAttachmentBlobMock: vi.fn(),
  // Default: empty base URL so existing tests render site-relative URLs
  // through the proxy (i.e. exactly the way the web app behaves). The
  // absolutize-specific suite below overrides this to simulate Desktop /
  // mobile webview, where the renderer's origin does NOT proxy /api.
  getBaseUrlMock: vi.fn(() => ""),
  downloadMock: vi.fn(),
  openExternalMock: vi.fn(),
  openByUrlMock: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getAttachmentTextContent: getAttachmentTextContentMock,
    getAttachment: getAttachmentMock,
    getAttachmentBlob: getAttachmentBlobMock,
    getBaseUrl: getBaseUrlMock,
  },
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
    hash: "",
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
function attachmentIdFromTestDownloadURL(url: string): string | undefined {
  const path = /^https?:\/\//i.test(url)
    ? (() => {
        try {
          return new URL(url).pathname;
        } catch (_err) {
          void _err;
          return "";
        }
      })()
    : url.split(/[?#]/, 1)[0] ?? "";
  const match = path.match(/^\/api\/attachments\/([^/]+)\/download$/);
  return match?.[1];
}
vi.mock("./attachment-download-context", () => ({
  useAttachmentDownloadResolver: () => ({
    resolveAttachmentId: (url: string) =>
      resolverState.attachments.find((a) => {
        const id = attachmentIdFromTestDownloadURL(url);
        return a.url === url || (id !== undefined && a.id === id);
      })?.id,
    resolveAttachment: (url: string) =>
      resolverState.attachments.find((a) => {
        const id = attachmentIdFromTestDownloadURL(url);
        return a.url === url || (id !== undefined && a.id === id);
      }),
    openByUrl: openByUrlMock,
  }),
  AttachmentDownloadProvider: ({ children }: { children: ReactNode }) =>
    <>{children}</>,
}));

import { Attachment } from "./attachment";
import { configStore } from "@multica/core/config";

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
    markdown_url: "https://cdn.example.test/api/attachments/att-1/download",
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

// jsdom implements neither half of the object-URL API, and the proxy-mode
// byte fallback depends on both. Stub them per-test so the created/revoked
// calls are assertable, and restore afterwards.
const OBJECT_URL = "blob:https://app.example/att-1";
let originalCreateObjectURL: typeof URL.createObjectURL | undefined;
let originalRevokeObjectURL: typeof URL.revokeObjectURL | undefined;
let createObjectURLMock: ReturnType<typeof vi.fn>;
let revokeObjectURLMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.clearAllMocks();
  resolverState.attachments = [];
  configStore.setState({ cdnDomain: "", cdnSigned: false });
  // Default to "no proxy override" — site-relative URLs stay as-is, mirroring
  // the web app's same-origin proxy. Tests that simulate Desktop / mobile
  // webview override per-case via getBaseUrlMock.mockReturnValue(...).
  getBaseUrlMock.mockReturnValue("");

  originalCreateObjectURL = URL.createObjectURL;
  originalRevokeObjectURL = URL.revokeObjectURL;
  createObjectURLMock = vi.fn(() => OBJECT_URL);
  revokeObjectURLMock = vi.fn();
  Object.defineProperty(URL, "createObjectURL", {
    configurable: true,
    value: createObjectURLMock,
  });
  Object.defineProperty(URL, "revokeObjectURL", {
    configurable: true,
    value: revokeObjectURLMock,
  });
});

afterEach(async () => {
  // Flush any pending blob URL GC timers that use fake timers in suites.
  if (vi.isFakeTimers()) {
    await vi.runAllTimersAsync();
    vi.useRealTimers();
  }
  try {
    const { __resetInlineMediaBlobCacheForTests } = await import("./hooks/use-inline-media-url");
    __resetInlineMediaBlobCacheForTests();
  } catch (_err) {
    void _err;
  }
  restoreObjectURL("createObjectURL", originalCreateObjectURL);
  restoreObjectURL("revokeObjectURL", originalRevokeObjectURL);
  vi.restoreAllMocks();
});

function restoreObjectURL(
  prop: "createObjectURL" | "revokeObjectURL",
  original: unknown,
): void {
  if (original) {
    Object.defineProperty(URL, prop, { configurable: true, value: original });
  } else {
    delete (URL as Partial<typeof URL>)[prop];
  }
}

describe("Attachment — image dispatch", () => {
  it("record image renders <img> with hover toolbar (View/Download/Copy)", () => {
    const att = makeRecord();
    renderWithQuery(<Attachment attachment={{ kind: "record", attachment: att }} />);
    const img = document.querySelector("img");
    expect(img).toBeTruthy();
    // The rendered src is the freshly-loadable URL — when the record
    // carries a signed download_url (CloudFront / S3 presign style) it
    // wins over the raw stored url so token-mode <img> loads work
    // without an Authorization header. See pickInlineMediaURL in
    // attachment.tsx for the rationale (MUL-3130 review follow-up).
    expect(img?.getAttribute("src")).toBe(att.download_url);
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
    // Once the URL resolves to a record, the rendered src swaps to
    // the record's signed download_url so the image is loadable in
    // token-mode clients that can't attach Authorization headers
    // (MUL-3130 review). The raw stored url is the fallback for
    // unresolved markdown only.
    expect(img?.getAttribute("src")).toBe(att.download_url);
    fireEvent.click(screen.getByTitle("Download"));
    expect(downloadMock).toHaveBeenCalledWith("att-1");
  });

  it("renders the configured CDN URL when description markdown stores the stable API URL", () => {
    configStore.setState({ cdnDomain: "cdn.example.test" });
    const id = "11111111-2222-3333-4444-555555555555";
    const markdownUrl = `https://multica-api.copilothub.ai/api/attachments/${id}/download`;
    const att = makeRecord({
      id,
      url: "https://cdn.example.test/uploads/ws/shot.png",
      // This is the shape persisted in issue descriptions on deployments
      // that keep markdown stable via the API endpoint. Once the URL
      // resolves to an attachment record, the rendered <img> must expose the
      // CDN URL instead of copying the API endpoint back to the user.
      markdown_url: markdownUrl,
      download_url: `/api/attachments/${id}/download`,
    });
    resolverState.attachments = [att];

    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: markdownUrl,
          filename: "shot.png",
          forceKind: "image",
        }}
      />,
    );

    const img = document.querySelector("img");
    expect(img?.getAttribute("src")).toBe(
      "https://cdn.example.test/uploads/ws/shot.png",
    );
  });

  it("prefers a local disk /uploads URL over API markdown in split-origin self-host", () => {
    getBaseUrlMock.mockReturnValue("https://api.example.test");
    const id = "11111111-2222-3333-4444-555555555555";
    const markdownUrl = `https://api.example.test/api/attachments/${id}/download`;
    const mediaUrl = "https://api.example.test/uploads/workspaces/ws-1/shot.png";
    const att = makeRecord({
      id,
      url: "/uploads/workspaces/ws-1/shot.png",
      markdown_url: markdownUrl,
      download_url: `/api/attachments/${id}/download`,
    });
    resolverState.attachments = [att];

    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: markdownUrl,
          filename: "shot.png",
          forceKind: "image",
        }}
      />,
    );

    expect(document.querySelector("img")?.getAttribute("src")).toBe(mediaUrl);

    fireEvent.click(screen.getByTitle("View"));

    const imageSrcs = [...document.querySelectorAll("img")].map((img) =>
      img.getAttribute("src"),
    );
    expect(imageSrcs).toEqual([mediaUrl, mediaUrl]);
  });

  it("opens preview with the same resolved media URL when a reopened draft record has no download_url", () => {
    configStore.setState({ cdnDomain: "cdn.example.test" });
    const id = "11111111-2222-3333-4444-555555555555";
    const markdownUrl = `https://multica-api.copilothub.ai/api/attachments/${id}/download`;
    const mediaUrl = "https://cdn.example.test/uploads/ws/shot.png";
    const att = makeRecord({
      id,
      url: mediaUrl,
      markdown_url: markdownUrl,
      download_url: "",
    });
    resolverState.attachments = [att];

    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: markdownUrl,
          filename: "shot.png",
          forceKind: "image",
        }}
      />,
    );

    fireEvent.click(screen.getByTitle("View"));

    const imageSrcs = [...document.querySelectorAll("img")].map((img) =>
      img.getAttribute("src"),
    );
    expect(imageSrcs).toEqual([mediaUrl, mediaUrl]);
    expect(imageSrcs).not.toContain("");
  });

  it("does not pick the raw CDN url when the server reports cdn_signed (MUL-3254)", () => {
    // CloudFront signed-URL mode: the CDN domain serves PRIVATE content and
    // a raw (unsigned) storage URL is a guaranteed 403. The pick must fall
    // through to the durable markdown_url instead.
    configStore.setState({ cdnDomain: "cdn.example.test", cdnSigned: true });
    const id = "11111111-2222-3333-4444-555555555555";
    const markdownUrl = `/api/attachments/${id}/download`;
    const att = makeRecord({
      id,
      url: "https://cdn.example.test/uploads/ws/shot.png",
      markdown_url: markdownUrl,
      download_url: "",
    });
    resolverState.attachments = [att];

    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: markdownUrl,
          filename: "shot.png",
          forceKind: "image",
        }}
      />,
    );

    const img = document.querySelector("img");
    expect(img?.getAttribute("src")).toBe(markdownUrl);
    // Web (same-origin proxy / same-site cookie): the API endpoint loads
    // natively, so no metadata re-fetch is needed.
    expect(getAttachmentMock).not.toHaveBeenCalled();
  });

  it("re-signs a cross-origin API image URL in the web editor", async () => {
    // The web app normally uses the same-origin /api proxy, so getBaseUrl is
    // empty. A self-hosted server can still persist an absolute markdown_url
    // on a different origin, though. Native <img> loading cannot rely on the
    // app session cookie being accepted by that host, while an authenticated
    // metadata request can return a freshly signed storage URL.
    configStore.setState({ cdnDomain: "cdn.example.test", cdnSigned: true });
    const id = "11111111-2222-3333-4444-555555555555";
    const markdownUrl = `https://api.example.test/api/attachments/${id}/download`;
    const signed =
      "https://cdn.example.test/uploads/ws/shot.png?Signature=fresh&Key-Pair-Id=K";
    resolverState.attachments = [
      makeRecord({
        id,
        url: "https://cdn.example.test/uploads/ws/shot.png",
        markdown_url: markdownUrl,
        download_url: `/api/attachments/${id}/download`,
      }),
    ];
    getAttachmentMock.mockResolvedValue(makeRecord({ id, download_url: signed }));

    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: markdownUrl,
          filename: "shot.png",
          forceKind: "image",
        }}
      />,
    );

    await waitFor(() => {
      expect(document.querySelector("img")?.getAttribute("src")).toBe(signed);
    });
    expect(getAttachmentMock).toHaveBeenCalledWith(id);
  });

  it("re-signs the inline media URL through getAttachment on token-mode clients (MUL-3254)", async () => {
    // Desktop / mobile webview: file:// document origin, Bearer-token auth.
    // The auth-gated /api/attachments/<id>/download endpoint 401s as a
    // native <img> fetch, so the renderer must swap in a freshly signed URL
    // from authenticated attachment metadata — the reopened-draft case where
    // the persisted record deliberately strips the expired download_url.
    getBaseUrlMock.mockReturnValue("https://multica-api.copilothub.ai");
    configStore.setState({ cdnDomain: "cdn.example.test", cdnSigned: true });
    const id = "11111111-2222-3333-4444-555555555555";
    const markdownUrl = `https://multica-api.copilothub.ai/api/attachments/${id}/download`;
    const signed =
      "https://cdn.example.test/uploads/ws/shot.png?Signature=fresh&Key-Pair-Id=K";
    const att = makeRecord({
      id,
      url: "https://cdn.example.test/uploads/ws/shot.png",
      markdown_url: markdownUrl,
      download_url: "",
    });
    resolverState.attachments = [att];
    getAttachmentMock.mockResolvedValue(makeRecord({ id, download_url: signed }));

    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: markdownUrl,
          filename: "shot.png",
          forceKind: "image",
        }}
      />,
    );

    await waitFor(() => {
      expect(document.querySelector("img")?.getAttribute("src")).toBe(signed);
    });
    expect(getAttachmentMock).toHaveBeenCalledWith(id);
  });

  it("re-signs URL-only inline media when no resolver record is available (MUL-3254)", async () => {
    // If the markdown parser has only the durable API URL, the attachment id
    // is still recoverable from the URL itself. Token-mode clients must not
    // depend on the context resolver having a hydrated record before they can
    // fetch fresh signed metadata.
    getBaseUrlMock.mockReturnValue("https://multica-api.copilothub.ai");
    configStore.setState({ cdnDomain: "cdn.example.test", cdnSigned: true });
    const id = "11111111-2222-3333-4444-555555555555";
    const markdownUrl = `https://multica-api.copilothub.ai/api/attachments/${id}/download`;
    const signed =
      "https://cdn.example.test/uploads/ws/shot.png?Signature=fresh&Key-Pair-Id=K";
    getAttachmentMock.mockResolvedValue(makeRecord({ id, download_url: signed }));

    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: markdownUrl,
          filename: "shot.png",
          forceKind: "image",
        }}
      />,
    );

    await waitFor(() => {
      expect(document.querySelector("img")?.getAttribute("src")).toBe(signed);
    });
    expect(getAttachmentMock).toHaveBeenCalledWith(id);
  });

  it("falls back to an authenticated byte fetch when the deployment has no signed URL (MUL-5445)", async () => {
    // Proxy download mode — the default `auto` classification for a storage
    // endpoint on an internal host (docker-compose MinIO). GET
    // /api/attachments/{id} hands back the auth-gated API path again, so
    // there is nothing to swap in: the renderer must pull the bytes through
    // the authenticated client and paint them from an object URL instead of
    // keeping a src it already knows 401s.
    getBaseUrlMock.mockReturnValue("https://multica-api.copilothub.ai");
    const id = "11111111-2222-3333-4444-555555555555";
    const markdownUrl = `https://multica-api.copilothub.ai/api/attachments/${id}/download`;
    const att = makeRecord({
      id,
      url: "https://minio:9000/multica/uploads/ws/shot.png",
      markdown_url: markdownUrl,
      download_url: "",
    });
    configStore.setState({ cdnDomain: "", cdnSigned: false });
    resolverState.attachments = [att];
    getAttachmentMock.mockResolvedValue(
      makeRecord({ id, download_url: `/api/attachments/${id}/download` }),
    );
    const blob = new Blob(["png-bytes"], { type: "image/png" });
    getAttachmentBlobMock.mockResolvedValue(blob);

    const { unmount } = renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: markdownUrl,
          filename: "shot.png",
          forceKind: "image",
        }}
      />,
    );

    await waitFor(() => {
      expect(document.querySelector("img")?.getAttribute("src")).toBe(OBJECT_URL);
    });
    expect(getAttachmentBlobMock).toHaveBeenCalledWith(id);
    expect(createObjectURLMock).toHaveBeenCalledWith(blob);

    // The bytes are scheduled for release 5 min after last unmount.
    vi.useFakeTimers();
    unmount();
    expect(revokeObjectURLMock).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000);
    expect(revokeObjectURLMock).toHaveBeenCalledWith(OBJECT_URL);
    vi.useRealTimers();
  });

  it("copies the durable URL, not the session-local object URL (MUL-5445)", async () => {
    // A `blob:` URL resolves only inside this renderer session, so Copy Link
    // must keep handing out the persisted attachment URL.
    getBaseUrlMock.mockReturnValue("https://multica-api.copilothub.ai");
    const id = "11111111-2222-3333-4444-555555555555";
    const markdownUrl = `https://multica-api.copilothub.ai/api/attachments/${id}/download`;
    resolverState.attachments = [
      makeRecord({
        id,
        url: "https://minio:9000/multica/uploads/ws/shot.png",
        markdown_url: markdownUrl,
        download_url: "",
      }),
    ];
    getAttachmentMock.mockResolvedValue(
      makeRecord({ id, download_url: `/api/attachments/${id}/download` }),
    );
    getAttachmentBlobMock.mockResolvedValue(
      new Blob(["png-bytes"], { type: "image/png" }),
    );
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: markdownUrl,
          filename: "shot.png",
          forceKind: "image",
        }}
      />,
    );

    await waitFor(() => {
      expect(document.querySelector("img")?.getAttribute("src")).toBe(OBJECT_URL);
    });
    fireEvent.click(screen.getByTitle("Copy link"));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(markdownUrl));
  });

  it("does not pull bytes for non-image attachments (MUL-5445)", async () => {
    // A file card only needs a link. Downloading a large archive into
    // renderer memory to draw a chip would be a bad trade, so the byte
    // fallback stays image-only.
    getBaseUrlMock.mockReturnValue("https://multica-api.copilothub.ai");
    const id = "11111111-2222-3333-4444-555555555555";
    const markdownUrl = `https://multica-api.copilothub.ai/api/attachments/${id}/download`;
    resolverState.attachments = [
      makeRecord({
        id,
        filename: "archive.zip",
        content_type: "application/zip",
        url: "https://minio:9000/multica/uploads/ws/archive.zip",
        markdown_url: markdownUrl,
        download_url: "",
      }),
    ];
    getAttachmentMock.mockResolvedValue(
      makeRecord({
        id,
        filename: "archive.zip",
        content_type: "application/zip",
        download_url: `/api/attachments/${id}/download`,
      }),
    );

    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: markdownUrl,
          filename: "archive.zip",
        }}
      />,
    );

    await waitFor(() => expect(getAttachmentMock).toHaveBeenCalledWith(id));
    expect(getAttachmentBlobMock).not.toHaveBeenCalled();
  });

  it("prefers the signed URL over a byte fetch when the server can presign (MUL-5445)", async () => {
    // Presign / CloudFront deployments already hand back a natively-loadable
    // URL. Pulling the bytes as well would double every image download.
    getBaseUrlMock.mockReturnValue("https://multica-api.copilothub.ai");
    configStore.setState({ cdnDomain: "cdn.example.test", cdnSigned: true });
    const id = "11111111-2222-3333-4444-555555555555";
    const markdownUrl = `https://multica-api.copilothub.ai/api/attachments/${id}/download`;
    const signed =
      "https://cdn.example.test/uploads/ws/shot.png?Signature=fresh&Key-Pair-Id=K";
    getAttachmentMock.mockResolvedValue(makeRecord({ id, download_url: signed }));

    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: markdownUrl,
          filename: "shot.png",
          forceKind: "image",
        }}
      />,
    );

    await waitFor(() => {
      expect(document.querySelector("img")?.getAttribute("src")).toBe(signed);
    });
    expect(getAttachmentBlobMock).not.toHaveBeenCalled();
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

  it("stable /api/attachments/<id>/download download_url falls through to the durable record.markdown_url for native <img> loadability (MUL-3192)", () => {
    // After MUL-3192, pickInlineMediaURL prefers `record.markdown_url`
    // (the server-chosen durable URL — public CDN passthrough or absolute
    // API endpoint) over the raw `record.url` (which can be a private
    // bucket URL on S3 / R2 / MinIO presign deployments). The signed
    // download_url stays the highest-priority pick when present, but the
    // unsigned `/api/attachments/<id>/download` shape now defers to
    // markdown_url instead of record.url.
    const att = makeRecord({
      // Raw private-bucket URL — must NOT be the rendered src.
      url: "https://prod.s3.amazonaws.com/key.png",
      markdown_url: "https://api.multica.test/api/attachments/att-1/download",
      // bare API path on download_url — no signature query.
      download_url: "/api/attachments/att-1/download",
    });
    renderWithQuery(<Attachment attachment={{ kind: "record", attachment: att }} />);
    const img = document.querySelector("img");
    expect(img?.getAttribute("src")).toBe(
      "https://api.multica.test/api/attachments/att-1/download",
    );
    expect(img?.getAttribute("src")).not.toContain("prod.s3.amazonaws.com");
  });

  it("legacy backend (no markdown_url on record) still falls back to record.url", () => {
    // A backend old enough to predate MUL-3192 omits markdown_url; the
    // fallback chain bottoms out on record.url, preserving render
    // behaviour for legacy attachment metadata in the cache.
    const att = makeRecord({
      url: "https://cdn.example.test/legacy.png",
      markdown_url: "",
      download_url: "/api/attachments/att-1/download",
    });
    renderWithQuery(<Attachment attachment={{ kind: "record", attachment: att }} />);
    const img = document.querySelector("img");
    expect(img?.getAttribute("src")).toBe("https://cdn.example.test/legacy.png");
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

  it("url-only stable attachment download file-card resolves to record and downloads by id", () => {
    const id = "11111111-2222-3333-4444-555555555555";
    const href = `/api/attachments/${id}/download`;
    resolverState.attachments = [
      makeRecord({
        id,
        filename: "manual.pdf",
        content_type: "application/pdf",
        url: "/uploads/manual.pdf",
        markdown_url: href,
        download_url: href,
      }),
    ];

    renderWithQuery(
      <Attachment
        attachment={{ kind: "url", url: href, filename: "manual.pdf" }}
      />,
    );

    expect(screen.getByText("manual.pdf")).toBeTruthy();
    fireEvent.mouseDown(screen.getByTitle("Download"));
    expect(downloadMock).toHaveBeenCalledWith(id);
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

// MUL-3192 — Desktop quick-create: site-relative `/api/attachments/<id>/
// download` and `/uploads/<key>` URLs only resolve in environments where the
// document origin proxies them to the API host (web via Next.js rewrites).
// In Electron desktop the renderer origin is `file://`, so the same path
// can't load. The Attachment renderer runs the picked URL through an
// absolutize pass that prefixes `apiBaseUrl` when one is configured.
describe("Attachment — absolutize site-relative URLs (MUL-3192)", () => {
  it("prefixes site-relative /api/attachments/<id>/download with apiBaseUrl in Desktop-like environments", () => {
    getBaseUrlMock.mockReturnValue("https://api.example.test");
    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: "/api/attachments/abc-1/download",
          filename: "shot.png",
          forceKind: "image",
        }}
      />,
    );
    const img = document.querySelector("img");
    expect(img?.getAttribute("src")).toBe(
      "https://api.example.test/api/attachments/abc-1/download",
    );
  });

  it("absolutizes the legacy site-relative /uploads/<key> when record.markdown_url is empty (legacy backend)", () => {
    // Legacy compat: a backend old enough to predate MUL-3192 omits
    // markdown_url, so pickInlineMediaURL falls through to record.url.
    // For LocalStorage with no LOCAL_UPLOAD_BASE_URL configured that's
    // a site-relative `/uploads/<key>` — the absolutize pass at the
    // renderer's edge prefixes the apiBaseUrl so Desktop's file:// origin
    // doesn't resolve it to file:///uploads/...
    getBaseUrlMock.mockReturnValue("https://api.example.test");
    const att = makeRecord({
      url: "/uploads/ws-1/abc.png",
      markdown_url: "",
      download_url: "/api/attachments/att-1/download",
    });
    renderWithQuery(<Attachment attachment={{ kind: "record", attachment: att }} />);
    const img = document.querySelector("img");
    expect(img?.getAttribute("src")).toBe(
      "https://api.example.test/uploads/ws-1/abc.png",
    );
  });

  it("strips a trailing slash on apiBaseUrl so the prefixed URL has exactly one separator", () => {
    getBaseUrlMock.mockReturnValue("https://api.example.test/");
    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: "/api/attachments/abc-2/download",
          filename: "shot.png",
          forceKind: "image",
        }}
      />,
    );
    const img = document.querySelector("img");
    expect(img?.getAttribute("src")).toBe(
      "https://api.example.test/api/attachments/abc-2/download",
    );
  });

  it("leaves absolute https URLs untouched even when apiBaseUrl is set", () => {
    getBaseUrlMock.mockReturnValue("https://api.example.test");
    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: "https://cdn.other.test/foo.png",
          filename: "foo.png",
          forceKind: "image",
        }}
      />,
    );
    const img = document.querySelector("img");
    expect(img?.getAttribute("src")).toBe("https://cdn.other.test/foo.png");
  });

  it("leaves blob: URLs untouched (in-flight upload preview)", () => {
    getBaseUrlMock.mockReturnValue("https://api.example.test");
    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: "blob:https://app.local/abc-123",
          filename: "in-flight.png",
          forceKind: "image",
          // uploading=true would hide the toolbar, but the src normalization
          // path runs regardless — keep it false so we still mount the <img>.
        }}
      />,
    );
    const img = document.querySelector("img");
    expect(img?.getAttribute("src")).toBe("blob:https://app.local/abc-123");
  });

  it("is a no-op when apiBaseUrl is empty (web app same-origin proxy)", () => {
    getBaseUrlMock.mockReturnValue("");
    renderWithQuery(
      <Attachment
        attachment={{
          kind: "url",
          url: "/api/attachments/abc-3/download",
          filename: "shot.png",
          forceKind: "image",
        }}
      />,
    );
    const img = document.querySelector("img");
    // Persisted markdown URL stays site-relative — Next.js rewrite proxies
    // /api/* to the API host, so the relative path loads through the same
    // origin as the rendered HTML.
    expect(img?.getAttribute("src")).toBe("/api/attachments/abc-3/download");
  });
});

describe("Attachment — blob URL GC (MUL-7741)", () => {
  it("defers revoke by 5 min after last unmount", async () => {
    getBaseUrlMock.mockReturnValue("https://multica-api.copilothub.ai");
    const id = "11111111-2222-3333-4444-555555555555";
    const markdownUrl = `https://multica-api.copilothub.ai/api/attachments/${id}/download`;
    resolverState.attachments = [
      makeRecord({ id, url: "https://minio:9000/multica/uploads/ws/shot.png", markdown_url: markdownUrl, download_url: "" }),
    ];
    getAttachmentMock.mockResolvedValue(makeRecord({ id, download_url: `/api/attachments/${id}/download` }));
    const blob = new Blob(["png-bytes"], { type: "image/png" });
    getAttachmentBlobMock.mockResolvedValue(blob);

    const { unmount } = renderWithQuery(
      <Attachment attachment={{ kind: "url", url: markdownUrl, filename: "shot.png", forceKind: "image" }} />,
    );
    await waitFor(() => expect(document.querySelector("img")?.getAttribute("src")).toBe(OBJECT_URL));
    expect(createObjectURLMock).toHaveBeenCalledTimes(1);
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "Date"] });
    unmount();
    expect(revokeObjectURLMock).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000 - 1000);
    expect(revokeObjectURLMock).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1000);
    expect(revokeObjectURLMock).toHaveBeenCalledWith(OBJECT_URL);
    vi.useRealTimers();
  });

  it("cancels pending GC on re-enter before expiry", async () => {
    getBaseUrlMock.mockReturnValue("https://multica-api.copilothub.ai");
    const id = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee";
    const markdownUrl = `https://multica-api.copilothub.ai/api/attachments/${id}/download`;
    resolverState.attachments = [makeRecord({ id, url: "https://minio:9000/a.png", markdown_url: markdownUrl, download_url: "" })];
    getAttachmentMock.mockResolvedValue(makeRecord({ id, download_url: `/api/attachments/${id}/download` }));
    const blob = new Blob(["x"], { type: "image/png" });
    getAttachmentBlobMock.mockResolvedValue(blob);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
    const first = render(
      <QueryClientProvider client={qc}>
        <Attachment attachment={{ kind: "url", url: markdownUrl, filename: "shot.png", forceKind: "image" }} />
      </QueryClientProvider>,
    );
    await waitFor(() => expect(document.querySelector("img")?.getAttribute("src")).toBe(OBJECT_URL));
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "Date"] });
    first.unmount();
    await vi.advanceTimersByTimeAsync(60 * 1000);
    vi.useRealTimers();
    const second = render(
      <QueryClientProvider client={qc}>
        <Attachment attachment={{ kind: "url", url: markdownUrl, filename: "shot.png", forceKind: "image" }} />
      </QueryClientProvider>,
    );
    await waitFor(() => expect(document.querySelector("img")?.getAttribute("src")).toBe(OBJECT_URL));
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "Date"] });
    // Pending GC was cancelled on re-enter — advancing to original expiry must NOT revoke
    await vi.advanceTimersByTimeAsync(4 * 60 * 1000);
    expect(revokeObjectURLMock).not.toHaveBeenCalled();
    second.unmount();
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000);
    expect(revokeObjectURLMock).toHaveBeenCalledWith(OBJECT_URL);
    vi.useRealTimers();
  });

  it("reuses cached blob URL on re-enter before GC expiry without new createObjectURL", async () => {
    getBaseUrlMock.mockReturnValue("https://multica-api.copilothub.ai");
    const id = "99999999-aaaa-bbbb-cccc-dddddddddddd";
    const markdownUrl = `https://multica-api.copilothub.ai/api/attachments/${id}/download`;
    resolverState.attachments = [makeRecord({ id, url: "https://minio:9000/r.png", markdown_url: markdownUrl, download_url: "" })];
    getAttachmentMock.mockResolvedValue(makeRecord({ id, download_url: `/api/attachments/${id}/download` }));
    getAttachmentBlobMock.mockResolvedValue(new Blob(["r"], { type: "image/png" }));
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
    const first = render(
      <QueryClientProvider client={qc}>
        <Attachment attachment={{ kind: "url", url: markdownUrl, filename: "r.png", forceKind: "image" }} />
      </QueryClientProvider>,
    );
    await waitFor(() => expect(document.querySelector("img")?.getAttribute("src")).toBe(OBJECT_URL));
    expect(createObjectURLMock).toHaveBeenCalledTimes(1);
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "Date"] });
    first.unmount();
    await vi.advanceTimersByTimeAsync(60 * 1000);
    createObjectURLMock.mockClear();
    const second = render(
      <QueryClientProvider client={qc}>
        <Attachment attachment={{ kind: "url", url: markdownUrl, filename: "r.png", forceKind: "image" }} />
      </QueryClientProvider>,
    );
    // Same session — cached fresh + blob URL reused synchronously (no network).
    expect(document.querySelector("img")?.getAttribute("src")).toBe(OBJECT_URL);
    expect(createObjectURLMock).not.toHaveBeenCalled();
    second.unmount();
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000);
    expect(revokeObjectURLMock).toHaveBeenCalledWith(OBJECT_URL);
    vi.useRealTimers();
  });

  it("both concurrent consumers of same id get blob URL (no fallback stick)", async () => {
    getBaseUrlMock.mockReturnValue("https://multica-api.copilothub.ai");
    const id = "cccccccc-cccc-cccc-cccc-cccccccccccc";
    const markdownUrl = `https://multica-api.copilothub.ai/api/attachments/${id}/download`;
    resolverState.attachments = [makeRecord({ id, url: "https://minio:9000/c.png", markdown_url: markdownUrl, download_url: "" })];
    getAttachmentMock.mockResolvedValue(makeRecord({ id, download_url: `/api/attachments/${id}/download` }));
    getAttachmentBlobMock.mockResolvedValue(new Blob(["c"], { type: "image/png" }));
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
    const { unmount } = render(
      <QueryClientProvider client={qc}>
        <Attachment attachment={{ kind: "url", url: markdownUrl, filename: "c.png", forceKind: "image" }} />
        <Attachment attachment={{ kind: "url", url: markdownUrl, filename: "c.png", forceKind: "image" }} />
      </QueryClientProvider>,
    );
    await waitFor(() => {
      const imgs = document.querySelectorAll("img");
      expect(imgs).toHaveLength(2);
      expect(imgs[0]?.getAttribute("src")).toBe(OBJECT_URL);
      expect(imgs[1]?.getAttribute("src")).toBe(OBJECT_URL);
    });
    expect(createObjectURLMock).toHaveBeenCalledTimes(1);
    vi.useFakeTimers();
    unmount();
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000);
    expect(revokeObjectURLMock).toHaveBeenCalledWith(OBJECT_URL);
    vi.useRealTimers();
  });

  it("does not show previous image when gallery id changes and new blob fetch fails (A→B) via rerender", async () => {
    getBaseUrlMock.mockReturnValue("https://multica-api.copilothub.ai");
    const idA = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa";
    const idB = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb";
    const urlA = `https://multica-api.copilothub.ai/api/attachments/${idA}/download`;
    const urlB = `https://multica-api.copilothub.ai/api/attachments/${idB}/download`;
    resolverState.attachments = [
      makeRecord({ id: idA, url: "https://minio:9000/a.png", markdown_url: urlA, download_url: "" }),
      makeRecord({ id: idB, url: "https://minio:9000/b.png", markdown_url: urlB, download_url: "" }),
    ];
    getAttachmentMock.mockImplementation((id: string) => Promise.resolve(makeRecord({ id, download_url: `/api/attachments/${id}/download` })));
    getAttachmentBlobMock.mockImplementation((id: string) => {
      if (id === idA) return Promise.resolve(new Blob(["a"], { type: "image/png" }));
      return Promise.reject(new Error("fetch failed"));
    });
    const { rerender, unmount } = renderWithQuery(<Attachment attachment={{ kind: "url", url: urlA, filename: "a.png", forceKind: "image" }} />);
    await waitFor(() => expect(document.querySelector("img")?.getAttribute("src")).toBe(OBJECT_URL));
    // Same instance id change — panel reuse, not unmount+mount
    rerender(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })}>
        <Attachment attachment={{ kind: "url", url: urlB, filename: "b.png", forceKind: "image" }} />
      </QueryClientProvider>,
    );
    await waitFor(() => expect(document.querySelector("img")?.getAttribute("src")).toBe(urlB));
    expect(document.querySelector("img")?.getAttribute("src")).not.toBe(OBJECT_URL);
    unmount();
  });

  it("does not reuse previous account blob URL after query cache clear on account switch", async () => {
    getBaseUrlMock.mockReturnValue("https://multica-api.copilothub.ai");
    const id = "dddddddd-eeee-ffff-aaaa-bbbbbbbbbbbb";
    const markdownUrl = `https://multica-api.copilothub.ai/api/attachments/${id}/download`;
    const record = makeRecord({ id, url: "https://minio:9000/d.png", markdown_url: markdownUrl, download_url: "" });
    resolverState.attachments = [record];
    getAttachmentMock.mockResolvedValue(record);
    getAttachmentBlobMock.mockResolvedValue(new Blob(["d"], { type: "image/png" }));
    const qcA = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
    const first = render(
      <QueryClientProvider client={qcA}>
        <Attachment attachment={{ kind: "url", url: markdownUrl, filename: "d.png", forceKind: "image" }} />
      </QueryClientProvider>,
    );
    await waitFor(() => expect(document.querySelector("img")?.getAttribute("src")).toBe(OBJECT_URL));
    first.unmount();
    expect(revokeObjectURLMock).not.toHaveBeenCalled();
    getAttachmentMock.mockImplementation(() => new Promise(() => {}));
    getAttachmentBlobMock.mockClear();
    const qcB = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
    const { unmount: unmountB } = render(
      <QueryClientProvider client={qcB}>
        <Attachment attachment={{ kind: "url", url: markdownUrl, filename: "d.png", forceKind: "image" }} />
      </QueryClientProvider>,
    );
    await new Promise((r) => setTimeout(r, 20));
    expect(document.querySelector("img")?.getAttribute("src")).toBe(markdownUrl);
    expect(document.querySelector("img")?.getAttribute("src")).not.toBe(OBJECT_URL);
    expect(getAttachmentBlobMock).not.toHaveBeenCalled();
    unmountB();
    getAttachmentMock.mockRejectedValue(new Error("forbidden"));
    const qcB2 = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
    const { unmount: unmountB2 } = render(
      <QueryClientProvider client={qcB2}>
        <Attachment attachment={{ kind: "url", url: markdownUrl, filename: "d.png", forceKind: "image" }} />
      </QueryClientProvider>,
    );
    await new Promise((r) => setTimeout(r, 20));
    expect(document.querySelector("img")?.getAttribute("src")).not.toBe(OBJECT_URL);
    expect(getAttachmentBlobMock).not.toHaveBeenCalled();
    unmountB2();
    // A's blob cache still exists (GC is 5 min) — verify revoke hasn't happened yet
    // and B still shows fallback, not A's bytes. The GC timer itself is
    // exercised by the sibling tests above.
    expect(revokeObjectURLMock).not.toHaveBeenCalled();
  });
});
