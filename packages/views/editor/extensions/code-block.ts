import CodeBlockLowlight from "@tiptap/extension-code-block-lowlight";
import type { Node as PMNode } from "@tiptap/pm/model";
import type { Editor } from "@tiptap/core";
import { NodeSelection, Plugin, PluginKey, Selection, TextSelection } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import type { createLowlight } from "lowlight";

type Lowlight = ReturnType<typeof createLowlight>;
interface HighlightSpan {
  from: number;
  to: number;
  className: string;
}
interface HastNode {
  value?: string;
  children?: HastNode[];
  properties?: { className?: string[] };
}

function highlightSpans(node: PMNode, lowlight: Lowlight, defaultLanguage: string | null): HighlightSpan[] {
  const language = node.attrs.language || defaultLanguage;
  const text = node.textContent;
  const tree = language && lowlight.registered(language) ? lowlight.highlight(language, text) : lowlight.highlightAuto(text);
  const spans: HighlightSpan[] = [];
  let at = 0;
  const walk = (nodes: HastNode[], classes: string[]) => {
    for (const child of nodes) {
      const className = [...classes, ...(child.properties?.className ?? [])];
      if (child.children) {
        walk(child.children, className);
        continue;
      }
      const length = child.value?.length ?? 0;
      if (className.length && length) spans.push({ from: at, to: at + length, className: className.join(" ") });
      at += length;
    }
  };
  walk(tree.children as HastNode[], []);
  return spans;
}

/**
 * Syntax highlighting that stays cheap on a long, live doc. The stock lowlight
 * plugin walks every node of the doc twice on each transaction, and
 * re-highlights every code block whenever a step spans one, which is every
 * change a collaborator makes (they arrive as a whole-document replace).
 * Here a code block is highlighted once per version of its node, and the walk
 * skips inline content.
 */
function codeHighlightPlugin(name: string, lowlight: Lowlight, defaultLanguage: string | null) {
  const highlighted = new WeakMap<PMNode, HighlightSpan[]>();
  const decorate = (doc: PMNode) => {
    const decorations: Decoration[] = [];
    doc.descendants((node, pos) => {
      if (node.type.name !== name) return !node.isTextblock;
      let spans = highlighted.get(node);
      if (!spans) {
        spans = highlightSpans(node, lowlight, defaultLanguage);
        highlighted.set(node, spans);
      }
      for (const span of spans) {
        decorations.push(Decoration.inline(pos + 1 + span.from, pos + 1 + span.to, { class: span.className }));
      }
      return false;
    });
    return DecorationSet.create(doc, decorations);
  };
  const plugin: Plugin<DecorationSet> = new Plugin<DecorationSet>({
    key: new PluginKey("codeHighlight"),
    state: {
      init: (_, { doc }) => decorate(doc),
      apply: (tr, set) => (tr.docChanged ? decorate(tr.doc) : set),
    },
    props: {
      decorations: (state) => plugin.getState(state),
    },
  });
  return plugin;
}

/**
 * Splits a fence info string into its language (the first word) and the rest,
 * e.g. ```mermaid title="Request flow" → `mermaid` + `title="Request flow"`.
 * Everything that dispatches on the language (preview, highlighting, the
 * label) reads `language`; `meta` is only carried so a save writes the info
 * string back unchanged (#10255).
 */
export function splitFenceInfo(info: string | null | undefined): {
  language: string | null;
  meta: string | null;
} {
  const trimmed = (info ?? "").trim();
  if (!trimmed) return { language: null, meta: null };
  const space = trimmed.search(/\s/);
  if (space === -1) return { language: trimmed, meta: null };
  return {
    language: trimmed.slice(0, space),
    meta: trimmed.slice(space).trim() || null,
  };
}

/**
 * Languages whose block renders in place — a preview in the shared dynamic
 * block frame — instead of as highlighted code (MUL-7649, MUL-9760).
 */
export function isDynamicBlockLanguage(language: unknown): language is "html" | "mermaid" {
  return language === "html" || language === "mermaid";
}

/**
 * Transaction meta: the position of the dynamic block whose source should
 * open. The NodeView at that position switches to its source view.
 */
export const OPEN_BLOCK_SOURCE_META = "multica:openBlockSource";

/**
 * Up or Down from the edge of a textblock onto a dynamic block that shows its
 * preview selects that block whole. Left to the browser, the caret would skip
 * the block going up and land in its hidden source going down. Mirrors
 * ProseMirror's own vertical block motion (moveSelectionBlock).
 */
function selectPreviewedBlock(editor: Editor, typeName: string, dir: 1 | -1): boolean {
  const { state, view } = editor;
  const { selection } = state;
  if (!(selection instanceof TextSelection) || !selection.empty) return false;
  const $side = selection.$head;
  if (!$side.depth || !view.endOfTextblock(dir < 0 ? "up" : "down")) return false;
  const next = Selection.findFrom(state.doc.resolve(dir > 0 ? $side.after() : $side.before()), dir);
  if (!(next instanceof TextSelection)) return false;
  const block = next.$head.parent;
  if (block.type.name !== typeName || !isDynamicBlockLanguage(block.attrs.language)) return false;
  const pos = next.$head.before();
  const dom = view.nodeDOM(pos);
  if (!(dom instanceof HTMLElement) || !dom.querySelector('[data-dynamic-block-view="preview"]')) return false;
  view.dispatch(state.tr.setSelection(NodeSelection.create(state.doc, pos)).scrollIntoView());
  return true;
}

export const CodeBlockExtension = CodeBlockLowlight.extend({
  addKeyboardShortcuts() {
    const inherited = this.parent?.() ?? {};
    return {
      ...inherited,
      ArrowUp: (props) =>
        selectPreviewedBlock(props.editor, this.name, -1) || (inherited.ArrowUp?.(props) ?? false),
      ArrowDown: (props) =>
        selectPreviewedBlock(props.editor, this.name, 1) || (inherited.ArrowDown?.(props) ?? false),
      // A previewed dynamic block is selected as a whole (its NodeView keeps
      // the caret out of the hidden source); Enter opens the source with the
      // caret at its start.
      Enter: (props) => {
        const { editor } = props;
        const { selection } = editor.state;
        if (
          editor.isEditable &&
          selection instanceof NodeSelection &&
          selection.node.type.name === this.name &&
          isDynamicBlockLanguage(selection.node.attrs.language)
        ) {
          const pos = selection.from;
          return editor.commands.command(({ tr }) => {
            tr.setSelection(TextSelection.create(tr.doc, pos + 1))
              .setMeta(OPEN_BLOCK_SOURCE_META, pos)
              .scrollIntoView();
            return true;
          });
        }
        return inherited.Enter?.(props) ?? false;
      },
    };
  },
  addProseMirrorPlugins() {
    // Drops the stock plugin, and ours when an extension of this one calls
    // back into it. Plugin keys are runtime strings ProseMirror leaves out of
    // its types.
    const inherited = (this.parent?.() ?? []).filter(
      (plugin) => !/^(lowlight|codeHighlight)\$/.test((plugin as unknown as { key: string }).key),
    );
    return [
      ...inherited,
      codeHighlightPlugin(this.name, this.options.lowlight as Lowlight, this.options.defaultLanguage ?? null),
    ];
  },
  addAttributes() {
    return {
      ...this.parent?.(),
      meta: {
        default: null,
        parseHTML: (element) => element.getAttribute("data-meta"),
        renderHTML: (attributes) =>
          attributes.meta ? { "data-meta": attributes.meta } : {},
      },
    };
  },
  parseMarkdown(token, helpers) {
    // Same acceptance rule as @tiptap/extension-code-block: fenced or indented.
    const raw = token.raw ?? "";
    if (!raw.startsWith("```") && !raw.startsWith("~~~") && token.codeBlockStyle !== "indented") {
      return [];
    }
    return helpers.createNode(
      "codeBlock",
      splitFenceInfo(token.lang),
      token.text ? [helpers.createTextNode(token.text)] : [],
    );
  },
  renderMarkdown(node, helpers) {
    const content = helpers.renderChildren(node.content ?? []);
    // A literal fence inside the code must not close the serialized block.
    let fenceLength = 3;
    for (const [run] of content.matchAll(/`+/g)) {
      fenceLength = Math.max(fenceLength, run.length + 1);
    }
    const fence = "`".repeat(fenceLength);
    const language = node.attrs?.language ?? "";
    const meta = node.attrs?.meta;
    const info = meta ? `${language} ${meta}` : language;
    return `${fence}${info}\n${content}\n${fence}`;
  },
});
