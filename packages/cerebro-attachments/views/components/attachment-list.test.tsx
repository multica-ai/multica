// @vitest-environment jsdom
// FIR-2710 — clicking an image chip opens the paginated gallery lightbox above
// the thread when cerebro_image_gallery is on, instead of routing each image to
// the legacy per-image new-tab viewer. The flag-gated open wiring is the unit
// under test (the ImageGallery component's own paging/counter/thumbnails are
// covered in image-gallery.test.tsx).
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { Attachment } from "@multica/core/types";

const openViewer = vi.fn();
const downloadFile = vi.fn();
const flags: Record<string, boolean> = {};

vi.mock("../use-attachment-actions", () => ({
  useAttachmentActions: () => ({
    openViewer,
    downloadFile,
    viewerHref: (id: string) => `/ws/attachments/${id}`,
  }),
}));
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws",
}));
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFlagValue: (key: string) => flags[key] ?? false,
}));

import { AttachmentList } from "./attachment-list";

function att(over: Partial<Attachment>): Attachment {
  return {
    id: "a1",
    workspace_id: "ws",
    issue_id: "i1",
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "u1",
    filename: "file.png",
    url: "https://cdn.test/file.png",
    download_url: "/api/attachments/a1/download",
    content_type: "image/png",
    size_bytes: 1024,
    created_at: "2026-06-26T09:00:00Z",
    ...over,
  };
}

const IMG1 = att({ id: "img1", filename: "shot1.png" });
const IMG2 = att({ id: "img2", filename: "shot2.png", url: "https://cdn.test/shot2.png" });

describe("AttachmentList image gallery wiring (FIR-2710)", () => {
  afterEach(() => {
    cleanup();
    openViewer.mockClear();
    downloadFile.mockClear();
    for (const k of Object.keys(flags)) delete flags[k];
  });

  it("opens the gallery lightbox above the thread when an image chip is clicked and the flag is on", () => {
    flags.cerebro_attachment_chips = true;
    flags.cerebro_image_gallery = true;
    render(<AttachmentList attachments={[IMG1, IMG2]} />);

    // Closed until a chip is clicked.
    expect(screen.queryByRole("dialog")).toBeNull();

    fireEvent.click(screen.getAllByLabelText("Open in viewer")[0]!);

    // The gallery lightbox is now open above the thread, and we did NOT route to
    // the legacy per-image new-tab viewer.
    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(openViewer).not.toHaveBeenCalled();
  });

  it("falls back to the legacy new-tab viewer when the gallery flag is off", () => {
    flags.cerebro_attachment_chips = true;
    flags.cerebro_image_gallery = false;
    render(<AttachmentList attachments={[IMG1, IMG2]} />);

    fireEvent.click(screen.getAllByLabelText("Open in viewer")[0]!);

    expect(openViewer).toHaveBeenCalledWith("img1", "shot1.png");
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
