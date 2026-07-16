import { beforeEach, describe, expect, it } from "vitest";
import { createChatStore, DRAFT_NEW_SESSION } from "./store";
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

// The pre-MUL-4864 scheme kept one new-chat draft per agent, in `__new__:<id>`
// slots. Those slots have no timestamp, so on upgrade only one can survive:
// the one for the agent the workspace has selected — the draft the user would
// have been shown. The rest were the invisible multi-draft state, and go.
describe("chat store — legacy per-agent new-chat draft migration", () => {
  const DRAFTS_KEY = "multica:chat:drafts";
  const ATTACHMENTS_KEY = "multica:chat:draft-attachments";
  const AGENT_KEY = "multica:chat:selectedAgentId";

  it("adopts the selected agent's legacy draft into the single new-chat slot", () => {
    const storage = memStorage();
    storage.setItem(AGENT_KEY, "agent-1");
    storage.setItem(
      DRAFTS_KEY,
      JSON.stringify({ "__new__:agent-1": "mine", "__new__:agent-2": "other" }),
    );

    const store = createChatStore({ storage });

    expect(store.getState().inputDrafts).toEqual({ [DRAFT_NEW_SESSION]: "mine" });
  });

  it("migrates the matching attachments with the text, not another agent's", () => {
    const storage = memStorage();
    storage.setItem(AGENT_KEY, "agent-1");
    storage.setItem(DRAFTS_KEY, JSON.stringify({ "__new__:agent-1": "mine" }));
    storage.setItem(
      ATTACHMENTS_KEY,
      JSON.stringify({
        "__new__:agent-1": [makeAttachment("att-mine")],
        "__new__:agent-2": [makeAttachment("att-other")],
      }),
    );

    const store = createChatStore({ storage });

    expect(store.getState().inputDraftAttachments[DRAFT_NEW_SESSION]?.map((a) => a.id)).toEqual([
      "att-mine",
    ]);
    expect(store.getState().inputDraftAttachments["__new__:agent-2"]).toBeUndefined();
  });

  it("persists the migration so the legacy slots do not come back on reload", () => {
    const storage = memStorage();
    storage.setItem(AGENT_KEY, "agent-1");
    storage.setItem(
      DRAFTS_KEY,
      JSON.stringify({ "__new__:agent-1": "mine", "__new__:agent-2": "other" }),
    );

    createChatStore({ storage });
    // A second store reads what the first one wrote — this is the reload.
    const reloaded = createChatStore({ storage });

    expect(JSON.parse(storage.getItem(DRAFTS_KEY) ?? "{}")).toEqual({ [DRAFT_NEW_SESSION]: "mine" });
    expect(reloaded.getState().inputDrafts).toEqual({ [DRAFT_NEW_SESSION]: "mine" });
  });

  it("drops every legacy slot when no agent is selected", () => {
    const storage = memStorage();
    storage.setItem(DRAFTS_KEY, JSON.stringify({ "__new__:agent-1": "a", "__new__:agent-2": "b" }));

    const store = createChatStore({ storage });

    expect(store.getState().inputDrafts).toEqual({});
    expect(storage.getItem(DRAFTS_KEY)).toBeNull();
  });

  it("leaves real session drafts untouched", () => {
    const storage = memStorage();
    storage.setItem(AGENT_KEY, "agent-1");
    storage.setItem(
      DRAFTS_KEY,
      JSON.stringify({ "session-a": "draft A", "session-b": "draft B", "__new__:agent-1": "mine" }),
    );

    const store = createChatStore({ storage });

    expect(store.getState().inputDrafts).toEqual({
      "session-a": "draft A",
      "session-b": "draft B",
      [DRAFT_NEW_SESSION]: "mine",
    });
  });

  it("keeps a current-scheme draft rather than overwriting it with a legacy one", () => {
    const storage = memStorage();
    storage.setItem(AGENT_KEY, "agent-1");
    storage.setItem(
      DRAFTS_KEY,
      JSON.stringify({ [DRAFT_NEW_SESSION]: "current", "__new__:agent-1": "stale" }),
    );

    const store = createChatStore({ storage });

    expect(store.getState().inputDrafts).toEqual({ [DRAFT_NEW_SESSION]: "current" });
  });

  it("does not touch storage when there is nothing to migrate", () => {
    const storage = memStorage();
    storage.setItem(DRAFTS_KEY, JSON.stringify({ [DRAFT_NEW_SESSION]: "typed" }));
    const before = storage.getItem(DRAFTS_KEY);

    createChatStore({ storage });

    expect(storage.getItem(DRAFTS_KEY)).toBe(before);
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

// The ledger is what makes a durable draft restore (#5219) apply at most once.
// A consume request can be lost — retries exhausted, app closed mid-flight — and
// the row then comes back on the next fetch. Without a record that survives the
// reload, the prompt would be restored into the composer a second time, after
// the user has already sent it.
describe("chat store — applied draft-restore ledger", () => {
  it("survives a reload so a lost consume cannot re-offer the restore", () => {
    const storage = memStorage();
    const store = createChatStore({ storage });

    store.getState().markDraftRestoreApplied("restore-1");
    expect(store.getState().appliedDraftRestoreIds).toEqual(["restore-1"]);

    const reloaded = createChatStore({ storage });
    expect(reloaded.getState().appliedDraftRestoreIds).toEqual(["restore-1"]);
  });

  it("is idempotent and drops the entry once the row is confirmed gone", () => {
    const store = createChatStore({ storage: memStorage() });

    store.getState().markDraftRestoreApplied("restore-1");
    store.getState().markDraftRestoreApplied("restore-1");
    expect(store.getState().appliedDraftRestoreIds).toEqual(["restore-1"]);

    store.getState().forgetDraftRestoreApplied("restore-1");
    expect(store.getState().appliedDraftRestoreIds).toEqual([]);
  });

  // Every entry in here is an unconfirmed consume: its row is still on the
  // server. Evicting one to cap the ledger would re-arm the restore it was
  // suppressing — the next fetch offers an already-applied prompt again and the
  // user can send it twice. Only server confirmation may compact this.
  it("never evicts an unconfirmed entry, however many pile up", () => {
    const store = createChatStore({ storage: memStorage() });
    for (let i = 0; i < 60; i++) store.getState().markDraftRestoreApplied(`r-${i}`);

    const ids = store.getState().appliedDraftRestoreIds;
    expect(ids).toHaveLength(60);
    expect(ids[0]).toBe("r-0");
    expect(ids[59]).toBe("r-59");

    // The one exit: the server confirmed the row is gone.
    store.getState().forgetDraftRestoreApplied("r-0");
    expect(store.getState().appliedDraftRestoreIds).toHaveLength(59);
    expect(store.getState().appliedDraftRestoreIds[0]).toBe("r-1");
  });
});
