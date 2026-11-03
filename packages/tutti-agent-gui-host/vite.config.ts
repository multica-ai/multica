import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";
import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const hostPresentationAdaptations = [
  {
    path: "/view/useAgentGUIDetailScroll.ts",
    original: 'const AGENT_GUI_TOP_HISTORY_PREFETCH_THRESHOLD_PX = 240;',
    replacement: 'import { useTimelineEndPosition } from "@multica-timeline-end-position";\n\nconst AGENT_GUI_TOP_HISTORY_PREFETCH_THRESHOLD_PX = 240;',
  },
  {
    path: "/view/useAgentGUIDetailScroll.ts",
    original: '  return {\n    followEndMode,\n    isTimelineScrolledToBottom: followEndMode === "following",',
    replacement: '  const isTimelineScrolledToBottom = useTimelineEndPosition(input, virtualScrollControllerRevision);\n  return {\n    followEndMode,\n    isTimelineScrolledToBottom,',
  },
  {
    path: "/controller/useAgentGUISubmitInteractionActions.ts",
    original: 'import { translate } from "../../../i18n/index";',
    replacement: 'import { getActiveUiLanguage, translate } from "../../../i18n/index";\nimport { promptHasAgentMention, promptSubmissionError } from "@multica-prompt-submission-policy";',
  },
  {
    path: "/controller/useAgentGUISubmitInteractionActions.ts",
    original: '      const targetIsActiveConversation =\n        activeConversationIdRef.current === agentSessionId;',
    replacement: '      const submissionError = promptSubmissionError(options ?? {}, getActiveUiLanguage());\n      if (submissionError) {\n        setDetailError(submissionError);\n        return;\n      }\n      const targetIsActiveConversation =\n        activeConversationIdRef.current === agentSessionId;',
  },
  {
    path: "/controller/useAgentGUISubmitInteractionActions.ts",
    original: '      const pendingQuestionResponse = resolvePendingQuestionComposerResponse({',
    // Explicit agent invocations must reach the queue, not answer a local question.
    replacement: '      const pendingQuestionResponse = promptHasAgentMention(normalizedContent) ? null : resolvePendingQuestionComposerResponse({',
  },
  {
    path: "/agentConversation/components/AgentMessageBlock.tsx",
    original: 'import { AgentSystemNoticeMessage } from "./AgentSystemNoticeMessage";',
    replacement: 'import { AgentSystemNoticeMessage } from "./AgentSystemNoticeMessage";\nimport { InvokedAgentReply } from "@multica-invoked-agent-reply";',
  },
  {
    path: "/agentConversation/components/AgentMessageBlock.tsx",
    original: '        renderedContent\n      );',
    replacement: '        isUser ? renderedContent : <InvokedAgentReply message={message}>{renderedContent}</InvokedAgentReply>\n      );',
  },
  {
    path: "/controller/useAgentGUILocalState.ts",
    original: 'import {\n  useCallback,',
    replacement: 'import { useComposerDrafts } from "@multica-composer-drafts";\nimport {\n  useCallback,',
  },
  {
    path: "/controller/useAgentGUILocalState.ts",
    original: 'useState<\n    Record<string, AgentComposerDraft>\n  >({})',
    replacement: 'useComposerDrafts()',
  },
  {
    path: "/composer/useComposerDraftAttachments.ts",
    original: "            const message =\n              error instanceof Error ? error.message : String(error);\n            reportAgentComposerDiagnostic(agentActivityRuntime, {\n              details: {\n                error: message.slice(0, 500),\n                imageId: draftImage.id",
    replacement: "            const message =\n              error instanceof Error ? error.message : String(error);\n            showErrorToast(translate(\"agentHost.agentGui.imageLoadFailed\"));\n            reportAgentComposerDiagnostic(agentActivityRuntime, {\n              details: {\n                error: message.slice(0, 500),\n                imageId: draftImage.id",
  },
  {
    path: "/composer/useComposerDraftAttachments.ts",
    original: "      for (const draftImage of nextImages) {",
    replacement: "      for (const { image: draftImage, reference } of imagePreparations) {",
  },
  {
    path: "/projection/agentMessageFinalTextProjection.ts",
    original: "import type { WorkspaceAgentSessionDetailViewModel }",
    replacement: "import { projectMessageImageCopies } from \"@multica-message-image-clipboard\";\nimport type { WorkspaceAgentSessionDetailViewModel }",
  },
  {
    path: "/projection/agentMessageFinalTextProjection.ts",
    original: "    return changed ? { ...row, messages } : row;",
    replacement: "    return projectMessageImageCopies(changed ? { ...row, messages } : row);",
  },
  {
    path: "/agentRichText/AgentRichTextEditor.tsx",
    original: "import { routeAgentRichTextTextPaste }",
    replacement: "import { pasteMessageImageReferences } from \"@multica-message-image-clipboard\";\nimport { routeAgentRichTextTextPaste }",
  },
  {
    path: "/agentRichText/AgentRichTextEditor.tsx",
    original: "        paste: (_view, event) => {",
    replacement: "        paste: (_view, event) => {\n          if (disabled) return false;\n          if (pasteMessageImageReferences({\n            text: event.clipboardData?.getData(\"text/plain\") ?? \"\",\n            supported: promptImagesSupportedRef.current,\n            onImages: onPasteImagesRef.current,\n            onUnsupported: onPromptImagesUnsupportedRef.current,\n            insertText: (text) => routeAgentRichTextTextPaste({\n              availableCapabilities: availableCapabilitiesRef.current,\n              availableSkills: availableSkillsRef.current,\n              editorRef, html: \"\", mentionSuggestionSuppression, text,\n            }),\n          })) {\n            event.preventDefault();\n            return true;\n          }",
  },
  {
    path: "/composer/useComposerDraftAttachments.ts",
    original: "import { flushSync } from \"react-dom\";",
    replacement: "import { flushSync } from \"react-dom\";\nimport { createMessageDraftImage, prepareMessageDraftImage } from \"@multica-message-image-clipboard\";",
  },
  {
    path: "/composer/useComposerDraftAttachments.ts",
    original: "      const nextImages = images.slice(0, remainingSlots).map((image) => ({\n        id: `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`,\n        name: image.name,\n        mimeType: image.mimeType,\n        data: image.data,\n        previewUrl: `data:${image.mimeType};base64,${image.data}`,\n        uploading: Boolean(uploadPromptContent)\n      }));",
    replacement: "      const imagePreparations = images.slice(0, remainingSlots).map((image) =>\n        createMessageDraftImage(image, Boolean(uploadPromptContent))\n      );\n      const nextImages = imagePreparations.map(({ image }) => image);",
  },
  {
    path: "/composer/useComposerDraftAttachments.ts",
    original: "        void uploadPromptContent({\n          workspaceId,\n          content: [\n            {\n              type: \"image\",\n              mimeType: draftImage.mimeType,\n              data: draftImage.data,\n              name: draftImage.name\n            }\n          ]\n        })",
    replacement: "        void prepareMessageDraftImage({\n          workspaceId, image: draftImage, reference, runtime: agentActivityRuntime, uploadPromptContent\n        })",
  },
  {
    path: "/composer/useComposerDraftAttachments.ts",
    original: "                        previewUrl: image.previewUrl,",
    replacement: "                        previewUrl: uploadedImage.previewUrl ?? image.previewUrl,",
  },
  {
    path: "/agentConversation/components/AgentSystemNoticeMessage.tsx",
    original: '        <div className="font-medium text-[var(--text-secondary)]">{title}</div>',
    replacement: '        <div className="font-medium text-[var(--text-secondary)]">{title}</div>\n        {notice?.noticeKind === "background_task" && message.body.trim() !== title ? <div role="status" className="mt-1 whitespace-pre-line break-words text-[12px] leading-5">{message.body}</div> : null}',
  },
  {
    path: "/agentGuiNode/agentGuiHeroCarouselImageLoad.ts",
    original: 'import type { AgentGUIAgentAvatarPresentation } from "./model/agentGuiAgentAvatarPresentation";',
    replacement: 'import type { AgentGUIAgentAvatarPresentation } from "./model/agentGuiAgentAvatarPresentation";\nimport { normalizeManagedAgentProvider } from "../../shared/managedAgentProviders";',
  },
  {
    path: "/agentGuiNode/agentGuiHeroCarouselImageLoad.ts",
    original: 'AGENT_VINYL_COVER_BY_PROVIDER[item.provider]',
    replacement: 'AGENT_VINYL_COVER_BY_PROVIDER[normalizeManagedAgentProvider(item.provider)]',
  },
  {
    path: "/composer/AgentComposerView.tsx",
    original: 'import { ComposerDraftAttachments } from "./ComposerDraftAttachments";',
    replacement: `import { ComposerDraftAttachments } from "./ComposerDraftAttachments";
import { pickComposerFiles } from "@multica-composer-file-picker";
import { readAgentRichTextPromptImages, supportedPromptImageMimeType } from "../agentRichText/agentRichTextPromptImages";
import { useOptionalAgentHostApi } from "../../../agentActivityHost";`,
  },
  {
    path: "/composer/AgentComposerView.tsx",
    original: '): React.JSX.Element {\n  const {',
    replacement: '): React.JSX.Element {\n  const filePickerHost = useOptionalAgentHostApi();\n  const {',
  },
  {
    path: "/composer/AgentComposerView.tsx",
    original: 'onWorkspaceReferencePicker={handleWorkspaceReferencePicker}',
    replacement: `onWorkspaceReferencePicker={input.externalPromptEntriesSupported ? async () => {
              try {
                const files = await pickComposerFiles();
                const images = files.filter((file) => supportedPromptImageMimeType(file.type));
                const regularFiles = files.filter((file) => !supportedPromptImageMimeType(file.type));
                if (images.length) handlePastedImages(await readAgentRichTextPromptImages(images));
                if (regularFiles.length) input.addExternalPromptEntries(regularFiles);
              } catch {
                filePickerHost?.toast?.error(document.documentElement.lang.startsWith("zh") ? "无法读取所选文件，请重新选择" : "Could not read the selected files. Please select them again.");
              }
            } : handleWorkspaceReferencePicker}`,
  },
  {
    path: "/model/agentComposerDraft.ts",
    original: '      const data = block.data?.trim();\n      const url = block.url?.trim();\n      const imagePath = block.path?.trim();',
    // Local previews are data URLs; submitted images already have a durable ID.
    // Drop the preview locator before applying the unchanged HTTPS URL policy.
    replacement: '      const data = block.data?.trim();\n      const url = attachmentId && block.url?.startsWith("data:") ? undefined : block.url?.trim();\n      const imagePath = block.path?.trim();',
  },
  {
    path: "/agentRichText/agentRichTextEditorSupport.ts",
    original: 'export const AGENT_RICH_TEXT_LARGE_PASTE_MIN_CHARS = 5_000;',
    replacement: 'import { shouldStagePastedText } from "@multica-large-text-paste";\n\nexport const AGENT_RICH_TEXT_LARGE_PASTE_MIN_CHARS = 5_000;',
  },
  {
    path: "/agentRichText/agentRichTextEditorSupport.ts",
    original: 'return text.trim().length >= AGENT_RICH_TEXT_LARGE_PASTE_MIN_CHARS;',
    replacement: 'return shouldStagePastedText(text, AGENT_RICH_TEXT_LARGE_PASTE_MIN_CHARS);',
  },
  {
    path: "/shared/AgentMessageMarkdown.tsx",
    original: 'import { cachedMarkdownParser } from "./cachedMarkdownParser";',
    replacement: 'import { cachedMarkdownParser } from "./cachedMarkdownParser";\nimport { ResourceMarkdownLink, remarkMessageResourceLinks } from "@multica-resource-link";',
  },
  {
    path: "/shared/AgentMessageMarkdown.tsx",
    original: '      ul: MarkdownUnorderedList,',
    replacement: '      table: MarkdownTable,\n      ul: MarkdownUnorderedList,',
  },
  {
    path: "/shared/AgentMessageMarkdown.tsx",
    original: 'import { MarkdownMedia } from "./AgentMessageMarkdownMedia";',
    replacement: 'import { MarkdownTable } from "@multica-markdown-table";\nimport { MarkdownMedia } from "./AgentMessageMarkdownMedia";',
  },
  {
    path: "/shared/AgentMessageMarkdown.tsx",
    original: '      remarkLiteralAutolinkBoundary,\n',
    replacement: '      remarkLiteralAutolinkBoundary,\n      remarkMessageResourceLinks,\n',
  },
  {
    path: "/shared/AgentMessageMarkdown.tsx",
    original: 'remarkPlugins={[remarkGfm, remarkLiteralAutolinkBoundary]}',
    replacement: 'remarkPlugins={[remarkGfm, remarkLiteralAutolinkBoundary, remarkMessageResourceLinks]}',
  },
  {
    path: "/shared/AgentMessageMarkdown.tsx",
    original: '        <MarkdownLink\n          {...props}\n          onLinkClick={handleLinkClick}\n          workspaceAppIcons={workspaceAppIcons}\n          agentTargets={effectiveAgentTargets}\n        />',
    replacement: '        <ResourceMarkdownLink href={props.href} fallback={<MarkdownLink {...props} onLinkClick={handleLinkClick} workspaceAppIcons={workspaceAppIcons} agentTargets={effectiveAgentTargets} />}>{props.children}</ResourceMarkdownLink>',
  },
  {
    path: "/agentConversation/components/AgentAskUserPromptSurface.tsx",
    original: '        {question.allowFreeText !== false &&\n        (!conversationReturn || question.options.length === 0) ? (',
    replacement: '        {question.allowFreeText !== false ? (',
  },
  {
    path: "/agentConversation/components/AgentAskUserPromptSurface.tsx",
    original: '          <textarea\n            value={flow.freeText}',
    replacement: '          <textarea\n            aria-label={question.question}\n            value={flow.freeText}',
  },
  {
    path: "/agentConversation/components/agentTranscriptModel.ts",
    original: '      rowIndex,\n      summary',
    replacement: '      rowIndex,\n      turnId: row.turnId ?? null,\n      summary',
  },
  {
    path: "/agentConversation/components/AgentMessageLocatorRail.tsx",
    original: 'data-agent-message-locator-turn-group-index={item.turnGroupIndex}',
    replacement: 'data-agent-message-locator-turn-group-index={item.turnGroupIndex} data-multica-locator-turn-id={(item as { turnId?: string }).turnId}',
  },
  {
    path: "/view/useAgentGUIDetailSideChrome.tsx",
    original: '        agentSessionId: baseComposerProps.agentSessionId,',
    // Preserve the draft and directory across the session-to-home provider handoff.
    replacement: '        agentSessionId: baseComposerProps.agentSessionId,\n        draftContent: baseComposerProps.draftContent,\n        onDraftContentChange: baseComposerProps.onDraftContentChange,\n        onProjectPathChange: baseComposerProps.onProjectPathChange,',
  },
  {
    path: "/view/useAgentGUIDetailSideChrome.tsx",
    original: '      baseComposerProps.agentSessionId,',
    replacement: '      baseComposerProps.agentSessionId,\n      baseComposerProps.draftContent,\n      baseComposerProps.onDraftContentChange,\n      baseComposerProps.onProjectPathChange,',
  },
  {
    path: "/agentGuiNode/AgentQueuedPromptPanel.tsx",
    original: 'import { ChevronRight } from "lucide-react";',
    replacement: 'import { ChevronRight } from "lucide-react";\nimport { SideQueueAction } from "@multica-side-entry";',
  },
  {
    path: "/agentGuiNode/AgentQueuedPromptPanel.tsx",
    original: '                      <DropdownMenuItem\n                        className={styles.composerMenuItem}',
    replacement: '                      <SideQueueAction agentSessionId={agentSessionId} promptId={queuedPrompt.id} disabled={isDraining} />\n                      <DropdownMenuItem\n                        className={styles.composerMenuItem}',
  },
  {
    path: "/agentGuiNode/AgentQueuedPromptPanel.tsx",
    original: '                      className={styles.composerMenuContent}\n                      sideOffset={8}',
    replacement: '                      className={`${styles.composerMenuContent} multica-queue-menu`}\n                      sideOffset={8}',
  },
  {
    path: "/agentConversation/components/AgentTranscriptView.tsx",
    original: 'data-agent-transcript-virtual-turn={group.key}',
    replacement: 'data-agent-transcript-virtual-turn={group.key} data-multica-turn-id={group.turnId ?? ""}',
  },
  {
    path: "/agentConversation/components/AgentTranscriptView.tsx",
    original: '<Fragment key={group.key}>\n          {renderTurnGroup(group)}',
    replacement: '<Fragment key={group.key}>\n          <div style={{ display: "contents" }} data-multica-turn-id={group.turnId ?? ""}>{renderTurnGroup(group)}</div>',
  },
  {
    path: "/agent/gui/app/renderer/i18n/locales/zh-CN.agentGui.ts",
    original: 'queuePausedByUserLabel: "由于你中断了当前响应，队列已暂停"',
    replacement: 'queuePausedByUserLabel: "队列已暂停"',
  },
  {
    path: "/agent/gui/app/renderer/i18n/locales/en.agentGui.ts",
    original: '"The queue is paused because you interrupted the current response."',
    replacement: '"Queue paused"',
  },
  {
    path: "/agentGuiNode/controller/useAgentGUIConversationMetadataActions.ts",
    original: '        input.selectConversation?.(result.targetAgentSessionId);',
    replacement: '        if (dataRef.current.lastActiveAgentSessionId === normalizedSourceAgentSessionId) input.selectConversation?.(result.targetAgentSessionId);',
  },
  {
    path: "/agent/gui/app/renderer/i18n/locales/zh-CN.agentGui.ts",
    original: 'forkThroughTurn: "从此轮进行会话 Fork"',
    replacement: 'forkThroughTurn: "从此轮分叉"',
  },
  {
    path: "/agent/gui/app/renderer/i18n/locales/zh-CN.agentGui.ts",
    original: 'forkThroughTurnPending: "正在进行会话 Fork"',
    replacement: 'forkThroughTurnPending: "正在分叉…"',
  },
  {
    path: "/at-panel/MentionPalette.tsx",
    original: 'import { mentionPaletteExpandLabel } from "./mentionPaletteModel.ts";',
    replacement: 'import { mentionPaletteExpandLabel } from "./mentionPaletteModel.ts";\nimport { Spinner } from "@multica/ui/components/ui/spinner";\nimport { useMentionScrollPagination } from "@multica-mention-scroll-pagination";',
  },
  {
    path: "/at-panel/MentionPalette.tsx",
    original: '  const scrollBodyRef = useRef<HTMLDivElement | null>(null);',
    replacement: '  const scrollBodyRef = useRef<HTMLDivElement | null>(null);\n  useMentionScrollPagination(scrollBodyRef, state, onExpandGroup);',
  },
  {
    path: "/at-panel/MentionPalette.tsx",
    original: '                  className={paletteStyles.expandButton}',
    replacement: '                  className={paletteStyles.expandButton}\n                  data-load-more-group={group.id}',
  },
  {
    path: "/at-panel/MentionPalette.tsx",
    original: '        const showGroupDivider = index > 0 && !group.hideTopDivider;',
    replacement: '        const showGroupDivider = index > 0 && !group.hideTopDivider;\n        const isLoadingMore = group.expandStatus === "loading" || state.status === "loading";',
  },
  {
    path: "/at-panel/MentionPalette.tsx",
    original: 'disabled={group.expandStatus === "loading"}',
    replacement: 'disabled={isLoadingMore}',
  },
  {
    path: "/at-panel/MentionPalette.tsx",
    original: 'aria-busy={group.expandStatus === "loading" || undefined}',
    replacement: 'aria-busy={isLoadingMore || undefined}',
  },
  {
    path: "/at-panel/MentionPalette.tsx",
    original: 'if (group.expandStatus !== "loading") {',
    replacement: 'if (!isLoadingMore) {',
  },
  {
    path: "/at-panel/MentionPalette.tsx",
    original: '                  {mentionPaletteExpandLabel(group)}',
    replacement: '                  {isLoadingMore ? <><Spinner aria-hidden="true" />{group.expandLoadingLabel ?? mentionPaletteExpandLabel(group)}</> : mentionPaletteExpandLabel(group)}',
  },
  {
    path: "/agentGuiNode/AgentFileMentionPalette.tsx",
    original: 'import { userAvatarPlaceholderUrl } from "../../shared/userAvatarPlaceholder";',
    replacement: 'import { userAvatarPlaceholderUrl } from "../../shared/userAvatarPlaceholder";\nimport { getActiveUiLanguage } from "../../i18n/index";\nimport { renderMulticaMentionRow } from "@multica-mention-row";',
  },
  {
    path: "/agentGuiNode/AgentFileMentionPalette.tsx",
    original: 'renderMentionRow(agentMentionItemToRowItem(item), {',
    replacement: 'renderMulticaMentionRow(item, getActiveUiLanguage(), agentMentionItemToRowItem(item), {',
  },
  {
    path: "/agentGuiNode/AgentMentionSearchModel.ts",
    original: '      topicId: scope.topicId,\n      name: label,',
    replacement: '      topicId: scope.topicId,\n      statusLabel: presentation.statusLabel,\n      statusCategory: presentation.multicaStatusCategory,\n      name: label,',
  },
  {
    path: "/shared/AgentMessageMarkdown.tsx",
    original: '  MarkdownPre,\n',
    replacement: '',
  },
  {
    path: "/shared/AgentMessageMarkdown.tsx",
    original: 'import { MarkdownMedia } from "./AgentMessageMarkdownMedia";',
    replacement: 'import { MarkdownMedia } from "./AgentMessageMarkdownMedia";\nimport { MarkdownPre } from "@multica-html-preview";',
  },
  {
    path: "/agentRichText/agentFileMentionExtension.ts",
    original: '            onExit: () => {\n              options.onSuggestionChange?.(null);',
    // Moving to a new @ query starts the next suggestion synchronously, then
    // Tiptap asynchronously exits the previous query. Keep the live suggestion.
    replacement: '            onExit: (props) => {\n              if (!props.editor.isDestroyed && agentFileMentionPluginKey.getState(props.editor.state)?.active) return;\n              options.onSuggestionChange?.(null);',
  },
  {
    path: "/agentRichText/useAgentRichTextEditorHandle.ts",
    original: '  onBeforeProgrammaticFocus?: () => void;',
    replacement: '  onBeforeProgrammaticFocus?: () => void;\n  onBeforeOpenMentionPalette?: () => void;',
  },
  {
    path: "/agentRichText/useAgentRichTextEditorHandle.ts",
    original: '        const triggerText = isMentionTriggerBoundaryBeforeSelection(',
    replacement: '        input.onBeforeOpenMentionPalette?.();\n        const triggerText = isMentionTriggerBoundaryBeforeSelection(',
  },
  {
    path: "/agentRichText/AgentRichTextEditor.tsx",
    original: '  useAgentRichTextEditorHandle({\n    availableCapabilitiesRef,',
    // Restored drafts suppress automatic suggestions. An explicit reference
    // button action must release that suppression just like typing @ does.
    replacement: '  useAgentRichTextEditorHandle({\n    onBeforeOpenMentionPalette: () => mentionSuggestionSuppression.setRestoredValue(""),\n    availableCapabilitiesRef,',
  },
  {
    path: "/composer/useComposerFocusAndDrop.ts",
    original: 'composerFocusRequestSequence === null ||',
    replacement: '!isActive || inputDisabled || composerFocusRequestSequence === null ||',
  },
  {
    path: "/composer/useComposerFocusAndDrop.ts",
    original: '[composerFocusRequestSequence, scheduleComposerFocus]',
    replacement: '[composerFocusRequestSequence, scheduleComposerFocus, isActive, inputDisabled]',
  },
  {
    path: "/agentGuiNode/AgentMentionSearchController.ts",
    original: 'this.currentFilter = DEFAULT_AGENT_MENTION_FILTER;',
    replacement: '// Retain the last reference category within this composer lifetime.',
  },
  {
    path: "/agentGuiNode/AgentMentionSearchController.ts",
    original: 'filter: DEFAULT_AGENT_MENTION_FILTER,',
    replacement: 'filter: this.currentFilter,',
  },
  {
    path: "/view/AgentGUIEmptyState.tsx",
    original: 'agentTargets.map(projectAgentGUIAgentTargetAvatar)',
    // Keep personal avatars in mentions and session chrome. The home artwork
    // retains the original provider icons and built-in vinyl covers.
    replacement: 'agentTargets.map((target) => ({ ...projectAgentGUIAgentTargetAvatar(target), iconUrl: agentGUIProviderRailIconPresentation(target.provider).iconUrl }))',
  },
  {
    path: "/view/AgentGUIEmptyState.tsx",
    original: '[projectAgentGUIAgentTargetAvatar(presentedSelectedAgentTarget)]',
    replacement: '[{ ...projectAgentGUIAgentTargetAvatar(presentedSelectedAgentTarget), iconUrl: agentGUIProviderRailIconPresentation(presentedSelectedAgentTarget.provider).iconUrl }]',
  },
  {
    path: "/view/AgentGUIEmptyState.tsx",
    original: '  composerProps: AgentComposerProps;\n  labels: AgentGUIViewLabels;',
    replacement: '  composerProps: AgentComposerProps | null;\n  labels: AgentGUIViewLabels;',
  },
  {
    path: "/view/AgentGUIEmptyState.tsx",
    original: '  composerProps: AgentComposerProps;\n  providerSelectLabel: string;',
    replacement: '  composerProps: AgentComposerProps | null;\n  providerSelectLabel: string;',
  },
  {
    path: "/view/AgentGUIEmptyState.tsx",
    original: 'return (\n    <div className={styles.emptyHero}>\n      <div className={styles.emptyHeroBody}>',
    replacement: 'return (\n    <div className={cn(styles.emptyHero, !composerProps && "multica-empty-conversation-welcome")}>\n      <div className={styles.emptyHeroBody}>',
  },
  {
    path: "/view/AgentGUIEmptyState.tsx",
    original: '        <AgentComposer {...composerProps} />',
    replacement: '        {composerProps ? <AgentComposer {...composerProps} /> : null}',
  },
  {
    path: "/view/AgentGUIDetailPane.tsx",
    original: 'import styles from "../AgentGUINode.styles";',
    replacement: 'import styles from "../AgentGUINode.styles";\nimport { useIsSidePane } from "@multica-side-entry";',
  },
  {
    path: "/view/AgentGUIDetailPane.tsx",
    original: '  "use memo";\n  const viewModel = {',
    replacement: '  "use memo";\n  const isHostSidePane = useIsSidePane();\n  const viewModel = {',
  },
  {
    path: "/view/AgentGUIDetailPane.tsx",
    original: '  const homeContent = !hasActiveConversation ? (',
    // A selected draft already has an ID, but its welcome belongs above the
    // existing dock. Never replace loading, execution or unavailable states.
    replacement: '  const showDraftWelcome = hasActiveConversation\n    && viewModel.detail.availability === "ready"\n    && conversation?.rows.length === 0\n    && viewModel.detail.conversationDetail?.turns.length === 0\n    && !isComposerSending;\n  const homeContent = !hasActiveConversation || showDraftWelcome ? (',
  },
  {
    path: "/view/AgentGUIDetailPane.tsx",
    original: '              composerProps={bottomDockComposerProps}',
    // Change presentation on the existing composer so draft and upload state
    // survive the first send and empty-session hydration.
    replacement: '              composerProps={showDraftWelcome && !isHostSidePane ? emptyHeroComposerProps : bottomDockComposerProps}',
  },
  {
    path: "/view/AgentGUIDetailPane.tsx",
    original: '      composerProps={emptyHeroComposerProps}',
    replacement: '      composerProps={hasActiveConversation ? null : emptyHeroComposerProps}',
  },
  {
    path: "/view/AgentGUIDetailPane.tsx",
    original: '      suggestions={labels.homeSuggestions ?? EMPTY_HOME_SUGGESTIONS}',
    replacement: '      suggestions={hasActiveConversation ? EMPTY_HOME_SUGGESTIONS : labels.homeSuggestions ?? EMPTY_HOME_SUGGESTIONS}',
  },
  {
    path: "/view/AgentGUIDetailPane.tsx",
    original: '            conversationFlowEmpty={conversationFlowEmpty}',
    replacement: '            conversationFlowEmpty={homeContent ?? conversationFlowEmpty}',
  },
  {
    path: "/agentGuiNode/agentMentionSearchHelpers.ts",
    original: 'export const AGENT_MENTION_FILTER_TAB_ORDER = [\n  "session",\n  "file",\n  "issue",\n  "agent",\n  "app"\n] as const satisfies readonly AgentMentionFilterId[];',
    replacement: 'export const AGENT_MENTION_FILTER_TAB_ORDER = ["session", "issue", "agent"] as const satisfies readonly AgentMentionFilterId[];',
  },
  {
    path: "/agent/gui/app/renderer/i18n/locales/en.agentGui.ts",
    original: 'initialPlaceholder: "Type @ to reference sessions, files, tasks, and apps"',
    replacement: 'initialPlaceholder: "Type @ to reference sessions, tasks, and agents"',
  },
  {
    path: "/agent/gui/app/renderer/i18n/locales/zh-CN.agentGui.ts",
    original: 'initialPlaceholder: "输入 @ 引用会话、文件、任务和应用"',
    replacement: 'initialPlaceholder: "输入 @ 引用会话、任务和智能体"',
  },
  {
    path: "/composer/useMentionPaletteFrame.ts",
    original: 'top: `${mentionPaletteFrame?.top ?? 0}px`,\n      width: `${mentionPaletteFrame?.width ?? 0}px`,\n      maxWidth: `${mentionPaletteFrame?.width ?? 0}px`,\n      minHeight: `${MENTION_PALETTE_MIN_HEIGHT_PX}px`,\n      maxHeight: `${MENTION_PALETTE_MAX_HEIGHT_PX}px`,\n      height: `${mentionPaletteFrame?.height ?? MENTION_PALETTE_MIN_HEIGHT_PX}px`,',
    // Keep the measured viewport budget and anchor. Let the panel fit its rows
    // upward from that anchor instead of reserving a full 320px for two agents.
    replacement: 'bottom: `calc(100% - ${(mentionPaletteFrame?.top ?? 0) + (mentionPaletteFrame?.height ?? 0)}px)`,\n      width: `${mentionPaletteFrame?.width ?? 0}px`,\n      maxWidth: `${mentionPaletteFrame?.width ?? 0}px`,\n      minHeight: "0px",\n      maxHeight: `${mentionPaletteFrame?.height ?? MENTION_PALETTE_MAX_HEIGHT_PX}px`,\n      height: "auto",',
  },
  {
    path: "/composer/useMentionPaletteFrame.ts",
    original: 'const rect = anchor.getBoundingClientRect();',
    replacement: 'const rect = (anchor.querySelector(".agent-gui-node__composer-prompt-input-area") ?? anchor).getBoundingClientRect();',
  },
  {
    path: "/composer/useMentionPaletteFrame.ts",
    original: 'resizeObserver?.observe(anchor);',
    replacement: 'resizeObserver?.observe(anchor);\n      const prompt = anchor.querySelector(".agent-gui-node__composer-prompt-input-area");\n      if (prompt) resizeObserver?.observe(prompt);',
  },
  {
    path: "/composer/useMentionPaletteFrame.ts",
    original: 'availableAbove >= MENTION_PALETTE_MIN_HEIGHT_PX\n        ? Math.min(MENTION_PALETTE_MAX_HEIGHT_PX, availableAbove)\n        : MENTION_PALETTE_MIN_HEIGHT_PX',
    replacement: 'Math.min(MENTION_PALETTE_MAX_HEIGHT_PX, Math.max(0, availableAbove))',
  },
  {
    path: "/composer/AgentComposerView.tsx",
    original: 'onNavigateHierarchy={navigateFileMentionHierarchy}',
    // All three exposed Multica reference categories are flat directories.
    replacement: 'onNavigateHierarchy={undefined}',
  },
  {
    path: "/composer/AgentComposerView.tsx",
    original: 'isHeroLayout || input.props.composerActionPlacement === "footer"',
    // Keep the action at the bottom of the shared home/conversation composer.
    replacement: 'true',
  },
  {
    path: "/composer/AgentComposerView.tsx",
    original: '!isHeroLayout && !showComposerActionInFooter',
    replacement: '!showComposerActionInFooter',
  },
  {
    path: "/composer/AgentComposerView.tsx",
    original: '!isHeroLayout &&\n                      "agent-gui-node__composer-prompt-input-line"',
    replacement: '"agent-gui-node__composer-prompt-input-line"',
  },
  {
    path: "/composer/useComposerPresentation.tsx",
    original: 'import { SendFilledIcon } from "./AgentComposerDraftPreview";',
    replacement: 'import { ArrowUp as SendFilledIcon } from "lucide-react";',
  },
  {
    path: "/agent/gui/app/renderer/i18n/locales/en.agentGui.ts",
    original: '"Tab switch category | ←→ enter/leave folder | ↑↓ switch selection"',
    replacement: '"Tab / ←→ switch category | ↑↓ switch selection"',
  },
  {
    path: "/agent/gui/app/renderer/i18n/locales/zh-CN.agentGui.ts",
    original: '"Tab 切换分类 ｜ ←→ 进入/返回文件夹 ｜ ↑↓ 切换选中"',
    replacement: '"Tab / ←→ 切换分类 ｜ ↑↓ 切换选中"',
  },
  {
    path: "/agent/gui/app/renderer/i18n/locales/en.agentGui.ts",
    original: 'fileMentionPalette: "Files"',
    replacement: 'fileMentionPalette: "References and agents"',
  },
  {
    path: "/agent/gui/app/renderer/i18n/locales/zh-CN.agentGui.ts",
    original: 'fileMentionPalette: "工作区文件"',
    replacement: 'fileMentionPalette: "引用或调用"',
  },
];

export default defineConfig({
  root: fileURLToPath(new URL(".", import.meta.url)),
  base: "./",
  plugins: [
    {
      name: "multica-html-preview-libraries",
      resolveId(id) { if (id === "virtual:multica-html-libraries") return `\0${id}`; },
      async load(id) {
        if (id !== "\0virtual:multica-html-libraries") return;
        const require = createRequire(import.meta.url);
        const [chart, d3] = await Promise.all([
          readFile(resolve(dirname(require.resolve("chart.js")), "chart.umd.min.js"), "utf8"),
          readFile(resolve(dirname(require.resolve("d3")), "../dist/d3.min.js"), "utf8"),
        ]);
        return `export const libraries = ${JSON.stringify({ chart, d3 })};`;
      },
    },
    {
      name: "multica-host-presentation",
      enforce: "pre",
      transform(source, id) {
        const adaptations = hostPresentationAdaptations.filter(({ path }) => id.replaceAll("\\", "/").endsWith(path));
        if (!adaptations.length) return;
        let code = source;
        // Keep the vendor snapshot intact and fail visibly when a pinned
        // presentation hook changes upstream instead of silently skipping it.
        for (const adaptation of adaptations) {
          if (code.split(adaptation.original).length !== 2) {
            throw new Error(`The pinned Agent GUI source changed at ${adaptation.path}; review the host presentation policy.`);
          }
          code = code.replace(adaptation.original, adaptation.replacement);
        }
        return {
          code,
          map: null,
        };
      },
    },
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      "@multica-timeline-end-position": fileURLToPath(new URL("./src/timeline-end-position.ts", import.meta.url)),
      "@multica-prompt-submission-policy": fileURLToPath(new URL("./src/runtime/prompt-submission-policy.ts", import.meta.url)),
      "@multica-invoked-agent-reply": fileURLToPath(new URL("./src/invoked-agent-reply.tsx", import.meta.url)),
      "@multica-composer-drafts": fileURLToPath(new URL("./src/composer-drafts.tsx", import.meta.url)),
      "@multica-message-image-clipboard": fileURLToPath(new URL("./src/message-image-clipboard.ts", import.meta.url)),
      "@multica-composer-file-picker": fileURLToPath(new URL("./src/composer-file-picker.ts", import.meta.url)),
      "@multica-large-text-paste": fileURLToPath(new URL("./src/large-text-paste.ts", import.meta.url)),
      "@multica-resource-link": fileURLToPath(new URL("./src/resource-link.tsx", import.meta.url)),
      "@multica-side-entry": fileURLToPath(new URL("./src/side-entry.tsx", import.meta.url)),
      "@multica-html-preview": fileURLToPath(new URL("./src/html-preview/markdown-pre.tsx", import.meta.url)),
      "@multica-mention-row": fileURLToPath(new URL("./src/mention-row.tsx", import.meta.url)),
      "@multica-markdown-table": fileURLToPath(new URL("./src/markdown-table.tsx", import.meta.url)),
      "@multica-mention-scroll-pagination": fileURLToPath(new URL("./src/mention-scroll-pagination.ts", import.meta.url)),
    },
    dedupe: ["react", "react-dom", "@tiptap/core", "@tiptap/pm", "@tiptap/react"],
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
    // Both Rolldown minifiers corrupt Unicode chunk-hash offsets in this GUI.
    minify: false,
  },
});
