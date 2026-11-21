import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Attachment as AttachmentRecord } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";

const {
  getAttachmentMock,
  getAttachmentBlobMock,
  thumbnailBlobMock,
  originalBlobMock,
  getBaseUrlMock,
  downloadMock,
  openAtMock,
  copyImageMock,
  copyTextMock,
} = vi.hoisted(() => ({
  getAttachmentMock: vi.fn(),
  getAttachmentBlobMock: vi.fn(),
  thumbnailBlobMock: vi.fn(async () => new Blob(["thumb"], { type: "image/jpeg" })),
  originalBlobMock: vi.fn(async () => new Blob(["original"], { type: "image/png" })),
  getBaseUrlMock: vi.fn(() => ""),
  downloadMock: vi.fn(),
  openAtMock: vi.fn((_key: string) => true),
  copyImageMock: vi.fn(async (_source: string | (() => Promise<Blob>)) => true),
  copyTextMock: vi.fn(async (_text: string) => true),
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: {
    getAttachment: getAttachmentMock,
    getAttachmentBlob: getAttachmentBlobMock,
    getAttachmentThumbnailBlob: thumbnailBlobMock,
    getAttachmentOriginalBlob: originalBlobMock,
    getBaseUrl: getBaseUrlMock,
  },
}));

vi.mock("@multica/ui/lib/clipboard", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/ui/lib/clipboard")>()),
  copyImage: copyImageMock,
  copyText: copyTextMock,
}));

vi.mock("./use-download-attachment", () => ({
  useDownloadAttachment: () => downloadMock,
}));

vi.mock("./preview-sequence-context", () => ({
  usePreviewSequence: () => ({ openAt: openAtMock }),
}));

import { MessageSquare } from "@multica/ui/icons";
import { Attachment } from "./attachment";
import { ImageThumbnailsProvider } from "./image-thumbnails-context";
import { AttachmentDownloadProvider } from "./attachment-download-context";
import { useContextMenuScope } from "../common/context-menu-scope";
import { __resetInlineMediaBlobCacheForTests } from "./hooks/use-inline-media-url";

const ID = "01a1fd7f-d98e-74a7-a9f3-650a6ca7bc15";
const THUMBNAIL = `/api/attachments/${ID}/thumbnail`;
const SIGNED = "https://cdn.example.test/shot.png?Signature=s";

function record(over: Partial<AttachmentRecord> = {}): AttachmentRecord {
  return {
    id: ID,
    workspace_id: "ws-1",
    issue_id: "issue-1",
    comment_id: "c-1",
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "agent",
    uploader_id: "lambda",
    filename: "shot.png",
    url: "https://cdn.example.test/shot.png",
    download_url: SIGNED,
    markdown_url: `https://cdn.example.test/api/attachments/${ID}/download`,
    content_type: "image/png",
    size_bytes: 6 * 1024 * 1024,
    width: 5472,
    height: 3072,
    thumbnail_url: THUMBNAIL,
    created_at: "2026-11-20T10:00:00Z",
    ...over,
  };
}

function CommentScope({ children }: { children: ReactNode }) {
  const ref = useContextMenuScope(() => ({
    label: "Comment actions",
    icon: <MessageSquare />,
    groups: [[{ key: "reply", icon: null, label: "Reply", onSelect: vi.fn() }]],
  }));
  return <div ref={ref}>{children}</div>;
}

function renderImage(
  attachment: AttachmentRecord,
  { thumbnails = true, editable = false }: { thumbnails?: boolean; editable?: boolean } = {},
) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <ImageThumbnailsProvider enabled={thumbnails}>
        <CommentScope>
          <Attachment attachment={{ kind: "record", attachment }} layout="card" editable={editable} />
        </CommentScope>
      </ImageThumbnailsProvider>
    </QueryClientProvider>,
  );
}

function sources() {
  return Array.from(document.querySelectorAll("img"), (img) => img.getAttribute("src"));
}

function image() {
  return document.querySelector<HTMLImageElement>("img")!;
}

beforeEach(() => {
  vi.clearAllMocks();
  getBaseUrlMock.mockReturnValue("");
  Object.defineProperty(URL, "createObjectURL", {
    configurable: true,
    value: vi.fn(() => "blob:thumbnail"),
  });
  Object.defineProperty(URL, "revokeObjectURL", { configurable: true, value: vi.fn() });
});

afterEach(() => {
  __resetInlineMediaBlobCacheForTests();
  Reflect.deleteProperty(navigator, "clipboard");
  vi.unstubAllGlobals();
});

describe("comment images under the thumbnails preference", () => {
  it("loads the server thumbnail, never the original", () => {
    renderImage(record());
    expect(sources()).toEqual([THUMBNAIL]);
    expect(getAttachmentBlobMock).not.toHaveBeenCalled();
    // The box keeps the original's size, so the layout is unchanged.
    expect(image()).toHaveAttribute("width", "5472");
    expect(image()).toHaveAttribute("height", "3072");
  });

  it("loads the original under Full size, in an editor, and without a thumbnail", () => {
    const { unmount } = renderImage(record(), { thumbnails: false });
    expect(sources()).toEqual([SIGNED]);
    unmount();

    const editing = renderImage(record(), { editable: true });
    expect(sources()).toEqual([SIGNED]);
    editing.unmount();

    renderImage(record({ thumbnail_url: undefined }));
    expect(sources()).toEqual([SIGNED]);
  });

  it("falls back to the original when the thumbnail fails to load", () => {
    renderImage(record());
    fireEvent.error(image());
    expect(sources()).toEqual([SIGNED]);
  });

  it("on desktop, fetches the thumbnail's bytes and nothing for the original", async () => {
    getBaseUrlMock.mockReturnValue("https://api.example.test");
    // Proxy storage: the original would be re-signed, then fetched as bytes.
    getAttachmentMock.mockResolvedValue(record({ download_url: "" }));
    renderImage(
      record({ download_url: "", markdown_url: `/api/attachments/${ID}/download`, url: "" }),
    );
    await waitFor(() => expect(sources()).toEqual(["blob:thumbnail"]));
    expect(thumbnailBlobMock).toHaveBeenCalledWith(ID, expect.anything());
    expect(getAttachmentMock).not.toHaveBeenCalled();
    expect(getAttachmentBlobMock).not.toHaveBeenCalled();
  });

  it("on desktop, re-signs the original once its thumbnail fails", async () => {
    getBaseUrlMock.mockReturnValue("https://api.example.test");
    thumbnailBlobMock.mockRejectedValueOnce(new Error("404"));
    getAttachmentMock.mockResolvedValue(record());
    renderImage(
      record({ download_url: "", markdown_url: `/api/attachments/${ID}/download`, url: "" }),
    );
    await waitFor(() => expect(sources()).toEqual([SIGNED]));
    expect(getAttachmentMock).toHaveBeenCalledTimes(1);
  });

  it("resolves a body image to its attachment and loads the thumbnail", () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
    const attachment = record();
    renderWithI18n(
      <QueryClientProvider client={qc}>
        <ImageThumbnailsProvider enabled>
          <AttachmentDownloadProvider attachments={[attachment]}>
            <Attachment
              attachment={{ kind: "url", url: attachment.markdown_url!, filename: "", forceKind: "image" }}
            />
          </AttachmentDownloadProvider>
        </ImageThumbnailsProvider>
      </QueryClientProvider>,
    );
    expect(sources()).toEqual([THUMBNAIL]);
  });
});

describe("a comment thumbnail's right-click menu", () => {
  async function openMenu() {
    // Handled, so neither the browser's nor the desktop host's image menu,
    // which would act on the thumbnail, opens.
    expect(fireEvent.contextMenu(image())).toBe(false);
    return screen.findByRole("menu");
  }

  async function menuItems() {
    await openMenu();
    return (await screen.findAllByRole("menuitem")).map((item) => item.textContent);
  }

  it("acts on the original and keeps the comment's actions", async () => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { write: vi.fn() },
    });
    vi.stubGlobal("ClipboardItem", class {});
    renderImage(record());

    expect(await menuItems()).toEqual([
      "View image",
      "Copy image",
      "Download",
      "Copy link",
      "Comment actions",
    ]);

    fireEvent.click(screen.getByRole("menuitem", { name: "Copy image" }));
    const load = copyImageMock.mock.calls[0]![0] as () => Promise<Blob>;
    expect(await (await load()).text()).toBe("original");
    expect(originalBlobMock).toHaveBeenCalledWith(ID);

    await openMenu();
    fireEvent.click(await screen.findByRole("menuitem", { name: "Download" }));
    expect(downloadMock).toHaveBeenCalledWith(ID);

    await openMenu();
    fireEvent.click(await screen.findByRole("menuitem", { name: "View image" }));
    expect(openAtMock).toHaveBeenCalledWith(ID);

    await openMenu();
    fireEvent.click(await screen.findByRole("menuitem", { name: "Copy link" }));
    expect(copyTextMock).toHaveBeenCalledWith(SIGNED);
  });

  // Plain http:// self-hosting has no async Clipboard API.
  it("offers no Copy image without the async Clipboard API", async () => {
    expect(navigator.clipboard?.write).toBeUndefined();
    renderImage(record());
    expect(await menuItems()).toEqual(["View image", "Download", "Copy link", "Comment actions"]);
  });

  it("leaves an original's right-click to the host menu", () => {
    renderImage(record(), { thumbnails: false });
    expect(fireEvent.contextMenu(image())).toBe(true);
    expect(screen.queryByRole("menu")).toBeNull();
  });
});
