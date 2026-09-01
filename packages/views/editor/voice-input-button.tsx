"use client";

import type { RefObject } from "react";
import { useDictationAdapter } from "@multica/core/platform/dictation";
import type { ContentEditorRef } from "./content-editor";
import { NativeDictationButton } from "./native-dictation-button";

export interface VoiceInputButtonProps {
  editorRef: RefObject<ContentEditorRef | null>;
  disabled?: boolean;
  className?: string;
  size?: "sm" | "default";
  /** Mount readonly-first composers before focusing the native paste target. */
  onBeforeRecord?: () => void;
}

/** Only the host's native dictation UI records audio. Web/mobile have no mic
 * entry; a missing or failed native bridge never enables an API fallback. */
export function VoiceInputButton(props: VoiceInputButtonProps) {
  const adapter = useDictationAdapter();
  return adapter ? <NativeDictationButton {...props} adapter={adapter} /> : null;
}
