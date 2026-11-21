import CodeBlockLowlight from "@tiptap/extension-code-block-lowlight";
import type { Node as PMNode } from "@tiptap/pm/model";
import { Plugin, PluginKey } from "@tiptap/pm/state";
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

export const CodeBlockExtension = CodeBlockLowlight.extend({
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
