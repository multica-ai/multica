import { beforeEach, describe, expect, it } from "vitest";
import type { StorageAdapter } from "../types";
import { createChatStore, newSessionDraftKey } from "./store";

function makeStorage(initial: Record<string, string> = {}): StorageAdapter & {
  snapshot: () => Record<string, string>;
} {
  const data = { ...initial };
  return {
    getItem: (key) => data[key] ?? null,
    setItem: (key, value) => {
      data[key] = value;
    },
    removeItem: (key) => {
      delete data[key];
    },
    snapshot: () => ({ ...data }),
  };
}

describe("chat store open state", () => {
  it("defaults the floating chat window to closed for first-time users", () => {
    const storage = makeStorage();
    const store = createChatStore({ storage });

    expect(store.getState().isOpen).toBe(false);
  });

  it("persists an explicit open preference", () => {
    const storage = makeStorage();
    const store = createChatStore({ storage });

    store.getState().setOpen(true);

    expect(storage.snapshot()["multica:chat:isOpen"]).toBe("true");
    expect(createChatStore({ storage }).getState().isOpen).toBe(true);
  });

  it("persists an explicit closed preference", () => {
    const storage = makeStorage({ "multica:chat:isOpen": "true" });
    const store = createChatStore({ storage });

    store.getState().setOpen(false);

    expect(storage.snapshot()["multica:chat:isOpen"]).toBe("false");
    expect(createChatStore({ storage }).getState().isOpen).toBe(false);
  });
});

describe("newSessionDraftKey", () => {
  it("derives a stable per-agent slot for an uncreated chat", () => {
    expect(newSessionDraftKey("agent-1")).toBe("__new__:agent-1");
    expect(newSessionDraftKey(null)).toBe("__new__:");
  });
});

describe("chat store - migrateInputDraft", () => {
  let store: ReturnType<typeof createChatStore>;

  beforeEach(() => {
    store = createChatStore({ storage: makeStorage() });
  });

  it("moves a draft to the new key and clears the source", () => {
    const from = newSessionDraftKey("agent-1");
    store.getState().setInputDraft(from, "!file[x.pdf]()");

    store.getState().migrateInputDraft(from, "session-1");

    const drafts = store.getState().inputDrafts;
    expect(drafts["session-1"]).toBe("!file[x.pdf]()");
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
