"use client";

import { useMemo } from "react";
import { useWorkspaceId } from "@multica/core/hooks";
import { MicButton } from "./mic-button";
import { createHttpTranscriber } from "../http-transcriber";

export interface EditorDictationMicProps {
  /** Insert the finished transcription into the host editor at the caret. */
  onTranscribed: (text: string) => void;
  disabled?: boolean;
  /** Positioning hook — host editors place the mic in a corner. */
  className?: string;
}

/**
 * The dictation mic as mounted inside an editor (ContentEditor / TitleEditor).
 * One component, so every editing surface gets the same one-shot dictation
 * (record → POST → text) by rendering this once. It resolves the
 * workspace-scoped transcribe endpoint and hands the clip to the cerebro
 * dictation backend, which proxies to the hviske model. Visibility is gated by
 * `cerebro_voice_dictation_enabled` inside MicButton, so this renders nothing
 * when the feature is off.
 */
export function EditorDictationMic({
  onTranscribed,
  disabled,
  className,
}: EditorDictationMicProps) {
  const workspaceId = useWorkspaceId();
  const transcribe = useMemo(
    () =>
      workspaceId
        ? createHttpTranscriber(
            `/api/workspaces/${workspaceId}/cerebro/dictation/transcribe`,
          )
        : undefined,
    [workspaceId],
  );

  if (!workspaceId || !transcribe) return null;

  return (
    <div className={className}>
      <MicButton
        disabled={disabled}
        transcribe={transcribe}
        onTranscribed={onTranscribed}
      />
    </div>
  );
}
