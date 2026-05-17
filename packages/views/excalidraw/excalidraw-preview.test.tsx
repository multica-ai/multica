import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render as rtlRender, screen, waitFor, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";
import type { Attachment } from "@multica/core/types";

const openExternalMock = vi.hoisted(() => vi.fn());

vi.mock("../platform", () => ({
  openExternal: openExternalMock,
}));

const {
  getAttachmentTextContentMock,
  FakePreviewTooLargeError,
  FakePreviewUnsupportedError,
  exportToSvgMock,
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
    FakePreviewTooLargeError,
    FakePreviewUnsupportedError,
    exportToSvgMock: vi.fn(),
  };
});

vi.mock("@multica/core/api", () => ({
  api: { getAttachmentTextContent: getAttachmentTextContentMock },
  PreviewTooLargeError: FakePreviewTooLargeError,
  PreviewUnsupportedError: FakePreviewUnsupportedError,
}));

// Heavy editor bundle — stub so the test stays light.
vi.mock("@excalidraw/excalidraw", () => ({
  exportToSvg: exportToSvgMock,
}));

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (sel: (s: Record<string, Record<string, string>>) => string) =>
      sel({
        attachment: {
          preview_loading: "Loading preview…",
          preview_failed: "Couldn't load preview",
          preview_too_large: "File is too large to preview. Please download.",
          preview_unsupported: "This file type can't be previewed.",
        },
        excalidraw: {
          expand: "Open full size",
          invalid_scene: "This Excalidraw file couldn't be rendered.",
        },
      }),
  }),
}));

import { ExcalidrawPreview } from "./excalidraw-preview";

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
    filename: "diagram.excalidraw",
    url: "https://cdn.example.test/diagram.excalidraw",
    download_url: "https://cdn.example.test/diagram.excalidraw?Signature=s",
    content_type: "application/vnd.excalidraw+json",
    size_bytes: 0,
    created_at: "2026-05-17T00:00:00Z",
    ...overrides,
  };
}

function makeSvg(): SVGSVGElement {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("width", "100");
  svg.setAttribute("height", "60");
  svg.setAttribute("data-testid", "excalidraw-svg");
  return svg;
}

const validSceneJson = JSON.stringify({
  type: "excalidraw",
  version: 2,
  elements: [{ type: "rectangle", id: "r1" }],
  appState: { viewBackgroundColor: "#fafafa" },
  files: {},
});

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ExcalidrawPreview", () => {
  it("renders the SVG returned by exportToSvg on a valid scene", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: validSceneJson,
      originalContentType: "application/vnd.excalidraw+json",
    });
    exportToSvgMock.mockResolvedValueOnce(makeSvg());

    render(<ExcalidrawPreview attachment={makeAttachment()} />);

    await waitFor(() => {
      expect(document.querySelector('[data-testid="excalidraw-svg"]')).toBeTruthy();
    });
    // The renderer strips the fixed width/height so CSS can govern sizing.
    const svg = document.querySelector('[data-testid="excalidraw-svg"]')!;
    expect(svg.hasAttribute("width")).toBe(false);
    expect(svg.hasAttribute("height")).toBe(false);
  });

  it("invokes onExpand when the preview is clicked (inline mode)", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: validSceneJson,
      originalContentType: "application/vnd.excalidraw+json",
    });
    exportToSvgMock.mockResolvedValueOnce(makeSvg());
    const onExpand = vi.fn();

    render(<ExcalidrawPreview attachment={makeAttachment()} onExpand={onExpand} />);

    const button = await screen.findByRole("button", { name: "Open full size" });
    fireEvent.click(button);
    expect(onExpand).toHaveBeenCalledTimes(1);
  });

  it("does not render the expand button when no onExpand handler is provided", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: validSceneJson,
      originalContentType: "application/vnd.excalidraw+json",
    });
    exportToSvgMock.mockResolvedValueOnce(makeSvg());

    render(<ExcalidrawPreview attachment={makeAttachment()} expanded />);

    await waitFor(() => {
      expect(document.querySelector('[data-testid="excalidraw-svg"]')).toBeTruthy();
    });
    expect(screen.queryByRole("button", { name: "Open full size" })).toBeNull();
  });

  it("shows a fallback link when the JSON is malformed", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "{ not valid json",
      originalContentType: "application/vnd.excalidraw+json",
    });

    render(<ExcalidrawPreview attachment={makeAttachment()} />);

    await waitFor(() => {
      expect(
        screen.getByText("This Excalidraw file couldn't be rendered."),
      ).toBeTruthy();
    });
    // exportToSvg must not be reached when parsing fails.
    expect(exportToSvgMock).not.toHaveBeenCalled();
  });

  it("shows a fallback link when the JSON is missing `elements`", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: JSON.stringify({ type: "excalidraw" }),
      originalContentType: "application/vnd.excalidraw+json",
    });

    render(<ExcalidrawPreview attachment={makeAttachment()} />);

    await waitFor(() => {
      expect(
        screen.getByText("This Excalidraw file couldn't be rendered."),
      ).toBeTruthy();
    });
  });

  it("shows the too-large fallback when the proxy returns 413", async () => {
    getAttachmentTextContentMock.mockRejectedValueOnce(new FakePreviewTooLargeError());

    render(<ExcalidrawPreview attachment={makeAttachment()} />);

    await waitFor(() => {
      expect(
        screen.getByText("File is too large to preview. Please download."),
      ).toBeTruthy();
    });
  });

  it("applies viewBackgroundColor from appState to the frame", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: validSceneJson,
      originalContentType: "application/vnd.excalidraw+json",
    });
    exportToSvgMock.mockResolvedValueOnce(makeSvg());

    const { container } = render(<ExcalidrawPreview attachment={makeAttachment()} />);

    await waitFor(() => {
      expect(document.querySelector('[data-testid="excalidraw-svg"]')).toBeTruthy();
    });
    const frame = container.querySelector('div[style*="background-color"]') as HTMLElement | null;
    expect(frame).toBeTruthy();
    expect(frame?.style.backgroundColor).toBe("rgb(250, 250, 250)");
  });
});
