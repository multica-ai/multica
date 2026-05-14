import { forwardRef, useRef, useImperativeHandle } from "react";
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enChat from "../../locales/en/chat.json";

const TEST_RESOURCES = { en: { common: enCommon, chat: enChat } };

// Track drop-zone callbacks so the test can simulate a real drop.
const dropHandlers = vi.hoisted(() => ({
  onDrop: null as null | ((files: File[]) => void),
}));

vi.mock("../../editor", () => ({
  useFileDropZone: ({ onDrop }: { onDrop: (files: File[]) => void }) => {
    dropHandlers.onDrop = onDrop;
    return { isDragOver: false, dropZoneProps: { "data-testid": "drop-zone" } };
  },
  FileDropOverlay: () => null,
  ContentEditor: forwardRef(function MockContentEditor(
    {
      defaultValue,
      onUpdate,
      placeholder,
      onUploadFile,
    }: {
      defaultValue?: string;
      onUpdate?: (md: string) => void;
      placeholder?: string;
      onUploadFile?: (file: File) => Promise<{ id: string; link: string; filename: string } | null>;
    },
    ref: React.Ref<unknown>,
  ) {
    const valueRef = useRef<string>(defaultValue ?? "");
    const uploadingRef = useRef(0);
    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => {
        valueRef.current = "";
      },
      blur: () => {},
      focus: () => {},
      uploadFile: async (file: File) => {
        uploadingRef.current += 1;
        try {
          const result = await onUploadFile?.(file);
          if (result) {
            valueRef.current = `${valueRef.current}![](${result.link})`.trim();
            onUpdate?.(valueRef.current);
          }
        } finally {
          uploadingRef.current = Math.max(0, uploadingRef.current - 1);
        }
      },
      hasActiveUploads: () => uploadingRef.current > 0,
    }));
    return (
      <textarea
        data-testid="editor"
        placeholder={placeholder}
        onChange={(e) => {
          valueRef.current = e.target.value;
          onUpdate?.(e.target.value);
        }}
      />
    );
  }),
}));

// Mock chat store with an in-memory implementation that supports both
// (selector) calls and getState().
vi.mock("@multica/core/chat", () => {
  const state = {
    activeSessionId: null as string | null,
    selectedAgentId: "agent-1",
    inputDrafts: {} as Record<string, string>,
    focusMode: false,
    setInputDraft: vi.fn(),
    clearInputDraft: vi.fn(),
  };
  return {
    DRAFT_NEW_SESSION: "__draft_new__",
    useChatStore: Object.assign(
      (selector?: (s: typeof state) => unknown) =>
        selector ? selector(state) : state,
      { getState: () => state },
    ),
  };
});

import { ChatInput } from "./chat-input";

function renderInput(props: Partial<React.ComponentProps<typeof ChatInput>> = {}) {
  const onSend = props.onSend ?? vi.fn();
  const onUploadFile =
    props.onUploadFile ??
    vi.fn(async (_file: File) => ({
      id: "att-1",
      link: "https://cdn.example/att-1.png",
      filename: "img.png",
    }));
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ChatInput onSend={onSend} onUploadFile={onUploadFile} agentName="Multica" {...props} />
    </I18nProvider>,
  );
  return { onSend, onUploadFile };
}

describe("ChatInput attachment wiring", () => {
  it("routes dropped files through the editor's upload handler", async () => {
    const { onUploadFile } = renderInput();
    expect(dropHandlers.onDrop).not.toBeNull();
    const file = new File(["x"], "drop.png", { type: "image/png" });
    dropHandlers.onDrop?.([file]);
    // Microtask: the mock editor awaits onUploadFile before mutating its value.
    await Promise.resolve();
    await Promise.resolve();
    expect(onUploadFile).toHaveBeenCalledWith(file);
  });

  it("passes attachment_ids to onSend for uploads still referenced in the content", async () => {
    const onSend = vi.fn();
    const onUploadFile = vi.fn(async (_file: File) => ({
      id: "att-42",
      link: "https://cdn.example/att-42.png",
      filename: "x.png",
    }));
    renderInput({ onSend, onUploadFile });

    // Simulate the drop → editor.uploadFile → onUploadFile happy path. The
    // mock editor appends the markdown link into its value and calls
    // onUpdate so the input flips out of the empty state.
    const file = new File(["x"], "drop.png", { type: "image/png" });
    dropHandlers.onDrop?.([file]);

    // Wait for the submit button to become enabled (onUpdate has fired and
    // React has re-rendered). SubmitButton has no aria-label, so we pick
    // the last action button on the bar (FileUploadButton, SubmitButton).
    let sendButton: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole("button");
      sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).not.toBeDisabled();
    });
    fireEvent.click(sendButton!);

    expect(onSend).toHaveBeenCalledTimes(1);
    const [, ids] = onSend.mock.calls[0]!;
    expect(ids).toEqual(["att-42"]);
  });

  it("disables send while an upload is in flight, re-enables after it resolves", async () => {
    let resolveUpload: (v: { id: string; link: string; filename: string }) => void;
    const uploadPromise = new Promise<{ id: string; link: string; filename: string }>((res) => {
      resolveUpload = res;
    });
    const onSend = vi.fn();
    const onUploadFile = vi.fn(() => uploadPromise);
    renderInput({ onSend, onUploadFile });

    // Give the editor some text so isEmpty=false — this isolates the
    // disabled state to the pending-upload condition (otherwise both
    // checks would fire and the test couldn't tell them apart).
    fireEvent.change(screen.getByTestId("editor"), { target: { value: "preview text" } });

    const file = new File(["x"], "slow.png", { type: "image/png" });
    dropHandlers.onDrop?.([file]);

    // While the upload is pending the SubmitButton must be disabled.
    // Bypassing this would send the message with the attachment id
    // missing from the body.
    await waitFor(() => {
      const buttons = screen.getAllByRole("button");
      const sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).toBeDisabled();
    });

    resolveUpload!({
      id: "att-slow",
      link: "https://cdn.example/att-slow.png",
      filename: "slow.png",
    });

    let sendButton: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole("button");
      sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).not.toBeDisabled();
    });
    fireEvent.click(sendButton!);
    expect(onSend).toHaveBeenCalledTimes(1);
    const [, ids] = onSend.mock.calls[0]!;
    expect(ids).toEqual(["att-slow"]);
  });

  it("does not render the file upload button when onUploadFile is omitted", () => {
    renderInput({ onUploadFile: undefined });
    // FileUploadButton renders an icon button labelled by its tooltip — when
    // upload wiring is absent the chat input falls back to "submit + extras"
    // only. Probe by counting buttons: with no upload, only the submit
    // button is in the action row.
    const buttons = screen.getAllByRole("button");
    // The agent picker / context anchor adornments may render zero buttons
    // in this test (no leftAdornment passed). So a single button = submit.
    expect(buttons.length).toBe(1);
  });
});
