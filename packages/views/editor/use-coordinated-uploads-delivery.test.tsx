import { useMemo, useRef } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { ApiClient, setApiInstance } from "@multica/core/api";
import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import { createChatStore, registerChatStore, useChatStore } from "@multica/core/chat";
import { abortAll, __trackedUploadCountForTest } from "@multica/core/drafts/upload-coordinator";
import type { DraftUpload } from "@multica/core/drafts";
import { I18nProvider } from "@multica/core/i18n/react";
import { createQueryClient } from "@multica/core/query-client";
import type { Attachment, StorageAdapter } from "@multica/core/types";
import enCommon from "../locales/en/common.json";
import enEditor from "../locales/en/editor.json";
import { ContentEditor, type ContentEditorRef } from "./content-editor";
import { useCoordinatedUploads, type UploadDraftBinding } from "./use-coordinated-uploads";
import { useUploadGate } from "./use-upload-gate";

const resources = { en: { common: enCommon, editor: enEditor } };
const emptyUploads: DraftUpload[] = [];
const attachment: Attachment = {
  id: "delayed-result", workspace_id: "ws", issue_id: null, comment_id: null,
  chat_session_id: null, chat_message_id: null, uploader_type: "member",
  uploader_id: "user", url: "/delayed.txt", download_url: "/delayed.txt",
  markdown_url: "/delayed.txt", filename: "delayed.txt", content_type: "text/plain",
  size_bytes: 7, created_at: "2026-01-01T00:00:00Z",
};
let finishUpload: (value: Attachment) => void;

beforeEach(() => {
  vi.useFakeTimers();
  const values = new Map<string, string>();
  const storage: StorageAdapter = {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => { values.set(key, value); },
    removeItem: (key) => { values.delete(key); },
  };
  const client = new ApiClient("");
  setApiInstance(client);
  registerAuthStore(createAuthStore({ api: client, storage, cookieAuth: true }));
  registerChatStore(createChatStore({ storage }));
  vi.spyOn(client, "uploadFile").mockImplementation(() => new Promise((resolve) => {
    finishUpload = resolve;
  }));
});

afterEach(() => {
  cleanup();
  abortAll();
  vi.clearAllTimers();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

// Readiness is controlled by mounting the real editor later. The upload hook,
// its retry timers, coordinator, draft store and Tiptap instance are production code.
function ReceivingComposer({ ready, target = "session" }: { ready: boolean; target?: string }) {
  const editorRef = useRef<ContentEditorRef | null>(null);
  const seen = useRef(new Set<string>());
  const present = useRef(new Set<string>());
  const uploads = useChatStore((s) => s.inputDraftAttachments[target] ?? emptyUploads);
  const binding = useMemo<UploadDraftBinding>(() => ({
    registryKey: `delivery:${target}`,
    getUploads: () => useChatStore.getState().inputDraftAttachments[target] ?? emptyUploads,
    addUpload: (u) => useChatStore.getState().addInputDraftUpload(target, u),
    settleUpload: (id, att) => useChatStore.getState().settleInputDraftUpload(target, id, att),
    failUpload: (id, err) => useChatStore.getState().failInputDraftUpload(target, id, err),
    removeUpload: (id) => useChatStore.getState().removeInputDraftUpload(target, id),
    getBody: () => useChatStore.getState().inputDrafts[target] ?? "",
    appendToBody: (md) => useChatStore.getState().appendToInputDraft(target, md),
  }), [target]);
  const gate = useUploadGate(editorRef);
  const coordinated = useCoordinatedUploads(binding, uploads, {}, gate, editorRef, {
    isUploadPlaceholderDeleted: (id) => seen.current.has(id) && !present.current.has(id),
  });
  return <>
    <button onClick={() => { void editorRef.current?.uploadFile(new File(["delayed"], "delayed.txt", { type: "text/plain" })); }}>Pick</button>
    {ready && <ContentEditor
      ref={editorRef}
      defaultValue={binding.getBody()}
      onUploadFile={coordinated.handleUpload}
      onUploadingChange={gate.onUploadingChange}
      onUploadPlaceholdersChange={(ids) => {
        ids.forEach((id) => seen.current.add(id));
        present.current = new Set(ids);
      }}
    />}
  </>;
}

function mountComposer(ready: boolean, target = "session") {
  const client = createQueryClient();
  const tree = (isReady: boolean) => <QueryClientProvider client={client}>
    <I18nProvider locale="en" resources={resources}>
      <ReceivingComposer ready={isReady} target={target} />
    </I18nProvider>
  </QueryClientProvider>;
  const view = render(tree(ready));
  return { ...view, setReady: () => view.rerender(tree(true)) };
}

async function tick(ms = 0) {
  await act(async () => { await vi.advanceTimersByTimeAsync(ms); });
}

async function startAndLeave() {
  const original = mountComposer(true);
  await tick();
  expect(original.container.querySelector(".tiptap")).not.toBeNull();
  fireEvent.click(original.getByText("Pick"));
  await tick();
  expect(original.container.querySelector(".tiptap")).toHaveTextContent("delayed.txt");
  expect(__trackedUploadCountForTest()).toBe(1);
  original.unmount();
}

async function settle() {
  await act(async () => { finishUpload(attachment); });
  expect(__trackedUploadCountForTest()).toBe(0);
}

describe("late upload delivery into a receiving editor", () => {
  it("retries rebuilding until ready and normally replaces the retained placeholder", async () => {
    await startAndLeave();
    const receiving = mountComposer(false);
    await tick(150);
    receiving.setReady();
    await tick(50);
    expect(receiving.container.querySelector(".tiptap")).toHaveTextContent("delayed.txt");
    await settle();
    expect(receiving.container.querySelector(".tiptap")?.textContent?.match(/delayed\.txt/g)).toHaveLength(1);
    expect(useChatStore.getState().inputDrafts.session).toContain("delayed.txt");
    expect(useChatStore.getState().inputDraftAttachments.session?.[0]?.status).toBe("uploaded");
  });

  it("delivers after false-to-ready when the upload settles before a placeholder can be drawn", async () => {
    await startAndLeave();
    const receiving = mountComposer(false);
    await settle();
    await tick(150);
    expect(useChatStore.getState().inputDrafts.session ?? "").toBe("");
    receiving.setReady();
    await tick(50);
    expect(receiving.container.querySelector(".tiptap")).toHaveTextContent("delayed.txt");
    expect(useChatStore.getState().inputDrafts.session).toContain("delayed.txt");
    expect(useChatStore.getState().inputDraftAttachments.session).toHaveLength(1);
  });

  it("keeps the live never-drawn fallback after rebuild retries stop", async () => {
    await startAndLeave();
    const timers = vi.spyOn(globalThis, "setTimeout");
    const receiving = mountComposer(false);
    await tick(5000);
    const retries = timers.mock.calls.filter(([, delay]) => delay === 50).length;
    expect(retries).toBeGreaterThan(0);
    await tick(5000);
    expect(timers.mock.calls.filter(([, delay]) => delay === 50)).toHaveLength(retries);
    receiving.setReady();
    await tick();
    expect(receiving.container.querySelector(".tiptap")).not.toHaveTextContent("delayed.txt");
    await settle();
    expect(receiving.container.querySelector(".tiptap")).toHaveTextContent("delayed.txt");
    expect(useChatStore.getState().inputDrafts.session).toContain("delayed.txt");
    expect(useChatStore.getState().inputDraftAttachments.session).toHaveLength(1);
  });

  it("stops delivery retries and preserves the result for a later editor mount", async () => {
    await startAndLeave();
    const receiving = mountComposer(false);
    const timers = vi.spyOn(globalThis, "setTimeout");
    await settle();
    await tick(5000);
    const retries = timers.mock.calls.filter(([, delay]) => delay === 50).length;
    expect(retries).toBeGreaterThan(0);
    const persisted = useChatStore.getState().inputDrafts.session;
    expect(persisted).toContain("delayed.txt");
    await tick(5000);
    expect(timers.mock.calls.filter(([, delay]) => delay === 50)).toHaveLength(retries);
    expect(useChatStore.getState().inputDrafts.session).toBe(persisted);
    receiving.setReady();
    await tick();
    expect(receiving.container.querySelector(".tiptap")?.textContent?.match(/delayed\.txt/g)).toHaveLength(1);
  });

  it("stops retrying a result after the target draft is cleared", async () => {
    await startAndLeave();
    mountComposer(false);
    await settle();
    act(() => { useChatStore.getState().clearInputDraft("session"); });
    const timers = vi.spyOn(globalThis, "setTimeout");
    await tick(10000);
    expect(timers.mock.calls.filter(([, delay]) => delay === 50)).toEqual([]);
    expect(useChatStore.getState().inputDrafts.session ?? "").toBe("");
    expect(useChatStore.getState().inputDraftAttachments.session ?? []).toEqual([]);
  });

  it("does not deliver one session's result into another session's live editor", async () => {
    await startAndLeave();
    const other = mountComposer(true, "other-session");
    await tick();
    await settle();
    expect(other.container.querySelector(".tiptap")).not.toHaveTextContent("delayed.txt");
    expect(useChatStore.getState().inputDrafts["other-session"] ?? "").toBe("");
    expect(useChatStore.getState().inputDraftAttachments["other-session"] ?? []).toEqual([]);
    expect(useChatStore.getState().inputDrafts.session).toContain("delayed.txt");
  });
});
