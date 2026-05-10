import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { forwardRef, useImperativeHandle, type Ref } from "react";

// ---------------------------------------------------------------------------
// Hoisted mocks. ContentEditor is replaced by a stub that exposes a fixed
// markdown payload and a click target ("trigger-submit") so tests can fire
// the editor's onSubmit at will. The store mocks default to "no active
// session" / "no draft", which matches the brand-new-chat path.
//
// JEH-756: focus is now Tiptap-native, configured via the `autoFocus` prop
// on ContentEditor (see content-editor.tsx). The chat-input layer only
// decides whether to set the prop; ContentEditor handles the timing. The
// mock therefore exposes the received `autoFocus` value so tests can
// assert the prop contract.
// ---------------------------------------------------------------------------

const editorMarkdown = vi.hoisted(() => ({ value: "hello agent" }));
const lastAutoFocus = vi.hoisted(() => ({ value: undefined as boolean | undefined }));
const chatStoreMock = vi.hoisted(() => {
  const setInputDraft = vi.fn();
  const clearInputDraft = vi.fn();
  return {
    setInputDraft,
    clearInputDraft,
    state: {
      activeSessionId: null as string | null,
      selectedAgentId: null as string | null,
      inputDrafts: {} as Record<string, string>,
      setInputDraft,
      clearInputDraft,
    },
  };
});

vi.mock("@multica/core/chat", () => ({
  DRAFT_NEW_SESSION: "draft:new-session",
  useChatStore: Object.assign(
    (selector?: (s: any) => unknown) => {
      return selector ? selector(chatStoreMock.state) : chatStoreMock.state;
    },
    { getState: () => chatStoreMock.state },
  ),
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn() }),
}));

vi.mock("@multica/core/api", () => ({ api: {} }));

vi.mock("@multica/core/logger", () => ({
  createLogger: () => ({ debug: vi.fn(), info: vi.fn(), warn: vi.fn(), error: vi.fn() }),
}));

vi.mock("@multica/cerebro-preferences/views", () => ({
  useSubmitOnEnter: () => false,
}));

vi.mock("../../editor", () => {
  const ContentEditor = forwardRef(
    (
      props: {
        onSubmit?: () => void;
        onUpdate?: (md: string) => void;
        defaultValue?: string;
        placeholder?: string;
        autoFocus?: boolean;
      },
      ref: Ref<{ getMarkdown: () => string; clearContent: () => void; uploadFile: () => void; blur: () => void; focus: () => void }>,
    ) => {
      lastAutoFocus.value = props.autoFocus;
      useImperativeHandle(ref, () => ({
        getMarkdown: () => editorMarkdown.value,
        clearContent: () => {
          editorMarkdown.value = "";
        },
        uploadFile: () => {},
        blur: () => {},
        focus: () => {},
      }));
      return (
        <div>
          <div data-testid="placeholder">{props.placeholder}</div>
          <div data-testid="default-value">{props.defaultValue}</div>
          <button type="button" data-testid="trigger-update" onClick={() => props.onUpdate?.("updated draft")}>
            update
          </button>
          <button type="button" data-testid="trigger-submit" onClick={() => props.onSubmit?.()}>
            submit
          </button>
        </div>
      );
    },
  );
  ContentEditor.displayName = "ContentEditor";
  return {
    ContentEditor,
    useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
    FileDropOverlay: () => null,
  };
});

vi.mock("@multica/ui/components/common/file-upload-button", () => ({
  FileUploadButton: () => <button type="button" aria-label="Upload file" />,
}));

import { ChatInput } from "./chat-input";

beforeEach(() => {
  editorMarkdown.value = "hello agent";
  lastAutoFocus.value = undefined;
  chatStoreMock.state.activeSessionId = null;
  chatStoreMock.state.selectedAgentId = null;
  chatStoreMock.state.inputDrafts = {};
  chatStoreMock.setInputDraft.mockClear();
  chatStoreMock.clearInputDraft.mockClear();
});

describe("ChatInput — JEH-330 lock removal", () => {
  it("calls onSend even while a previous turn is mid-stream (isRunning=true)", async () => {
    const user = userEvent.setup();
    const onSend = vi.fn();
    render(<ChatInput onSend={onSend} isRunning={true} onStop={vi.fn()} />);

    await user.click(screen.getByTestId("trigger-submit"));

    // The lock fix: handleSend must not abort on isRunning. If the regression
    // returns, onSend is silently dropped and this assertion fails.
    expect(onSend).toHaveBeenCalledTimes(1);
    expect(onSend).toHaveBeenCalledWith("hello agent");
  });

  it("still blocks send when the session is archived (disabled=true)", async () => {
    const user = userEvent.setup();
    const onSend = vi.fn();
    render(<ChatInput onSend={onSend} disabled={true} />);

    await user.click(screen.getByTestId("trigger-submit"));
    expect(onSend).not.toHaveBeenCalled();
  });

  it("still blocks send when the editor is empty", async () => {
    editorMarkdown.value = "   \n  ";
    const user = userEvent.setup();
    const onSend = vi.fn();
    render(<ChatInput onSend={onSend} isRunning={true} onStop={vi.fn()} />);

    await user.click(screen.getByTestId("trigger-submit"));
    expect(onSend).not.toHaveBeenCalled();
  });

  it("renders a Stop button while isRunning so the user can cancel mid-turn", () => {
    render(<ChatInput onSend={vi.fn()} isRunning={true} onStop={vi.fn()} />);
    expect(screen.getByRole("button", { name: /stop/i })).toBeInTheDocument();
  });

  it("hides the Stop button when no turn is in flight", () => {
    render(<ChatInput onSend={vi.fn()} isRunning={false} onStop={vi.fn()} />);
    expect(screen.queryByRole("button", { name: /stop/i })).not.toBeInTheDocument();
  });
});

describe("ChatInput — JEH-806 session-scoped drafts", () => {
  it("uses the explicit draft session instead of the global active session", async () => {
    const user = userEvent.setup();
    chatStoreMock.state.activeSessionId = "session-a";
    chatStoreMock.state.inputDrafts = {
      "session-a": "draft from A",
      "session-b": "draft from B",
    };

    render(<ChatInput onSend={vi.fn()} draftSessionId="session-b" />);

    expect(screen.getByTestId("default-value")).toHaveTextContent("draft from B");
    await user.click(screen.getByTestId("trigger-update"));
    expect(chatStoreMock.setInputDraft).toHaveBeenCalledWith("session-b", "updated draft");
  });
});

describe("ChatInput — JEH-756 autofocus prop wiring", () => {
  // The chat-input layer only decides whether to ask Tiptap for autofocus.
  // The actual focus call lives in ContentEditor (driven by Tiptap's
  // `autofocus` option). These tests pin the parent's gating contract.

  it("forwards autoFocus={true} to ContentEditor when the prop is set", () => {
    render(<ChatInput onSend={vi.fn()} autoFocus />);
    expect(lastAutoFocus.value).toBe(true);
  });

  it("forwards autoFocus={false} when the prop is omitted", () => {
    render(<ChatInput onSend={vi.fn()} />);
    expect(lastAutoFocus.value).toBe(false);
  });

  it("suppresses autoFocus when the session is archived (disabled)", () => {
    render(<ChatInput onSend={vi.fn()} autoFocus disabled />);
    expect(lastAutoFocus.value).toBe(false);
  });

  it("suppresses autoFocus when no agent is available", () => {
    render(<ChatInput onSend={vi.fn()} autoFocus noAgent />);
    expect(lastAutoFocus.value).toBe(false);
  });
});
