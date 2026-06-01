import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";

vi.mock("@multica/core/api", () => ({
  api: { getBaseUrl: () => "http://api.test" },
}));

import { PdfViewer } from "./pdf-viewer";

describe("PdfViewer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    // jsdom does not implement the object-URL APIs.
    URL.createObjectURL = vi.fn(() => "blob:fake-url");
    URL.revokeObjectURL = vi.fn();
  });

  it("fetches a relative file via the API origin and frames the blob URL", async () => {
    const blob = new Blob(["%PDF-1.4"], { type: "application/pdf" });
    const fetchMock = vi
      .fn()
      .mockResolvedValue({ ok: true, blob: async () => blob });
    vi.stubGlobal("fetch", fetchMock);

    render(<PdfViewer fileUrl="/uploads/x.pdf" title="My Doc" />);

    await waitFor(() =>
      expect(screen.getByTitle("My Doc")).toHaveAttribute("src", "blob:fake-url"),
    );
    expect(fetchMock).toHaveBeenCalledWith(
      "http://api.test/uploads/x.pdf",
      expect.objectContaining({ credentials: "include" }),
    );
  });

  it("shows a download fallback when the fetch fails", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 404 });
    vi.stubGlobal("fetch", fetchMock);

    render(<PdfViewer fileUrl="https://cdn.test/x.pdf" title="My Doc" />);

    await waitFor(() =>
      expect(
        screen.getByText(/could not be displayed/i),
      ).toBeInTheDocument(),
    );
    expect(screen.getByRole("link", { name: /download/i })).toHaveAttribute(
      "href",
      "https://cdn.test/x.pdf",
    );
  });
});
