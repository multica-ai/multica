import { afterEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { EditorContent, ReactNodeViewRenderer, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { createLowlight } from "lowlight";
import { CodeBlockExtension } from "./code-block";
import { CodeBlockView } from "./code-block-view";

const clipboardDescriptor = Object.getOwnPropertyDescriptor(navigator, "clipboard");

vi.mock("../mermaid-diagram", () => ({ MermaidDiagram: () => null }));
vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (select: (value: unknown) => string) =>
      select({ code_block: { copy_code: "Copy code" } }),
  }),
}));

function CopyableEditor() {
  const editor = useEditor({
    extensions: [
      StarterKit.configure({ codeBlock: false }),
      CodeBlockExtension.extend({
        addNodeView: () => ReactNodeViewRenderer(CodeBlockView),
      }).configure({ lowlight: createLowlight() }),
    ],
    content: {
      type: "doc",
      content: [{
        type: "codeBlock",
        attrs: { language: "text" },
        content: [{ type: "text", text: "synthetic editable text" }],
      }],
    },
  });
  return <EditorContent editor={editor} />;
}

afterEach(() => {
  cleanup();
  document.getSelection()?.removeAllRanges();
  vi.restoreAllMocks();
  if (clipboardDescriptor) {
    Object.defineProperty(navigator, "clipboard", clipboardDescriptor);
  } else {
    Reflect.deleteProperty(navigator, "clipboard");
  }
});

describe("CodeBlockView clipboard wiring with a real Tiptap editor", () => {
  it.each([true, false])("keeps the editing selection and gates the copied indicator on %s", async (copied) => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: undefined,
    });
    const focus = HTMLTextAreaElement.prototype.focus;
    // jsdom does not replace DOM Selection when a textarea takes focus.
    vi.spyOn(HTMLTextAreaElement.prototype, "focus").mockImplementation(
      function (this: HTMLTextAreaElement) {
        focus.call(this);
        document.getSelection()?.removeAllRanges();
      },
    );
    const payloads: string[] = [];
    Object.defineProperty(document, "execCommand", {
      configurable: true,
      value: vi.fn(() => {
        payloads.push((document.activeElement as HTMLTextAreaElement).value);
        return copied;
      }),
    });
    const { container } = render(<CopyableEditor />);
    const button = await screen.findByRole("button", { name: "Copy code" });
    const editor = container.querySelector<HTMLElement>(".tiptap")!;
    const text = document.createTreeWalker(
      container.querySelector("code")!,
      NodeFilter.SHOW_TEXT,
    ).nextNode()!;
    act(() => {
      editor.focus();
      button.focus();
      // jsdom moves Selection into focused buttons; real browsers retain the
      // editor selection on a copy click (also covered by the browser fixture).
      document.getSelection()!.setBaseAndExtent(text, 12, text, 2);
    });
    fireEvent.click(button);
    await waitFor(() => expect(payloads).toEqual(["synthetic editable text"]));
    await waitFor(() =>
      expect(container.querySelector(".icon-check") !== null).toBe(copied),
    );
    const selection = document.getSelection()!;
    expect(selection.anchorNode).toBe(text);
    expect(selection.anchorOffset).toBe(12);
    expect(selection.focusOffset).toBe(2);
    expect(document.activeElement).toBe(button);
    expect(document.querySelector("textarea")).toBeNull();
  });
});
