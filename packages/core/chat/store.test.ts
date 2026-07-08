import { beforeEach, describe, expect, it } from "vitest";
import { createChatStore, newSessionDraftKey } from "./store";
import type { StorageAdapter } from "../types";
import type { Attachment } from "../types";

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

describe("chat store — floating window preference", () => {
  it("defaults OFF when no preference is stored (opt-in)", () => {
    const store = createChatStore({ storage: memStorage() });
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
