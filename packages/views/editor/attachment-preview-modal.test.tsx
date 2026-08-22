import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render as rtlRender, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactElement } from "react";
import type { Attachment } from "@multica/core/types";

const openExternalMock = vi.hoisted(() => vi.fn());

vi.mock("../platform", () => ({
  openExternal: openExternalMock,
}));

// vi.hoisted: factories run before module evaluation, letting us name mocks
// referenced from inside vi.mock factories below. The Error classes must be
// hoisted too because vi.mock is itself hoisted above the top-level `class`
// declarations.
const {
  getAttachmentMock,
  getAttachmentBlobMock,
  getAttachmentTextContentMock,
  downloadMock,
  getBaseUrlMock,
  FakePreviewTooLargeError,
  FakePreviewUnsupportedError,
} = vi.hoisted(() => {
  class FakePreviewTooLargeError extends Error {
    constructor() {
      super("too large");
      this.name = "PreviewTooLargeError";
    }
  }
  class FakePreviewUnsupportedError extends Error {
    constructor() {
      super("unsupported");
      this.name = "PreviewUnsupportedError";
    }
  }
  return {
    getAttachmentMock: vi.fn(),
    getAttachmentBlobMock: vi.fn(),
    getAttachmentTextContentMock: vi.fn(),
    downloadMock: vi.fn(),
    // Default to the web shape (empty base, same-origin). Tests covering
    // the desktop-renderer / standalone-shell case override per-test.
    getBaseUrlMock: vi.fn(() => ""),
    FakePreviewTooLargeError,
    FakePreviewUnsupportedError,
  };
});

vi.mock("@multica/core/api", () => ({
  api: {
    getAttachment: getAttachmentMock,
    getAttachmentBlob: getAttachmentBlobMock,
    getAttachmentTextContent: getAttachmentTextContentMock,
    getBaseUrl: getBaseUrlMock,
  },
  PreviewTooLargeError: FakePreviewTooLargeError,
  PreviewUnsupportedError: FakePreviewUnsupportedError,
}));

vi.mock("./use-download-attachment", () => ({
  useDownloadAttachment: () => downloadMock,
}));

// Module-level flags toggled per-test: simulate desktop (openInNewTab
// adapter present) vs web (omitted), and the no-slug case where the
// modal sits outside a workspace route.
const { openInNewTabMock, getShareableUrlMock, navState, slugState } =
  vi.hoisted(() => ({
    openInNewTabMock: vi.fn(),
    getShareableUrlMock: vi.fn((p: string) => `https://app.example${p}`),
    navState: { hasOpenInNewTab: true },
    slugState: { value: "acme" as string | null },
  }));

vi.mock("../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/issues",
    searchParams: new URLSearchParams(),
    ...(navState.hasOpenInNewTab ? { openInNewTab: openInNewTabMock } : {}),
    getShareableUrl: getShareableUrlMock,
  }),
}));

vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useWorkspaceSlug: () => slugState.value,
  };
});

// ReadonlyContent has a heavy import surface (lowlight + KaTeX + Mermaid).
// Stub it so the markdown dispatch test only verifies wiring.
vi.mock("./readonly-content", () => ({
  ReadonlyContent: ({ content }: { content: string }) => (
    <div data-testid="readonly-content">{content}</div>
  ),
}));

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (sel: (s: Record<string, Record<string, string>>) => string) =>
      sel({
        image: {
          download: "Download",
          canvas_label: "Image canvas",
        },
        canvas: {
          zoom_in: "Zoom in",
          zoom_out: "Zoom out",
          zoom_fit: "Fit to view",
          zoom_actual: "Actual size",
        },
        attachment: {
          preview: "Preview",
          preview_loading: "Loading preview…",
          preview_failed: "Couldn't load preview",
          preview_too_large: "File is too large to preview. Please download.",
          preview_unsupported: "This file type can't be previewed.",
          close: "Close",
          download_failed: "",
          open_in_new_tab: "Open in new tab",
        },
      }),
  }),
}));

import {
  AttachmentPreviewModal,
  useAttachmentPreview,
} from "./attachment-preview-modal";
import { renderHook, act as hookAct } from "@testing-library/react";

// Fresh QueryClient per render — no retries (preview errors are typed,
// not transient) and no caching across tests so each scenario is hermetic.
function render(ui: ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return rtlRender(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

function makeAttachment(overrides: Partial<Attachment> = {}): Attachment {
  return {
    id: "att-1",
    workspace_id: "ws-1",
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "u-1",
    filename: "test.bin",
    url: "https://cdn.example.test/att-1.bin",
    download_url: "https://cdn.example.test/att-1.bin?Signature=s",
    markdown_url: "https://cdn.example.test/api/attachments/att-1/download",
    content_type: "application/octet-stream",
    size_bytes: 0,
    created_at: "2026-05-13T00:00:00Z",
    ...overrides,
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function ClosablePreview({ attachment }: { attachment: Attachment }) {
  const [open, setOpen] = useState(true);
  return (
    <AttachmentPreviewModal
      source={{ kind: "full", attachment }}
      open={open}
      onClose={() => setOpen(false)}
    />
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  navState.hasOpenInNewTab = true;
  slugState.value = "acme";
  // Default to web's same-origin empty base so existing absolute-URL tests
  // remain unaffected by the relative-URL resolution added in normalize().
  getBaseUrlMock.mockReturnValue("");
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("AttachmentPreviewModal — dispatch", () => {
  it("renders an <img> centered in the modal for image content types", () => {
    const att = makeAttachment({ filename: "shot.png", content_type: "image/png" });
    render(<AttachmentPreviewModal source={{ kind: "full", attachment: att }} open onClose={() => {}} />);
    const img = document.querySelector("img");
    expect(img).toBeTruthy();
    expect(img?.getAttribute("src")).toBe(att.download_url);
    expect(img?.getAttribute("alt")).toBe(att.filename);
  });

  it("falls back to durable media URLs when a full attachment has no download_url", () => {
    const att = makeAttachment({
      filename: "shot.png",
      content_type: "image/png",
      download_url: "",
      markdown_url: "https://api.example.test/api/attachments/att-1/download",
      url: "https://cdn.example.test/att-1.png?Signature=old",
    });
    render(
      <AttachmentPreviewModal
        source={{ kind: "full", attachment: att }}
        open
        onClose={() => {}}
      />,
    );
    const img = document.querySelector("img");
    expect(img?.getAttribute("src")).toBe(att.markdown_url);
    expect(img?.getAttribute("src")).not.toContain("Signature=");
  });

  it("renders an <img> from a URL-only source for image filenames", () => {
    const url = "https://cdn.example.test/orphan.png?Signature=s";
    render(
      <AttachmentPreviewModal
        source={{ kind: "url", url, filename: "orphan.png" }}
        open
        onClose={() => {}}
      />,
    );
    const img = document.querySelector("img");
    expect(img?.getAttribute("src")).toBe(url);
  });

  it("renders a PDF iframe pointing at the signed download URL", () => {
    const att = makeAttachment({ filename: "manual.pdf", content_type: "application/pdf" });
    render(<AttachmentPreviewModal source={{ kind: "full", attachment: att }} open onClose={() => {}} />);
    const iframe = document.querySelector("iframe");
    expect(iframe).toBeTruthy();
    expect(iframe?.getAttribute("src")).toBe(att.download_url);
  });

  it("renders a <video> for video/* content types", () => {
    const att = makeAttachment({ filename: "clip.mp4", content_type: "video/mp4" });
    render(<AttachmentPreviewModal source={{ kind: "full", attachment: att }} open onClose={() => {}} />);
    const video = document.querySelector("video");
    expect(video).toBeTruthy();
    expect(video?.getAttribute("src")).toBe(att.download_url);
  });

  it("renders an <audio> for audio/* content types", () => {
    const att = makeAttachment({ filename: "note.mp3", content_type: "audio/mpeg" });
    render(<AttachmentPreviewModal source={{ kind: "full", attachment: att }} open onClose={() => {}} />);
    const audio = document.querySelector("audio");
    expect(audio).toBeTruthy();
  });

  it("fetches text and hands it to ReadonlyContent for Markdown", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "# heading\n\nbody\n",
      originalContentType: "text/markdown",
    });
    const att = makeAttachment({ filename: "README.md", content_type: "text/markdown" });
    render(<AttachmentPreviewModal source={{ kind: "full", attachment: att }} open onClose={() => {}} />);

    expect(getAttachmentTextContentMock).toHaveBeenCalledWith("att-1");

    await waitFor(() => {
      expect(screen.getByTestId("readonly-content")).toBeTruthy();
    });
    expect(screen.getByTestId("readonly-content").textContent).toContain("# heading");
  });

  it("renders an iframe with srcdoc + sandbox='allow-scripts' for HTML", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>hi</p>",
      originalContentType: "text/html",
    });
    const att = makeAttachment({ filename: "page.html", content_type: "text/html" });
    render(<AttachmentPreviewModal source={{ kind: "full", attachment: att }} open onClose={() => {}} />);

    await waitFor(() => {
      const frame = document.querySelector("iframe[sandbox]") as HTMLIFrameElement | null;
      expect(frame).toBeTruthy();
      // `allow-scripts` is required so vanilla-JS chart libraries render
      // (MUL-2330). The combination with `allow-same-origin` would defeat
      // the sandbox, so this assertion must stay exact.
      expect(frame?.getAttribute("sandbox")).toBe("allow-scripts");
      // srcdoc carries the original HTML plus the fragment-nav shim
      // appended at the end (see utils/iframe-fragment-nav.ts).
      const srcdoc = frame?.getAttribute("srcdoc") ?? "";
      expect(srcdoc.startsWith("<p>hi</p>")).toBe(true);
      expect(srcdoc).toContain("scrollIntoView");
    });
  });

  it("renders a code block with lowlight for source files", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "package main\n",
      originalContentType: "text/plain",
    });
    const att = makeAttachment({ filename: "main.go", content_type: "text/plain" });
    render(<AttachmentPreviewModal source={{ kind: "full", attachment: att }} open onClose={() => {}} />);

    await waitFor(() => {
      const code = document.querySelector("code.hljs");
      expect(code).toBeTruthy();
      expect(code?.className).toContain("language-go");
    });
  });

  it("shows unsupported fallback when no PreviewKind matches", () => {
    const att = makeAttachment({ filename: "blob.zip", content_type: "application/zip" });
    render(<AttachmentPreviewModal source={{ kind: "full", attachment: att }} open onClose={() => {}} />);
    expect(screen.getByText("This file type can't be previewed.")).toBeTruthy();
  });
});

describe("AttachmentPreviewModal — server-relative download_url resolution (MUL-2976)", () => {
  // The unified `/api/attachments/{id}/download` endpoint returns a
  // server-relative path on non-CloudFront deployments. The web app keeps
  // working same-origin because `apiBaseUrl=""`, but the desktop renderer
  // is loaded from `app://` / file: / dev-server origin and needs the
  // absolute URL — otherwise `<img src>`, `<iframe src>`, `<video src>`
  // hit the shell origin and fail.
  it("prefixes the configured API base for image previews when download_url is server-relative", () => {
    getBaseUrlMock.mockReturnValue("https://api.example.test");
    const att = makeAttachment({
      filename: "shot.png",
      content_type: "image/png",
      download_url: "/api/attachments/att-1/download",
    });
    render(
      <AttachmentPreviewModal
        source={{ kind: "full", attachment: att }}
        open
        onClose={() => {}}
      />,
    );
    const img = document.querySelector("img");
    expect(img?.getAttribute("src")).toBe(
      "https://api.example.test/api/attachments/att-1/download",
    );
  });

  it("prefixes the configured API base for PDF previews when download_url is server-relative", () => {
    getBaseUrlMock.mockReturnValue("https://api.example.test");
    const att = makeAttachment({
      filename: "manual.pdf",
      content_type: "application/pdf",
      download_url: "/api/attachments/att-1/download",
    });
    render(
      <AttachmentPreviewModal
        source={{ kind: "full", attachment: att }}
        open
        onClose={() => {}}
      />,
    );
    const iframe = document.querySelector("iframe");
    expect(iframe?.getAttribute("src")).toBe(
      "https://api.example.test/api/attachments/att-1/download",
    );
  });

  it("keeps a same-origin relative URL untouched when the configured base is empty (web)", () => {
    // Default web shape — empty base. Browser resolves the relative path
    // against the document origin, no prefix needed.
    const att = makeAttachment({
      filename: "shot.png",
      content_type: "image/png",
      download_url: "/api/attachments/att-1/download",
    });
    render(
      <AttachmentPreviewModal
        source={{ kind: "full", attachment: att }}
        open
        onClose={() => {}}
      />,
    );
    const img = document.querySelector("img");
    expect(img?.getAttribute("src")).toBe("/api/attachments/att-1/download");
  });

  it("trims a trailing slash on the configured base when joining a relative URL", () => {
    getBaseUrlMock.mockReturnValue("https://api.example.test/");
    const att = makeAttachment({
      filename: "shot.png",
      content_type: "image/png",
      download_url: "/api/attachments/att-1/download",
    });
    render(
      <AttachmentPreviewModal
        source={{ kind: "full", attachment: att }}
        open
        onClose={() => {}}
      />,
    );
    const img = document.querySelector("img");
    expect(img?.getAttribute("src")).toBe(
      "https://api.example.test/api/attachments/att-1/download",
    );
  });

  it("passes an already-absolute CloudFront/presigned download_url through unchanged", () => {
    getBaseUrlMock.mockReturnValue("https://api.example.test");
    const att = makeAttachment({
      filename: "shot.png",
      content_type: "image/png",
      download_url: "https://cdn.example.test/att-1.png?Signature=s",
    });
    render(
      <AttachmentPreviewModal
        source={{ kind: "full", attachment: att }}
        open
        onClose={() => {}}
      />,
    );
    const img = document.querySelector("img");
    expect(img?.getAttribute("src")).toBe(
      "https://cdn.example.test/att-1.png?Signature=s",
    );
  });
});

describe("AttachmentPreviewModal — pending image URL upgrade", () => {
  const attachmentId = "11111111-1111-4111-8111-111111111111";
  const apiUrl = `https://api.example.test/api/attachments/${attachmentId}/download`;
  const blobUrl = "blob:https://app.example/preview-att-1";
  const originalCreateObjectURL = Object.getOwnPropertyDescriptor(
    URL,
    "createObjectURL",
  );
  const originalRevokeObjectURL = Object.getOwnPropertyDescriptor(
    URL,
    "revokeObjectURL",
  );
  const originalDecode = Object.getOwnPropertyDescriptor(
    HTMLImageElement.prototype,
    "decode",
  );
  let probedUrls: string[];

  beforeEach(() => {
    probedUrls = [];
    getBaseUrlMock.mockReturnValue("https://api.example.test");
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      value: vi.fn(() => blobUrl),
    });
    Object.defineProperty(URL, "revokeObjectURL", {
      configurable: true,
      value: vi.fn(),
    });
    Object.defineProperty(HTMLImageElement.prototype, "decode", {
      configurable: true,
      value: vi.fn(function (this: HTMLImageElement) {
        probedUrls.push(this.src);
        return this.src === apiUrl
          ? Promise.reject(new Error("401"))
          : Promise.resolve();
      }),
    });
  });

  afterEach(() => {
    if (originalCreateObjectURL) {
      Object.defineProperty(URL, "createObjectURL", originalCreateObjectURL);
    } else {
      Reflect.deleteProperty(URL, "createObjectURL");
    }
    if (originalRevokeObjectURL) {
      Object.defineProperty(URL, "revokeObjectURL", originalRevokeObjectURL);
    } else {
      Reflect.deleteProperty(URL, "revokeObjectURL");
    }
    if (originalDecode) {
      Object.defineProperty(
        HTMLImageElement.prototype,
        "decode",
        originalDecode,
      );
    } else {
      Reflect.deleteProperty(HTMLImageElement.prototype, "decode");
    }
  });

  function proxyAttachment(): Attachment {
    return makeAttachment({
      id: attachmentId,
      filename: "cold-cache.png",
      content_type: "image/png",
      download_url: `/api/attachments/${attachmentId}/download`,
      markdown_url: `/api/attachments/${attachmentId}/download`,
      url: "/uploads/cold-cache.png",
    });
  }

  it("waits for the authenticated blob fallback before probing or rendering the target", async () => {
    const metadata = deferred<Attachment>();
    const bytes = deferred<Blob>();
    const onImageError = vi.fn();
    getAttachmentMock.mockReturnValue(metadata.promise);
    getAttachmentBlobMock.mockReturnValue(bytes.promise);

    render(
      <AttachmentPreviewModal
        source={{ kind: "full", attachment: proxyAttachment() }}
        open
        onClose={() => {}}
        onImageError={onImageError}
      />,
    );

    expect(getAttachmentMock).toHaveBeenCalledWith(attachmentId);
    expect(probedUrls).toEqual([]);
    expect(document.querySelector(`img[src="${apiUrl}"]`)).toBeNull();
    expect(onImageError).not.toHaveBeenCalled();

    metadata.resolve(proxyAttachment());
    await waitFor(() => {
      expect(getAttachmentBlobMock).toHaveBeenCalledWith(attachmentId);
    });
    expect(probedUrls).toEqual([]);
    expect(onImageError).not.toHaveBeenCalled();

    bytes.resolve(new Blob(["png-bytes"], { type: "image/png" }));
    await waitFor(() => {
      expect(probedUrls).toEqual([blobUrl]);
      expect(document.querySelector("img")?.getAttribute("src")).toBe(blobUrl);
    });
    expect(onImageError).not.toHaveBeenCalled();
  });

  it("reports a real failure after the blob fallback has definitively failed", async () => {
    const onImageError = vi.fn();
    getAttachmentMock.mockResolvedValue(proxyAttachment());
    getAttachmentBlobMock.mockRejectedValue(new Error("blob fetch failed"));

    render(
      <AttachmentPreviewModal
        source={{ kind: "full", attachment: proxyAttachment() }}
        open
        onClose={() => {}}
        onImageError={onImageError}
      />,
    );

    await waitFor(() => {
      expect(onImageError).toHaveBeenCalledTimes(1);
    });
    expect(probedUrls).toEqual([apiUrl]);
  });

  it("probes an already-presigned URL immediately without metadata refresh", async () => {
    const signedUrl =
      `https://storage.example.test/${attachmentId}.png?X-Amz-Signature=signed`;

    render(
      <AttachmentPreviewModal
        source={{
          kind: "full",
          attachment: makeAttachment({
            id: attachmentId,
            filename: "presigned.png",
            content_type: "image/png",
            download_url: signedUrl,
          }),
        }}
        open
        onClose={() => {}}
      />,
    );

    await waitFor(() => {
      expect(probedUrls).toEqual([signedUrl]);
    });
    expect(document.querySelector("img")?.getAttribute("src")).toBe(signedUrl);
    expect(getAttachmentMock).not.toHaveBeenCalled();
  });

  it("keeps same-origin web previews on the existing immediate path", async () => {
    getBaseUrlMock.mockReturnValue("");
    const relativeUrl = `/api/attachments/${attachmentId}/download`;

    render(
      <AttachmentPreviewModal
        source={{ kind: "full", attachment: proxyAttachment() }}
        open
        onClose={() => {}}
      />,
    );

    await waitFor(() => {
      expect(probedUrls).toHaveLength(1);
    });
    expect(probedUrls[0]?.endsWith(relativeUrl)).toBe(true);
    expect(document.querySelector("img")?.getAttribute("src")).toBe(relativeUrl);
    expect(getAttachmentMock).not.toHaveBeenCalled();
  });
});

describe("AttachmentPreviewModal — error states", () => {
  it("shows the too-large fallback on PreviewTooLargeError", async () => {
    getAttachmentTextContentMock.mockRejectedValueOnce(new FakePreviewTooLargeError());
    const att = makeAttachment({ filename: "huge.txt", content_type: "text/plain" });
    render(<AttachmentPreviewModal source={{ kind: "full", attachment: att }} open onClose={() => {}} />);
    await waitFor(() => {
      expect(screen.getByText("File is too large to preview. Please download.")).toBeTruthy();
    });
  });

  it("shows the unsupported fallback on PreviewUnsupportedError (server/client drift)", async () => {
    getAttachmentTextContentMock.mockRejectedValueOnce(new FakePreviewUnsupportedError());
    const att = makeAttachment({ filename: "weird.txt", content_type: "text/plain" });
    render(<AttachmentPreviewModal source={{ kind: "full", attachment: att }} open onClose={() => {}} />);
    await waitFor(() => {
      expect(screen.getByText("This file type can't be previewed.")).toBeTruthy();
    });
  });

  it("shows the generic failed fallback on a transport error", async () => {
    getAttachmentTextContentMock.mockRejectedValueOnce(new Error("network down"));
    const att = makeAttachment({ filename: "x.md", content_type: "text/markdown" });
    render(<AttachmentPreviewModal source={{ kind: "full", attachment: att }} open onClose={() => {}} />);
    await waitFor(() => {
      expect(screen.getByText("Couldn't load preview")).toBeTruthy();
    });
  });
});

describe("AttachmentPreviewModal — controls", () => {
  it("ESC closes the modal", () => {
    const onClose = vi.fn();
    const att = makeAttachment({ filename: "manual.pdf", content_type: "application/pdf" });
    render(<AttachmentPreviewModal source={{ kind: "full", attachment: att }} open onClose={onClose} />);
    act(() => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    });
    expect(onClose).toHaveBeenCalled();
  });

  it("Download button invokes useDownloadAttachment with the attachment id", () => {
    const att = makeAttachment({ filename: "manual.pdf", content_type: "application/pdf" });
    render(<AttachmentPreviewModal source={{ kind: "full", attachment: att }} open onClose={() => {}} />);
    // Two Download CTAs may exist (header + unsupported fallback). The header
    // button is always present, look it up by aria-label/title.
    const buttons = screen.getAllByTitle("Download");
    expect(buttons.length).toBeGreaterThan(0);
    fireEvent.click(buttons[0]!);
    expect(downloadMock).toHaveBeenCalledWith("att-1");
  });

  it("clicking the backdrop closes the modal", () => {
    const onClose = vi.fn();
    const att = makeAttachment({ filename: "manual.pdf", content_type: "application/pdf" });
    render(<AttachmentPreviewModal source={{ kind: "full", attachment: att }} open onClose={onClose} />);
    const dialog = screen.getByRole("dialog");
    fireEvent.click(dialog);
    expect(onClose).toHaveBeenCalled();
  });

  it("keeps the dialog mounted for its exit and then removes it", async () => {
    const att = makeAttachment({
      filename: "manual.pdf",
      content_type: "application/pdf",
    });
    render(<ClosablePreview attachment={att} />);

    fireEvent.click(screen.getByTitle("Close"));

    expect(screen.getByRole("dialog")).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });
});

describe("AttachmentPreviewModal — URL-only source", () => {
  it("renders a PDF iframe from the URL when no attachment record is available", () => {
    const url = "https://cdn.example.test/orphan.pdf?Signature=s";
    render(
      <AttachmentPreviewModal
        source={{ kind: "url", url, filename: "orphan.pdf" }}
        open
        onClose={() => {}}
      />,
    );
    const iframe = document.querySelector("iframe");
    expect(iframe).toBeTruthy();
    expect(iframe?.getAttribute("src")).toBe(url);
  });

  it("renders <video> from the URL when no attachment record is available", () => {
    const url = "https://cdn.example.test/clip.mp4?Signature=s";
    render(
      <AttachmentPreviewModal
        source={{ kind: "url", url, filename: "clip.mp4" }}
        open
        onClose={() => {}}
      />,
    );
    const video = document.querySelector("video");
    expect(video?.getAttribute("src")).toBe(url);
  });

  it("falls back to unsupported when a text kind is forced through a URL source", () => {
    // The tryOpen gate normally prevents this; direct mount tests the
    // defensive branch inside PreviewContent.
    render(
      <AttachmentPreviewModal
        source={{ kind: "url", url: "https://x/y.md", filename: "y.md" }}
        open
        onClose={() => {}}
      />,
    );
    expect(screen.getByText("This file type can't be previewed.")).toBeTruthy();
  });

  it("Download button opens the raw URL externally when no attachment id is available", () => {
    const url = "https://cdn.example.test/orphan.pdf?Signature=s";
    render(
      <AttachmentPreviewModal
        source={{ kind: "url", url, filename: "orphan.pdf" }}
        open
        onClose={() => {}}
      />,
    );
    const button = screen.getAllByTitle("Download")[0]!;
    fireEvent.click(button);
    expect(openExternalMock).toHaveBeenCalledWith(url);
    expect(downloadMock).not.toHaveBeenCalled();
  });
});

describe("AttachmentPreviewModal — open-in-new-tab (HTML only)", () => {
  it("renders the open-in-new-tab button in the header for HTML attachments", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>hi</p>",
      originalContentType: "text/html",
    });
    const att = makeAttachment({
      filename: "report.html",
      content_type: "text/html",
    });
    render(
      <AttachmentPreviewModal
        source={{ kind: "full", attachment: att }}
        open
        onClose={() => {}}
      />,
    );
    expect(screen.getByTitle("Open in new tab")).toBeTruthy();
  });

  it("invokes navigation.openInNewTab with the preview path and closes the modal (desktop)", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>hi</p>",
      originalContentType: "text/html",
    });
    const att = makeAttachment({
      filename: "report.html",
      content_type: "text/html",
    });
    const onClose = vi.fn();
    render(
      <AttachmentPreviewModal
        source={{ kind: "full", attachment: att }}
        open
        onClose={onClose}
      />,
    );
    fireEvent.click(screen.getByTitle("Open in new tab"));
    expect(openInNewTabMock).toHaveBeenCalledWith(
      "/acme/attachments/att-1/preview?name=report.html",
      "report.html",
      { activate: true },
    );
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("falls back to window.open against the shareable URL and closes the modal (web)", async () => {
    navState.hasOpenInNewTab = false;
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>hi</p>",
      originalContentType: "text/html",
    });
    const windowOpenSpy = vi
      .spyOn(window, "open")
      .mockImplementation(() => null);
    const att = makeAttachment({
      filename: "report.html",
      content_type: "text/html",
    });
    const onClose = vi.fn();
    render(
      <AttachmentPreviewModal
        source={{ kind: "full", attachment: att }}
        open
        onClose={onClose}
      />,
    );
    fireEvent.click(screen.getByTitle("Open in new tab"));
    expect(openInNewTabMock).not.toHaveBeenCalled();
    expect(windowOpenSpy).toHaveBeenCalledWith(
      "https://app.example/acme/attachments/att-1/preview?name=report.html",
      "_blank",
      "noopener,noreferrer",
    );
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("does not render the new-tab button for non-HTML kinds", () => {
    const att = makeAttachment({
      filename: "manual.pdf",
      content_type: "application/pdf",
    });
    render(
      <AttachmentPreviewModal
        source={{ kind: "full", attachment: att }}
        open
        onClose={() => {}}
      />,
    );
    expect(screen.queryByTitle("Open in new tab")).toBeNull();
  });

  it("does not render the new-tab button when there is no workspace slug", async () => {
    slugState.value = null;
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>hi</p>",
      originalContentType: "text/html",
    });
    const att = makeAttachment({
      filename: "report.html",
      content_type: "text/html",
    });
    render(
      <AttachmentPreviewModal
        source={{ kind: "full", attachment: att }}
        open
        onClose={() => {}}
      />,
    );
    expect(screen.queryByTitle("Open in new tab")).toBeNull();
  });
});

describe("useAttachmentPreview — tryOpen gate", () => {
  it("accepts a full attachment for a media kind", () => {
    const { result } = renderHook(() => useAttachmentPreview());
    const att = makeAttachment({ filename: "x.pdf", content_type: "application/pdf" });
    let opened = false;
    hookAct(() => {
      opened = result.current.tryOpen({ kind: "full", attachment: att });
    });
    expect(opened).toBe(true);
  });

  it("accepts a URL source for a media kind", () => {
    const { result } = renderHook(() => useAttachmentPreview());
    let opened = false;
    hookAct(() => {
      opened = result.current.tryOpen({
        kind: "url",
        url: "https://x/y.pdf",
        filename: "y.pdf",
      });
    });
    expect(opened).toBe(true);
  });

  it("rejects a URL source for a text kind — /content proxy needs an id", () => {
    const { result } = renderHook(() => useAttachmentPreview());
    let opened = true;
    hookAct(() => {
      opened = result.current.tryOpen({
        kind: "url",
        url: "https://x/y.md",
        filename: "y.md",
      });
    });
    expect(opened).toBe(false);
  });

  it("rejects a source whose filename isn't a previewable type", () => {
    const { result } = renderHook(() => useAttachmentPreview());
    let opened = true;
    hookAct(() => {
      opened = result.current.tryOpen({
        kind: "url",
        url: "https://x/y.zip",
        filename: "y.zip",
      });
    });
    expect(opened).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Image zoom canvas
// ---------------------------------------------------------------------------

// jsdom has no layout and never decodes images, so both inputs the canvas
// needs — its own size and the image's intrinsic size — have to be pinned.
const CANVAS_VIEWPORT = { width: 800, height: 400 };

function stubCanvasViewport() {
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(
    CANVAS_VIEWPORT.width,
  );
  vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(
    CANVAS_VIEWPORT.height,
  );
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({
    bottom: CANVAS_VIEWPORT.height,
    height: CANVAS_VIEWPORT.height,
    left: 0,
    right: CANVAS_VIEWPORT.width,
    top: 0,
    width: CANVAS_VIEWPORT.width,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  });
}

function stubNaturalSize(size: { width: number; height: number }) {
  vi.spyOn(HTMLImageElement.prototype, "naturalWidth", "get").mockReturnValue(
    size.width,
  );
  vi.spyOn(HTMLImageElement.prototype, "naturalHeight", "get").mockReturnValue(
    size.height,
  );
}

function imageAttachment(): Attachment {
  return makeAttachment({ filename: "shot.png", content_type: "image/png" });
}

function zoomCanvas(): HTMLElement {
  return screen.getByRole("application");
}

function canvasContent(): HTMLElement {
  return document.querySelector<HTMLElement>(".zoom-canvas-content")!;
}

function currentScale(): number {
  const match = /scale\(([\d.]+)\)/.exec(canvasContent().style.transform);
  return Number.parseFloat(match![1]!);
}

function renderImagePreview(attachment: Attachment = imageAttachment()) {
  return render(
    <AttachmentPreviewModal
      source={{ kind: "full", attachment }}
      open
      onClose={() => {}}
    />,
  );
}

describe("AttachmentPreviewModal — image zoom", () => {
  beforeEach(() => {
    stubCanvasViewport();
  });

  it("fits an oversized image to the canvas on open", () => {
    stubNaturalSize({ width: 1600, height: 800 });
    renderImagePreview();

    // 1600x800 in an 800x400 canvas fits at exactly 50%.
    expect(currentScale()).toBeCloseTo(0.5, 5);
    expect(screen.getByText("50%")).toBeInTheDocument();
  });

  it("opens a small image at 100% instead of magnifying it", () => {
    stubNaturalSize({ width: 200, height: 100 });
    renderImagePreview();

    expect(currentScale()).toBe(1);
  });

  it("shows a long screenshot in full, below the 25% floor", () => {
    // The regression this pins: with a hard MIN_SCALE floor the fit snapped
    // back up to 25% and a full-page screenshot opened cropped, with no zoom
    // level that showed all of it.
    stubNaturalSize({ width: 1600, height: 8000 });
    renderImagePreview();

    const scale = currentScale();
    expect(scale).toBeLessThan(0.25);
    expect(8000 * scale).toBeLessThanOrEqual(CANVAS_VIEWPORT.height + 0.001);
  });

  it("zooms in and out from the toolbar", () => {
    stubNaturalSize({ width: 1600, height: 800 });
    renderImagePreview();

    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    expect(currentScale()).toBeCloseTo(0.5 * 1.2, 5);

    fireEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    expect(currentScale()).toBeCloseTo(0.5, 5);
  });

  it("jumps to natural size and back to fit", () => {
    stubNaturalSize({ width: 1600, height: 800 });
    renderImagePreview();

    fireEvent.click(screen.getByRole("button", { name: "Actual size" }));
    expect(currentScale()).toBe(1);

    fireEvent.click(screen.getByRole("button", { name: "Fit to view" }));
    expect(currentScale()).toBeCloseTo(0.5, 5);
  });

  it("zooms on wheel without scrolling the page behind the modal", () => {
    stubNaturalSize({ width: 1600, height: 800 });
    renderImagePreview();

    const wheel = new WheelEvent("wheel", {
      deltaY: -100,
      clientX: 400,
      clientY: 200,
      bubbles: true,
      cancelable: true,
    });
    // Dispatched raw rather than via fireEvent.wheel so the test can assert
    // preventDefault — the thing that stops the page behind from scrolling.
    act(() => {
      zoomCanvas().dispatchEvent(wheel);
    });

    expect(currentScale()).toBeGreaterThan(0.5);
    expect(wheel.defaultPrevented).toBe(true);
  });

  it("pans on drag", () => {
    stubNaturalSize({ width: 1600, height: 800 });
    renderImagePreview();
    const before = canvasContent().style.transform;

    fireEvent.pointerDown(zoomCanvas(), { pointerId: 1, clientX: 400, clientY: 200 });
    fireEvent.pointerMove(zoomCanvas(), { pointerId: 1, clientX: 340, clientY: 170 });
    fireEvent.pointerUp(zoomCanvas(), { pointerId: 1 });

    expect(canvasContent().style.transform).not.toBe(before);
  });

  it("toggles between fit and 100% on double-click", () => {
    stubNaturalSize({ width: 1600, height: 800 });
    renderImagePreview();

    fireEvent.doubleClick(zoomCanvas(), { clientX: 400, clientY: 200 });
    expect(currentScale()).toBe(1);

    fireEvent.doubleClick(zoomCanvas(), { clientX: 400, clientY: 200 });
    expect(currentScale()).toBeCloseTo(0.5, 5);
  });

  it("focuses the canvas on open so the keyboard controls work without a click first", () => {
    stubNaturalSize({ width: 1600, height: 800 });
    renderImagePreview();

    expect(document.activeElement).toBe(zoomCanvas());

    fireEvent.keyDown(zoomCanvas(), { key: "+" });
    expect(currentScale()).toBeCloseTo(0.5 * 1.2, 5);
  });

  it("re-fits on reopen instead of restoring the previous zoom", async () => {
    stubNaturalSize({ width: 1600, height: 800 });
    const att = imageAttachment();
    render(<ClosablePreview attachment={att} />);

    fireEvent.click(screen.getByRole("button", { name: "Actual size" }));
    expect(currentScale()).toBe(1);

    fireEvent.click(screen.getByTitle("Close"));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });

    renderImagePreview(att);
    expect(currentScale()).toBeCloseTo(0.5, 5);
  });

  it("letterboxes an image with no intrinsic size and hides the zoom controls", () => {
    // An SVG that declares only a viewBox has no intrinsic size in Chromium,
    // so there is nothing to build a transform against. It must still render.
    stubNaturalSize({ width: 0, height: 0 });
    renderImagePreview(
      makeAttachment({ filename: "chart.svg", content_type: "image/svg+xml" }),
    );

    expect(document.querySelector(".zoom-canvas-content")).toBeNull();
    expect(document.querySelector(".zoom-canvas-fit")).not.toBeNull();
    expect(document.querySelector("img")?.className).toContain("object-contain");
    expect(screen.queryByRole("button", { name: "Zoom in" })).toBeNull();
  });

  it("keeps a pan that ends over the backdrop from closing the modal", () => {
    stubNaturalSize({ width: 1600, height: 800 });
    const onClose = vi.fn();
    render(
      <AttachmentPreviewModal
        source={{ kind: "full", attachment: imageAttachment() }}
        open
        onClose={onClose}
      />,
    );

    // A drag released outside the panel bubbles its click to the backdrop.
    fireEvent.click(zoomCanvas());
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("dialog"));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("gives the canvas a flex-column parent so it can claim the modal body height", () => {
    // jsdom has no layout, so this is asserted structurally: `.zoom-canvas`
    // sizes itself with `flex: 1 1 auto` and positions its content
    // absolutely. In a plain block parent it collapses to zero height and the
    // image disappears entirely.
    stubNaturalSize({ width: 1600, height: 800 });
    renderImagePreview();

    const body = zoomCanvas().parentElement!;
    expect(body.className).toContain("flex-col");
    expect(body.className).toContain("flex-1");
    expect(body.className).toContain("overflow-hidden");
  });

  it("keeps the scrolling block body for non-image kinds", () => {
    render(
      <AttachmentPreviewModal
        source={{
          kind: "full",
          attachment: makeAttachment({
            filename: "notes.md",
            content_type: "text/markdown",
          }),
        }}
        open
        onClose={() => {}}
      />,
    );

    // A flex column here would let a tall markdown preview shrink to fit
    // instead of scrolling.
    const body = document.querySelector(".min-h-0.flex-1")!;
    expect(body.className).toContain("overflow-auto");
    expect(body.className).not.toContain("flex-col");
  });

  it("shows no zoom controls for non-image kinds", () => {
    render(
      <AttachmentPreviewModal
        source={{
          kind: "full",
          attachment: makeAttachment({
            filename: "manual.pdf",
            content_type: "application/pdf",
          }),
        }}
        open
        onClose={() => {}}
      />,
    );

    expect(screen.queryByRole("button", { name: "Zoom in" })).toBeNull();
    expect(screen.queryByRole("application")).toBeNull();
  });
});
