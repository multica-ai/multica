// @vitest-environment jsdom
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { useIssueDraftStore } from "./draft-store";
import { setCurrentWorkspace } from "../../platform/workspace-storage";

const flush = () => new Promise((resolve) => queueMicrotask(() => resolve(null)));

// Node 25 ships a partial `localStorage` shim under jsdom that's missing
// `clear`/`removeItem`; replace it with a real in-memory Storage so persist
// can round-trip values.
beforeAll(() => {
  if (typeof globalThis.localStorage?.clear !== "function") {
    const values = new Map<string, string>();
    const storage: Storage = {
      get length() { return values.size; },
      clear: () => values.clear(),
      getItem: (k) => values.get(k) ?? null,
      key: (i) => Array.from(values.keys())[i] ?? null,
      removeItem: (k) => { values.delete(k); },
      setItem: (k, v) => { values.set(k, v); },
    };
    Object.defineProperty(globalThis, "localStorage", { configurable: true, value: storage });
    Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
  }
});

const RESET_STATE = {
  draft: {
    title: "",
    description: "",
    status: "todo" as const,
    priority: "none" as const,
    assigneeType: undefined,
    assigneeId: undefined,
    startDate: null,
    dueDate: null,
    labelIds: [],
    propertyValues: {},
    attachments: [],
  },
  lastAssigneeType: undefined,
  lastAssigneeId: undefined,
};

describe("issue draft store — last assignee", () => {
  beforeEach(() => {
    useIssueDraftStore.setState(RESET_STATE);
  });

  it("clearDraft prefills the next draft with the remembered assignee", () => {
    const { setDraft, setLastAssignee, clearDraft } =
      useIssueDraftStore.getState();

    setDraft({ title: "first", assigneeType: "member", assigneeId: "alice" });
    setLastAssignee("member", "alice");
    clearDraft();

    const { draft } = useIssueDraftStore.getState();
    expect(draft.title).toBe("");
    expect(draft.assigneeType).toBe("member");
    expect(draft.assigneeId).toBe("alice");
  });

  it("clearDraft yields an empty assignee when none has ever been remembered", () => {
    const { setDraft, clearDraft } = useIssueDraftStore.getState();

    setDraft({ title: "first" });
    clearDraft();

    const { draft } = useIssueDraftStore.getState();
    expect(draft.assigneeType).toBeUndefined();
    expect(draft.assigneeId).toBeUndefined();
  });

  it("clearDraft removes persisted draft attachments", () => {
    const { setDraft, clearDraft } = useIssueDraftStore.getState();

    setDraft({
      title: "first",
      attachments: [
        {
          id: "11111111-2222-3333-4444-555555555555",
          workspace_id: "ws-1",
          issue_id: null,
          comment_id: null,
          chat_session_id: null,
          chat_message_id: null,
          uploader_type: "member",
          uploader_id: "alice",
          filename: "shot.png",
          url: "https://cdn.example.test/shot.png",
          download_url: "https://cdn.example.test/shot.png",
          markdown_url: "https://app.example.test/api/attachments/11111111-2222-3333-4444-555555555555/download",
          content_type: "image/png",
          size_bytes: 123,
          created_at: "2026-06-12T00:00:00Z",
        },
      ],
    });
    clearDraft();

    expect(useIssueDraftStore.getState().draft.attachments).toEqual([]);
  });

  it("clearDraft removes persisted custom property values", () => {
    const { setDraft, clearDraft } = useIssueDraftStore.getState();

    setDraft({ propertyValues: { "property-1": "option-1" } });
    clearDraft();

    expect(useIssueDraftStore.getState().draft.propertyValues).toEqual({});
  });

  it("setLastAssignee(undefined) lets the user opt back out of a default", () => {
    const { setLastAssignee, clearDraft } = useIssueDraftStore.getState();

    setLastAssignee("member", "alice");
    clearDraft();
    expect(useIssueDraftStore.getState().draft.assigneeId).toBe("alice");

    setLastAssignee(undefined, undefined);
    clearDraft();
    expect(useIssueDraftStore.getState().draft.assigneeId).toBeUndefined();
    expect(useIssueDraftStore.getState().draft.assigneeType).toBeUndefined();
  });
});

describe("issue draft store — legacy rehydrate", () => {
  beforeEach(() => {
    localStorage.clear();
    setCurrentWorkspace(null, null);
  });

  afterEach(() => {
    setCurrentWorkspace(null, null);
  });

  it("backfills attachments and custom properties for legacy drafts", async () => {
    localStorage.setItem(
      "multica_issue_draft:acme",
      JSON.stringify({
        state: {
          draft: {
            title: "legacy",
            description: "body",
            status: "todo",
            priority: "none",
            startDate: null,
            dueDate: null,
            // no `attachments` — written by a build that predates the field
          },
        },
        version: 0,
      }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();

    const { draft } = useIssueDraftStore.getState();
    expect(draft.title).toBe("legacy");
    expect(draft.attachments).toEqual([]);
    expect(draft.propertyValues).toEqual({});
  });
});
