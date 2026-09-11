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

  it("drops an upload that was in flight at reload", async () => {
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
    // The bytes are gone and nothing can act on the record: no surface renders
    // it and the document has no node for it (placeholders are never
    // serialised). Keeping it only held this empty draft alive for the TTL.
    expect(state.getUploads("new:issue-1")).toEqual([]);
    expect(state.getAttachments("new:issue-1")).toEqual([]);
    expect(state.getDraft("new:issue-1")).toBeFalsy();
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

  it("an identical setDraft is a no-op that preserves entry identity", () => {
    // The stale-submit guard compares entry identity; the composers' tab-switch
    // flush re-writes identical content mid-flight and must not mint a new
    // entry (that would read as an edit and keep a submitted draft alive).
    const s = useCommentDraftStore.getState();
    s.setDraft(KEY, "same words");
    const before = useCommentDraftStore.getState().drafts[KEY];
    s.setDraft(KEY, "same words");
    expect(useCommentDraftStore.getState().drafts[KEY]).toBe(before);

    s.setDraft(KEY, "different words");
    expect(useCommentDraftStore.getState().drafts[KEY]).not.toBe(before);
  });

  it("appendToDraftContent lands a fragment after existing text, keeping uploads", () => {
    const s = useCommentDraftStore.getState();
    s.setDraft(KEY, "wip text");
    s.addUpload(KEY, { clientUploadId: "c1", status: "uploading", filename: "shot.png", size: 9 });

    s.appendToDraftContent(KEY, "![shot.png](https://cdn.example/shot.png)");

    expect(useCommentDraftStore.getState().getDraft(KEY)).toBe(
      "wip text\n\n![shot.png](https://cdn.example/shot.png)",
    );
    expect(useCommentDraftStore.getState().getUploads(KEY)).toHaveLength(1);
  });

  it("appendToDraftContent on an empty draft is just the fragment", () => {
    const s = useCommentDraftStore.getState();
    s.appendToDraftContent(KEY, "[doc.pdf](https://cdn.example/doc.pdf)");

    expect(useCommentDraftStore.getState().getDraft(KEY)).toBe(
      "[doc.pdf](https://cdn.example/doc.pdf)",
    );
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

describe("reply annotations", () => {
  const key = "reply:issue-1:root" as const;
  const annotation = { id: "a", sourceCommentId: "nested-agent", sourceActorName: "Emacs", quote: "Selected text", note: "", start: 0, prefix: "", suffix: "" };
  beforeEach(async () => {
    setCurrentWorkspace("annotation-tests", "ws_annotations");
    await flush();
    useCommentDraftStore.setState({ drafts: {} });
  });
  afterEach(() => setCurrentWorkspace(null, null));

  it("keeps annotation-only drafts through text/upload changes and preserves the first exact reply target", () => {
    const store = useCommentDraftStore.getState();
    store.addAnnotation(key, annotation);
    store.setDraft(key, "");
    store.setAttachments(key, [makeAttachment("att")]);
    store.appendToDraftContent(key, "uploaded link");
    store.setAttachments(key, []);
    store.setDraft(key, "");
    store.addAnnotation(key, { ...annotation, id: "b", sourceCommentId: "another-agent" });
    store.removeAnnotation(key, "a");
    expect(store.getAnnotations(key)).toHaveLength(1);
    expect(useCommentDraftStore.getState().drafts[key]?.replyTarget?.commentId).toBe("nested-agent");
    store.removeAnnotation(key, "b");
    expect(store.getDraft(key)).toBeUndefined();
  });

  it("persists description annotations with the new comment draft without a reply target", async () => {
    const store = useCommentDraftStore.getState();
    store.setDraft("new:issue", "Existing draft");
    store.addAnnotation("new:issue", { ...annotation, sourceCommentId: "description:issue" });
    store.updateAnnotation("new:issue", annotation.id, "Description note");
    await flush();
    useCommentDraftStore.getState().drafts = {};
    await useCommentDraftStore.persist.rehydrate();
    expect(useCommentDraftStore.getState().getDraft("new:issue")).toBe("Existing draft");
    expect(useCommentDraftStore.getState().getAnnotations("new:issue")[0]?.note).toBe("Description note");
    expect(useCommentDraftStore.getState().drafts["new:issue"]?.replyTarget).toBeUndefined();
    expect(useCommentDraftStore.getState().addAnnotation("edit:issue:comment", annotation)).toBeUndefined();
  });

  it("reopens duplicate ranges, bounds collection, and changes identity only on actual edits", () => {
    const store = useCommentDraftStore.getState();
    expect(store.addAnnotation(key, annotation)).toBe("a");
    const snapshot = useCommentDraftStore.getState().drafts[key];
    expect(store.addAnnotation(key, { ...annotation, id: "duplicate" })).toBe("a");
    store.updateAnnotation(key, "a", "");
    store.setDraft(key, "");
    expect(useCommentDraftStore.getState().drafts[key]).toBe(snapshot);
    store.updateAnnotation(key, "a", "New intent");
    expect(useCommentDraftStore.getState().drafts[key]).not.toBe(snapshot);
    for (let i = 1; i < 20; i++) store.addAnnotation(key, { ...annotation, id: `a${i}`, start: i });
    expect(store.addAnnotation(key, { ...annotation, id: "overflow", start: 21 })).toBeUndefined();
    expect(store.addAnnotation("reply:other:root", { ...annotation, quote: "x".repeat(4001) })).toBeUndefined();
    expect(store.getAnnotations(key)).toHaveLength(20);
    expect(store.getAnnotations("reply:other:root")).toHaveLength(0);
  });

  it("restores annotation-only drafts and targets across rehydration", async () => {
    const store = useCommentDraftStore.getState();
    store.addAnnotation(key, annotation);
    store.updateAnnotation(key, "a", "Saved note");
    await flush();
    useCommentDraftStore.getState().drafts = {};
    await useCommentDraftStore.persist.rehydrate();
    expect(useCommentDraftStore.getState().getAnnotations(key)[0]?.note).toBe("Saved note");
    expect(useCommentDraftStore.getState().drafts[key]?.replyTarget?.commentId).toBe("nested-agent");
  });
  it("isolates annotations by workspace, expires old entries, and clears them on logout", async () => {
    const { resetAllRegisteredDrafts } = await import("../../drafts/cleanup-registry");
    setCurrentWorkspace("annotations-a", "ws_a");
    await flush();
    useCommentDraftStore.getState().addAnnotation(key, annotation);
    setCurrentWorkspace("annotations-b", "ws_b");
    await flush();
    expect(useCommentDraftStore.getState().getAnnotations(key)).toHaveLength(0);
    setCurrentWorkspace("annotations-a", "ws_a");
    await flush();
    expect(useCommentDraftStore.getState().getAnnotations(key)).toHaveLength(1);
    const saved = JSON.parse(localStorage.getItem("multica_comment_drafts:annotations-a")!);
    saved.state.drafts[key].updatedAt = Date.now() - 31 * DAY_MS;
    localStorage.setItem("multica_comment_drafts:annotations-a", JSON.stringify(saved));
    await useCommentDraftStore.persist.rehydrate();
    expect(useCommentDraftStore.getState().getAnnotations(key)).toHaveLength(0);
    useCommentDraftStore.getState().addAnnotation(key, annotation);
    resetAllRegisteredDrafts();
    expect(useCommentDraftStore.getState().getAnnotations(key)).toHaveLength(0);
    setCurrentWorkspace(null, null);
  });

});
