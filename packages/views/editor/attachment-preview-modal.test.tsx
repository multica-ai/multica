import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render as rtlRender, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";
import type { Attachment } from "@multica/core/types";

// vi.hoisted: factories run before module evaluation, letting us name mocks
// referenced from inside vi.mock factories below. The Error classes must be
// hoisted too because vi.mock is itself hoisted above the top-level `class`
// declarations.
const {
  getAttachmentTextContentMock,
  downloadMock,
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
    getAttachmentTextContentMock: vi.fn(),
    downloadMock: vi.fn(),
    FakePreviewTooLargeError,
    FakePreviewUnsupportedError,
  };
});

vi.mock("@multica/core/api", () => ({
  api: { getAttachmentTextContent: getAttachmentTextContentMock },
  PreviewTooLargeError: FakePreviewTooLargeError,
  PreviewUnsupportedError: FakePreviewUnsupportedError,
}));

vi.mock("./use-download-attachment", () => ({
  useDownloadAttachment: () => downloadMock,
}));

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
        image: { download: "Download" },
        attachment: {
          preview: "Preview",
          preview_loading: "Loading preview…",
          preview_failed: "Couldn't load preview",
          preview_too_large: "File is too large to preview. Please download.",
          preview_unsupported: "This file type can't be previewed.",
          close: "Close",
          download_failed: "",
        },
      }),
  }),
}));

import { AttachmentPreviewModal } from "./attachment-preview-modal";

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
    content_type: "application/octet-stream",
    size_bytes: 0,
    created_at: "2026-05-13T00:00:00Z",
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("AttachmentPreviewModal — dispatch", () => {
  it("renders a PDF iframe pointing at the signed download URL", () => {
    const att = makeAttachment({ filename: "manual.pdf", content_type: "application/pdf" });
    render(<AttachmentPreviewModal attachment={att} open onClose={() => {}} />);
    const iframe = document.querySelector("iframe");
    expect(iframe).toBeTruthy();
    expect(iframe?.getAttribute("src")).toBe(att.download_url);
  });

  it("renders a <video> for video/* content types", () => {
    const att = makeAttachment({ filename: "clip.mp4", content_type: "video/mp4" });
    render(<AttachmentPreviewModal attachment={att} open onClose={() => {}} />);
    const video = document.querySelector("video");
    expect(video).toBeTruthy();
    expect(video?.getAttribute("src")).toBe(att.download_url);
  });

  it("renders an <audio> for audio/* content types", () => {
    const att = makeAttachment({ filename: "note.mp3", content_type: "audio/mpeg" });
    render(<AttachmentPreviewModal attachment={att} open onClose={() => {}} />);
    const audio = document.querySelector("audio");
    expect(audio).toBeTruthy();
  });

  it("fetches text and hands it to ReadonlyContent for Markdown", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "# heading\n\nbody\n",
      originalContentType: "text/markdown",
    });
    const att = makeAttachment({ filename: "README.md", content_type: "text/markdown" });
    render(<AttachmentPreviewModal attachment={att} open onClose={() => {}} />);

    expect(getAttachmentTextContentMock).toHaveBeenCalledWith("att-1");

    await waitFor(() => {
      expect(screen.getByTestId("readonly-content")).toBeTruthy();
    });
    expect(screen.getByTestId("readonly-content").textContent).toContain("# heading");
  });

  it("renders an iframe with srcdoc + sandbox='' for HTML", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>hi</p>",
      originalContentType: "text/html",
    });
    const att = makeAttachment({ filename: "page.html", content_type: "text/html" });
    render(<AttachmentPreviewModal attachment={att} open onClose={() => {}} />);

    await waitFor(() => {
      const frame = document.querySelector("iframe[sandbox]") as HTMLIFrameElement | null;
      expect(frame).toBeTruthy();
      expect(frame?.getAttribute("sandbox")).toBe("");
      expect(frame?.getAttribute("srcdoc")).toBe("<p>hi</p>");
    });
  });

  it("renders a code block with lowlight for source files", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "package main\n",
      originalContentType: "text/plain",
    });
    const att = makeAttachment({ filename: "main.go", content_type: "text/plain" });
    render(<AttachmentPreviewModal attachment={att} open onClose={() => {}} />);

    await waitFor(() => {
      const code = document.querySelector("code.hljs");
      expect(code).toBeTruthy();
      expect(code?.className).toContain("language-go");
    });
  });

  it("shows unsupported fallback when no PreviewKind matches", () => {
    const att = makeAttachment({ filename: "blob.zip", content_type: "application/zip" });
    render(<AttachmentPreviewModal attachment={att} open onClose={() => {}} />);
    expect(screen.getByText("This file type can't be previewed.")).toBeTruthy();
  });
});

describe("AttachmentPreviewModal — error states", () => {
  it("shows the too-large fallback on PreviewTooLargeError", async () => {
    getAttachmentTextContentMock.mockRejectedValueOnce(new FakePreviewTooLargeError());
    const att = makeAttachment({ filename: "huge.txt", content_type: "text/plain" });
    render(<AttachmentPreviewModal attachment={att} open onClose={() => {}} />);
    await waitFor(() => {
      expect(screen.getByText("File is too large to preview. Please download.")).toBeTruthy();
    });
  });

  it("shows the unsupported fallback on PreviewUnsupportedError (server/client drift)", async () => {
    getAttachmentTextContentMock.mockRejectedValueOnce(new FakePreviewUnsupportedError());
    const att = makeAttachment({ filename: "weird.txt", content_type: "text/plain" });
    render(<AttachmentPreviewModal attachment={att} open onClose={() => {}} />);
    await waitFor(() => {
      expect(screen.getByText("This file type can't be previewed.")).toBeTruthy();
    });
  });

  it("shows the generic failed fallback on a transport error", async () => {
    getAttachmentTextContentMock.mockRejectedValueOnce(new Error("network down"));
    const att = makeAttachment({ filename: "x.md", content_type: "text/markdown" });
    render(<AttachmentPreviewModal attachment={att} open onClose={() => {}} />);
    await waitFor(() => {
      expect(screen.getByText("Couldn't load preview")).toBeTruthy();
    });
  });
});

describe("AttachmentPreviewModal — controls", () => {
  it("ESC closes the modal", () => {
    const onClose = vi.fn();
    const att = makeAttachment({ filename: "manual.pdf", content_type: "application/pdf" });
    render(<AttachmentPreviewModal attachment={att} open onClose={onClose} />);
    act(() => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    });
    expect(onClose).toHaveBeenCalled();
  });

  it("Download button invokes useDownloadAttachment with the attachment id", () => {
    const att = makeAttachment({ filename: "manual.pdf", content_type: "application/pdf" });
    render(<AttachmentPreviewModal attachment={att} open onClose={() => {}} />);
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
    render(<AttachmentPreviewModal attachment={att} open onClose={onClose} />);
    const dialog = screen.getByRole("dialog");
    fireEvent.click(dialog);
    expect(onClose).toHaveBeenCalled();
  });
});
