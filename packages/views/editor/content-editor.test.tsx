import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";

const mockFocus = vi.hoisted(() => vi.fn());
const lastUseEditorOptions = vi.hoisted(
  () => ({ value: undefined as Record<string, unknown> | undefined }),
);

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

vi.mock("./extensions", () => ({
  createEditorExtensions: () => [],
}));

vi.mock("./extensions/file-upload", () => ({
  uploadAndInsertFile: vi.fn(),
}));

vi.mock("./utils/preprocess", () => ({
  preprocessMarkdown: (value: string) => value,
}));

vi.mock("./bubble-menu", () => ({
  EditorBubbleMenu: () => null,
}));

vi.mock("@tiptap/react", () => ({
  useEditor: (options: Record<string, unknown>) => {
    lastUseEditorOptions.value = options;
    return {
      commands: {
        focus: mockFocus,
        clearContent: vi.fn(),
      },
      getMarkdown: () => "",
      state: {
        doc: {
          content: {
            size: 0,
          },
        },
        selection: {
          empty: true,
        },
      },
    };
  },
  EditorContent: ({ className }: { className?: string }) => (
    <div className={className} data-testid="editor-content">
      <div className="ProseMirror rich-text-editor" data-testid="prosemirror" />
    </div>
  ),
}));

import { ContentEditor } from "./content-editor";

function createFocusedDialog() {
  const dialog = document.createElement("div");
  dialog.setAttribute("role", "dialog");
  const dialogInput = document.createElement("input");
  dialog.appendChild(dialogInput);
  document.body.appendChild(dialog);
  dialogInput.focus();
  return { dialog, dialogInput };
}

async function flushAutofocusFrames() {
  await act(async () => {
    vi.runOnlyPendingTimers();
  });
  await act(async () => {
    vi.runOnlyPendingTimers();
  });
}

describe("ContentEditor", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    lastUseEditorOptions.value = undefined;
  });

  it("focuses the editor when clicking the empty container area", () => {
    render(<ContentEditor placeholder="Add description..." />);

    const shell = screen.getByTestId("editor-content").parentElement;
    expect(shell).not.toBeNull();

    fireEvent.mouseDown(shell!);

    expect(mockFocus).toHaveBeenCalledWith("end");
  });

  it("does not hijack clicks that land inside the ProseMirror node", () => {
    render(<ContentEditor placeholder="Add description..." />);

    fireEvent.mouseDown(screen.getByTestId("prosemirror"));

    expect(mockFocus).not.toHaveBeenCalled();
  });
});

describe("ContentEditor — JEH-756 autofocus", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) =>
      window.setTimeout(() => callback(performance.now()), 0),
    );
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation((id) =>
      window.clearTimeout(id),
    );
    vi.clearAllMocks();
    lastUseEditorOptions.value = undefined;
  });

  afterEach(() => {
    document.querySelectorAll('[role="dialog"], [role="alertdialog"]').forEach((node) => {
      node.remove();
    });
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("focuses after the editor instance exists when autoFocus is true", async () => {
    render(<ContentEditor autoFocus placeholder="…" />);
    expect(lastUseEditorOptions.value?.autofocus).toBe(false);

    await flushAutofocusFrames();

    expect(mockFocus).toHaveBeenCalledWith("end");
  });

  it("does not focus when autoFocus is omitted", async () => {
    render(<ContentEditor placeholder="…" />);
    expect(lastUseEditorOptions.value?.autofocus).toBe(false);

    await flushAutofocusFrames();

    expect(mockFocus).not.toHaveBeenCalled();
  });

  it("yields focus while a focus-trapping dialog still owns focus", async () => {
    const { dialogInput } = createFocusedDialog();

    render(<ContentEditor autoFocus placeholder="…" />);

    await flushAutofocusFrames();

    expect(mockFocus).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(dialogInput);
  });

  it("focuses when a dismissing dialog has closed before deferred focus", async () => {
    const { dialog } = createFocusedDialog();

    render(<ContentEditor autoFocus placeholder="…" />);
    dialog.remove();

    await flushAutofocusFrames();

    expect(mockFocus).toHaveBeenCalledWith("end");
  });
});
