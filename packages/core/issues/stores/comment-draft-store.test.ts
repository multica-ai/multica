// @vitest-environment jsdom
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { useCommentDraftStore } from "./comment-draft-store";
import { setCurrentWorkspace } from "../../platform/workspace-storage";
import type { Attachment } from "../../types";

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

function makeAttachment(id: string): Attachment {
  return {
    id,
    workspace_id: "ws-1",
    issue_id: "issue-1",
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "alice",
    filename: `${id}.png`,
    url: `https://cdn.example.test/${id}.png`,
    download_url: `https://cdn.example.test/${id}.png`,
    markdown_url: `https://app.example.test/api/attachments/${id}/download`,
    content_type: "image/png",
    size_bytes: 123,
    created_at: "2026-06-12T00:00:00Z",
  };
}

const DAY_MS = 24 * 60 * 60 * 1000;

describe("comment draft store — attachments in the draft", () => {
  beforeEach(() => {
    useCommentDraftStore.setState({ drafts: {} });
  });

  it("persists attachments alongside content under the same key", () => {
    const { setDraft, setAttachments, getDraft, getAttachments } =
      useCommentDraftStore.getState();

    setDraft("new:issue-1", "look at this");
    setAttachments("new:issue-1", [makeAttachment("att-1")]);

    expect(getDraft("new:issue-1")).toBe("look at this");
    expect(getAttachments("new:issue-1").map((a) => a.id)).toEqual(["att-1"]);
  });

  it("setDraft preserves already-uploaded attachments", () => {
    const { setAttachments, setDraft, getAttachments } =
      useCommentDraftStore.getState();

    setAttachments("new:issue-1", [makeAttachment("att-1")]);
    setDraft("new:issue-1", "typed after uploading");

    expect(getAttachments("new:issue-1").map((a) => a.id)).toEqual(["att-1"]);
  });

  it("setAttachments preserves the in-progress text", () => {
    const { setDraft, setAttachments, getDraft } = useCommentDraftStore.getState();

    setDraft("new:issue-1", "half a sentence");
    setAttachments("new:issue-1", [makeAttachment("att-1")]);

    expect(getDraft("new:issue-1")).toBe("half a sentence");
  });

  it("returns a stable empty-attachments reference for a missing draft", () => {
    const { getAttachments } = useCommentDraftStore.getState();
    // A fresh allocation on each read would re-render every subscribing editor.
    expect(getAttachments("new:issue-1")).toBe(getAttachments("reply:issue-1:c-1"));
  });

  it("keeps an attachment-only draft (empty text) instead of dropping it", () => {
    const { setAttachments, setDraft, getAttachments, getDraft } =
      useCommentDraftStore.getState();

    setAttachments("new:issue-1", [makeAttachment("att-1")]);
    // The user clears all text but the uploaded file must survive.
    setDraft("new:issue-1", "");

    expect(getDraft("new:issue-1")).toBe("");
    expect(getAttachments("new:issue-1").map((a) => a.id)).toEqual(["att-1"]);
  });

  it("drops the entry once text AND attachments are both empty", () => {
    const { setDraft, setAttachments } = useCommentDraftStore.getState();

    setDraft("new:issue-1", "something");
    setAttachments("new:issue-1", [makeAttachment("att-1")]);

    setDraft("new:issue-1", "");
    setAttachments("new:issue-1", []);

    expect("new:issue-1" in useCommentDraftStore.getState().drafts).toBe(false);
  });

  it("clearDraft removes both content and attachments", () => {
    const { setDraft, setAttachments, clearDraft } = useCommentDraftStore.getState();

    setDraft("new:issue-1", "draft body");
    setAttachments("new:issue-1", [makeAttachment("att-1")]);
    clearDraft("new:issue-1");

    expect("new:issue-1" in useCommentDraftStore.getState().drafts).toBe(false);
    expect(useCommentDraftStore.getState().getAttachments("new:issue-1")).toEqual([]);
  });
});

describe("comment draft store — prune on rehydrate", () => {
  const KEY = "multica_comment_drafts:acme";

  beforeEach(() => {
    localStorage.clear();
    setCurrentWorkspace(null, null);
    useCommentDraftStore.setState({ drafts: {} });
  });

  afterEach(() => {
    setCurrentWorkspace(null, null);
  });

  function seed(drafts: Record<string, unknown>) {
    localStorage.setItem(KEY, JSON.stringify({ state: { drafts }, version: 0 }));
  }

  it("keeps a recent attachment-only draft through the TTL prune", async () => {
    seed({
      "new:issue-1": {
        content: "",
        attachments: [makeAttachment("att-1")],
        updatedAt: Date.now(),
      },
    });

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();

    const state = useCommentDraftStore.getState();
    expect(state.getDraft("new:issue-1")).toBe("");
    expect(state.getAttachments("new:issue-1").map((a) => a.id)).toEqual(["att-1"]);
  });

  it("drops a draft with neither text nor attachments", async () => {
    seed({
      "new:issue-1": { content: "   ", attachments: [], updatedAt: Date.now() },
    });

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();

    expect("new:issue-1" in useCommentDraftStore.getState().drafts).toBe(false);
  });

  it("drops a stale draft even when it still carries attachments", async () => {
    seed({
      "new:issue-1": {
        content: "old",
        attachments: [makeAttachment("att-1")],
        updatedAt: Date.now() - 31 * DAY_MS,
      },
    });

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();

    expect("new:issue-1" in useCommentDraftStore.getState().drafts).toBe(false);
  });

  it("backfills an empty attachments array for legacy drafts written before the field", async () => {
    seed({
      // No `attachments` — persisted by a build that predated the field.
      "new:issue-1": { content: "legacy body", updatedAt: Date.now() },
    });

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();

    const state = useCommentDraftStore.getState();
    expect(state.getDraft("new:issue-1")).toBe("legacy body");
    expect(state.getAttachments("new:issue-1")).toEqual([]);
  });

  it("coerces an upload that was in flight at reload into an interrupted placeholder", async () => {
    seed({
      "new:issue-1": {
        content: "",
        attachments: [
          { clientUploadId: "c1", status: "uploading", filename: "shot.png", size: 9 },
        ],
        updatedAt: Date.now(),
      },
    });

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();

    const state = useCommentDraftStore.getState();
    // The placeholder survives (so the UI can show "interrupted"), but its
    // bytes are gone — it is NOT a bindable attachment.
    const uploads = state.getUploads("new:issue-1");
    expect(uploads.map((u) => u.status)).toEqual(["interrupted"]);
    expect(state.getAttachments("new:issue-1")).toEqual([]);
  });
});

describe("comment draft store — upload lifecycle", () => {
  beforeEach(() => {
    useCommentDraftStore.setState({ drafts: {} });
  });

  const KEY = "new:issue-1" as const;

  it("addUpload records an uploading placeholder that blocks the bindable set", () => {
    const s = useCommentDraftStore.getState();
    s.addUpload(KEY, { clientUploadId: "c1", status: "uploading", filename: "shot.png", size: 9 });

    expect(s.getUploads(KEY).map((u) => u.status)).toEqual(["uploading"]);
    // A placeholder is not yet bindable.
    expect(s.getAttachments(KEY)).toEqual([]);
  });

  it("settleUpload swaps the placeholder for its attachment", () => {
    const s = useCommentDraftStore.getState();
    s.addUpload(KEY, { clientUploadId: "c1", status: "uploading", filename: "shot.png", size: 9 });
    s.settleUpload(KEY, "c1", makeAttachment("att-1"));

    expect(s.getUploads(KEY).map((u) => u.status)).toEqual(["uploaded"]);
    expect(s.getAttachments(KEY).map((a) => a.id)).toEqual(["att-1"]);
  });

  it("settleUpload is a no-op once the placeholder is gone (generation guard)", () => {
    const s = useCommentDraftStore.getState();
    s.addUpload(KEY, { clientUploadId: "c1", status: "uploading", filename: "shot.png", size: 9 });
    s.clearDraft(KEY);
    // Late settle after the draft was submitted/cleared must not resurrect it.
    s.settleUpload(KEY, "c1", makeAttachment("att-1"));

    expect(KEY in useCommentDraftStore.getState().drafts).toBe(false);
  });

  it("failUpload marks the placeholder failed but keeps it", () => {
    const s = useCommentDraftStore.getState();
    s.addUpload(KEY, { clientUploadId: "c1", status: "uploading", filename: "shot.png", size: 9 });
    s.failUpload(KEY, "c1", "network down");

    const uploads = s.getUploads(KEY);
    expect(uploads.map((u) => u.status)).toEqual(["failed"]);
    expect(uploads[0]).toMatchObject({ error: "network down" });
    expect(s.getAttachments(KEY)).toEqual([]);
  });

  it("removeUpload drops a placeholder and clears the draft when nothing is left", () => {
    const s = useCommentDraftStore.getState();
    s.addUpload(KEY, { clientUploadId: "c1", status: "uploading", filename: "shot.png", size: 9 });
    s.removeUpload(KEY, "c1");

    expect(KEY in useCommentDraftStore.getState().drafts).toBe(false);
  });

  it("keeps getAttachments referentially stable across unrelated touches", () => {
    const s = useCommentDraftStore.getState();
    s.addUpload(KEY, { clientUploadId: "c1", status: "uploading", filename: "shot.png", size: 9 });
    s.settleUpload(KEY, "c1", makeAttachment("att-1"));

    const first = useCommentDraftStore.getState().getAttachments(KEY);
    // A read again without a mutation must return the identical array.
    const second = useCommentDraftStore.getState().getAttachments(KEY);
    expect(first).toBe(second);
  });
});
