import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import type { TaskCompletedPayload } from "@multica/core/types";
import { useTabStore } from "@/stores/tab-store";
import { useTabAttentionStore } from "@/stores/tab-attention-store";
import { useTabAttentionSync } from "./use-tab-attention-sync";

// Capture the handlers the hook registers; tests fire them as if the
// workspace-scoped WebSocket had delivered a finished run.
const wsHandlers = vi.hoisted(() => new Map<string, (payload: unknown) => void>());
vi.mock("@multica/core/realtime", () => ({
  useWSEvent: (event: string, handler: (payload: unknown) => void) => {
    wsHandlers.set(event, handler);
  },
}));

function fireTaskCompleted(payload: Partial<TaskCompletedPayload>) {
  wsHandlers.get("task:completed")!({
    task_id: "task-1",
    agent_id: "agent-1",
    issue_id: "",
    status: "completed",
    ...payload,
  });
}

function dottedTabIds() {
  return Object.keys(useTabAttentionStore.getState().attention);
}

/** url of the workspace's tab with this resourceKey, by title for lookup. */
function tabIdFor(url: string) {
  const group = useTabStore.getState().byWorkspace.acme!;
  const { pathname } = { pathname: url.split("?")[0]! };
  const tab = group.tabs.find((t) => t.url === url || t.url.startsWith(`${pathname}?`));
  if (!tab) throw new Error(`no tab for ${url}`);
  return tab.id;
}

describe("useTabAttentionSync", () => {
  beforeEach(() => {
    localStorage.clear();
    wsHandlers.clear();
    useTabAttentionStore.setState({ attention: {} });
    useTabStore.getState().reset();
    useTabStore.getState().switchWorkspace("acme", "/acme/issues");
    renderHook(() => useTabAttentionSync());
  });

  it("dots a background chat tab when its session's run finishes", () => {
    useTabStore.getState().openTab("/acme/chat?session=sess-1", "Chat");

    fireTaskCompleted({ chat_session_id: "sess-1", issue_id: "issue-chat" });

    expect(dottedTabIds()).toEqual([tabIdFor("/acme/chat")]);
  });

  it("dots a background issue tab when its issue's run finishes", () => {
    useTabStore.getState().openTab("/acme/issues/issue-9", "MUL-9");

    fireTaskCompleted({ issue_id: "issue-9" });

    expect(dottedTabIds()).toEqual([tabIdFor("/acme/issues/issue-9")]);
  });

  it("does not dot the chat tab the user is currently looking at", () => {
    useTabStore.getState().openTab("/acme/chat?session=sess-1", "Chat", { activate: true });

    fireTaskCompleted({ chat_session_id: "sess-1", issue_id: "issue-chat" });

    expect(dottedTabIds()).toEqual([]);
  });

  it("also dots on task:failed — a failed run finished too", () => {
    useTabStore.getState().openTab("/acme/issues/issue-9", "MUL-9");

    wsHandlers.get("task:failed")!({
      task_id: "task-1",
      agent_id: "agent-1",
      issue_id: "issue-9",
      status: "failed",
    });

    expect(dottedTabIds()).toEqual([tabIdFor("/acme/issues/issue-9")]);
  });

  it("ignores a finished chat session the chat tab is not showing", () => {
    useTabStore.getState().openTab("/acme/chat?session=sess-1", "Chat");

    fireTaskCompleted({ chat_session_id: "sess-other", issue_id: "issue-chat" });

    expect(dottedTabIds()).toEqual([]);
  });
});
