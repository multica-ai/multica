import { beforeEach, describe, expect, it } from "vitest";
import { createChatStore, newSessionDraftKey } from "./store";
import type { StorageAdapter } from "../types";
import type { Attachment } from "../types";
import { setCurrentWorkspace } from "../platform/workspace-storage";

function memStorage(): StorageAdapter {
  const m = new Map<string, string>();
  return {
    getItem: (k) => m.get(k) ?? null,
    setItem: (k, v) => {
      m.set(k, v);
    },
    removeItem: (k) => {
      m.delete(k);
    },
  };
}

function flushMicrotasks() {
  return new Promise<void>((resolve) => queueMicrotask(resolve));
}

function makeAttachment(id: string): Attachment {
  return {
    id,
    workspace_id: "ws-1",
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "user-1",
    filename: `${id}.png`,
    url: `/uploads/${id}.png`,
    download_url: `/api/attachments/${id}/download`,
    markdown_url: `/api/attachments/${id}/download`,
    content_type: "image/png",
    size_bytes: 1,
    created_at: new Date(0).toISOString(),
  };
}

describe("newSessionDraftKey", () => {
  it("derives a stable per-agent slot for an uncreated chat", () => {
    expect(newSessionDraftKey("agent-1")).toBe("__new__:agent-1");
    expect(newSessionDraftKey(null)).toBe("__new__:");
  });
});

describe("chat store — open/closed default", () => {
  it("starts closed when no preference is stored", () => {
    const store = createChatStore({ storage: memStorage() });
    expect(store.getState().isOpen).toBe(false);
  });

  it("honours an explicit stored 'open' preference", () => {
    const storage = memStorage();
    storage.setItem("multica:chat:isOpen", "true");
    const store = createChatStore({ storage });
    expect(store.getState().isOpen).toBe(true);
  });

  it("persists a toggle so the choice survives reload", () => {
    const storage = memStorage();
    const store = createChatStore({ storage });
    store.getState().setOpen(true);
    expect(storage.getItem("multica:chat:isOpen")).toBe("true");

    const reloaded = createChatStore({ storage });
    expect(reloaded.getState().isOpen).toBe(true);
  });
});

describe("chat store — draft attachments", () => {
  let store: ReturnType<typeof createChatStore>;

  beforeEach(() => {
    setCurrentWorkspace(null, null);
    store = createChatStore({ storage: memStorage() });
  });

  it("deduplicates attachment drafts by id", () => {
    store.getState().addInputDraftAttachment("draft-1", makeAttachment("att-1"));
    store.getState().addInputDraftAttachment("draft-1", {
      ...makeAttachment("att-1"),
      filename: "updated.png",
    });

    expect(store.getState().inputDraftAttachments["draft-1"]).toHaveLength(1);
    expect(store.getState().inputDraftAttachments["draft-1"]?.[0]?.filename).toBe("updated.png");
  });

  it("clearInputDraft clears both text and attachment records", () => {
    store.getState().setInputDraft("draft-1", "hello");
    store.getState().addInputDraftAttachment("draft-1", makeAttachment("att-1"));

    store.getState().clearInputDraft("draft-1");

    expect(store.getState().inputDrafts["draft-1"]).toBeUndefined();
    expect(store.getState().inputDraftAttachments["draft-1"]).toBeUndefined();
  });
});

describe("chat store — workspace rehydration", () => {
  beforeEach(() => {
    setCurrentWorkspace(null, null);
  });

  it("ignores legacy bare active session storage before a workspace slug exists", async () => {
    const storage = memStorage();
    storage.setItem("multica:chat:activeSessionId", "stale-session");
    const store = createChatStore({ storage });

    expect(store.getState().activeSessionId).toBeNull();

    setCurrentWorkspace("new-workspace", "ws_new");
    await flushMicrotasks();

    expect(store.getState().activeSessionId).toBeNull();
  });

  it("keeps the active chat session scoped to the current workspace", async () => {
    const storage = memStorage();
    const store = createChatStore({ storage });

    setCurrentWorkspace("acme", "ws_acme");
    await flushMicrotasks();
    store.getState().setActiveSession("session-acme");

    setCurrentWorkspace("beta", "ws_beta");
    await flushMicrotasks();

    expect(store.getState().activeSessionId).toBeNull();

    store.getState().setActiveSession("session-beta");

    setCurrentWorkspace("acme", "ws_acme");
    await flushMicrotasks();
    expect(store.getState().activeSessionId).toBe("session-acme");

    setCurrentWorkspace("beta", "ws_beta");
    await flushMicrotasks();
    expect(store.getState().activeSessionId).toBe("session-beta");
  });

  it("restores expanded chat state after the workspace slug becomes available", async () => {
    const storage = memStorage();
    storage.setItem("multica:chat:expanded:acme", "true");
    const store = createChatStore({ storage });

    expect(store.getState().isExpanded).toBe(false);

    setCurrentWorkspace("acme", "ws_acme");
    await flushMicrotasks();

    expect(store.getState().isExpanded).toBe(true);
  });

  it("clears expanded chat state when switching to a workspace without it", async () => {
    const storage = memStorage();
    storage.setItem("multica:chat:expanded:acme", "true");
    const store = createChatStore({ storage });

    setCurrentWorkspace("acme", "ws_acme");
    await flushMicrotasks();
    expect(store.getState().isExpanded).toBe(true);

    setCurrentWorkspace("beta", "ws_beta");
    await flushMicrotasks();

    expect(store.getState().isExpanded).toBe(false);
  });

  // Note: open/closed state is intentionally GLOBAL in Chat V2 (upstream
  // design), so it deliberately carries across workspaces — no per-workspace
  // open test here. Session/agent/drafts above remain workspace-scoped.

  it("restores chat size per workspace when switching workspaces", async () => {
    const storage = memStorage();
    const store = createChatStore({ storage });

    setCurrentWorkspace("acme", "ws_acme");
    await flushMicrotasks();
    store.getState().setChatSize(720, 640);

    setCurrentWorkspace("beta", "ws_beta");
    await flushMicrotasks();
    expect(store.getState().chatWidth).toBe(380);
    expect(store.getState().chatHeight).toBe(600);

    store.getState().setChatSize(420, 520);

    setCurrentWorkspace("acme", "ws_acme");
    await flushMicrotasks();
    expect(store.getState().chatWidth).toBe(720);
    expect(store.getState().chatHeight).toBe(640);
  });
});

describe("chat store — floating window preference", () => {
  it("defaults ON when no preference is stored", () => {
    const store = createChatStore({ storage: memStorage() });
    expect(store.getState().floatingChatEnabled).toBe(true);
  });

  it("honours an explicit stored 'false' preference (opt-out)", () => {
    const storage = memStorage();
    storage.setItem("multica:chat:floatingChatEnabled", "false");
    const store = createChatStore({ storage });
    expect(store.getState().floatingChatEnabled).toBe(false);
  });

  it("honours an explicit stored 'true' preference", () => {
    const storage = memStorage();
    storage.setItem("multica:chat:floatingChatEnabled", "true");
    const store = createChatStore({ storage });
    expect(store.getState().floatingChatEnabled).toBe(true);
  });

  it("persists an enable, then collapses an open overlay when disabled again", () => {
    const storage = memStorage();
    storage.setItem("multica:chat:floatingChatEnabled", "true");
    storage.setItem("multica:chat:isOpen", "true");
    const store = createChatStore({ storage });
    expect(store.getState().floatingChatEnabled).toBe(true);
    expect(store.getState().isOpen).toBe(true);

    store.getState().setFloatingChatEnabled(false);
    expect(store.getState().floatingChatEnabled).toBe(false);
    expect(store.getState().isOpen).toBe(false);
    expect(storage.getItem("multica:chat:floatingChatEnabled")).toBe("false");

    // A fresh store rehydrates the persisted preference.
    const reopened = createChatStore({ storage });
    expect(reopened.getState().floatingChatEnabled).toBe(false);

    store.getState().setFloatingChatEnabled(true);
    expect(store.getState().floatingChatEnabled).toBe(true);
    expect(storage.getItem("multica:chat:floatingChatEnabled")).toBe("true");
  });
});
