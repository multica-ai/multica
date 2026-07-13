// @vitest-environment jsdom
import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import {
  useCommentDraftStore,
  pruneStaleDrafts,
  type CommentDraft,
} from "./comment-draft-store";
import { setCurrentWorkspace } from "../../platform/workspace-storage";
import type { Attachment } from "../../types";

// Node 25 ships a partial `localStorage` shim under jsdom missing
// `clear`/`removeItem`; replace it so the persist middleware can round-trip.
beforeAll(() => {
  if (typeof globalThis.localStorage?.clear !== "function") {
    const values = new Map<string, string>();
    const storage: Storage = {
      get length() {
        return values.size;
      },
      clear: () => values.clear(),
      getItem: (k) => values.get(k) ?? null,
      key: (i) => Array.from(values.keys())[i] ?? null,
      removeItem: (k) => {
        values.delete(k);
      },
      setItem: (k, v) => {
        values.set(k, v);
      },
    };
    Object.defineProperty(globalThis, "localStorage", { configurable: true, value: storage });
    Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
  }
  setCurrentWorkspace("ws-1", "ws-1");
});

beforeEach(() => {
  useCommentDraftStore.setState({ drafts: {} });
});

function makeAttachment(id: string, url: string): Attachment {
  return {
    id,
    workspace_id: "ws-1",
    issue_id: "issue-1",
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "u1",
    filename: `${id}.png`,
    url,
    download_url: url,
    markdown_url: url,
    content_type: "image/png",
    size_bytes: 1,
    created_at: "2026-01-01T00:00:00Z",
  };
}

describe("comment-draft-store", () => {
  it("round-trips content, attachments and suppressed agents", () => {
    const att = makeAttachment("a1", "http://x/a1.png");
    useCommentDraftStore.getState().setDraft("new:issue-1", {
      content: "hi",
      attachments: [att],
      suppressedAgentIds: ["ag1"],
    });

    const draft = useCommentDraftStore.getState().getDraft("new:issue-1");
    expect(draft?.content).toBe("hi");
    expect(draft?.attachments).toEqual([att]);
    expect(draft?.suppressedAgentIds).toEqual(["ag1"]);
  });

  it("setDraft merges a partial patch, preserving unrelated fields", () => {
    const att = makeAttachment("a1", "http://x/a1.png");
    const store = useCommentDraftStore.getState();
    store.setDraft("new:issue-1", { content: "hi", attachments: [att] });
    store.setDraft("new:issue-1", { content: "hi there" }); // patch content only

    const draft = useCommentDraftStore.getState().getDraft("new:issue-1");
    expect(draft?.content).toBe("hi there");
    expect(draft?.attachments).toEqual([att]); // preserved
    expect(draft?.suppressedAgentIds).toEqual([]);
  });

  it("clearDraft removes the entry", () => {
    const store = useCommentDraftStore.getState();
    store.setDraft("new:issue-1", { content: "hi" });
    store.clearDraft("new:issue-1");
    expect(useCommentDraftStore.getState().getDraft("new:issue-1")).toBeUndefined();
  });

  it("normalizes an older content-only persisted draft (migration)", () => {
    const pruned = pruneStaleDrafts({
      "new:issue-1": {
        content: "legacy",
        updatedAt: Date.now(),
      } as Partial<CommentDraft>,
    });
    expect(pruned["new:issue-1"]).toEqual({
      content: "legacy",
      attachments: [],
      suppressedAgentIds: [],
      updatedAt: expect.any(Number),
    });
  });

  it("drops content-less and stale drafts on prune", () => {
    const now = Date.now();
    const pruned = pruneStaleDrafts({
      // Attachment-only draft with no body text: not submittable, dropped.
      empty: { content: "   ", attachments: [makeAttachment("a", "u")], updatedAt: now },
      stale: { content: "old", updatedAt: now - 40 * 24 * 60 * 60 * 1000 },
      keep: { content: "fresh", updatedAt: now },
    });
    expect(Object.keys(pruned)).toEqual(["keep"]);
  });
});
