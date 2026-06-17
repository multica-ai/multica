import { beforeEach, describe, expect, it } from "vitest";
import { createChatStore, newSessionDraftKey } from "./store";
import type { StorageAdapter } from "../types";

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

describe("newSessionDraftKey", () => {
  it("derives a stable per-agent slot for an uncreated chat", () => {
    expect(newSessionDraftKey("agent-1")).toBe("__new__:agent-1");
    expect(newSessionDraftKey(null)).toBe("__new__:");
  });
});

describe("chat store — migrateInputDraft", () => {
  let store: ReturnType<typeof createChatStore>;

  beforeEach(() => {
    store = createChatStore({ storage: memStorage() });
  });

  it("moves a draft to the new key and clears the source", () => {
    const from = newSessionDraftKey("agent-1");
    store.getState().setInputDraft(from, "!file[x.pdf]()");

    store.getState().migrateInputDraft(from, "session-1");

    const drafts = store.getState().inputDrafts;
    expect(drafts["session-1"]).toBe("!file[x.pdf]()");
    // Source slot is cleared so it can't resurface in the next new chat.
    expect(from in drafts).toBe(false);
  });

  it("is a no-op when the source draft is absent", () => {
    store.getState().setInputDraft("session-1", "keep me");

    store.getState().migrateInputDraft(newSessionDraftKey("agent-1"), "session-1");

    expect(store.getState().inputDrafts["session-1"]).toBe("keep me");
  });

  it("is a no-op when from === to", () => {
    store.getState().setInputDraft("session-1", "keep me");

    store.getState().migrateInputDraft("session-1", "session-1");

    expect(store.getState().inputDrafts["session-1"]).toBe("keep me");
  });
});
