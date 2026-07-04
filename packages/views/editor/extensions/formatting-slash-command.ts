import { Extension } from "@tiptap/core";
import Suggestion, { type SuggestionOptions } from "@tiptap/suggestion";
import { PluginKey } from "@tiptap/pm/state";
import { createSuggestionPopupRender } from "./suggestion-popup";
import { FormattingSlashCommandList, type FormattingCommandItem, type FormattingSlashCommandListRef, type FormattingSlashCommandListProps } from "./formatting-slash-command-list";
import { Heading1, Heading2, Heading3, List, ListOrdered, ListTodo, Code, Quote, Type } from "lucide-react";

export const FormattingSlashCommandExtension = Extension.create({
  name: "formattingSlashCommand",

  addOptions() {
    return {
      suggestion: {
        char: "/",
        command: ({ editor, range, props }) => {
          props.action(editor, range);
        },
      } as Partial<SuggestionOptions<FormattingCommandItem>>,
    };
  },

  addProseMirrorPlugins() {
    return [
      Suggestion({
        editor: this.editor,
        ...this.options.suggestion,
      }),
    ];
  },
});

const getFormattingCommands = (): FormattingCommandItem[] => [
  {
    id: "text",
    title: "Text",
    description: "Just start typing with plain text.",
    icon: Type,
    action: (editor, range) => {
      editor.chain().focus().deleteRange(range).setNode("paragraph").run();
    },
  },
  {
    id: "h1",
    title: "Heading 1",
    description: "Big section heading.",
    icon: Heading1,
    action: (editor, range) => {
      editor.chain().focus().deleteRange(range).setNode("heading", { level: 1 }).run();
    },
  },
  {
    id: "h2",
    title: "Heading 2",
    description: "Medium section heading.",
    icon: Heading2,
    action: (editor, range) => {
      editor.chain().focus().deleteRange(range).setNode("heading", { level: 2 }).run();
    },
  },
  {
    id: "h3",
    title: "Heading 3",
    description: "Small section heading.",
    icon: Heading3,
    action: (editor, range) => {
      editor.chain().focus().deleteRange(range).setNode("heading", { level: 3 }).run();
    },
  },
  {
    id: "bulletList",
    title: "Bulleted List",
    description: "Create a simple bulleted list.",
    icon: List,
    action: (editor, range) => {
      editor.chain().focus().deleteRange(range).toggleBulletList().run();
    },
  },
  {
    id: "orderedList",
    title: "Numbered List",
    description: "Create a list with numbering.",
    icon: ListOrdered,
    action: (editor, range) => {
      editor.chain().focus().deleteRange(range).toggleOrderedList().run();
    },
  },
  {
    id: "taskList",
    title: "To-do List",
    description: "Track tasks with a to-do list.",
    icon: ListTodo,
    action: (editor, range) => {
      editor.chain().focus().deleteRange(range).toggleTaskList().run();
    },
  },
  {
    id: "blockquote",
    title: "Quote",
    description: "Capture a quote.",
    icon: Quote,
    action: (editor, range) => {
      editor.chain().focus().deleteRange(range).toggleBlockquote().run();
    },
  },
  {
    id: "codeBlock",
    title: "Code Block",
    description: "Capture a code snippet.",
    icon: Code,
    action: (editor, range) => {
      editor.chain().focus().deleteRange(range).toggleCodeBlock().run();
    },
  },
];

export function createFormattingSlashCommandSuggestion(): Omit<SuggestionOptions<FormattingCommandItem>, "editor"> {
  const pluginKey = new PluginKey("formattingSlashCommandSuggestion");

  return {
    char: "/",
    pluginKey,
    // Only trigger at the start of a line
    allow: ({ editor, range }) => {
      const $from = editor.state.doc.resolve(range.from);
      const isRootDepth = $from.depth === 1;
      const isParagraph = $from.parent.type.name === "paragraph";
      const isStartOfNode = $from.parent.textContent.charAt(0) === "/";
      return isRootDepth && isParagraph && isStartOfNode;
    },
    items: ({ query }) => {
      const q = query.toLowerCase();
      return getFormattingCommands().filter(
        (cmd) =>
          cmd.title.toLowerCase().includes(q) ||
          cmd.description.toLowerCase().includes(q)
      );
    },
    render: createSuggestionPopupRender<FormattingCommandItem, FormattingCommandItem, FormattingSlashCommandListRef, FormattingSlashCommandListProps>({
      pluginKey,
      component: FormattingSlashCommandList,
      getProps: (props) => ({
        items: props.items,
        command: props.command,
      }),
      onKeyDown: (ref, props) => ref?.onKeyDown(props) ?? false,
    }),
  };
}
