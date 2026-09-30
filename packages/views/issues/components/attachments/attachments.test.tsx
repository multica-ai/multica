import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import {
  collectIssueAttachmentFiles,
  type IssueAttachmentFile,
} from "@multica/core/attachments/issue-attachments";
import type { Attachment, TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../../test/i18n";

const { openAtMock, tryOpenMock, downloadMock } = vi.hoisted(() => ({
  openAtMock: vi.fn((_key: string) => true),
  tryOpenMock: vi.fn(() => false),
  downloadMock: vi.fn(),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (type: string, id: string) => (type === "agent" ? `Agent ${id}` : `Member ${id}`),
  }),
}));

vi.mock("../../../common/actor-avatar", () => ({ ActorAvatar: () => null }));

vi.mock("../../../editor", () => ({
  usePreviewSequence: () => ({ openAt: openAtMock }),
  useAttachmentPreview: () => ({ open: vi.fn(), tryOpen: tryOpenMock, modal: null }),
  useDownloadAttachment: () => downloadMock,
}));

vi.mock("../../../editor/hooks/use-inline-media-url", () => ({
  useResignedInlineMedia: (_id: string | undefined, url: string) => ({ url, pending: false }),
}));

vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: (url: string) => url,
}));

vi.mock("../../../platform", () => ({ useImmersiveMode: () => {} }));

import { AttachmentsSection } from "./attachments-section";
import { AttachmentsOverview } from "./attachments-overview";

function attachment(over: Partial<Attachment> & { id: string }): Attachment {
  return {
    workspace_id: "ws-1",
    issue_id: "issue-1",
    comment_id: "c-1",
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "lambda",
    filename: "spec.pdf",
    url: `https://cdn.example.test/${over.id}`,
    download_url: `https://cdn.example.test/${over.id}?sig=1`,
    markdown_url: `https://cdn.example.test/${over.id}`,
    content_type: "application/pdf",
    size_bytes: 6 * 1024,
    created_at: "2026-09-20T10:00:00Z",
    ...over,
  };
}

function comment(
  id: string,
  attachments: Attachment[],
  over: Partial<TimelineEntry> = {},
): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "lambda",
    actor_name: "Lambda",
    created_at: attachments[0]?.created_at ?? "2026-09-20T10:00:00Z",
    content: "See the files.",
    attachments: attachments.map((a) => ({ ...a, comment_id: id })),
    ...over,
  };
}

const BRIEF = attachment({
  id: "brief",
  comment_id: null,
  filename: "brief.pdf",
  content_type: "application/pdf",
  size_bytes: 24 * 1024,
  created_at: "2026-09-20T09:00:00Z",
});
const SHOT = attachment({
  id: "shot",
  filename: "settings.png",
  content_type: "image/png",
  size_bytes: 412 * 1024,
  created_at: "2026-09-20T10:00:00Z",
});
const SPEC_V1 = attachment({ id: "spec-1", created_at: "2026-09-20T10:00:01Z" });
const SPEC_V2 = attachment({ id: "spec-2", created_at: "2026-09-21T09:00:00Z" });
const CSV = attachment({
  id: "csv",
  filename: "latency.csv",
  content_type: "text/csv",
  size_bytes: 18 * 1024,
  created_at: "2026-09-21T09:00:01Z",
});
const TIMELINE: TimelineEntry[] = [
  comment("c-1", [SHOT, SPEC_V1]),
  comment("c-2", [SPEC_V2, CSV], { created_at: "2026-09-21T09:00:00Z" }),
];
const FILES = collectIssueAttachmentFiles({
  description: `Kickoff: [brief.pdf](${BRIEF.url})`,
  attachments: [BRIEF],
  comments: TIMELINE,
});

function withQuery(children: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

function renderSection(files: ReadonlyArray<IssueAttachmentFile>, onOpenOverview = vi.fn()) {
  return renderWithI18n(
    withQuery(<AttachmentsSection files={files} onOpenOverview={onOpenOverview} />),
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("AttachmentsSection", () => {
  it("renders nothing while nothing has been attached", () => {
    const { container } = renderSection([]);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the latest version of each file and the total", () => {
    renderSection(FILES);

    expect(screen.getByRole("button", { name: /Attachments/ })).toHaveTextContent("4");
    const spec = screen.getByRole("button", { name: "spec.pdf, version 2" });
    expect(spec).toHaveTextContent("v2");
    expect(screen.getAllByTitle("spec.pdf")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "settings.png" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "brief.pdf" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "View all 4 attachments" })).toBeInTheDocument();
  });

  it("opens the latest version in the page's viewer", () => {
    renderSection(FILES);
    fireEvent.click(screen.getByRole("button", { name: "spec.pdf, version 2" }));
    expect(openAtMock).toHaveBeenCalledWith("spec-2");
  });

  it("downloads a file neither the sequence nor the viewer can open", () => {
    openAtMock.mockReturnValueOnce(false);
    renderSection(
      collectIssueAttachmentFiles({
        comments: [
          comment("c-9", [
            attachment({ id: "zip", filename: "bundle.zip", content_type: "application/zip" }),
          ]),
        ],
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "bundle.zip" }));
    expect(tryOpenMock).toHaveBeenCalled();
    expect(downloadMock).toHaveBeenCalledWith("zip");
  });

  it("opens the overview from view all", () => {
    const onOpenOverview = vi.fn();
    renderSection(FILES, onOpenOverview);
    fireEvent.click(screen.getByRole("button", { name: "View all 4 attachments" }));
    expect(onOpenOverview).toHaveBeenCalledTimes(1);
  });
});

describe("AttachmentsOverview", () => {
  const commentById = new Map(TIMELINE.map((entry) => [entry.id, entry]));

  function renderOverview(props: Partial<Parameters<typeof AttachmentsOverview>[0]> = {}) {
    const onClose = vi.fn();
    const onLocate = vi.fn();
    renderWithI18n(
      withQuery(
        <AttachmentsOverview
          open
          onClose={onClose}
          identifier="MUL-7649"
          files={FILES}
          commentById={commentById}
          onLocate={onLocate}
          returnKey={null}
          {...props}
        />,
      ),
    );
    return { onClose, onLocate };
  }

  it("counts exactly what the sidebar counts", () => {
    renderOverview();
    const dialog = screen.getByRole("dialog", { name: "MUL-7649 attachments" });
    expect(within(dialog).getByText(/^4 attachments/)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: /^All\s*4$/ })).toBeInTheDocument();

    const description = within(dialog).getByRole("region", { name: "Issue description" });
    expect(within(description).getByTitle("brief.pdf")).toBeInTheDocument();

    const comments = within(dialog).getAllByRole("region", { name: "Comment by Lambda" });
    expect(comments).toHaveLength(2);
    expect(within(comments[0]!).getByTitle("settings.png")).toBeInTheDocument();
    expect(within(comments[1]!).getByTitle("spec.pdf")).toHaveTextContent("v2");
  });

  it("filters by kind", () => {
    renderOverview();
    fireEvent.click(screen.getByRole("button", { name: /^Images\s*1$/ }));
    expect(screen.getByTitle("settings.png")).toBeInTheDocument();
    expect(screen.queryByTitle("spec.pdf")).toBeNull();
    expect(screen.queryByTitle("brief.pdf")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /^Videos\s*0$/ }));
    expect(screen.getByText("No attachments of this type.")).toBeInTheDocument();
  });

  it("opens a file and locates the description or a comment, closing itself first", () => {
    const { onClose, onLocate } = renderOverview();
    fireEvent.click(screen.getByTitle("latency.csv"));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(openAtMock).toHaveBeenCalledWith("csv");

    fireEvent.click(screen.getByRole("button", { name: "Show in description" }));
    expect(onClose).toHaveBeenCalledTimes(2);
    expect(onLocate).toHaveBeenCalledWith({ kind: "description" });

    const [, secondGroup] = screen.getAllByRole("region", { name: "Comment by Lambda" });
    fireEvent.click(within(secondGroup!).getByRole("button", { name: "Show in comments" }));
    expect(onClose).toHaveBeenCalledTimes(3);
    expect(onLocate).toHaveBeenCalledWith({ kind: "comment", commentId: "c-2" });
  });

  it("goes back to the viewer's file with G, and closes with Escape", () => {
    const { onClose } = renderOverview({ returnKey: "shot" });
    act(() => {
      fireEvent.keyDown(document, { key: "g" });
    });
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(openAtMock).toHaveBeenCalledWith("shot");

    act(() => {
      fireEvent.keyDown(document, { key: "Escape" });
    });
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});

describe("AttachmentsOverview focus", () => {
  it("takes focus when it opens and returns it to the opener on close", async () => {
    function Harness() {
      const [open, setOpen] = useState(false);
      return (
        <>
          <button type="button" onClick={() => setOpen(true)}>
            View all
          </button>
          <AttachmentsOverview
            open={open}
            onClose={() => setOpen(false)}
            identifier="MUL-7649"
            files={FILES}
            commentById={new Map(TIMELINE.map((entry) => [entry.id, entry]))}
            onLocate={vi.fn()}
            returnKey={null}
          />
        </>
      );
    }
    renderWithI18n(withQuery(<Harness />));
    const opener = screen.getByRole("button", { name: "View all" });
    opener.focus();
    fireEvent.click(opener);

    const dialog = await screen.findByRole("dialog", { name: "MUL-7649 attachments" });
    await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true));

    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(document.activeElement).toBe(opener));
  });
});
