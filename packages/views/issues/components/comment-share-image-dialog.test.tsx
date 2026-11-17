import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import type { TimelineEntry } from "@multica/core/types";
import { useCommentShareStore } from "@multica/core/issues/stores";
import { renderWithI18n } from "../../test/i18n";

const domToBlob = vi.fn(async () => new Blob(["png"], { type: "image/png" }));
vi.mock("modern-screenshot", () => ({ domToBlob: (...args: unknown[]) => domToBlob(...(args as [])) }));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Ada" }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

vi.mock("../../editor", () => ({
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
}));

import { CommentShareBar } from "./comment-share-image-dialog";

function comment(id: string, parentId: string | null, createdAt: string, extra: Partial<TimelineEntry> = {}): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "user-1",
    actor_name: "Ada",
    content: `body ${id}`,
    parent_id: parentId,
    comment_type: "comment",
    reactions: [],
    attachments: [],
    created_at: createdAt,
    updated_at: createdAt,
    revision: 1,
    ...extra,
  };
}

const rootA = comment("a", null, "2026-09-11T07:00:00Z");
const rootB = comment("b", null, "2026-09-11T08:00:00Z");
const replyA = comment("a1", "a", "2026-09-11T07:30:00Z");
const deletedReply = comment("a2", "a", "2026-09-11T07:40:00Z", { content: "", deleted_at: "2026-09-11T07:41:00Z" } as Partial<TimelineEntry>);

function renderBar() {
  return renderWithI18n(
    <CommentShareBar
      issueId="issue-1"
      identifier="YOT-1"
      title="Ship it"
      timeline={[rootA, rootB]}
      threadReplies={new Map([["a", [replyA, deletedReply]]])}
    />,
  );
}

describe("CommentShareBar", () => {
  beforeEach(() => {
    useCommentShareStore.getState().clear();
    domToBlob.mockClear();
  });

  it("renders nothing until threads are being picked on this issue", () => {
    useCommentShareStore.getState().start("issue-2", "x");
    renderBar();
    expect(screen.queryByRole("button", { name: "Create image" })).toBeNull();
  });

  it("renders the picked threads in timeline order and captures them", async () => {
    useCommentShareStore.getState().start("issue-1", "b");
    useCommentShareStore.getState().toggle("a");
    renderBar();
    expect(screen.getByText("Selected: 2")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Create image" }));
    const card = await screen.findByText("Ship it");
    const bodies = Array.from(card.closest("[data-share-card]")!.querySelectorAll("div"))
      .map((el) => el.textContent)
      .filter((text) => text?.startsWith("body "));
    expect(bodies).toEqual(["body a", "body a1", "body b"]);

    await waitFor(() => expect(domToBlob).toHaveBeenCalledTimes(1), { timeout: 2000 });
    await waitFor(() => expect(screen.getByRole("button", { name: "Download" })).toBeEnabled());
  });

  it("ends the selection when the bar leaves the issue", () => {
    useCommentShareStore.getState().start("issue-1", "a");
    const view = renderBar();
    act(() => view.unmount());
    expect(useCommentShareStore.getState().issueId).toBeNull();
  });
});
