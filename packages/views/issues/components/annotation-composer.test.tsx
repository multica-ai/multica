import { createRef } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import {
  DEFAULT_VOICE_SETTINGS,
  cancelVoice,
  connectVoiceRuntime,
  getVoiceState,
  stopVoice,
  useVoiceModelsStore,
  useVoiceSettingsStore,
} from "@multica/core/voice";
import type { ReplyAnnotation } from "@multica/core/drafts/reply-annotation";
import { useCommentDraftStore } from "@multica/core/issues/stores";
import type { Attachment } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import type { ContentEditorRef } from "../../editor";
import { AnnotationComposer } from "./annotation-composer";

const engine = vi.hoisted(() => ({ transcribe: vi.fn() }));
const uploadFile = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async (original) => {
  const actual = await original<typeof import("@multica/core/api")>();
  return { ...actual, api: { uploadFile } };
});
vi.mock("../../voice/recorder", () => ({ voiceInputSupported: () => true }));
vi.mock("../../voice/engine", async () => {
  const { useVoiceModelsStore } = await import("@multica/core/voice");
  return { useVoiceModels: () => useVoiceModelsStore((s) => s.models) };
});

const annotation: ReplyAnnotation = {
  id: "a1", sourceCommentId: "root", sourceActorName: "Root", quote: "Root text",
  start: 0, prefix: "", suffix: "", note: "",
};

beforeAll(() => {
  connectVoiceRuntime(() => ({
    record: () => ({
      start: () => Promise.resolve(),
      finish: () => Promise.resolve({ blob: new Blob(["clip"]), heardSpeech: true, durationMs: 1500 }),
      discard: () => {},
    }),
    prepare: () => {},
    transcribe: (...args) => engine.transcribe(...args),
    watchModels: () => {},
  }));
});

beforeEach(() => {
  uploadFile.mockReset();
  act(() => useVoiceModelsStore.getState().setModels({
    engineAvailable: true,
    models: [{ id: "m", name: "M", author: "A", licenseUrl: "https://example.com", languages: ["zh"],
      languageHint: true, sizeBytes: 1, status: "ready", receivedBytes: 1 }],
  }));
  act(() => useVoiceSettingsStore.getState().update({ enabled: true }));
});

afterEach(() => {
  act(() => cancelVoice());
  act(() => useVoiceSettingsStore.getState().update(DEFAULT_VOICE_SETTINGS));
  cleanup();
  useCommentDraftStore.setState({ drafts: {} });
});

function renderComposer(
  onChange = vi.fn(),
  overrides: Partial<React.ComponentProps<typeof AnnotationComposer>> = {},
) {
  const editorRef = createRef<ContentEditorRef>();
  const view = renderWithI18n(<QueryClientProvider client={new QueryClient()}>
    <AnnotationComposer owner="reply:issue:root" annotation={annotation} existing={false} number={1}
      editorRef={editorRef} sourceMissing={false} sourceChanged={false} canSave
      onChange={onChange} onSave={() => true} onBusyChange={() => {}}
      controls={{
        submit: async () => {}, submitting: false, uploading: false, restart: false,
        recipients: <span>Lambda · Will start when sent</span>, draftContent: "", annotations: [],
      }} {...overrides} />
  </QueryClientProvider>);
  return { ...view, editorRef };
}

describe("AnnotationComposer Chat attachments", () => {
  const draftKey = "chat:test-chat" as const;
  const fileUrl = "/api/attachments/attachment-1/download";
  const attachment: Attachment = {
    id: "attachment-1", filename: "notes.txt", size_bytes: 5, content_type: "text/plain",
    url: fileUrl, download_url: fileUrl, markdown_url: fileUrl,
    workspace_id: "workspace", issue_id: null, comment_id: null,
    chat_session_id: "test-chat", chat_message_id: null,
    uploader_type: "member", uploader_id: "member", created_at: new Date(0).toISOString(),
  };

  it("uploads from the picker and prevents saving before the file is ready", async () => {
    let finish!: (result: Attachment) => void;
    uploadFile.mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
    const onSave = vi.fn(() => true);
    const { container, editorRef } = renderComposer(vi.fn(), {
      owner: draftKey, attachmentDraftKey: draftKey, annotation: { ...annotation, note: "See this" }, onSave,
    });
    const file = new File(["notes"], "notes.txt", { type: "text/plain" });
    const note = screen.getByRole("textbox", { name: "Comment" });
    expect(screen.getByRole("button", { name: "Attach file" })).toBeEnabled();
    fireEvent.change(container.querySelector('input[type="file"]')!, { target: { files: [file] } });

    expect(uploadFile).toHaveBeenCalledWith(file, expect.objectContaining({ chatSessionId: "test-chat" }), expect.any(AbortSignal));
    expect(screen.getByRole("button", { name: "Add annotation" })).toBeDisabled();
    fireEvent.keyDown(note, { key: "Enter" });
    expect(onSave).not.toHaveBeenCalled();

    await act(async () => finish(attachment));
    expect(await screen.findByText("notes.txt")).toBeInTheDocument();
    expect(editorRef.current?.getMarkdown()).toBe("See this");
    expect(note).not.toContainElement(screen.getByText("notes.txt"));
    fireEvent.click(screen.getByRole("button", { name: "Add annotation" }));
    expect(onSave).toHaveBeenCalledWith("See this", true);
  });

  it("clears a rebuilt placeholder when an attachment-only draft fails after reopening", async () => {
    let fail!: (error: Error) => void;
    uploadFile.mockImplementation(() => new Promise((_resolve, reject) => { fail = reject; }));
    const props = { owner: draftKey, attachmentDraftKey: draftKey };
    const first = renderComposer(vi.fn(), props);
    const file = new File(["notes"], "notes.txt", { type: "text/plain" });
    fireEvent.change(first.container.querySelector('input[type="file"]')!, { target: { files: [file] } });
    const pending = useCommentDraftStore.getState().getPendingAnnotation(draftKey);
    expect(pending?.annotation.note).toBe("");
    first.unmount();

    renderComposer(vi.fn(), { ...props, annotation: pending!.annotation });
    expect(await screen.findByText("notes.txt")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add annotation" })).toBeDisabled();
    await act(async () => fail(new Error("Upload unavailable")));
    await waitFor(() => expect(screen.queryByText("notes.txt")).not.toBeInTheDocument());
    expect(useCommentDraftStore.getState().getPendingAnnotation(draftKey)).toBeUndefined();
    expect(screen.getByRole("button", { name: "Attach file" })).toBeEnabled();
  });

  it("stages successive image pastes outside the note and removes only the selected tile", async () => {
    uploadFile.mockImplementation(async (file: File) => ({
      ...attachment, id: file.name, filename: file.name, content_type: file.type,
    }));
    const save = vi.fn(() => true);
    const { editorRef } = renderComposer(vi.fn(), { owner: draftKey, attachmentDraftKey: draftKey, onSave: save });
    const note = screen.getByRole("textbox", { name: "Comment" });
    for (const name of ["before.png", "after.png"]) {
      const file = new File([name], name, { type: "image/png" });
      fireEvent.paste(note, { clipboardData: { files: [file] } });
      expect(await screen.findByRole("img", { name })).toBeInTheDocument();
    }
    expect(editorRef.current?.getMarkdown()).toBe("");
    expect(note.querySelector("img")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Remove attachment (before.png)" }));
    expect(screen.queryByRole("img", { name: "before.png" })).not.toBeInTheDocument();
    expect(screen.getByRole("img", { name: "after.png" })).toBeInTheDocument();
    expect(useCommentDraftStore.getState().getUploads(draftKey)).toMatchObject([{ filename: "after.png" }]);
    fireEvent.click(screen.getByRole("button", { name: "Add annotation" }));
    expect(save).toHaveBeenCalledWith("", true);
  });
});

describe("AnnotationComposer voice input", () => {
  it("dictates into the note, keeping the recipient beside the strip", async () => {
    engine.transcribe.mockResolvedValue({ ok: true, text: "请改一下" });
    const onChange = vi.fn();
    const { editorRef } = renderComposer(onChange);

    const mic = screen.getByRole("button", { name: /^Dictate/ });
    const send = screen.getByRole("button", { name: "Send" });
    expect(mic.compareDocumentPosition(send) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    await act(async () => fireEvent.click(mic, { detail: 0 }));
    expect(getVoiceState().phase).toBe("recording");
    expect(screen.getByText("Lambda · Will start when sent")).toBeVisible();
    expect(screen.getByRole("button", { name: "Insert" })).toBeInTheDocument();
    // Enter saves a note, so dictation here only inserts; nothing sends from the strip.
    expect(screen.queryByRole("button", { name: "Send" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add annotation" })).not.toBeInTheDocument();

    await act(async () => stopVoice(false));
    await waitFor(() => expect(editorRef.current?.getMarkdown()).toContain("请改一下"));
    expect(screen.getByRole("button", { name: "Add annotation" })).toBeInTheDocument();
  });

  it("opens model setup as part of the annotation, so it does not dismiss it", async () => {
    act(() => useVoiceModelsStore.getState().setModels({ engineAvailable: true, models: [] }));
    renderComposer();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: /^Dictate/ }), { detail: 0 }));
    expect(await screen.findByRole("dialog")).toHaveAttribute("data-editor-overlay-owner", "reply:issue:root");
  });
});
