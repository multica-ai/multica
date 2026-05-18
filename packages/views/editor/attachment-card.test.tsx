import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render as rtlRender, screen, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

// vi.hoisted lets us reference the mock from inside the vi.mock factory
// even though the factory hoists above the file's top-level statements.
const { getAttachmentTextContentMock } = vi.hoisted(() => ({
  getAttachmentTextContentMock: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: { getAttachmentTextContent: getAttachmentTextContentMock },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (sel: (s: Record<string, Record<string, string>>) => string) =>
      sel({
        image: { download: "Download" },
        attachment: {
          preview: "Preview",
          preview_loading: "Loading preview…",
        },
        file_card: { uploading: "Uploading {{filename}}" },
      }),
  }),
}));

import { AttachmentCard } from "./attachment-card";

function render(ui: ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return rtlRender(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

beforeEach(() => vi.clearAllMocks());
afterEach(() => vi.restoreAllMocks());

describe("AttachmentCard — kind dispatch", () => {
  it("renders chrome only for non-html kinds (image, video, other)", () => {
    render(
      <AttachmentCard
        filename="snapshot.png"
        contentType="image/png"
        attachmentId="att-1"
        href="https://cdn.example/snapshot.png"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    expect(screen.getByText("snapshot.png")).toBeTruthy();
    // No inline iframe for an image-kind attachment.
    expect(document.querySelector("iframe")).toBeNull();
  });

  it("renders chrome only for an html URL-only source (no attachmentId)", () => {
    render(
      <AttachmentCard
        filename="report.html"
        contentType="text/html"
        href="https://cdn.example/report.html"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    // Without an attachment id we cannot hit the ID-keyed /content proxy,
    // so the card must fall back to chrome-only.
    expect(document.querySelector("iframe")).toBeNull();
    expect(screen.getByText("report.html")).toBeTruthy();
  });

  it("hides the Eye button for an html URL-only source (the modal's /content proxy is ID-keyed)", () => {
    // Regression: a cross-comment / copy-pasted `!file[report.html](url)`
    // used to surface a dead Eye button — the AttachmentCard allowed
    // preview when `previewableFromUrl` was true even without an
    // attachmentId, but the modal's tryOpen rejects URL-only text kinds
    // and the click became a silent no-op.
    render(
      <AttachmentCard
        filename="report.html"
        contentType="text/html"
        href="https://cdn.example/report.html"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    expect(screen.queryByTitle("Preview")).toBeNull();
    // Download stays available — the underlying URL is still reachable.
    expect(screen.getByTitle("Download")).toBeTruthy();
  });

  it("still shows the Eye button for an html source when an attachmentId is available", () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>ok</p>",
      originalContentType: "text/html",
    });
    render(
      <AttachmentCard
        filename="report.html"
        contentType="text/html"
        attachmentId="att-1"
        href="https://cdn.example/report.html"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    expect(screen.getByTitle("Preview")).toBeTruthy();
  });

  it("shows the Eye button for a URL-only pdf source (modal renders pdfs directly from URL)", () => {
    // Counterpart to the html regression: media kinds (pdf/video/audio)
    // ARE URL-previewable because the modal renders them via
    // <iframe src=url>/<video>/<audio>, not via the /content proxy.
    render(
      <AttachmentCard
        filename="manual.pdf"
        contentType="application/pdf"
        href="https://cdn.example/manual.pdf"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    expect(screen.getByTitle("Preview")).toBeTruthy();
  });

  it("renders an inline iframe with sandbox='allow-scripts' for an HTML attachment", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>chart goes here</p>",
      originalContentType: "text/html",
    });
    render(
      <AttachmentCard
        filename="report.html"
        contentType="text/html"
        attachmentId="att-1"
        href="https://cdn.example/report.html"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    await waitFor(() => {
      const frame = document.querySelector("iframe") as HTMLIFrameElement | null;
      expect(frame).toBeTruthy();
      expect(frame?.getAttribute("sandbox")).toBe("allow-scripts");
      expect(frame?.getAttribute("srcdoc")).toBe("<p>chart goes here</p>");
    });
  });
});

describe("AttachmentCard — Eye / Download buttons", () => {
  it("invokes onPreview when Eye is clicked", () => {
    const onPreview = vi.fn();
    render(
      <AttachmentCard
        filename="manual.pdf"
        contentType="application/pdf"
        attachmentId="att-1"
        href="https://cdn.example/manual.pdf"
        onPreview={onPreview}
        onDownload={() => {}}
      />,
    );
    fireEvent.mouseDown(screen.getByTitle("Preview"));
    expect(onPreview).toHaveBeenCalled();
  });

  it("invokes onDownload when Download is clicked", () => {
    const onDownload = vi.fn();
    render(
      <AttachmentCard
        filename="manual.pdf"
        contentType="application/pdf"
        attachmentId="att-1"
        href="https://cdn.example/manual.pdf"
        onPreview={() => {}}
        onDownload={onDownload}
      />,
    );
    fireEvent.mouseDown(screen.getByTitle("Download"));
    expect(onDownload).toHaveBeenCalled();
  });

  it("hides the Eye button while uploading and skips the inline HTML preview", () => {
    render(
      <AttachmentCard
        filename="report.html"
        contentType="text/html"
        attachmentId="att-1"
        href="https://cdn.example/report.html"
        uploading
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    expect(screen.queryByTitle("Preview")).toBeNull();
    expect(screen.queryByTitle("Download")).toBeNull();
    expect(document.querySelector("iframe")).toBeNull();
    // The mock `t()` returns the i18n template as-is; the production t-fn
    // interpolates {{filename}} → "report.html". Asserting the template
    // proves the uploading branch was selected without depending on the
    // interpolation behavior of the mock.
    expect(screen.getByText("Uploading {{filename}}")).toBeTruthy();
  });
});
