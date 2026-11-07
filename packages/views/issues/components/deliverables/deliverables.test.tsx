import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import {
  collectDeliverableFiles,
  type DeliverableFile,
} from "@multica/core/attachments/deliverables";
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

import { DeliverablesSection } from "./deliverables-section";
import { DeliverablesOverview } from "./deliverables-overview";
import { useDeliverableDetails, type DeliverableOrigin } from "./deliverable-details";

function attachment(over: Partial<Attachment> & { id: string }): Attachment {
  return {
    workspace_id: "ws-1",
    issue_id: "issue-1",
    comment_id: "c-1",
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "agent",
    uploader_id: "lambda",
    filename: "report.md",
    url: `https://cdn.example.test/${over.id}`,
    download_url: `https://cdn.example.test/${over.id}?sig=1`,
    markdown_url: `https://cdn.example.test/${over.id}`,
    content_type: "text/markdown",
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
    actor_type: "agent",
    actor_id: "lambda",
    actor_name: "Lambda",
    created_at: attachments[0]?.created_at ?? "2026-09-20T10:00:00Z",
    content: "Done — see the files.",
    attachments: attachments.map((a) => ({ ...a, comment_id: id })),
    ...over,
  };
}

// A run posted a screenshot and a report, a later run re-uploaded the
// report next to a CSV: four uploads, three deliverables.
const SHOT = attachment({
  id: "shot",
  filename: "settings.png",
  content_type: "image/png",
  size_bytes: 412 * 1024,
  created_at: "2026-09-20T10:00:00Z",
});
const REPORT_V1 = attachment({ id: "report-1", created_at: "2026-09-20T10:00:01Z" });
const REPORT_V2 = attachment({ id: "report-2", created_at: "2026-09-21T09:00:00Z" });
const CSV = attachment({
  id: "csv",
  filename: "latency.csv",
  content_type: "text/csv",
  size_bytes: 18 * 1024,
  created_at: "2026-09-21T09:00:01Z",
});
const TIMELINE: TimelineEntry[] = [
  comment("c-1", [SHOT, REPORT_V1]),
  comment("c-2", [REPORT_V2, CSV], { created_at: "2026-09-21T09:00:00Z" }),
];
const FILES = collectDeliverableFiles(TIMELINE);

function withQuery(children: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

function renderSection(files: ReadonlyArray<DeliverableFile>, onOpenOverview = vi.fn()) {
  return renderWithI18n(
    withQuery(<DeliverablesSection files={files} onOpenOverview={onOpenOverview} />),
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("DeliverablesSection", () => {
  it("renders nothing while nothing has been delivered", () => {
    const { container } = renderSection([]);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the latest version of each file and the total", () => {
    renderSection(FILES);

    expect(screen.getByRole("button", { name: /Deliverables/ })).toHaveTextContent("3");
    // One row for the report, marked as its second version.
    const report = screen.getByRole("button", { name: "report.md, version 2" });
    expect(report).toHaveTextContent("v2");
    expect(screen.getAllByTitle("report.md")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "settings.png" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "View all 3 deliverables" })).toBeInTheDocument();
  });

  it("opens the latest version in the page's viewer", () => {
    renderSection(FILES);
    fireEvent.click(screen.getByRole("button", { name: "report.md, version 2" }));
    expect(openAtMock).toHaveBeenCalledWith("report-2");
  });

  it("downloads a file neither the sequence nor the viewer can open", () => {
    openAtMock.mockReturnValueOnce(false);
    renderSection(
      collectDeliverableFiles([
        comment("c-9", [
          attachment({ id: "zip", filename: "bundle.zip", content_type: "application/zip" }),
        ]),
      ]),
    );
    fireEvent.click(screen.getByRole("button", { name: "bundle.zip" }));
    expect(tryOpenMock).toHaveBeenCalled();
    expect(downloadMock).toHaveBeenCalledWith("zip");
  });

  it("opens the overview from view all", () => {
    const onOpenOverview = vi.fn();
    renderSection(FILES, onOpenOverview);
    fireEvent.click(screen.getByRole("button", { name: "View all 3 deliverables" }));
    expect(onOpenOverview).toHaveBeenCalledTimes(1);
  });
});

describe("DeliverablesOverview", () => {
  const commentById = new Map(TIMELINE.map((entry) => [entry.id, entry]));

  function renderOverview(props: Partial<Parameters<typeof DeliverablesOverview>[0]> = {}) {
    const onClose = vi.fn();
    const onLocate = vi.fn();
    renderWithI18n(
      withQuery(
        <DeliverablesOverview
          open
          onClose={onClose}
          identifier="MUL-7588"
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
    const dialog = screen.getByRole("dialog", { name: "MUL-7588 deliverables" });
    expect(within(dialog).getByText(/^3 deliverables/)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: /^All\s*3$/ })).toBeInTheDocument();
    // One group per posting comment; the report sits with its v2.
    const groups = within(dialog).getAllByRole("region", { name: "Comment by Lambda" });
    expect(groups).toHaveLength(2);
    expect(within(groups[0]!).getByTitle("settings.png")).toBeInTheDocument();
    expect(within(groups[1]!).getByTitle("report.md")).toHaveTextContent("v2");
  });

  it("filters by kind", () => {
    renderOverview();
    fireEvent.click(screen.getByRole("button", { name: /^Images\s*1$/ }));
    expect(screen.getByTitle("settings.png")).toBeInTheDocument();
    expect(screen.queryByTitle("report.md")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /^Videos\s*0$/ }));
    expect(screen.getByText("No deliverables of this type.")).toBeInTheDocument();
  });

  it("opens a file and locates a comment, closing itself first", () => {
    const { onClose, onLocate } = renderOverview();
    fireEvent.click(screen.getByTitle("latency.csv"));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(openAtMock).toHaveBeenCalledWith("csv");

    const [, secondGroup] = screen.getAllByRole("region", { name: "Comment by Lambda" });
    fireEvent.click(within(secondGroup!).getByRole("button", { name: "Show in comments" }));
    expect(onClose).toHaveBeenCalledTimes(2);
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

describe("DeliverablesOverview focus", () => {
  // A full-window overlay the keyboard can't reach is not a dialog: focus
  // must move in when it opens and go back to the opener when it closes.
  it("takes focus when it opens and returns it to the opener on close", async () => {
    function Harness() {
      const [open, setOpen] = useState(false);
      return (
        <>
          <button type="button" onClick={() => setOpen(true)}>
            View all
          </button>
          <DeliverablesOverview
            open={open}
            onClose={() => setOpen(false)}
            identifier="MUL-7588"
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

    const dialog = await screen.findByRole("dialog", { name: "MUL-7588 deliverables" });
    await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true));

    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(document.activeElement).toBe(opener));
  });
});

describe("useDeliverableDetails", () => {
  // The viewer covers the page, so "Show in comments" in the info panel has
  // to close it first, exactly like the same action in the top bar.
  it("closes the viewer before locating, from the info panel and the top bar", () => {
    const calls: string[] = [];
    const close = vi.fn(() => calls.push("close"));
    const onLocate = vi.fn((origin: DeliverableOrigin) => calls.push(`locate:${JSON.stringify(origin)}`));
    let locateFromTopBar: (() => void) | undefined;

    function Harness() {
      const describe = useDeliverableDetails({
        files: FILES,
        commentById: new Map(TIMELINE.map((entry) => [entry.id, entry])),
        onLocate,
      });
      const item = { key: "csv", url: CSV.url, filename: CSV.filename, attachment: CSV, imageByConstruction: false, blockId: "c-2" };
      const details = describe(item, { items: [item], goTo: () => true, close });
      locateFromTopBar = details.locate?.onSelect;
      return <>{details.info}</>;
    }
    renderWithI18n(withQuery(<Harness />));

    fireEvent.click(screen.getByRole("button", { name: "Show in comments" }));
    locateFromTopBar?.();
    const located = `locate:${JSON.stringify({ kind: "comment", commentId: "c-2" })}`;
    expect(calls).toEqual(["close", located, "close", located]);
  });
});
