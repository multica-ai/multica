import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

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
          preview_failed: "Couldn't load preview",
        },
      }),
  }),
}));

import { HtmlAttachmentPreview } from "./html-attachment-preview";

function renderWithQuery(ui: ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

beforeEach(() => vi.clearAllMocks());
afterEach(() => vi.restoreAllMocks());

describe("HtmlAttachmentPreview — visual shell (does not use file-card chrome)", () => {
  it("does not render the filename row that AttachmentCard chrome would render", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>ok</p>",
      originalContentType: "text/html",
    });
    renderWithQuery(
      <HtmlAttachmentPreview
        attachmentId="att-1"
        filename="report.html"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    await waitFor(() => {
      expect(document.querySelector("iframe")).toBeTruthy();
    });
    // The chrome row would surface the filename as text; we replace that
    // entirely with an iframe + floating toolbar.
    expect(screen.queryByText("report.html")).toBeNull();
  });

  it("renders iframe with sandbox='allow-scripts' and srcdoc when text loads", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>chart goes here</p>",
      originalContentType: "text/html",
    });
    renderWithQuery(
      <HtmlAttachmentPreview
        attachmentId="att-1"
        filename="report.html"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    await waitFor(() => {
      const frame = document.querySelector("iframe") as HTMLIFrameElement | null;
      expect(frame).toBeTruthy();
      // Critical: sandbox must not include allow-same-origin, otherwise the
      // sandbox is defeated per the HTML spec.
      expect(frame?.getAttribute("sandbox")).toBe("allow-scripts");
      expect(frame?.getAttribute("srcdoc")).toBe("<p>chart goes here</p>");
    });
  });
});

describe("HtmlAttachmentPreview — toolbar actions", () => {
  it("invokes onPreview when Maximize is clicked", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>ok</p>",
      originalContentType: "text/html",
    });
    const onPreview = vi.fn();
    renderWithQuery(
      <HtmlAttachmentPreview
        attachmentId="att-1"
        filename="report.html"
        onPreview={onPreview}
        onDownload={() => {}}
      />,
    );
    await waitFor(() => expect(screen.getByTitle("Preview")).toBeTruthy());
    fireEvent.mouseDown(screen.getByTitle("Preview"));
    expect(onPreview).toHaveBeenCalled();
  });

  it("invokes onDownload when Download is clicked", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>ok</p>",
      originalContentType: "text/html",
    });
    const onDownload = vi.fn();
    renderWithQuery(
      <HtmlAttachmentPreview
        attachmentId="att-1"
        filename="report.html"
        onPreview={() => {}}
        onDownload={onDownload}
      />,
    );
    await waitFor(() => expect(screen.getByTitle("Download")).toBeTruthy());
    fireEvent.mouseDown(screen.getByTitle("Download"));
    expect(onDownload).toHaveBeenCalled();
  });

  it("does not render a Copy code button — attachments are files, not source snippets", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>ok</p>",
      originalContentType: "text/html",
    });
    renderWithQuery(
      <HtmlAttachmentPreview
        attachmentId="att-1"
        filename="report.html"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    await waitFor(() => expect(document.querySelector("iframe")).toBeTruthy());
    expect(screen.queryByTitle("Copy code")).toBeNull();
  });
});

describe("HtmlAttachmentPreview — failure mode does not unmount the toolbar", () => {
  it("keeps Preview and Download enabled when fetch errors", async () => {
    getAttachmentTextContentMock.mockRejectedValueOnce(new Error("nope"));
    const onPreview = vi.fn();
    const onDownload = vi.fn();
    renderWithQuery(
      <HtmlAttachmentPreview
        attachmentId="att-1"
        filename="report.html"
        onPreview={onPreview}
        onDownload={onDownload}
      />,
    );
    // Wait for the error placeholder — guarantees the query has settled.
    await waitFor(() => {
      expect(
        screen.getByTestId("html-attachment-preview-error"),
      ).toBeTruthy();
    });
    // Critical: the figure does NOT collapse, and the chrome row is NOT
    // rendered as a fallback. Preview and Download stay reachable.
    expect(document.querySelector("iframe")).toBeNull();
    expect(screen.queryByText("report.html")).toBeNull();

    const previewBtn = screen.getByTitle("Preview") as HTMLButtonElement;
    const downloadBtn = screen.getByTitle("Download") as HTMLButtonElement;
    expect(previewBtn.disabled).toBe(false);
    expect(downloadBtn.disabled).toBe(false);

    fireEvent.mouseDown(previewBtn);
    expect(onPreview).toHaveBeenCalled();
    fireEvent.mouseDown(downloadBtn);
    expect(onDownload).toHaveBeenCalled();
  });
});
