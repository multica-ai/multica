import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { forwardRef, useImperativeHandle, type Ref } from "react";

// ---------------------------------------------------------------------------
// Hoisted mocks. ContentEditor is replaced by a stub that exposes a fixed
// markdown payload and a click target ("trigger-submit") so tests can fire
// the editor's onSubmit at will. The store mocks default to "no active
// session" / "no draft", which matches the brand-new-chat path.
// ---------------------------------------------------------------------------

const editorMarkdown = vi.hoisted(() => ({ value: "hello agent" }));

vi.mock("@multica/core/chat", () => ({
  DRAFT_NEW_SESSION: "draft:new-session",
  useChatStore: Object.assign(
    (selector?: (s: any) => unknown) => {
      const state = {
        activeSessionId: null,
        selectedAgentId: null,
        inputDrafts: {} as Record<string, string>,
        setInputDraft: vi.fn(),
        clearInputDraft: vi.fn(),
      };
      return selector ? selector(state) : state;
    },
    { getState: () => ({}) },
  ),
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn() }),
}));

vi.mock("@multica/core/api", () => ({ api: {} }));

vi.mock("@multica/core/logger", () => ({
  createLogger: () => ({ debug: vi.fn(), info: vi.fn(), warn: vi.fn(), error: vi.fn() }),
}));

vi.mock("../../preferences/use-submit-on-enter", () => ({
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
      },
      ref: Ref<{ getMarkdown: () => string; clearContent: () => void; uploadFile: () => void }>,
    ) => {
      useImperativeHandle(ref, () => ({
        getMarkdown: () => editorMarkdown.value,
        clearContent: () => {
          editorMarkdown.value = "";
        },
        uploadFile: () => {},
      }));
      return (
        <div>
          <div data-testid="placeholder">{props.placeholder}</div>
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
