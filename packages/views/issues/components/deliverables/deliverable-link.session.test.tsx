import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useCallback, useState } from "react";
import { collectDeliverableFiles } from "@multica/core/attachments/deliverables";
import type { Attachment } from "@multica/core/types";
import { renderWithI18n } from "../../../test/i18n";

// The real viewer (PreviewSequenceProvider) and the real overview, wired to
// the link session the way IssueDetail wires them. `onLinkDone` stands for
// the route putting the plain issue address back.
vi.mock("@multica/core/api", () => ({
  api: { getBaseUrl: () => "", getAttachmentTextContent: vi.fn() },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));
vi.mock("../../../platform", () => ({ openExternal: vi.fn(), useImmersiveMode: () => {} }));
vi.mock("../../../editor/use-download-attachment", () => ({ useDownloadAttachment: () => vi.fn() }));
vi.mock("../../../editor/readonly-content", () => ({ ReadonlyContent: () => null }));
vi.mock("../../../editor/hooks/use-inline-media-url", () => ({
  useResignedInlineMedia: (_id: string | undefined, url: string) => ({ url, pending: false }),
}));
vi.mock("@multica/core/workspace/avatar-url", () => ({ resolvePublicFileUrl: (url: string) => url }));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Lambda" }),
}));
vi.mock("../../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/issues/MUL-1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p: string) => `https://app.example.test${p}`,
  }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { PreviewSequenceProvider, collectPreviewSequence } from "../../../editor";
import { DeliverablesOverview } from "./deliverables-overview";
import { OpenDeliverableOnLoad, useDeliverableLinkSession } from "./deliverable-link";

function image(id: string, filename: string, commentId: string): Attachment {
  return {
    id,
    workspace_id: "ws-1",
    issue_id: "issue-1",
    comment_id: commentId,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "agent",
    uploader_id: "lambda",
    filename,
    url: `https://cdn.example.test/${id}.png`,
    download_url: `https://cdn.example.test/${id}.png?sig=1`,
    markdown_url: `https://cdn.example.test/${id}.png`,
    content_type: "image/png",
    size_bytes: 10,
    created_at: "2026-09-20T10:00:00Z",
  };
}

const CHART = image("a-chart", "chart.png", "c-1");
const PHOTO = image("a-photo", "photo.png", "c-2");
const COMMENTS = [
  { id: "c-1", attachments: [CHART] },
  { id: "c-2", attachments: [PHOTO] },
];
const FILES = collectDeliverableFiles(COMMENTS);
const SEQUENCE = collectPreviewSequence(COMMENTS);

function LinkedIssue({ onLinkDone }: { onLinkDone: () => void }) {
  const link = useDeliverableLinkSession(onLinkDone);
  const [overview, setOverview] = useState<{ open: boolean; returnKey: string | null }>({
    open: false,
    returnKey: null,
  });
  const { onOverviewOpenedFromViewer, onOverviewClosed } = link;
  const openOverviewFromViewer = useCallback(
    (key: string) => {
      onOverviewOpenedFromViewer();
      setOverview({ open: true, returnKey: key });
    },
    [onOverviewOpenedFromViewer],
  );
  const closeOverview = useCallback(() => {
    setOverview((current) => ({ ...current, open: false }));
    onOverviewClosed();
  }, [onOverviewClosed]);
  return (
    <PreviewSequenceProvider
      items={SEQUENCE}
      onOpenOverview={openOverviewFromViewer}
      onOpened={link.onViewerOpened}
      onClosed={link.onViewerClosed}
    >
      <OpenDeliverableOnLoad
        name="chart.png"
        files={FILES}
        read="ready"
        onOpenedInViewer={link.onOpenedInViewer}
        onDone={onLinkDone}
      />
      <DeliverablesOverview
        open={overview.open}
        onClose={closeOverview}
        returnKey={overview.returnKey}
        identifier="MUL-1"
        files={FILES}
        commentById={new Map()}
        onLocate={() => {}}
      />
    </PreviewSequenceProvider>
  );
}

function renderLink() {
  const onLinkDone = vi.fn();
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <LinkedIssue onLinkDone={onLinkDone} />
    </QueryClientProvider>,
  );
  return { onLinkDone };
}

const overviewTitle = () => screen.queryByText("MUL-1 deliverables");
const viewerFile = (name: string) => screen.queryByRole("img", { name });

async function waitForViewerClosed() {
  await waitFor(() => expect(viewerFile("chart.png")).toBeNull());
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("a deliverable link's session", () => {
  it("gives the address back when the reader closes the viewer", async () => {
    const { onLinkDone } = renderLink();
    expect(viewerFile("chart.png")).not.toBeNull();
    act(() => {
      fireEvent.keyDown(document, { key: "Escape" });
    });
    await waitForViewerClosed();
    await waitFor(() => expect(onLinkDone).toHaveBeenCalledTimes(1));
  });

  it("keeps the address while the viewer hands over to the overview (G)", async () => {
    const { onLinkDone } = renderLink();
    act(() => {
      fireEvent.keyDown(document, { key: "g" });
    });
    await waitForViewerClosed();
    expect(overviewTitle()).not.toBeNull();
    // Let any queued work run: the address must still be the link's.
    await act(async () => {});
    expect(onLinkDone).not.toHaveBeenCalled();
  });

  it("keeps the address on the All deliverables button too", async () => {
    const { onLinkDone } = renderLink();
    act(() => {
      fireEvent.click(screen.getByRole("button", { name: "All deliverables" }));
    });
    await waitForViewerClosed();
    expect(overviewTitle()).not.toBeNull();
    await act(async () => {});
    expect(onLinkDone).not.toHaveBeenCalled();
  });

  it("gives the address back once the overview is closed", async () => {
    const { onLinkDone } = renderLink();
    act(() => {
      fireEvent.keyDown(document, { key: "g" });
    });
    await waitForViewerClosed();
    act(() => {
      fireEvent.click(screen.getByRole("button", { name: "Close" }));
    });
    await waitFor(() => expect(onLinkDone).toHaveBeenCalledTimes(1));
  });

  it("follows the reader back into the viewer, and ends when that closes", async () => {
    const { onLinkDone } = renderLink();
    act(() => {
      fireEvent.keyDown(document, { key: "g" });
    });
    await waitForViewerClosed();
    // G again: back to the file the viewer was showing.
    act(() => {
      fireEvent.keyDown(document, { key: "g" });
    });
    await waitFor(() => expect(viewerFile("chart.png")).not.toBeNull());
    await act(async () => {});
    expect(onLinkDone).not.toHaveBeenCalled();

    act(() => {
      fireEvent.keyDown(document, { key: "Escape" });
    });
    await waitForViewerClosed();
    await waitFor(() => expect(onLinkDone).toHaveBeenCalledTimes(1));
  });
});
