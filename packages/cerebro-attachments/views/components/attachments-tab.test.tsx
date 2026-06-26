// @vitest-environment jsdom
// FIR-2034 — the Attachments tab renders standalone attachments as rows and
// wires open/download per file. Hooks that need workspace/query context are
// mocked; the row layout + action wiring is the unit under test.
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { Attachment } from "@multica/core/types";

const openViewer = vi.fn();
const downloadFile = vi.fn();

vi.mock("../use-attachment-actions", () => ({
  useAttachmentActions: () => ({ openViewer, downloadFile }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Jesper" }),
}));
vi.mock("@multica/views/i18n", () => ({
  useTimeAgo: () => () => "2h ago",
}));

import { AttachmentsTab, AttachmentsTabLabel } from "./attachments-tab";

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

const IMG = att({ id: "img", filename: "shot.png", content_type: "image/png", size_bytes: 1024 });
const CODE = att({
  id: "code",
  filename: "chips.tsx",
  url: "https://cdn.test/chips.tsx",
  content_type: "text/plain",
  size_bytes: 4096,
});
const BIN = att({
  id: "bin",
  filename: "data.bin",
  url: "https://cdn.test/data.bin",
  content_type: "application/octet-stream",
  size_bytes: 2048,
});

describe("AttachmentsTab", () => {
  afterEach(cleanup);

  it("renders a row per standalone file with name, type label, and size", () => {
    render(<AttachmentsTab attachments={[IMG, CODE]} />);
    expect(screen.getByText("shot.png")).toBeTruthy();
    expect(screen.getByText("chips.tsx")).toBeTruthy();
    expect(screen.getByText("Code")).toBeTruthy(); // .tsx → code palette
    expect(screen.getByText("1.0 KB")).toBeTruthy();
    expect(screen.getByText("4.0 KB")).toBeTruthy();
  });

  it("shows a count + total size summary", () => {
    render(<AttachmentsTab attachments={[IMG, CODE]} />);
    expect(screen.getByText("2 files · 5.0 KB")).toBeTruthy();
  });

  it("opens the viewer for a viewable file and downloads a non-viewable one", () => {
    render(<AttachmentsTab attachments={[IMG, BIN]} />);
    fireEvent.click(screen.getByText("shot.png"));
    expect(openViewer).toHaveBeenCalledWith("img", "shot.png");
    fireEvent.click(screen.getByText("data.bin"));
    expect(downloadFile).toHaveBeenCalledWith("bin");
  });

  it("hides inline attachments and shows an empty state when none remain", () => {
    render(<AttachmentsTab attachments={[IMG]} content={`![x](${IMG.url})`} />);
    expect(screen.getByText("No attachments yet.")).toBeTruthy();
  });
});

describe("AttachmentsTabLabel", () => {
  afterEach(cleanup);

  it("shows the standalone count as a badge", () => {
    render(<AttachmentsTabLabel attachments={[IMG, CODE]} />);
    expect(screen.getByText("Attachments")).toBeTruthy();
    expect(screen.getByText("2")).toBeTruthy();
  });

  it("omits the badge when there are no standalone files", () => {
    render(<AttachmentsTabLabel attachments={[]} />);
    expect(screen.queryByText("0")).toBeNull();
  });
});
