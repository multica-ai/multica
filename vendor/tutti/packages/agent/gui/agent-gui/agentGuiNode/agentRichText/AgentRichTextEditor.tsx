import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import {
  forwardRef,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent
} from "react";
import type { Editor } from "@tiptap/core";
import { useEditor } from "@tiptap/react";
import { cn } from "../../../app/renderer/lib/utils";
import { useTranslation } from "../../../i18n/index";
import { createAgentRichTextInputExtensions } from "./agentRichTextExtensions";
import {
  agentRichTextContentToPromptText,
  editorToPromptText,
  plainTextToAgentRichTextInlineContent,
  plainTextToAgentRichTextDoc
} from "./agentRichTextDocument";
import { isAgentRichTextImeComposing } from "./agentRichTextIme";
import {
  hasWorkspaceFileDropData,
  readWorkspaceFileDropEntries
} from "../../terminalNode/workspaceFileDrop";
import {
  readAgentRichTextPromptImages,
  routeAgentRichTextExternalFiles,
  systemFileDragInfoFromDataTransfer
} from "./agentRichTextPromptImages";
import type {
  AgentRichTextContextMenuState,
  AgentRichTextEditorHandle,
  AgentRichTextEditorProps
} from "./AgentRichTextEditor.types";
import {
  buildWorkspaceFileMentionDropContent,
  createAgentRichTextCaretAnchorExtension,
  createAgentRichTextPlaceholderExtension,
  isAgentRichTextLargeTextPaste,
  isPromptVisualLineStart,
  readEditorDomSelectionRange,
  readPlainTextFromClipboard,
  readPromptSelection,
  readPromptTextRange,
  readSelectedPlainText,
  serializeAgentRichTextSelection,
  scrollEditorSelectionIntoView,
  writePlainTextToClipboard
} from "./agentRichTextEditorSupport";
export { isAgentRichTextLargeTextPaste } from "./agentRichTextEditorSupport";
import { pasteMessageImageReferences } from "@multica/tutti-agent-gui-host/message-image-clipboard";
import { routeAgentRichTextTextPaste } from "./routeAgentRichTextTextPaste";
import { useAgentRichTextEditorHandle } from "./useAgentRichTextEditorHandle";
import { AgentRichTextEditorSurface } from "./AgentRichTextEditorSurface";
import { handleAgentRichTextKeyDownCapture } from "./agentRichTextKeyboard";
import {
  isAgentRichTextUserContentInsertion,
  markAgentRichTextPointerFocus
} from "./agentRichTextEngagement";
import {
  createAgentRichTextControlledValueTracker,
  recordAgentRichTextLocalEdit,
  resolveAgentRichTextControlledSelection,
  shouldApplyAgentRichTextControlledValue
} from "./agentRichTextControlledValue";
import { createAgentRichTextMentionSuggestionSuppression } from "./agentRichTextMentionSuggestionSuppression";
import { observer } from "mobx-react-lite";

export type {
  AgentRichTextEditorHandle,
  AgentRichTextEditorProps,
  AgentRichTextPastedImage
} from "./AgentRichTextEditor.types";
export const AgentRichTextEditor = observer(
  forwardRef<AgentRichTextEditorHandle, AgentRichTextEditorProps>(
    function AgentRichTextEditor(
      {
        value,
        contentScopeKey = "default",
        disabled,
        placeholder,
        removeMentionLabel,
        className,
        testId,
        onChange,
        onContentLayoutInvalidated,
        onFocus,
        onUserContentChange,
        onSubmit,
        onSubmitGuidance,
        availableCapabilities = [],
        availableSkills = [],
        submitOnEnter = true,
        enableFileMentionSuggestions = true,
        onKeyDownForPalette,
        onHistoryNavigation,
        onFileMentionSuggestionChange,
        onFileMentionSuggestionKeyDown,
        onLinkClick,
        promptImagesSupported = true,
        onPromptImagesUnsupported,
        onPasteImages,
        onPasteLargeText,
        onPasteFiles,
        onDropFiles,
        onResolvePastedPath
      },
      ref
    ): React.JSX.Element {
      "use memo";
      const { t } = useTranslation();
      const lastEmittedPromptRef = useRef<string | null>(value);
      const controlledValueTrackerRef = useRef(
        createAgentRichTextControlledValueTracker(contentScopeKey)
      );
      const appliedContentScopeKeyRef = useRef(contentScopeKey);
      const editorRef = useRef<Editor | null>(null);
      const onChangeRef = useRef(onChange);
      const onContentLayoutInvalidatedRef = useRef(onContentLayoutInvalidated);
      const onFocusRef = useRef(onFocus);
      const onUserContentChangeRef = useRef(onUserContentChange);
      const pendingFocusMethodRef = useRef<"pointer" | "programmatic" | null>(
        null
      );
      const onSubmitRef = useRef(onSubmit);
      const onSubmitGuidanceRef = useRef(onSubmitGuidance);
      const onKeyDownForPaletteRef = useRef(onKeyDownForPalette);
      const onHistoryNavigationRef = useRef(onHistoryNavigation);
      const onFileMentionSuggestionChangeRef = useRef(
        onFileMentionSuggestionChange
      );
      const onFileMentionSuggestionKeyDownRef = useRef(
        onFileMentionSuggestionKeyDown
      );
      const onLinkClickRef = useRef(onLinkClick);
      const onPromptImagesUnsupportedRef = useRef(onPromptImagesUnsupported);
      const onPasteImagesRef = useRef(onPasteImages);
      const onPasteLargeTextRef = useRef(onPasteLargeText);
      const onPasteFilesRef = useRef(onPasteFiles);
      const onDropFilesRef = useRef(onDropFiles);
      const onResolvePastedPathRef = useRef(onResolvePastedPath);
      const promptImagesSupportedRef = useRef(promptImagesSupported);
      const placeholderRef = useRef(placeholder);
      const removeMentionLabelRef = useRef(removeMentionLabel);
      const availableSkillsRef = useRef(availableSkills);
      const availableCapabilitiesRef = useRef(availableCapabilities);
      const [mentionSuggestionSuppression] = useState(() =>
        createAgentRichTextMentionSuggestionSuppression(value)
      );
      const scrollFrameRef = useRef<number | null>(null);
      const [contextMenu, setContextMenu] =
        useState<AgentRichTextContextMenuState | null>(null);
      const closeContextMenu = useCallback(() => {
        setContextMenu(null);
      }, []);
      const insertPlainText = useCallback((text: string): void => {
        const currentEditor = editorRef.current;
        if (!currentEditor || currentEditor.isDestroyed || !text) {
          return;
        }
        if (
          onPasteLargeTextRef.current &&
          isAgentRichTextLargeTextPaste(text)
        ) {
          onPasteLargeTextRef.current(text);
          return;
        }
        mentionSuggestionSuppression.suppressTextInsertion(text);
        currentEditor
          .chain()
          .focus()
          .insertContent(
            plainTextToAgentRichTextInlineContent(text, {
              capabilities: availableCapabilitiesRef.current,
              skills: availableSkillsRef.current
            })
          )
          .run();
      }, []);

      const copySelection = useCallback(async (): Promise<void> => {
        const currentEditor = editorRef.current;
        const selectedText =
          contextMenu && contextMenu.hasSelection && currentEditor
            ? readPromptTextRange(
                currentEditor,
                contextMenu.selectionFrom,
                contextMenu.selectionTo
              )
            : currentEditor
              ? readSelectedPlainText(currentEditor)
              : "";
        closeContextMenu();
        if (!currentEditor || currentEditor.isDestroyed) {
          return;
        }
        await writePlainTextToClipboard(selectedText);
      }, [closeContextMenu, contextMenu]);
      const copySelectionCallback = usePersistCallback(copySelection);

      const cutSelection = useCallback(async (): Promise<void> => {
        const currentEditor = editorRef.current;
        const selectionFrom = contextMenu?.selectionFrom ?? null;
        const selectionTo = contextMenu?.selectionTo ?? null;
        const selectedText =
          contextMenu && contextMenu.hasSelection && currentEditor
            ? readPromptTextRange(
                currentEditor,
                contextMenu.selectionFrom,
                contextMenu.selectionTo
              )
            : currentEditor
              ? readSelectedPlainText(currentEditor)
              : "";
        closeContextMenu();
        if (!currentEditor || currentEditor.isDestroyed || disabled) {
          return;
        }
        if (!(await writePlainTextToClipboard(selectedText))) {
          return;
        }
        const { from, to } =
          selectionFrom !== null &&
          selectionTo !== null &&
          selectionFrom < selectionTo
            ? { from: selectionFrom, to: selectionTo }
            : currentEditor.state.selection;
        currentEditor.chain().focus().deleteRange({ from, to }).run();
      }, [closeContextMenu, contextMenu, disabled]);
      const cutSelectionCallback = usePersistCallback(cutSelection);

      const pasteClipboardText = useCallback(async (): Promise<void> => {
        closeContextMenu();
        if (disabled) {
          return;
        }
        const text = await readPlainTextFromClipboard();
        if (text) {
          insertPlainText(text);
        }
      }, [closeContextMenu, disabled, insertPlainText]);
      const pasteClipboardTextCallback = usePersistCallback(pasteClipboardText);

      const scheduleSelectionScroll = (targetEditor: Editor): void => {
        if (typeof window.requestAnimationFrame !== "function") {
          scrollEditorSelectionIntoView(targetEditor);
          return;
        }
        if (scrollFrameRef.current !== null) {
          window.cancelAnimationFrame(scrollFrameRef.current);
        }
        scrollFrameRef.current = window.requestAnimationFrame(() => {
          scrollFrameRef.current = null;
          const currentEditor = editorRef.current;
          if (
            !currentEditor ||
            currentEditor !== targetEditor ||
            currentEditor.isDestroyed
          ) {
            return;
          }
          scrollEditorSelectionIntoView(currentEditor);
        });
      };
      const extensions = useMemo(
        () => [
          ...createAgentRichTextInputExtensions(
            {
              enableSuggestions: enableFileMentionSuggestions,
              onSuggestionChange: (state) =>
                onFileMentionSuggestionChangeRef.current?.(state),
              onSuggestionKeyDown: (event) =>
                onFileMentionSuggestionKeyDownRef.current?.(event) ?? false,
              removeActionAriaLabel: removeMentionLabelRef.current,
              shouldSuppressSuggestion:
                mentionSuggestionSuppression.isSuppressed
            },
            { skills: availableSkillsRef.current },
            { capabilities: availableCapabilitiesRef.current }
          ),
          createAgentRichTextCaretAnchorExtension(),
          createAgentRichTextPlaceholderExtension(() => placeholderRef.current)
        ],
        [enableFileMentionSuggestions]
      );

      onChangeRef.current = onChange;
      onContentLayoutInvalidatedRef.current = onContentLayoutInvalidated;
      onFocusRef.current = onFocus;
      onUserContentChangeRef.current = onUserContentChange;
      onSubmitRef.current = onSubmit;
      onSubmitGuidanceRef.current = onSubmitGuidance;
      onKeyDownForPaletteRef.current = onKeyDownForPalette;
      onHistoryNavigationRef.current = onHistoryNavigation;
      onFileMentionSuggestionChangeRef.current = onFileMentionSuggestionChange;
      onFileMentionSuggestionKeyDownRef.current =
        onFileMentionSuggestionKeyDown;
      onLinkClickRef.current = onLinkClick;
      onPromptImagesUnsupportedRef.current = onPromptImagesUnsupported;
      onPasteImagesRef.current = onPasteImages;
      onPasteLargeTextRef.current = onPasteLargeText;
      onPasteFilesRef.current = onPasteFiles;
      onDropFilesRef.current = onDropFiles;
      onResolvePastedPathRef.current = onResolvePastedPath;
      promptImagesSupportedRef.current = promptImagesSupported;
      placeholderRef.current = placeholder;
      removeMentionLabelRef.current = removeMentionLabel;
      availableSkillsRef.current = availableSkills;
      availableCapabilitiesRef.current = availableCapabilities;

      const editor = useEditor({
        immediatelyRender: false,
        editable: !disabled,
        extensions,
        content: plainTextToAgentRichTextDoc(value, {
          capabilities: availableCapabilities,
          skills: availableSkills
        }),
        editorProps: {
          attributes: {
            role: "textbox",
            "aria-label": placeholder,
            "aria-disabled": disabled ? "true" : "false",
            "aria-multiline": "true",
            class: cn(
              className,
              "overflow-y-auto whitespace-pre-wrap break-words [&_p]:m-0"
            )
          },
          clipboardTextSerializer: (slice) =>
            agentRichTextContentToPromptText(slice.content.toJSON()),
          handleDOMEvents: {
            click: (_view, event) => {
              if (
                !onLinkClickRef.current ||
                !(event.target instanceof Element)
              ) {
                return false;
              }
              const mention = event.target.closest(
                '[data-agent-file-mention="true"]'
              );
              if (!(mention instanceof HTMLElement)) {
                return false;
              }
              const href =
                mention instanceof HTMLAnchorElement
                  ? mention.getAttribute("href") || ""
                  : mention.getAttribute("data-agent-mention-href") || "";
              if (!href) {
                return false;
              }
              event.preventDefault();
              event.stopPropagation();
              onLinkClickRef.current(href);
              return true;
            },
            contextmenu: (_view, event) => {
              const currentEditor = editorRef.current;
              if (!currentEditor || currentEditor.isDestroyed) {
                return false;
              }
              event.preventDefault();
              event.stopPropagation();
              const stateSelection = currentEditor.state.selection;
              const domSelection = stateSelection.empty
                ? readEditorDomSelectionRange(currentEditor)
                : null;
              const from = domSelection?.from ?? stateSelection.from;
              const to = domSelection?.to ?? stateSelection.to;
              setContextMenu({
                canEdit: !disabled,
                hasSelection: from < to,
                selectionFrom: from,
                selectionTo: to,
                x: event.clientX,
                y: event.clientY
              });
              return true;
            },
            copy: (_view, event) => {
              const currentEditor = editorRef.current;
              if (
                !currentEditor ||
                currentEditor.isDestroyed ||
                !event.clipboardData
              ) {
                return false;
              }
              const clipboard = serializeAgentRichTextSelection(currentEditor);
              if (!clipboard) {
                return false;
              }
              event.clipboardData.setData("text/plain", clipboard.text);
              event.clipboardData.setData("text/html", clipboard.html);
              event.preventDefault();
              return true;
            },
            cut: (_view, event) => {
              const currentEditor = editorRef.current;
              if (
                disabled ||
                !currentEditor ||
                currentEditor.isDestroyed ||
                !event.clipboardData
              ) {
                return false;
              }
              const selection = readPromptSelection(currentEditor);
              const clipboard = serializeAgentRichTextSelection(currentEditor);
              if (!clipboard) {
                return false;
              }
              event.clipboardData.setData("text/plain", clipboard.text);
              event.clipboardData.setData("text/html", clipboard.html);
              event.preventDefault();
              currentEditor.commands.deleteRange({
                from: selection.from,
                to: selection.to
              });
              return true;
            },
            paste: (_view, event) => {
              if (disabled) return false;
              if (
                pasteMessageImageReferences({
                  text: event.clipboardData?.getData("text/plain") ?? "",
                  supported: promptImagesSupportedRef.current,
                  onImages: onPasteImagesRef.current,
                  onUnsupported: onPromptImagesUnsupportedRef.current,
                  insertText: (text) =>
                    routeAgentRichTextTextPaste({
                      availableCapabilities: availableCapabilitiesRef.current,
                      availableSkills: availableSkillsRef.current,
                      editorRef,
                      html: "",
                      mentionSuggestionSuppression,
                      text
                    })
                })
              ) {
                event.preventDefault();
                return true;
              }
              const { externalFiles, imageFiles, imagesHandledAsFiles } =
                routeAgentRichTextExternalFiles(event.clipboardData, {
                  externalFilesSupported: Boolean(onPasteFilesRef.current),
                  promptImagesSupported: promptImagesSupportedRef.current
                });
              if (imageFiles.length > 0 || externalFiles.length > 0) {
                event.preventDefault();
                if (externalFiles.length > 0) {
                  onPasteFilesRef.current?.(externalFiles);
                }
                if (imageFiles.length > 0 && !imagesHandledAsFiles) {
                  if (!promptImagesSupportedRef.current) {
                    onPromptImagesUnsupportedRef.current?.();
                  } else {
                    void readAgentRichTextPromptImages(imageFiles).then(
                      (images) => {
                        if (images.length > 0) {
                          onPasteImagesRef.current?.(images);
                        }
                      }
                    );
                  }
                }
                return true;
              }
              const html = event.clipboardData?.getData("text/html") ?? "";
              const text = event.clipboardData?.getData("text/plain") ?? "";
              const handled = routeAgentRichTextTextPaste({
                availableCapabilities: availableCapabilitiesRef.current,
                availableSkills: availableSkillsRef.current,
                editorRef,
                html,
                mentionSuggestionSuppression,
                onPasteLargeText: onPasteLargeTextRef.current,
                resolvePastedPath: onResolvePastedPathRef.current,
                text
              });
              if (handled) {
                event.preventDefault();
              }
              return handled;
            },
            keydown: (_view, event) => {
              if (isAgentRichTextImeComposing(event)) {
                return false;
              }
              if (disabled) {
                return false;
              }
              if (onKeyDownForPaletteRef.current?.(event)) {
                return true;
              }
              if (
                event.key === "Enter" &&
                (event.metaKey || event.ctrlKey) &&
                !event.shiftKey &&
                !event.altKey
              ) {
                event.preventDefault();
                if (!submitOnEnter) {
                  return true;
                }
                onSubmitGuidanceRef.current?.();
                return true;
              }
              if (
                event.key !== "Enter" ||
                event.shiftKey ||
                event.metaKey ||
                event.ctrlKey ||
                event.altKey
              ) {
                return false;
              }
              if (!submitOnEnter) {
                return false;
              }
              event.preventDefault();
              onSubmitRef.current();
              return true;
            },
            dragover: (_view, event) => {
              const dataTransfer = event.dataTransfer;
              if (!dataTransfer || disabled) {
                return false;
              }
              const systemFileDragInfo =
                systemFileDragInfoFromDataTransfer(dataTransfer);
              const canDropRegularSystemFiles =
                systemFileDragInfo.hasRegularFiles &&
                Boolean(onDropFilesRef.current);
              const canDropImagesAsFiles =
                systemFileDragInfo.hasImageFiles &&
                !promptImagesSupportedRef.current &&
                Boolean(onDropFilesRef.current);
              if (
                systemFileDragInfo.hasImageFiles ||
                canDropRegularSystemFiles
              ) {
                event.preventDefault();
                dataTransfer.dropEffect =
                  canDropRegularSystemFiles ||
                  canDropImagesAsFiles ||
                  (systemFileDragInfo.hasImageFiles &&
                    promptImagesSupportedRef.current)
                    ? "copy"
                    : "none";
                return true;
              }
              if (!hasWorkspaceFileDropData(dataTransfer)) {
                return false;
              }
              const entries = readWorkspaceFileDropEntries(dataTransfer);
              if (entries.length === 0) {
                return false;
              }
              event.preventDefault();
              dataTransfer.dropEffect = "copy";
              return true;
            },
            drop: (_view, event) => {
              const dataTransfer = event.dataTransfer;
              if (!dataTransfer || disabled) {
                return false;
              }
              const {
                externalFiles: filesForExternalPreparation,
                imageFiles,
                imagesHandledAsFiles
              } = routeAgentRichTextExternalFiles(dataTransfer, {
                externalFilesSupported: Boolean(onDropFilesRef.current),
                promptImagesSupported: promptImagesSupportedRef.current
              });
              if (
                imageFiles.length > 0 ||
                filesForExternalPreparation.length > 0
              ) {
                event.preventDefault();
                const currentEditor = editorRef.current;
                if (
                  filesForExternalPreparation.length > 0 &&
                  onDropFilesRef.current &&
                  currentEditor &&
                  !currentEditor.isDestroyed
                ) {
                  const coordinatePosition = currentEditor.view.posAtCoords({
                    left: event.clientX,
                    top: event.clientY
                  })?.pos;
                  const selectionPosition = currentEditor.state.selection.from;
                  const insertPosition =
                    coordinatePosition ??
                    (Number.isInteger(selectionPosition)
                      ? selectionPosition
                      : null) ??
                    currentEditor.state.doc.content.size;
                  currentEditor
                    .chain()
                    .focus()
                    .setTextSelection(insertPosition)
                    .run();
                  onDropFilesRef.current(filesForExternalPreparation);
                }
                if (imageFiles.length === 0 || imagesHandledAsFiles) {
                  return true;
                }
                if (!promptImagesSupportedRef.current) {
                  onPromptImagesUnsupportedRef.current?.();
                  return true;
                }
                void readAgentRichTextPromptImages(imageFiles).then(
                  (images) => {
                    if (images.length > 0) {
                      onPasteImagesRef.current?.(images);
                    }
                  }
                );
                return true;
              }
              if (!hasWorkspaceFileDropData(dataTransfer)) {
                return false;
              }
              event.preventDefault();
              const currentEditor = editorRef.current;
              if (!currentEditor || currentEditor.isDestroyed) {
                return true;
              }
              const entries = readWorkspaceFileDropEntries(dataTransfer);
              if (entries.length === 0) {
                return true;
              }
              const coordinatePosition = currentEditor.view.posAtCoords({
                left: event.clientX,
                top: event.clientY
              })?.pos;
              const fallbackSelectionPosition =
                currentEditor.state.selection.from;
              const insertPosition =
                coordinatePosition ??
                (Number.isInteger(fallbackSelectionPosition)
                  ? fallbackSelectionPosition
                  : null) ??
                currentEditor.state.doc.content.size;
              currentEditor
                .chain()
                .focus()
                .insertContentAt(
                  insertPosition,
                  buildWorkspaceFileMentionDropContent(entries, {
                    prefixCaretAnchor: isPromptVisualLineStart(
                      currentEditor,
                      insertPosition
                    )
                  })
                )
                .run();
              return true;
            }
          }
        },
        onUpdate: ({ editor: nextEditor, transaction }) => {
          editorRef.current = nextEditor;
          scheduleSelectionScroll(nextEditor);
          onContentLayoutInvalidatedRef.current?.();
          const nextPrompt = editorToPromptText(nextEditor);
          if (nextPrompt === lastEmittedPromptRef.current) {
            return;
          }
          lastEmittedPromptRef.current = nextPrompt;
          recordAgentRichTextLocalEdit(
            controlledValueTrackerRef.current,
            nextPrompt
          );
          if (isAgentRichTextUserContentInsertion(transaction)) {
            onUserContentChangeRef.current?.(nextPrompt);
          }
          onChangeRef.current(nextPrompt);
        },
        onFocus: ({ editor: nextEditor }) => {
          const pendingMethod = pendingFocusMethodRef.current;
          const focusMethod =
            pendingMethod ??
            (nextEditor.view.dom.matches(":focus-visible")
              ? "keyboard"
              : "programmatic");
          pendingFocusMethodRef.current = null;
          onFocusRef.current?.(focusMethod);
        },
        onBlur: () => {
          pendingFocusMethodRef.current = null;
        },
        onCreate: ({ editor: nextEditor }) => {
          editorRef.current = nextEditor;
          nextEditor.commands.setTextSelection(
            nextEditor.state.doc.content.size
          );
        }
      });

      const handleKeyDownCapture = (
        event: ReactKeyboardEvent<HTMLDivElement>
      ): void => {
        mentionSuggestionSuppression.releaseForContentEditingKey(event);
        handleAgentRichTextKeyDownCapture(event, {
          disabled,
          editorRef,
          onKeyDownForPaletteRef,
          onHistoryNavigationRef,
          onSubmitGuidanceRef,
          onSubmitRef,
          submitOnEnter
        });
      };
      const handleKeyDownCaptureCallback =
        usePersistCallback(handleKeyDownCapture);
      useEffect(
        () => () => {
          if (
            scrollFrameRef.current !== null &&
            typeof window.cancelAnimationFrame === "function"
          ) {
            window.cancelAnimationFrame(scrollFrameRef.current);
          }
        },
        []
      );

      useEffect(() => {
        if (!contextMenu) {
          return;
        }

        const close = () => closeContextMenu();
        const closeOnEscape = (event: KeyboardEvent) => {
          if (event.key === "Escape") {
            closeContextMenu();
          }
        };
        window.addEventListener("pointerdown", close);
        window.addEventListener("resize", close);
        window.addEventListener("keydown", closeOnEscape);
        return () => {
          window.removeEventListener("pointerdown", close);
          window.removeEventListener("resize", close);
          window.removeEventListener("keydown", closeOnEscape);
        };
      }, [closeContextMenu, contextMenu]);

      useAgentRichTextEditorHandle({
        onBeforeOpenMentionPalette: () =>
          mentionSuggestionSuppression.setRestoredValue(""),
        availableCapabilitiesRef,
        availableSkillsRef,
        editorRef,
        onBeforeProgrammaticFocus: () => {
          pendingFocusMethodRef.current = "programmatic";
        },
        ref
      });

      useEffect(() => {
        if (!editor || editor.isDestroyed) {
          return;
        }
        if (
          !shouldApplyAgentRichTextControlledValue({
            lastEmittedValue: lastEmittedPromptRef.current,
            scopeKey: contentScopeKey,
            tracker: controlledValueTrackerRef.current,
            value
          })
        ) {
          return;
        }
        const nextDoc = plainTextToAgentRichTextDoc(value, {
          capabilities: availableCapabilities,
          skills: availableSkills
        });
        const scopeChanged =
          appliedContentScopeKeyRef.current !== contentScopeKey;
        const previousSelection = editor.state.selection;
        mentionSuggestionSuppression.setRestoredValue(value);
        if (JSON.stringify(editor.getJSON()) === JSON.stringify(nextDoc)) {
          lastEmittedPromptRef.current = value;
          appliedContentScopeKeyRef.current = contentScopeKey;
          return;
        }
        editor.commands.setContent(nextDoc, { emitUpdate: false });
        const documentEnd = editor.state.doc.content.size;
        editor.commands.setTextSelection(
          resolveAgentRichTextControlledSelection(
            scopeChanged,
            previousSelection,
            documentEnd
          )
        );
        lastEmittedPromptRef.current = value;
        appliedContentScopeKeyRef.current = contentScopeKey;
        onContentLayoutInvalidatedRef.current?.();
      }, [
        availableCapabilities,
        availableSkills,
        contentScopeKey,
        editor,
        value
      ]);

      const handlePointerDownCaptureProp1 = usePersistCallback<
        NonNullable<
          React.ComponentProps<
            typeof AgentRichTextEditorSurface
          >["handlePointerDownCapture"]
        >
      >(() => markAgentRichTextPointerFocus(pendingFocusMethodRef));
      return (
        <AgentRichTextEditorSurface
          className={className}
          contextMenu={contextMenu}
          copySelection={copySelectionCallback}
          cutSelection={cutSelectionCallback}
          disabled={disabled}
          editor={editor}
          handleKeyDownCapture={handleKeyDownCaptureCallback}
          handlePointerDownCapture={handlePointerDownCaptureProp1}
          pasteClipboardText={pasteClipboardTextCallback}
          placeholder={placeholder}
          testId={testId}
          t={t}
        />
      );
    }
  )
);
