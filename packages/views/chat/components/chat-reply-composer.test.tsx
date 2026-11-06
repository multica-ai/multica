import { act, render, screen, fireEvent, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { createChatStore, registerChatStore, useChatStore } from "@multica/core/chat";
import type { Editor } from "@tiptap/core";
import enCommon from "../../locales/en/common.json";
import enChat from "../../locales/en/chat.json";
import enIssues from "../../locales/en/issues.json";
import type { ReplyAnnotation } from "@multica/core/drafts/reply-annotation";
import enEditor from "../../locales/en/editor.json";
import { ChatInput } from "./chat-input";

const live = vi.hoisted(() => ({ editor: null as Editor | null }));
// 使用真实 Tiptap 文档和防抖逻辑，仅保留测试文字与引用所需的扩展。
vi.mock("../../editor/extensions", async () => {
  const { default: StarterKit } = await import("@tiptap/starter-kit");
  const { Markdown } = await import("@tiptap/markdown");
  return { createEditorExtensions: () => [StarterKit, Markdown] };
});
vi.mock("@tiptap/react", async (importOriginal) => {
  const real = await importOriginal<typeof import("@tiptap/react")>();
  return { ...real, useEditor: (...args: Parameters<typeof real.useEditor>) => {
    const editor = real.useEditor(...args);
    live.editor = editor;
    return editor;
  } };
});
vi.mock("../../editor/bubble-menu", () => ({ EditorBubbleMenu: () => null }));
vi.mock("./chat-add-menu", () => ({ ChatAddMenu: () => null }));

beforeEach(() => {
  registerChatStore(createChatStore({ storage: {
    getItem: () => null, setItem: () => {}, removeItem: () => {},
  } }));
  useChatStore.getState().setActiveSession("session-a");
});

function annotation(id: string, note = "First opinion"): ReplyAnnotation {
  return { id, sourceCommentId: id, sourceActorName: "Mika", quote: `Quoted ${id}`,
    note, start: 0, prefix: "", suffix: "" };
}

function fixture(onSend = vi.fn()) {
  const onEditAnnotation = vi.fn(() => true);
  render(
    <I18nProvider locale="en" resources={{ en: { common: enCommon, chat: enChat, editor: enEditor, issues: enIssues } }}>
      <QueryClientProvider client={new QueryClient()}>
        <ChatInput onSend={onSend} onEditAnnotation={onEditAnnotation} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { onSend, onEditAnnotation };
}

function stage(...items: ReplyAnnotation[]) {
  act(() => useChatStore.getState().setInputDraftAnnotations("session-a", items));
}

describe("chat reply composer with real Tiptap", () => {
  it("keeps consecutive annotations compact without touching live typing, and composes them only on send", async () => {
    const { onSend, onEditAnnotation } = fixture();
    await waitFor(() => expect(live.editor).not.toBeNull());
    act(() => live.editor!.commands.insertContent("Unsaved draft"));
    stage(annotation("first"), annotation("second", "Second opinion"));
    expect(onSend).not.toHaveBeenCalled();
    expect(live.editor!.getText()).toBe("Unsaved draft");
    expect(document.querySelector(".tiptap blockquote")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "2 annotations" }));
    expect(await screen.findByText("Quoted first")).toBeVisible();
    expect(screen.getByText("Second opinion")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Edit annotation 1" }));
    await waitFor(() => expect(onEditAnnotation).toHaveBeenCalledWith("first"));
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(onSend).toHaveBeenCalledOnce());
    expect(onSend.mock.calls[0]![0]).toContain("> Quoted first");
    expect(onSend.mock.calls[0]![0]).toContain("Second opinion");
    expect(onSend.mock.calls[0]![0]).toContain("Unsaved draft");
    expect(useChatStore.getState().inputDraftAnnotations["session-a"]).toBeUndefined();
  });

  it("sends annotations with an empty body, retaining the compact draft after rejection", async () => {
    const onSend = vi.fn().mockResolvedValueOnce(false).mockResolvedValueOnce(true);
    fixture(onSend);
    await waitFor(() => expect(live.editor).not.toBeNull());
    stage(annotation("first"));
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(onSend).toHaveBeenCalledOnce());
    expect(screen.getByRole("button", { name: "1 annotation" })).toBeVisible();
    expect(live.editor!.getText()).toBe("");
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(onSend).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.queryByRole("button", { name: "1 annotation" })).toBeNull());
  });

  it("preserves annotations edited or added while a send is pending", async () => {
    let accept!: (value: boolean) => void;
    const onSend = vi.fn(() => new Promise<boolean>((resolve) => { accept = resolve; }));
    fixture(onSend);
    await waitFor(() => expect(live.editor).not.toBeNull());
    stage(annotation("first"));
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(onSend).toHaveBeenCalledOnce());
    stage(annotation("first", "Revised while sending"), annotation("second"));
    act(() => live.editor!.commands.insertContent("New body"));
    await act(async () => accept(true));
    expect(useChatStore.getState().inputDraftAnnotations["session-a"]).toHaveLength(2);
    expect(live.editor!.getText()).toBe("New body");
  });
});
