import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  type RefObject
} from "react";
import type {
  AgentRichTextEditorHandle,
  AgentRichTextPastedImage
} from "../agentRichText/AgentRichTextEditor";
import { useComposerFileDrop } from "./useComposerFileDrop";

interface Input {
  composerControlsHardDisabled: boolean;
  inputDisabled: boolean;
  editorHandleRef: RefObject<AgentRichTextEditorHandle | null>;
  composerRef: RefObject<HTMLFormElement | null>;
  wasActiveRef: RefObject<boolean>;
  lastComposerFocusRequestRef: RefObject<number | null>;
  isActive: boolean;
  composerFocusRequestSequence: number | null;
  promptFilesSupported: boolean;
  promptImagesSupported: boolean;
  addDraftImages: (images: AgentRichTextPastedImage[]) => void;
  addDraftFiles: (files: readonly File[]) => void;
  onPromptImagesUnsupported?: () => void;
}

function canRestoreComposerFocus(composer: HTMLFormElement): boolean {
  let owner: Element = composer;
  let ownerDocument = composer.ownerDocument;
  // The host's search lives outside the editor document. Walk the frame chain
  // rather than treating an unfocused iframe body as permission to take focus.
  while (true) {
    const focused = ownerDocument.activeElement;
    if (
      focused &&
      focused !== ownerDocument.body &&
      focused !== ownerDocument.documentElement &&
      focused !== owner &&
      !owner.contains(focused)
    ) {
      return false;
    }
    try {
      const frame = ownerDocument.defaultView?.frameElement;
      if (!frame) {
        return ownerDocument.defaultView?.parent === ownerDocument.defaultView;
      }
      owner = frame;
      ownerDocument = frame.ownerDocument;
    } catch {
      // A cross-origin host cannot grant automatic focus restoration here.
      return false;
    }
  }
}

export function useComposerFocusAndDrop(input: Input) {
  const {
    composerControlsHardDisabled,
    inputDisabled,
    editorHandleRef,
    composerRef,
    wasActiveRef,
    lastComposerFocusRequestRef,
    isActive,
    composerFocusRequestSequence,
    promptFilesSupported,
    promptImagesSupported,
    addDraftImages,
    addDraftFiles,
    onPromptImagesUnsupported
  } = input;
  const pendingFocusFrameRef = useRef<number | null>(null);
  const canFocusRef = useRef(false);
  const cancelComposerFocus = useCallback(() => {
    if (pendingFocusFrameRef.current !== null) {
      window.cancelAnimationFrame(pendingFocusFrameRef.current);
      pendingFocusFrameRef.current = null;
    }
  }, []);
  useLayoutEffect(() => {
    canFocusRef.current =
      isActive && !inputDisabled && !composerControlsHardDisabled;
    return () => {
      canFocusRef.current = false;
      cancelComposerFocus();
    };
  }, [isActive, inputDisabled, composerControlsHardDisabled, cancelComposerFocus]);
  const handleMentionPaletteButton = useCallback((): void => {
    if (composerControlsHardDisabled || inputDisabled) {
      return;
    }
    editorHandleRef.current?.openMentionPalette();
  }, [composerControlsHardDisabled, inputDisabled, editorHandleRef]);
  const scheduleComposerFocus = useCallback(
    (intent: "restore" | "explicit" = "explicit") => {
      cancelComposerFocus();
      if (!canFocusRef.current) {
        return;
      }
      pendingFocusFrameRef.current = window.requestAnimationFrame(() => {
        pendingFocusFrameRef.current = window.requestAnimationFrame(() => {
          pendingFocusFrameRef.current = null;
          if (!canFocusRef.current) return;
          if (intent === "restore") {
            const composer = composerRef.current;
            if (!composer || !canRestoreComposerFocus(composer)) return;
          }
          editorHandleRef.current?.focusAtEnd();
        });
      });
    },
    [cancelComposerFocus, composerRef, editorHandleRef]
  );
  const handlePastedImages = useCallback(
    (images: AgentRichTextPastedImage[]): void => {
      addDraftImages(images);
      scheduleComposerFocus();
    },
    [addDraftImages, scheduleComposerFocus]
  );
  const { fileDropOverlayActive, fileDropOverlayHost } = useComposerFileDrop({
    composerRef,
    editorHandleRef,
    inputDisabled,
    promptFilesSupported,
    promptImagesSupported,
    addDraftImages,
    addDraftFiles,
    scheduleComposerFocus,
    onPromptImagesUnsupported
  });
  useEffect(() => {
    if (!isActive) {
      wasActiveRef.current = false;
      return;
    }
    if (!wasActiveRef.current) {
      scheduleComposerFocus("restore");
    }
    wasActiveRef.current = true;
  }, [isActive, scheduleComposerFocus, wasActiveRef]);
  useEffect(() => {
    if (
      !isActive ||
      inputDisabled ||
      composerControlsHardDisabled ||
      composerFocusRequestSequence === null ||
      composerFocusRequestSequence === lastComposerFocusRequestRef.current
    ) {
      return;
    }
    lastComposerFocusRequestRef.current = composerFocusRequestSequence;
    scheduleComposerFocus();
  }, [
    isActive,
    inputDisabled,
    composerControlsHardDisabled,
    composerFocusRequestSequence,
    scheduleComposerFocus,
    lastComposerFocusRequestRef
  ]);

  return {
    fileDropOverlayActive,
    fileDropOverlayHost,
    handleMentionPaletteButton,
    handlePastedImages
  };
}
