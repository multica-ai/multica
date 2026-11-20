import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import type { Editor } from "@tiptap/core";
import { closeHistory } from "@tiptap/pm/history";
import { QueryClientProvider } from "@tanstack/react-query";
import { ApiClient, setApiInstance } from "@multica/core/api";
import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import { createChatStore, registerChatStore, useChatStore } from "@multica/core/chat";
import { abortAll, __trackedUploadCountForTest } from "@multica/core/drafts/upload-coordinator";
import { I18nProvider } from "@multica/core/i18n/react";
import { createQueryClient } from "@multica/core/query-client";
import type { Attachment, StorageAdapter } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enChat from "../../locales/en/chat.json";
import enEditor from "../../locales/en/editor.json";
import enIssues from "../../locales/en/issues.json";
import { ChatInput } from "./chat-input";

const attachment: Attachment = {
  id: "upload-result",
  workspace_id: "ws",
  issue_id: null,
  comment_id: null,
  chat_session_id: null,
  chat_message_id: null,
  uploader_type: "member",
  uploader_id: "user",
  url: "/synthetic.txt",
  download_url: "/synthetic.txt",
  markdown_url: "/synthetic.txt",
  filename: "synthetic.txt",
  content_type: "text/plain",
  size_bytes: 9,
  created_at: "2026-01-01T00:00:00Z",
};
const resources = { en: { common: enCommon, chat: enChat, editor: enEditor, issues: enIssues } };
let finishUploads: Array<(value: Attachment) => void>;
let storedValues: Map<string, string>;

beforeEach(() => {
  finishUploads = [];
  const values = new Map<string, string>();
  storedValues = values;
  const storage: StorageAdapter = {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => { values.set(key, value); },
    removeItem: (key) => { values.delete(key); },
  };
  const client = new ApiClient("");
  setApiInstance(client);
  registerAuthStore(createAuthStore({ api: client, storage, cookieAuth: true }));
  registerChatStore(createChatStore({ storage }));
  useChatStore.setState({ activeSessionId: "session", selectedAgentId: "agent" });
  vi.spyOn(client, "uploadFile").mockImplementation(() => new Promise((resolve) => {
    finishUploads.push(resolve);
  }));
});

afterEach(() => {
  cleanup();
  abortAll();
  vi.restoreAllMocks();
});

function mountComposer() {
  const queryClient = createQueryClient();
  return render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider locale="en" resources={resources}>
        <ChatInput uploadEnabled agentName="Synthetic agent" onSend={() => false} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

async function pickFile(view: ReturnType<typeof mountComposer>) {
  await waitFor(() => expect(view.container.querySelector(".tiptap")).not.toBeNull());
  fireEvent.change(view.container.querySelector('input[type="file"]')!, {
    target: { files: [new File(["synthetic"], "synthetic.txt", { type: "text/plain" })] },
  });
  await waitFor(() => expect(view.container.querySelector(".tiptap")).toHaveTextContent("synthetic.txt"));
  expect(__trackedUploadCountForTest()).toBe(1);
}

async function deleteFile(view: ReturnType<typeof mountComposer>) {
  const editor = view.container.querySelector<HTMLElement>(".tiptap")!;
  editor.focus();
  fireEvent.keyDown(editor, { key: "a", code: "KeyA", metaKey: true, ctrlKey: true });
  fireEvent.keyDown(editor, { key: "Backspace", code: "Backspace" });
  await waitFor(() => expect(editor).not.toHaveTextContent("synthetic.txt"));
}

async function settle() {
  await act(async () => {
    finishUploads.forEach((finish, index) => finish({ ...attachment, id: `upload-result-${index}` }));
  });
  await waitFor(() => expect(__trackedUploadCountForTest()).toBe(0));
}

describe("ChatInput pending upload deletion across unmount", () => {
  it("keeps a rebuilt placeholder deleted while the reopened composer stays mounted", async () => {
    const view = mountComposer();
    await pickFile(view);
    view.unmount();
    const reopened = mountComposer();
    await waitFor(() => expect(reopened.container.querySelector(".tiptap")).toHaveTextContent("synthetic.txt"));
    await deleteFile(reopened);
    await settle();
    expect(reopened.container.querySelector(".tiptap")).not.toHaveTextContent("synthetic.txt");
    expect(useChatStore.getState().inputDrafts.session ?? "").toBe("");
    expect(useChatStore.getState().inputDraftAttachments.session ?? []).toEqual([]);
    expect(JSON.stringify([...storedValues])).not.toContain("synthetic.txt");
    reopened.unmount();
    const again = mountComposer();
    await waitFor(() => expect(again.container.querySelector(".tiptap")).not.toBeNull());
    expect(again.container.querySelector(".tiptap")).not.toHaveTextContent("synthetic.txt");
  });

  it("settles a retained rebuilt placeholder exactly once", async () => {
    const view = mountComposer();
    await pickFile(view);
    view.unmount();
    const reopened = mountComposer();
    await waitFor(() => expect(reopened.container.querySelector(".tiptap")).toHaveTextContent("synthetic.txt"));
    await settle();
    await waitFor(() => expect(useChatStore.getState().inputDrafts.session).toContain("synthetic.txt"));
    expect(reopened.container.querySelector(".tiptap")?.textContent?.match(/synthetic\.txt/g)).toHaveLength(1);
    expect(useChatStore.getState().inputDraftAttachments.session).toHaveLength(1);
    expect(useChatStore.getState().inputDraftAttachments.session?.[0]?.status).toBe("uploaded");
  });

  it("settles a rebuilt placeholder restored by undo before the response", async () => {
    const view = mountComposer();
    await pickFile(view);
    view.unmount();
    const reopened = mountComposer();
    await waitFor(() => expect(reopened.container.querySelector(".tiptap")).toHaveTextContent("synthetic.txt"));
    const editor = reopened.container.querySelector<HTMLElement & { editor: Editor }>(".tiptap")!;
    act(() => { editor.editor.view.dispatch(closeHistory(editor.editor.state.tr)); });
    await deleteFile(reopened);
    fireEvent.keyDown(editor, { key: "z", code: "KeyZ", ctrlKey: true });
    await waitFor(() => expect(editor).toHaveTextContent("synthetic.txt"));
    await settle();
    await waitFor(() => expect(useChatStore.getState().inputDrafts.session).toContain("synthetic.txt"));
    expect(editor.textContent?.match(/synthetic\.txt/g)).toHaveLength(1);
    expect(useChatStore.getState().inputDraftAttachments.session).toHaveLength(1);
  });

  it.each([[0, 1], [1, 0]])("preserves only B after deleting rebuilt A with completion order %s,%s", async (first, second) => {
    const view = mountComposer();
    await pickFile(view);
    view.unmount();
    const reopened = mountComposer();
    await waitFor(() => expect(reopened.container.querySelector(".tiptap")).toHaveTextContent("synthetic.txt"));
    await deleteFile(reopened);
    fireEvent.change(reopened.container.querySelector('input[type="file"]')!, {
      target: { files: [new File(["retained"], "retained.txt", { type: "text/plain" })] },
    });
    await waitFor(() => expect(finishUploads).toHaveLength(2));
    for (const index of [first, second]) {
      await act(async () => {
        finishUploads[index]!({
          ...attachment,
          id: `upload-result-${index}`,
          filename: index === 0 ? "synthetic.txt" : "retained.txt",
          url: `/upload-result-${index}.txt`,
          download_url: `/upload-result-${index}.txt`,
          markdown_url: `/upload-result-${index}.txt`,
        });
      });
      await waitFor(() => expect(__trackedUploadCountForTest()).toBe(index === first ? 1 : 0));
    }
    await waitFor(() => expect(useChatStore.getState().inputDrafts.session).toContain("retained.txt"));
    expect(reopened.container.querySelector(".tiptap")).not.toHaveTextContent("synthetic.txt");
    expect(reopened.container.querySelector(".tiptap")).toHaveTextContent("retained.txt");
    expect(useChatStore.getState().inputDraftAttachments.session).toMatchObject([
      { status: "uploaded", attachment: { id: "upload-result-1" } },
    ]);
    expect(JSON.stringify([...storedValues])).not.toContain("synthetic.txt");
  });

  it("keeps a deleted placeholder absent after unmount, late success and reopen", async () => {
    const view = mountComposer();
    await pickFile(view);
    await deleteFile(view);
    view.unmount();
    await settle();
    expect(useChatStore.getState().inputDrafts.session ?? "").toBe("");
    expect(useChatStore.getState().inputDraftAttachments.session ?? []).toEqual([]);
    const reopened = mountComposer();
    await waitFor(() => expect(reopened.container.querySelector(".tiptap")).not.toBeNull());
    expect(reopened.container.querySelector(".tiptap")).not.toHaveTextContent("synthetic.txt");
  });

  it("keeps an undeleted upload across unmount and late success", async () => {
    const view = mountComposer();
    await pickFile(view);
    view.unmount();
    await settle();
    expect(useChatStore.getState().inputDrafts.session).toContain("synthetic.txt");
    expect(useChatStore.getState().inputDraftAttachments.session?.[0]?.status).toBe("uploaded");
  });

  it("preserves a placeholder restored with undo before unmount", async () => {
    const view = mountComposer();
    await pickFile(view);
    const editor = view.container.querySelector<HTMLElement & { editor: Editor }>(".tiptap")!;
    // Give deletion its own undo step without depending on the history timer.
    act(() => { editor.editor.view.dispatch(closeHistory(editor.editor.state.tr)); });
    await deleteFile(view);
    fireEvent.keyDown(editor, { key: "z", code: "KeyZ", ctrlKey: true });
    await waitFor(() => expect(editor).toHaveTextContent("synthetic.txt"));
    view.unmount();
    await settle();
    expect(useChatStore.getState().inputDrafts.session).toContain("synthetic.txt");
    expect(useChatStore.getState().inputDraftAttachments.session?.[0]?.status).toBe("uploaded");
  });

  it("keeps deletion across a rapid reopen while the response is still held", async () => {
    const view = mountComposer();
    await pickFile(view);
    await deleteFile(view);
    view.unmount();
    const reopened = mountComposer();
    await waitFor(() => expect(reopened.container.querySelector(".tiptap")).not.toBeNull());
    await settle();
    expect(reopened.container.querySelector(".tiptap")).not.toHaveTextContent("synthetic.txt");
    expect(useChatStore.getState().inputDrafts.session ?? "").toBe("");
    expect(useChatStore.getState().inputDraftAttachments.session ?? []).toEqual([]);
  });

  it("prunes only the deleted upload when another file remains", async () => {
    const view = mountComposer();
    await pickFile(view);
    await deleteFile(view);
    fireEvent.change(view.container.querySelector('input[type="file"]')!, {
      target: { files: [new File(["retained"], "retained.txt", { type: "text/plain" })] },
    });
    await waitFor(() => expect(view.container.querySelector(".tiptap")).toHaveTextContent("retained.txt"));
    view.unmount();
    await settle();
    expect(useChatStore.getState().inputDraftAttachments.session).toHaveLength(1);
    expect(useChatStore.getState().inputDraftAttachments.session).toMatchObject([
      { status: "uploaded", attachment: { id: "upload-result-1" } },
    ]);
  });
});
