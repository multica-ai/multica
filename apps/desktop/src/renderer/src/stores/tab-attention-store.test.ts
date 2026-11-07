import { beforeEach, describe, expect, it } from "vitest";
import {
  attentionTargetTabIds,
  useTabAttentionStore,
  type TaskFinishedEvent,
} from "./tab-attention-store";

// Matching is pure; the store under it is plain (unpersisted) zustand, so
// this suite needs no DOM beyond jsdom's default presence.
const CHAT_EVENT: TaskFinishedEvent = {
  chat_session_id: "sess-1",
  issue_id: "issue-chat",
};
const ISSUE_EVENT: TaskFinishedEvent = { issue_id: "issue-1" };

function group(tabs: { id: string; url: string }[], activeTabId = tabs[0]!.id) {
  return { tabs, activeTabId };
}

describe("attentionTargetTabIds", () => {
  it("dots the chat tab showing the finished session", () => {
    const byWorkspace = {
      acme: group(
        [
          { id: "chat", url: "/acme/chat?session=sess-1" },
          { id: "other", url: "/acme/issues" },
        ],
        "other",
      ),
    };
    expect(attentionTargetTabIds(byWorkspace, "acme", CHAT_EVENT)).toEqual(["chat"]);
  });

  it("ignores the chat tab when it shows a different session or none", () => {
    const byWorkspace = {
      acme: group(
        [
          { id: "chat-a", url: "/acme/chat?session=sess-2" },
          { id: "chat-b", url: "/acme/chat" },
        ],
        "chat-b",
      ),
    };
    expect(attentionTargetTabIds(byWorkspace, "acme", CHAT_EVENT)).toEqual([]);
  });

  it("dots the issue detail tab matching the finished run's issue", () => {
    const byWorkspace = {
      acme: group(
        [
          { id: "detail", url: "/acme/issues/issue-1" },
          { id: "list", url: "/acme/issues" },
          { id: "other", url: "/acme/issues/issue-2" },
        ],
        "list",
      ),
    };
    expect(attentionTargetTabIds(byWorkspace, "acme", ISSUE_EVENT)).toEqual(["detail"]);
  });

  it("never dots the visible tab — active tab of the active workspace", () => {
    const byWorkspace = {
      acme: group([{ id: "chat", url: "/acme/chat?session=sess-1" }], "chat"),
    };
    expect(attentionTargetTabIds(byWorkspace, "acme", CHAT_EVENT)).toEqual([]);
    // Same tab, but its workspace is in the background: the user is not
    // looking at it, so it dots.
    expect(attentionTargetTabIds(byWorkspace, "other", CHAT_EVENT)).toEqual(["chat"]);
  });

  it("keeps hash and other params on a chat tab's url out of the match", () => {
    const byWorkspace = {
      acme: group(
        [
          { id: "chat", url: "/acme/chat?agent=a&session=sess-1#bottom" },
          { id: "list", url: "/acme/issues" },
        ],
        "list",
      ),
    };
    expect(attentionTargetTabIds(byWorkspace, "acme", CHAT_EVENT)).toEqual(["chat"]);
  });
});

describe("tabAttentionStore", () => {
  beforeEach(() => {
    useTabAttentionStore.setState({ attention: {} });
  });

  it("marks and clears without churning on repeated marks", () => {
    const states: number[] = [];
    const unsub = useTabAttentionStore.subscribe((s) => states.push(Object.keys(s.attention).length));

    useTabAttentionStore.getState().markTabAttention("t1");
    useTabAttentionStore.getState().markTabAttention("t1");
    expect(useTabAttentionStore.getState().attention).toEqual({ t1: true });

    useTabAttentionStore.getState().clearTabAttention("t1");
    useTabAttentionStore.getState().clearTabAttention("t1");
    expect(useTabAttentionStore.getState().attention).toEqual({});

    // mark / clear / clear each notified exactly once; the repeats were no-ops.
    expect(states).toEqual([1, 0]);
    unsub();
  });
});
