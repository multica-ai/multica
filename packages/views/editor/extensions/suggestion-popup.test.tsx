import { Extension, Editor } from "@tiptap/core";
import { EditorContent } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { Suggestion, type SuggestionProps } from "@tiptap/suggestion";
import { PluginKey } from "@tiptap/pm/state";
import { forwardRef, useImperativeHandle } from "react";
import { afterEach, beforeAll, describe, expect, it } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createSuggestionPopupRender } from "./suggestion-popup";

interface TestItem {
  id: string;
  label: string;
}

interface TestListRef {
  onKeyDown: (props: { event: KeyboardEvent }) => boolean;
}

interface TestListProps {
  items: TestItem[];
  command: (item: TestItem) => void;
}

const TestSuggestionList = forwardRef<TestListRef, TestListProps>(
  function TestSuggestionList({ items, command }, ref) {
    useImperativeHandle(ref, () => ({
      onKeyDown: () => false,
    }));

    return (
      <div data-testid="suggestion-popup">
        {items.map((item) => (
          <button key={item.id} type="button" onClick={() => command(item)}>
            {item.label}
          </button>
        ))}
      </div>
    );
  },
);

let editor: Editor | null = null;

beforeAll(() => {
  const rect = () => new DOMRect(0, 0, 0, 0);
  const rectList = () => ({ length: 0, item: () => null, [Symbol.iterator]: function* () {} }) as DOMRectList;
  Object.defineProperty(Range.prototype, "getBoundingClientRect", {
    configurable: true,
    value: rect,
  });
  Object.defineProperty(Range.prototype, "getClientRects", {
    configurable: true,
    value: rectList,
  });
  Object.defineProperty(HTMLElement.prototype, "getClientRects", {
    configurable: true,
    value: rectList,
  });
  Object.defineProperty(Text.prototype, "getClientRects", {
    configurable: true,
    value: rectList,
  });
});

afterEach(() => {
  act(() => {
    editor?.destroy();
  });
  editor = null;
  document.body.innerHTML = "";
});

function makeEditor(char: "@" | "/") {
  const pluginKey = new PluginKey(`test-${char}-suggestion`);
  const item = char === "@"
    ? { id: "u1", label: "Alice" }
    : { id: "s1", label: "ship" };

  const TestSuggestionExtension = Extension.create({
    name: `testSuggestion${char}`,
    addProseMirrorPlugins() {
      return [
        Suggestion<TestItem, TestItem>({
          editor: this.editor,
          char,
          pluginKey,
          items: () => [item],
          command: ({ editor: ed, range, props }) => {
            ed.commands.insertContentAt(range, `${char}${props.label}`);
          },
          render: createSuggestionPopupRender<TestItem, TestItem, TestListRef, TestListProps>({
            pluginKey,
            component: TestSuggestionList,
            getProps: (props: SuggestionProps<TestItem, TestItem>) => ({
              items: props.items,
              command: props.command,
            }),
            onKeyDown: (ref, props) => ref?.onKeyDown(props) ?? false,
          }),
        }),
      ];
    },
  });

  editor = new Editor({
    extensions: [StarterKit, TestSuggestionExtension],
    content: "",
  });
  render(<EditorContent editor={editor} />);
  return editor;
}

async function triggerSuggestion(ed: Editor, text: string) {
  await act(async () => {
    ed.commands.focus("end");
    ed.commands.insertContent(text);
  });
  await waitFor(() => {
    expect(screen.getByTestId("suggestion-popup")).toBeInTheDocument();
  });
}

async function expectPopupClosed() {
  await waitFor(() => {
    expect(screen.queryByTestId("suggestion-popup")).not.toBeInTheDocument();
  });
}

describe("createSuggestionPopupRender", () => {
  it.each(["@", "/"] as const)(
    "closes the %s popup through a real pluginKey on outside pointerdown",
    async (char) => {
      const ed = makeEditor(char);
      await triggerSuggestion(ed, `${char}a`);

      const outside = document.createElement("button");
      document.body.appendChild(outside);
      act(() => {
        fireEvent.pointerDown(outside);
      });

      await expectPopupClosed();
    },
  );

  it.each(["@", "/"] as const)(
    "closes the %s popup through a real pluginKey on outside focusin",
    async (char) => {
      const ed = makeEditor(char);
      await triggerSuggestion(ed, `${char}a`);

      const outside = document.createElement("input");
      document.body.appendChild(outside);
      act(() => {
        fireEvent.focusIn(outside);
      });

      await expectPopupClosed();
    },
  );

  it.each(["@", "/"] as const)(
    "closes the %s popup through a real pluginKey on window blur",
    async (char) => {
      const ed = makeEditor(char);
      await triggerSuggestion(ed, `${char}a`);

      act(() => {
        fireEvent.blur(window);
      });

      await expectPopupClosed();
    },
  );

  it.each(["@", "/"] as const)(
    "can reopen the %s popup after an explicit exit",
    async (char) => {
      const ed = makeEditor(char);
      await triggerSuggestion(ed, `${char}a`);

      const outside = document.createElement("button");
      document.body.appendChild(outside);
      fireEvent.pointerDown(outside);
      await expectPopupClosed();

      await act(async () => {
        ed.commands.insertContent(` ${char}b`);
      });

      await waitFor(() => {
        expect(screen.getByTestId("suggestion-popup")).toBeInTheDocument();
      });
    },
  );

  it.each(["@", "/"] as const)(
    "keeps the %s popup open long enough for candidate row clicks to insert",
    async (char) => {
      const ed = makeEditor(char);
      await triggerSuggestion(ed, `${char}a`);

      const label = char === "@" ? "Alice" : "ship";
      const row = screen.getByRole("button", { name: label });
      act(() => {
        fireEvent.pointerDown(row);
        fireEvent.click(row);
      });

      await waitFor(() => {
        expect(ed.getText()).toContain(`${char}${label}`);
      });
    },
  );
});
