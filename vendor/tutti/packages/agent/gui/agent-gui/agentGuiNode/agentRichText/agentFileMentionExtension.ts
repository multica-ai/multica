import { mergeAttributes, Node, type Editor, type Range } from "@tiptap/core";
import { PluginKey } from "@tiptap/pm/state";
import { ReactNodeViewRenderer } from "@tiptap/react";
import Suggestion, { exitSuggestion } from "@tiptap/suggestion";
import { isRichTextTriggerPrefixBoundary } from "@tutti-os/ui-rich-text/editor";
import {
  resolveAgentMentionFileThumbnailUrl,
  resolveAgentMentionFileVisualKind
} from "../../shared/mentionFilePresentation";
import { AgentMentionNodeView } from "./AgentMentionNodeView";
import { AGENT_RICH_TEXT_CARET_ANCHOR } from "./agentRichTextCaretAnchor";
import type {
  AgentContextMentionItem,
  AgentFileMentionExtensionOptions
} from "./agentFileMentionContracts";
import {
  attrsToMentionItem,
  mentionItemToAttrs,
  parseAgentMentionHTMLElementAttrs
} from "./agentMentionAttrs";
import { formatAgentMentionMarkdown } from "./agentMentionMarkdown";
import { mentionVisual } from "./agentMentionPresentation";

export type {
  AgentContextMentionItem,
  AgentComposerFileMentionStatus,
  AgentFileMentionExtensionOptions,
  AgentFileMentionItem,
  AgentFileMentionKind,
  AgentFileMentionSuggestionState,
  AgentMentionAgentTargetItem,
  AgentMentionCustomItem,
  AgentMentionFileItem,
  AgentMentionFileNavigationAction,
  AgentMentionKind,
  AgentMentionReferenceSource,
  AgentMentionScope,
  AgentMentionSessionItem,
  AgentMentionSuggestionState,
  AgentMentionWorkspaceAppFactoryItem,
  AgentMentionWorkspaceAppItem,
  AgentMentionWorkspaceIssueItem,
  AgentMentionWorkspaceReferenceItem,
  ParsedAgentMentionMarkdown
} from "./agentFileMentionContracts";
export { attrsToMentionItem, mentionItemToAttrs } from "./agentMentionAttrs";
export {
  createAgentSessionHandoffPrompt,
  createAgentSessionMarkdownLink,
  createAgentSessionMentionHref,
  formatAgentFileMentionMarkdown,
  formatAgentMentionMarkdown,
  normalizeAgentSessionMentionTitle,
  parseAgentMentionMarkdown,
  parseMentionItemFromHref
} from "./agentMentionMarkdown";
export type { CreateAgentSessionHandoffPromptInput } from "./agentMentionMarkdown";
export const agentFileMentionPluginKey = new PluginKey(
  "agentFileMentionSuggestion"
);

export function exitAgentFileMentionSuggestion(editor: Editor): void {
  exitSuggestion(editor.view, agentFileMentionPluginKey);
}

function mentionItemIconUrl(item: AgentContextMentionItem): string | undefined {
  if (item.kind === "session") {
    return item.agentIconUrl;
  }
  if (
    item.kind === "workspace-app" ||
    item.kind === "agent-target" ||
    item.kind === "workspace-reference" ||
    item.kind === "workspace-issue"
  ) {
    return item.iconUrl;
  }
  return undefined;
}

/** Non-text/leaf nodes surface as this sentinel in {@link expandRangeOverMentionPlaceholder}. */
const MENTION_PLACEHOLDER_LEAF_SENTINEL = "￼";
/** Upper bound (in chars) for a `{ … }` group we treat as a mention placeholder. */
const MENTION_PLACEHOLDER_MAX_LENGTH = 40;

/**
 * Grow a suggestion range so that inserting a mention replaces a surrounding
 * `{ … }` placeholder rather than leaving its braces behind. Starter prompts
 * prefill mention slots as literal placeholders (e.g. `{ @agent }`); when the
 * user opens the @ palette inside one and picks a target, we want the braces
 * gone. Returns the original range unchanged when the trigger is not inside a
 * short, single-line, brace-only group (so ordinary `@` mentions and legitimate
 * user braces such as inline JSON are never touched).
 */
export function expandRangeOverMentionPlaceholder(
  editor: Editor,
  range: Range
): Range {
  const { doc } = editor.state;
  const $from = doc.resolve(range.from);
  const blockStart = $from.start();
  const blockEnd = $from.end();
  const blockText = doc.textBetween(
    blockStart,
    blockEnd,
    "\n",
    MENTION_PLACEHOLDER_LEAF_SENTINEL
  );
  const fromOffset = range.from - blockStart;
  const toOffset = Math.min(range.to, blockEnd) - blockStart;

  // Nearest "{" to the left, staying within a single group (bail on "}").
  let open = -1;
  for (let index = fromOffset - 1; index >= 0; index -= 1) {
    const char = blockText[index];
    if (char === "}") {
      return range;
    }
    if (char === "{") {
      open = index;
      break;
    }
  }
  if (open < 0) {
    return range;
  }

  // Nearest "}" to the right, staying within a single group (bail on "{").
  let close = -1;
  for (let index = toOffset; index < blockText.length; index += 1) {
    const char = blockText[index];
    if (char === "{") {
      return range;
    }
    if (char === "}") {
      close = index;
      break;
    }
  }
  if (close < 0) {
    return range;
  }

  // Only swallow short, single-line groups without embedded chips — real mention
  // placeholders are tiny, and this keeps larger user-authored braces intact.
  const group = blockText.slice(open, close + 1);
  if (
    close - open > MENTION_PLACEHOLDER_MAX_LENGTH ||
    group.includes("\n") ||
    group.includes(MENTION_PLACEHOLDER_LEAF_SENTINEL)
  ) {
    return range;
  }

  // The inserted mention re-adds its own trailing space; consume one adjacent
  // whitespace so a mid-sentence placeholder does not leave a double space.
  let end = close + 1;
  if (blockText[end] === " " || blockText[end] === "\t") {
    end += 1;
  }
  return { from: blockStart + open, to: blockStart + end };
}

export function createAgentFileMentionExtension(
  options: AgentFileMentionExtensionOptions = {}
): Node {
  return Node.create({
    name: "agentFileMention",
    group: "inline",
    inline: true,
    atom: true,
    selectable: false,

    addOptions() {
      return options;
    },

    addAttributes() {
      return {
        name: { default: "" },
        kind: { default: "file" },
        href: { default: "" },
        path: { default: "" },
        entryKind: { default: "unknown" },
        directoryPath: { default: "" },
        workspaceId: { default: "" },
        targetId: { default: "" },
        agentTargetId: { default: "" },
        scope: { default: "" },
        title: { default: "" },
        initiatorName: { default: "" },
        agentName: { default: "" },
        status: { default: "" },
        inputPreview: { default: "" },
        summaryPreview: { default: "" },
        creatorName: { default: "" },
        topicId: { default: "" },
        appId: { default: "" },
        agentProviderId: { default: "" },
        jobId: { default: "" },
        action: { default: "" },
        contextPath: { default: "" },
        description: { default: "" },
        iconUrl: { default: "" },
        contentPreview: { default: "" },
        thumbnailUrl: { default: "" },
        source: { default: "" },
        groupId: { default: "" },
        fileCount: { default: "" },
        customKind: { default: "" },
        sourceLabel: { default: "" },
        preview: { default: "" },
        attachmentId: { default: "" },
        attachmentStatus: { default: "" },
        attachmentErrorCode: { default: "" }
      };
    },

    parseHTML() {
      return [
        {
          tag: "span[data-agent-file-mention]",
          getAttrs: parseAgentMentionHTMLElementAttrs
        },
        {
          tag: "a[data-agent-file-mention]",
          getAttrs: parseAgentMentionHTMLElementAttrs
        }
      ];
    },

    renderHTML({ HTMLAttributes }) {
      const item = attrsToMentionItem(HTMLAttributes);
      const fileThumbnailUrl =
        item.kind === "file"
          ? resolveAgentMentionFileThumbnailUrl(item)
          : undefined;
      const href = item.href;
      const iconUrl = mentionItemIconUrl(item);
      const tagName = options.renderAsLink ? "a" : "span";
      const visual = mentionVisual(item);
      const sharedAttributes = {
        "data-agent-file-mention": "true",
        "data-agent-mention-href": href,
        "data-agent-mention-kind": item.kind,
        "aria-label":
          item.kind === "file"
            ? visual.primary
            : `${visual.kindLabel} ${visual.primary}`.trim()
      };
      return [
        tagName,
        mergeAttributes(HTMLAttributes, {
          ...sharedAttributes,
          ...(item.kind === "file"
            ? {
                "data-agent-file-entry-kind": item.entryKind,
                "data-agent-file-directory-path": item.directoryPath,
                "data-agent-file-visual-kind":
                  resolveAgentMentionFileVisualKind(item)
              }
            : {}),
          ...(options.renderAsLink ? { href } : {}),
          ...(iconUrl ? { "data-agent-mention-icon-url": iconUrl } : {}),
          ...(item.kind === "workspace-reference"
            ? { "data-agent-mention-file-count": String(item.fileCount) }
            : {}),
          ...(fileThumbnailUrl
            ? { "data-agent-mention-thumbnail-url": fileThumbnailUrl }
            : {}),
          class:
            item.kind === "file"
              ? "tsh-agent-object-token tsh-agent-object-token--file"
              : "tsh-agent-object-token tsh-agent-object-token--entity"
        }),
        ...(item.kind === "file"
          ? [
              fileThumbnailUrl
                ? [
                    "span",
                    {
                      class: "agent-gui-node__mention-file-thumb",
                      "data-agent-mention-file-thumb": "true",
                      "aria-hidden": "true"
                    },
                    [
                      "img",
                      {
                        src: fileThumbnailUrl,
                        alt: "",
                        decoding: "async",
                        loading: "lazy",
                        draggable: "false"
                      }
                    ]
                  ]
                : [
                    "span",
                    {
                      class: "tsh-agent-object-token__icon",
                      "aria-hidden": "true"
                    },
                    ""
                  ],
              [
                "span",
                { class: "tsh-agent-object-token__main" },
                visual.primary
              ]
            ]
          : [
              [
                "span",
                {
                  class: "tsh-agent-object-token__kind",
                  "aria-hidden": "true"
                },
                [
                  "span",
                  {
                    class: "tsh-agent-object-token__kind-icon",
                    "aria-hidden": "true"
                  },
                  ""
                ]
              ],
              [
                "span",
                { class: "tsh-agent-object-token__main" },
                visual.primary
              ]
            ])
      ];
    },

    renderText({ node }) {
      return formatAgentMentionMarkdown(attrsToMentionItem(node.attrs ?? {}));
    },

    addNodeView() {
      return ReactNodeViewRenderer(AgentMentionNodeView);
    },

    addProseMirrorPlugins() {
      if (options.enableSuggestions === false) {
        return [];
      }
      return [
        Suggestion<AgentContextMentionItem, AgentContextMentionItem>({
          editor: this.editor,
          pluginKey: agentFileMentionPluginKey,
          char: "@",
          allowedPrefixes: [" ", "\n", "\t"],
          allowSpaces: true,
          items: () => [],
          allow: ({ state, range }) => {
            if (options.shouldSuppressSuggestion?.()) {
              return false;
            }
            if (range.from <= 1) {
              return true;
            }
            const previous = state.doc.textBetween(
              range.from - 1,
              range.from,
              "\n",
              "\n"
            );
            return isRichTextTriggerPrefixBoundary(previous, "whitespace");
          },
          command: ({ editor, range, props }) => {
            // Starter prompts seed mention slots as literal `{ … }` placeholders
            // (e.g. `{ @agent }`). When the palette is triggered inside one, grow
            // the replaced range over the whole placeholder so inserting a chip
            // cleanly removes its braces instead of leaving them behind.
            const insertRange = expandRangeOverMentionPlaceholder(
              editor,
              range
            );
            const prefixCaretAnchor =
              insertRange.from <= 1 ||
              editor.state.doc.textBetween(
                Math.max(1, insertRange.from - 1),
                insertRange.from,
                "\n",
                "\n"
              ) === "\n";
            editor
              .chain()
              .focus()
              .insertContentAt(insertRange, [
                ...(prefixCaretAnchor
                  ? ([
                      {
                        type: "text",
                        text: AGENT_RICH_TEXT_CARET_ANCHOR
                      }
                    ] as const)
                  : []),
                {
                  type: this.name,
                  attrs: mentionItemToAttrs(props)
                },
                { type: "text", text: " " }
              ])
              .run();
          },
          render: () => ({
            // Agent GUI owns candidate search outside Tiptap. Notify it before
            // Suggestion awaits its intentionally empty `items()` result so a
            // controlled editor remount cannot lose the activation callback.
            onBeforeStart: (props) => {
              options.onSuggestionChange?.({
                editor: props.editor,
                range: props.range,
                query: props.query,
                text: props.text,
                command: props.command,
                clientRect: props.clientRect
              });
            },
            onBeforeUpdate: (props) => {
              options.onSuggestionChange?.({
                editor: props.editor,
                range: props.range,
                query: props.query,
                text: props.text,
                command: props.command,
                clientRect: props.clientRect
              });
            },
            onExit: (props) => {
              if (!props.editor.isDestroyed && agentFileMentionPluginKey.getState(props.editor.state)?.active) return;
              options.onSuggestionChange?.(null);
            },
            onKeyDown: (props) =>
              options.onSuggestionKeyDown?.(props.event) ?? false
          })
        })
      ];
    }
  });
}
