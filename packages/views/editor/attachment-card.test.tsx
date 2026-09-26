import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (sel: (s: Record<string, Record<string, string>>) => string) =>
      sel({
        image: { download: "Download" },
        attachment: {
          preview: "Preview",
          preview_loading: "Loading preview…",
          remove: "Remove attachment",
        },
        file_card: { uploading: "Uploading {{filename}}" },
      }),
  }),
}));

import { AttachmentCard, AttachmentFileCard } from "./attachment-card";

beforeEach(() => vi.clearAllMocks());
afterEach(() => vi.restoreAllMocks());

describe("AttachmentCard — chrome row", () => {
  it("renders chrome only and never an inline iframe", () => {
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
    expect(screen.getByText("report.html")).toBeTruthy();
    expect(document.querySelector("iframe")).toBeNull();
  });

  it("hides the Eye button for an html URL-only source (the modal's /content proxy is ID-keyed)", () => {
    // Regression: a cross-comment / copy-pasted `!file[report.html](url)`
    // used to surface a dead Eye button — text kinds need an attachmentId,
    // otherwise tryOpen rejects and the click becomes a silent no-op.
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

  it("shows the Eye button for an html source when an attachmentId is available", () => {
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

  it("hides Eye and Download buttons while uploading", () => {
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
    // The mock `t()` returns the i18n template as-is; the production t-fn
    // interpolates {{filename}} → "report.html". Asserting the template
    // proves the uploading branch was selected without depending on the
    // interpolation behavior of the mock.
    expect(screen.getByText("Uploading {{filename}}")).toBeTruthy();
  });
});

describe("AttachmentFileCard — a file in a grid of cards (MUL-7649)", () => {
  it("shows the name, its badge and TYPE · size", () => {
    render(
      <AttachmentFileCard
        filename="实现说明.md"
        contentType="text/markdown"
        sizeBytes={6 * 1024}
        canPreview
        canDownload
        badge={<span>v2</span>}
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    expect(screen.getByText("实现说明.md")).toBeTruthy();
    expect(screen.getByText("v2")).toBeTruthy();
    expect(screen.getByText("MD · 6 KB")).toBeTruthy();
  });

  it("opens the file when the card is clicked, and downloads from its own button", () => {
    const onPreview = vi.fn();
    const onDownload = vi.fn();
    render(
      <AttachmentFileCard
        filename="spec.pdf"
        canPreview
        canDownload
        onPreview={onPreview}
        onDownload={onDownload}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "spec.pdf" }));
    expect(onPreview).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Download" }));
    expect(onDownload).toHaveBeenCalledTimes(1);
    expect(onPreview).toHaveBeenCalledTimes(1);
  });

  it("downloads on click when the viewer cannot show the file", () => {
    const onPreview = vi.fn();
    const onDownload = vi.fn();
    render(
      <AttachmentFileCard
        filename="bundle.zip"
        canPreview={false}
        canDownload
        onPreview={onPreview}
        onDownload={onDownload}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "bundle.zip" }));
    expect(onDownload).toHaveBeenCalledTimes(1);
    expect(onPreview).not.toHaveBeenCalled();
  });

  it("offers remove only when the surface is editable", () => {
    const onDelete = vi.fn();
    const { rerender } = render(
      <AttachmentFileCard filename="a.csv" canPreview canDownload onPreview={() => {}} onDownload={() => {}} />,
    );
    expect(screen.queryByRole("button", { name: "Remove attachment" })).toBeNull();
    rerender(
      <AttachmentFileCard
        filename="a.csv"
        canPreview
        canDownload
        onPreview={() => {}}
        onDownload={() => {}}
        onDelete={onDelete}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Remove attachment" }));
    expect(onDelete).toHaveBeenCalledTimes(1);
  });
});
