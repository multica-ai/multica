import { describe, it, expect, beforeAll, vi } from "vitest";
import { Editor, Extension } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { Suggestion } from "@tiptap/suggestion";
import { EditorView } from "@tiptap/pm/view";
import type { QueryClient } from "@tanstack/react-query";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { createMarkdownPasteExtension } from "./markdown-paste";
import { SuggestionTriggerArmingExtension } from "./suggestion-trigger-arming";

// The mention picker's boundary rule, end to end. It is driven through the real
// pipeline — the production `createMentionSuggestion()` config on a real editor,
// with typing routed through `handleTextInput` exactly the way
// prosemirror-view's readDOMChange does it — because the rule that decides
// whether the picker opens lives inside Tiptap's `findSuggestionMatch` and
// @tiptap/suggestion's `shouldShow` call, not in anything this package owns.

vi.mock("@multica/core/platform", () => ({
  getCurrentWsId: () => "ws-1",
}));

vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => ({ iconOf: () => null, colorOf: () => null }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    searchIssues: vi.fn().mockResolvedValue({ issues: [] }),
    searchProjects: vi.fn().mockResolvedValue({ projects: [] }),
  },
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: { getState: () => ({ user: { id: "u1" } }) },
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

import { createMentionSuggestion, type MentionItem } from "./mention-suggestion";

function fakeQc(): QueryClient {
  return {
    getQueryData: (key: unknown) => {
      if (JSON.stringify(key) === JSON.stringify(workspaceKeys.members("ws-1"))) {
        return [{ user_id: "u1", name: "Henry", role: "owner" }];
      }
      if (JSON.stringify(key) === JSON.stringify(workspaceKeys.agents("ws-1"))) {
        return [
          {
            id: "agent-1",
            name: "Mika",
            archived_at: null,
            runtime_id: "rt-1",
            runtime_bound: true,
            owner_id: null,
            permission_mode: "public_to",
            invocation_targets: [{ target_type: "workspace", target_id: null }],
          },
        ];
      }
      return undefined;
    },
    getQueriesData: () => [],
  } as unknown as QueryClient;
}

function makeEditor() {
  const element = document.createElement("div");
  document.body.appendChild(element);
  const config = createMentionSuggestion(fakeQc());
  const probe = Extension.create({
    name: "mentionBoundaryProbe",
    addProseMirrorPlugins() {
      return [Suggestion({ ...config, editor: this.editor, render: () => ({}) })];
    },
  });
  const editor = new Editor({
    element,
    extensions: [
      StarterKit,
      Markdown,
      createMarkdownPasteExtension(),
      SuggestionTriggerArmingExtension,
      probe,
    ],
    content: { type: "doc", content: [{ type: "paragraph" }] },
  });
  return { editor, config };
}

/** Types text the way ProseMirror does: through `handleTextInput`. */
function type(editor: Editor, text: string): void {
  for (const ch of text) {
    if (ch === "\n") {
      editor.view.dispatch(editor.state.tr.split(editor.state.selection.from));
      continue;
    }
    const { from, to } = editor.state.selection;
    const handled = editor.view.someProp("handleTextInput", (fn) =>
      fn(editor.view, from, to, ch, () => editor.state.tr.insertText(ch, from, to)),
    );
    if (!handled) editor.view.dispatch(editor.state.tr.insertText(ch, from, to));
  }
}

function paste(editor: Editor, text: string): void {
  const event = new Event("paste", { bubbles: false, cancelable: true });
  Object.defineProperty(event, "clipboardData", {
    value: {
      files: [],
      getData: (type: string) =>
        type === "text/plain" ? text : type === "text/html" ? "" : "",
    },
  });
  editor.view.dom.dispatchEvent(event);
}

function picker(editor: Editor, config: ReturnType<typeof createMentionSuggestion>) {
  const key = config.pluginKey;
  if (!key) throw new Error("mention suggestion config has no plugin key");
  const state = key.getState(editor.state) as
    | { active?: boolean; query?: string | null }
    | undefined;
  return { active: state?.active ?? false, query: state?.query ?? null };
}

beforeAll(() => {
  // jsdom has no layout, so ProseMirror's post-dispatch scroll walks
  // coordsAtPos into getClientRects() on nodes jsdom does not implement.
  (EditorView.prototype as unknown as { scrollToSelection: () => void }).scrollToSelection =
    () => {};
  Element.prototype.scrollIntoView = () => {};
});

describe("mention picker boundary", () => {
  describe("opens where an @ starts a token", () => {
    const cases: Array<[string, string]> = [
      ["empty comment box", "@Mi"],
      ["after a half-width space", "hello @Mi"],
      // A Chinese IME inserts U+3000 for the space key. Tiptap's default
      // allowedPrefixes ([" "]) rejected it, so the picker never opened.
      ["after a full-width space", "你好　@Mi"],
      ["after a tab", "hello\t@Mi"],
      // CJK text is written without a separator before @, so requiring a space
      // made the picker unreachable for the most common way to address someone.
      ["after CJK text with no separator", "你好@Mi"],
      ["after katakana", "テレビ@Mi"],
      // U+30FC carries Katakana only as a Script_Extensions value; the shared
      // rule lists it by hand so the word still ends here.
      ["after a prolonged sound mark", "コーヒー@Mi"],
      ["after hangul with no separator", "안녕하세요@Mi"],
      ["after thai with no separator", "สวัสดี@Mi"],
      ["after punctuation", "hello(@Mi"],
      ["on a new line", "hello\n@Mi"],
    ];

    it.each(cases)("%s", (_name, typed) => {
      const { editor, config } = makeEditor();
      editor.commands.focus("end");

      type(editor, typed);

      expect(picker(editor, config)).toEqual({ active: true, query: "Mi" });
    });

    it("lists the agent the query names", () => {
      const { editor, config } = makeEditor();
      editor.commands.focus("end");

      type(editor, "你好@Mi");

      const items = config.items!({ query: "Mi" } as never) as MentionItem[];
      expect(items.map((i) => i.label)).toContain("Mika");
      expect(picker(editor, config).active).toBe(true);
    });
  });

  describe("stays shut where an @ continues a token", () => {
    const cases: Array<[string, string]> = [
      ["after an ASCII word", "hello@Mi"],
      ["after a digit", "2024@Mi"],
      ["after an underscore", "snake_case@Mi"],
      ["inside an address", "user@example.com"],
      // Non-ASCII word characters count too. An ASCII-only class made every one
      // of these a boundary, re-opening the address case the rule exists to
      // keep shut.
      ["inside an accented address", "josé@example.com"],
      ["inside a cyrillic address", "почта@mail.ru"],
      ["inside a greek address", "αλφα@example.com"],
      ["after an accented word", "café@Mi"],
    ];

    it.each(cases)("%s", (_name, typed) => {
      const { editor, config } = makeEditor();
      editor.commands.focus("end");

      type(editor, typed);

      expect(picker(editor, config)).toEqual({ active: false, query: null });
    });

    it("over an @ the user did not type", () => {
      // MUL-5429: provenance still comes from the arming extension, so widening
      // the boundary rule does not re-open the picker over pasted text.
      const { editor, config } = makeEditor();
      editor.commands.focus("end");

      paste(editor, "npx @aiforui/install --token=abc");

      expect(picker(editor, config)).toEqual({ active: false, query: null });
    });
  });
});
