import { createRef } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cancelVoice,
  connectVoiceRuntime,
  getVoiceState,
  stopVoice,
  useVoiceModelsStore,
} from "@multica/core/voice";
import type { ReplyAnnotation } from "@multica/core/drafts/reply-annotation";
import { renderWithI18n } from "../../test/i18n";
import type { ContentEditorRef } from "../../editor";
import { AnnotationComposer } from "./annotation-composer";

const engine = vi.hoisted(() => ({ transcribe: vi.fn() }));

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
  act(() => useVoiceModelsStore.getState().setModels({
    engineAvailable: true,
    models: [{ id: "m", name: "M", author: "A", licenseUrl: "https://example.com", languages: ["zh"],
      languageHint: true, sizeBytes: 1, status: "ready", receivedBytes: 1 }],
  }));
});

afterEach(() => {
  act(() => cancelVoice());
  cleanup();
});

function renderComposer(onChange = vi.fn()) {
  const editorRef = createRef<ContentEditorRef>();
  renderWithI18n(<QueryClientProvider client={new QueryClient()}>
    <AnnotationComposer owner="reply:issue:root" annotation={annotation} existing={false} number={1}
      editorRef={editorRef} sourceMissing={false} sourceChanged={false} canSave
      onChange={onChange} onSave={() => true} onBusyChange={() => {}}
      controls={{
        submit: async () => {}, submitting: false, uploading: false, restart: false,
        recipients: <span>Lambda · Will start when sent</span>, draftContent: "", annotations: [],
      }} />
  </QueryClientProvider>);
  return editorRef;
}

describe("AnnotationComposer voice input", () => {
  it("dictates into the note, keeping the recipient beside the strip", async () => {
    engine.transcribe.mockResolvedValue({ ok: true, text: "请改一下" });
    const onChange = vi.fn();
    const editorRef = renderComposer(onChange);

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
