"use client";

import { useMemo } from "react";
import { useWorkspaceId } from "@multica/core/hooks";
import { MicButton } from "./mic-button";
import { createHttpTranscriber, createWarmup } from "../http-transcriber";
import type { DictationStatus } from "../types";

export interface EditorDictationMicProps {
  /** Insert the finished transcription into the host editor at the caret. */
  onTranscribed: (text: string) => void;
  disabled?: boolean;
  /** Positioning hook — host editors place the mic in a corner. */
  className?: string;
  /**
   * Forwarded from MicButton: every dictation status change. Hosts use it to
   * show a destination-side cue (the transcribing skeleton, Forslag B) while
   * the clip is in flight. Memoize the callback.
   */
  onStatusChange?: (status: DictationStatus) => void;
  /**
   * Forwarded to MicButton. "floating" renders the round grey record button for
   * note / document overlays; "inline" (default) the composer-cluster mic.
   */
  appearance?: "inline" | "floating";
  /** Forwarded to MicButton: green success-cue label ("Inserted" / "Added"). */
  successLabel?: string;
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
  onStatusChange,
  appearance,
  successLabel,
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
  const warmup = useMemo(
    () =>
      workspaceId
        ? createWarmup(`/api/workspaces/${workspaceId}/cerebro/dictation/warmup`)
        : undefined,
    [workspaceId],
  );

  if (!workspaceId || !transcribe) return null;

  return (
    <div className={className}>
      <MicButton
        disabled={disabled}
        transcribe={transcribe}
        warmup={warmup}
        onTranscribed={onTranscribed}
        onStatusChange={onStatusChange}
        appearance={appearance}
        successLabel={successLabel}
      />
    </div>
  );
}
