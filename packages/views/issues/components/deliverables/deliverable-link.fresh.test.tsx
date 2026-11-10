import { beforeEach, describe, expect, it, vi } from "vitest";
import { act } from "@testing-library/react";
import { useMemo } from "react";
import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import { createQueryClient } from "@multica/core/query-client";
import { issueKeys } from "@multica/core/issues/queries";
import { collectDeliverableFiles } from "@multica/core/attachments/deliverables";
import type { Attachment, TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../../test/i18n";

// The real timeline hook and QueryClient defaults (staleTime: Infinity), with
// the API answer held back so each test decides when the server replies.
const { listTimelineMock, openAtMock, toastMock } = vi.hoisted(() => ({
  listTimelineMock: vi.fn(),
  openAtMock: vi.fn((_key: string, _options?: { stayOnError?: boolean }) => true),
  toastMock: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@multica/core/api", () => ({ api: { listTimeline: listTimelineMock } }));
vi.mock("@multica/core/realtime", () => ({ useWSEvent: () => {}, useWSReconnect: () => {} }));
// Comment mutations need a workspace and play no part in reading the timeline.
vi.mock("@multica/core/issues/mutations", () => {
  const mutation = () => ({ mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false });
  return {
    useCreateComment: mutation,
    useUpdateComment: mutation,
    useDeleteComment: mutation,
    useResolveComment: mutation,
    useToggleCommentReaction: mutation,
  };
});
vi.mock("sonner", () => ({ toast: toastMock }));
vi.mock("../../../editor", () => ({
  usePreviewSequence: () => ({ openAt: openAtMock }),
  useAttachmentPreview: () => ({ open: vi.fn(), tryOpen: () => false, modal: null }),
  useDownloadAttachment: () => vi.fn(),
}));

import { useIssueTimeline } from "../../hooks/use-issue-timeline";
import { OpenDeliverableOnLoad, useFreshTimelineRead } from "./deliverable-link";

const ISSUE = "issue-1";
const NAME = "report.html";

function attachment(id: string, commentId: string): Attachment {
  return {
    id,
    workspace_id: "ws-1",
    issue_id: ISSUE,
    comment_id: commentId,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "agent",
    uploader_id: "lambda",
    filename: NAME,
    url: `https://cdn.example.test/${id}`,
    download_url: `https://cdn.example.test/${id}?sig=1`,
    markdown_url: `https://cdn.example.test/${id}`,
    content_type: "text/html",
    size_bytes: 1024,
    created_at: `2026-09-2${commentId.slice(-1)}T10:00:00Z`,
  };
}

function comment(id: string, files: Attachment[]): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "agent",
    actor_id: "lambda",
    content: "",
    created_at: files[0]!.created_at,
    updated_at: files[0]!.created_at,
    attachments: files,
  } as TimelineEntry;
}

const WITH_V1 = [comment("c-1", [attachment("v1", "c-1")])];
const WITH_V2 = [...WITH_V1, comment("c-2", [attachment("v2", "c-2")])];

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

// What IssueDetail does with a deliverable link, minus the rest of the page.
function LinkedIssue({ onDone }: { onDone: () => void }) {
  const { timeline, dataUpdatedAt } = useIssueTimeline(ISSUE);
  const files = useMemo(() => collectDeliverableFiles(timeline), [timeline]);
  const read = useFreshTimelineRead(ISSUE, NAME, dataUpdatedAt);
  return (
    <OpenDeliverableOnLoad
      name={NAME}
      files={files}
      read={read}
      onOpenedInViewer={() => {}}
      onDone={onDone}
    />
  );
}

function renderLink(qc: QueryClient) {
  const onDone = vi.fn();
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <LinkedIssue onDone={onDone} />
    </QueryClientProvider>,
  );
  return { onDone };
}

let qc: QueryClient;
beforeEach(() => {
  vi.clearAllMocks();
  qc = createQueryClient();
});

describe("a deliverable link with the issue's timeline already cached", () => {
  it("opens the version the server has now, not the cached one", async () => {
    // An earlier visit cached v1; v2 was uploaded since, and the cache was
    // invalidated (or not) on the way back.
    qc.setQueryData(issueKeys.timeline(ISSUE), WITH_V1);
    await qc.invalidateQueries({ queryKey: issueKeys.timeline(ISSUE), refetchType: "none" });
    const reply = deferred<TimelineEntry[]>();
    listTimelineMock.mockReturnValueOnce(reply.promise);

    const { onDone } = renderLink(qc);
    expect(openAtMock).not.toHaveBeenCalled();

    await act(async () => reply.resolve(WITH_V2));
    expect(openAtMock).toHaveBeenCalledTimes(1);
    expect(openAtMock).toHaveBeenCalledWith("v2", { stayOnError: true });
    expect(onDone).not.toHaveBeenCalled();
  });

  it("reads the server even when the cache counts as fresh", async () => {
    // staleTime: Infinity, not invalidated: mounting alone would not refetch.
    qc.setQueryData(issueKeys.timeline(ISSUE), WITH_V1);
    const reply = deferred<TimelineEntry[]>();
    listTimelineMock.mockReturnValueOnce(reply.promise);

    renderLink(qc);
    expect(listTimelineMock).toHaveBeenCalledTimes(1);
    expect(openAtMock).not.toHaveBeenCalled();

    await act(async () => reply.resolve(WITH_V2));
    expect(openAtMock).toHaveBeenCalledWith("v2", { stayOnError: true });
  });

  it("does not call a file missing because a cached timeline is empty", async () => {
    qc.setQueryData(issueKeys.timeline(ISSUE), []);
    await qc.invalidateQueries({ queryKey: issueKeys.timeline(ISSUE), refetchType: "none" });
    const reply = deferred<TimelineEntry[]>();
    listTimelineMock.mockReturnValueOnce(reply.promise);

    const { onDone } = renderLink(qc);
    expect(toastMock.error).not.toHaveBeenCalled();
    expect(onDone).not.toHaveBeenCalled();

    await act(async () => reply.resolve(WITH_V1));
    expect(openAtMock).toHaveBeenCalledWith("v1", { stayOnError: true });
    expect(toastMock.error).not.toHaveBeenCalled();
    expect(onDone).not.toHaveBeenCalled();
  });

  it("still reports a file the current timeline really does not have", async () => {
    qc.setQueryData(issueKeys.timeline(ISSUE), WITH_V1);
    listTimelineMock.mockResolvedValueOnce([]);

    const { onDone } = renderLink(qc);
    await act(async () => {});
    expect(openAtMock).not.toHaveBeenCalled();
    expect(toastMock.error).toHaveBeenCalledWith(expect.stringContaining(NAME));
    expect(onDone).toHaveBeenCalledTimes(1);
  });
});

describe("a deliverable link whose timeline read fails", () => {
  it("says the load failed and keeps the link, instead of calling the file missing", async () => {
    qc.setQueryData(issueKeys.timeline(ISSUE), []);
    // The app retries a failed query once; both attempts fail here.
    listTimelineMock.mockRejectedValue(new Error("offline"));

    const { onDone } = renderLink(qc);
    await vi.waitFor(() => expect(toastMock.error).toHaveBeenCalledTimes(1), { timeout: 5000 });
    expect(toastMock.error).toHaveBeenCalledWith(expect.stringMatching(/load|reload/i));
    expect(toastMock.error).not.toHaveBeenCalledWith(expect.stringContaining("No deliverable"));
    expect(openAtMock).not.toHaveBeenCalled();
    // The address keeps the link, so reloading tries again.
    expect(onDone).not.toHaveBeenCalled();
  });
});
