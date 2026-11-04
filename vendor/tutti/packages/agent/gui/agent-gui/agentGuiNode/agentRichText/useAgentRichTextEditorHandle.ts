import { useImperativeHandle, type ForwardedRef, type RefObject } from "react";
import type { Editor } from "@tiptap/core";
import type { AgentGUIProviderSkillOption } from "../model/agentGuiNodeTypes";
import type { AgentCapabilityTokenOption } from "./agentCapabilityTokenExtension";
import type { AgentRichTextEditorHandle } from "./AgentRichTextEditor.types";
import {
  editorToPromptText,
  plainTextToAgentRichTextInlineContent
} from "./agentRichTextDocument";
import {
  createAgentFileMentionContent,
  createAgentMentionContent
} from "./agentWorkspaceFileReferences";
import {
  isMentionTriggerBoundaryBeforeSelection,
  isPromptVisualLineStart
} from "./agentRichTextEditorSupport";
import { AGENT_RICH_TEXT_SKIP_USER_CONTENT_EVENT_META } from "./agentRichTextEngagement";

export function useAgentRichTextEditorHandle(input: {
  availableCapabilitiesRef: RefObject<readonly AgentCapabilityTokenOption[]>;
  availableSkillsRef: RefObject<readonly AgentGUIProviderSkillOption[]>;
  editorRef: RefObject<Editor | null>;
  onBeforeProgrammaticFocus?: () => void;
  onBeforeOpenMentionPalette?: () => void;
  ref: ForwardedRef<AgentRichTextEditorHandle>;
}): void {
  useImperativeHandle(
    input.ref,
    () => ({
      focusAtStart() {
        const currentEditor = input.editorRef.current;
        if (!currentEditor || currentEditor.isDestroyed) {
          return;
        }
        if (!currentEditor.isFocused) input.onBeforeProgrammaticFocus?.();
        currentEditor
          .chain()
          .focus()
          .setTextSelection(1)
          .scrollIntoView()
          .run();
      },
      focusAtEnd() {
        const currentEditor = input.editorRef.current;
        if (!currentEditor || currentEditor.isDestroyed) {
          return;
        }
        const end = currentEditor.state.doc.content.size;
        if (!currentEditor.isFocused) input.onBeforeProgrammaticFocus?.();
        currentEditor.chain().focus().setTextSelection(end).run();
      },
      getPromptTextBeforeSelection() {
        const currentEditor = input.editorRef.current;
        if (!currentEditor || currentEditor.isDestroyed) {
          return "";
        }
        return currentEditor.state.doc.textBetween(
          0,
          currentEditor.state.selection.from,
          "\n",
          "\n"
        );
      },
      insertPlainTextAtSelection(text) {
        const currentEditor = input.editorRef.current;
        if (
          !currentEditor ||
          currentEditor.isDestroyed ||
          !currentEditor.isEditable ||
          text.length === 0
        ) {
          return null;
        }
        const { from, to } = currentEditor.state.selection;
        if (!currentEditor.isFocused) input.onBeforeProgrammaticFocus?.();
        currentEditor
          .chain()
          .focus()
          .setMeta(AGENT_RICH_TEXT_SKIP_USER_CONTENT_EVENT_META, true)
          .insertContentAt(
            { from, to },
            plainTextToAgentRichTextInlineContent(text, {
              capabilities: input.availableCapabilitiesRef.current,
              skills: input.availableSkillsRef.current
            })
          )
          .scrollIntoView()
          .run();
        return editorToPromptText(currentEditor);
      },
      openMentionPalette() {
        const currentEditor = input.editorRef.current;
        if (
          !currentEditor ||
          currentEditor.isDestroyed ||
          !currentEditor.isEditable
        ) {
          return;
        }
        input.onBeforeOpenMentionPalette?.();
        const triggerText = isMentionTriggerBoundaryBeforeSelection(
          currentEditor
        )
          ? "@"
          : " @";
        if (!currentEditor.isFocused) input.onBeforeProgrammaticFocus?.();
        currentEditor
          .chain()
          .focus()
          .setMeta(AGENT_RICH_TEXT_SKIP_USER_CONTENT_EVENT_META, true)
          .insertContent(triggerText)
          .run();
      },
      insertWorkspaceReferences(items) {
        const currentEditor = input.editorRef.current;
        if (!currentEditor || currentEditor.isDestroyed || items.length === 0) {
          return;
        }
        if (!currentEditor.isFocused) input.onBeforeProgrammaticFocus?.();
        currentEditor
          .chain()
          .focus()
          .insertContent(
            createAgentFileMentionContent(items, {
              prefixCaretAnchor: isPromptVisualLineStart(
                currentEditor,
                currentEditor.state.selection.from
              )
            })
          )
          .run();
      },
      insertMentionItems(items) {
        const currentEditor = input.editorRef.current;
        if (!currentEditor || currentEditor.isDestroyed || items.length === 0) {
          return;
        }
        if (!currentEditor.isFocused) input.onBeforeProgrammaticFocus?.();
        currentEditor
          .chain()
          .focus()
          .insertContent(
            createAgentMentionContent(items, {
              prefixCaretAnchor: isPromptVisualLineStart(
                currentEditor,
                currentEditor.state.selection.from
              )
            })
          )
          .run();
      },
      insertComposerFiles(items) {
        const currentEditor = input.editorRef.current;
        if (!currentEditor || currentEditor.isDestroyed || items.length === 0) {
          return;
        }
        if (!currentEditor.isFocused) input.onBeforeProgrammaticFocus?.();
        currentEditor
          .chain()
          .focus()
          .insertContent(
            createAgentMentionContent(
              items.map((item) => ({
                kind: "file" as const,
                href: "",
                path: "",
                name: item.name,
                entryKind: "file" as const,
                directoryPath: "",
                attachmentId: item.id,
                attachmentStatus: item.status,
                attachmentErrorCode: item.errorCode
              })),
              {
                prefixCaretAnchor: isPromptVisualLineStart(
                  currentEditor,
                  currentEditor.state.selection.from
                )
              }
            )
          )
          .run();
      },
      updateComposerFiles(items) {
        const currentEditor = input.editorRef.current;
        if (!currentEditor || currentEditor.isDestroyed || items.length === 0) {
          return false;
        }
        const updateById = new Map(items.map((item) => [item.id, item]));
        let transaction = currentEditor.state.tr;
        let changed = false;
        currentEditor.state.doc.descendants((node, position) => {
          if (node.type.name !== "agentFileMention") return true;
          const attachmentId =
            typeof node.attrs.attachmentId === "string"
              ? node.attrs.attachmentId
              : "";
          const update = updateById.get(attachmentId);
          if (!update) return false;
          transaction = transaction.setNodeMarkup(position, undefined, {
            ...node.attrs,
            name: update.name,
            attachmentStatus: update.status,
            attachmentErrorCode: update.errorCode ?? ""
          });
          changed = true;
          return false;
        });
        if (changed) currentEditor.view.dispatch(transaction);
        return changed;
      },
      replaceTextBeforeSelection(length, text) {
        const currentEditor = input.editorRef.current;
        if (!currentEditor || currentEditor.isDestroyed || length <= 0) {
          return null;
        }
        const to = currentEditor.state.selection.from;
        const from = Math.max(1, to - length);
        if (!currentEditor.isFocused) input.onBeforeProgrammaticFocus?.();
        currentEditor
          .chain()
          .focus()
          .insertContentAt(
            { from, to },
            plainTextToAgentRichTextInlineContent(text, {
              capabilities: input.availableCapabilitiesRef.current,
              skills: input.availableSkillsRef.current
            })
          )
          .run();
        return editorToPromptText(currentEditor);
      }
    }),
    []
  );
}
