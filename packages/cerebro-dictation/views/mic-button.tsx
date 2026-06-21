"use client";

import { useCallback, useEffect, useMemo } from "react";
import { Loader2, Mic, Square } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useDictation } from "../use-dictation";
import { createWebSocketStreamingTranscriber } from "../streaming-transcriber";
import { Waveform } from "./waveform";
import type {
  DictationError,
  DictationStatus,
  StreamingTranscriber,
  Transcriber,
} from "../types";

const backendNotDeployedTranscriber: Transcriber = async () => {
  throw new Error("Dictation backend is not deployed yet.");
};

export interface MicButtonProps {
  disabled?: boolean;
  transcribe?: Transcriber;
  streamTranscribe?: StreamingTranscriber;
  streamUrl?: string;
  onPartialTranscribed?: (text: string) => void;
  onTranscribed: (text: string) => void;
  onError?: (error: DictationError) => void;
  /**
   * Notifies the host of every dictation status change. Lets a wrapper render a
   * destination-side cue (the transcribing skeleton, Forslag B) without owning
   * the dictation hook itself. Memoize the callback — it is read in an effect
   * dependency list.
   */
  onStatusChange?: (status: DictationStatus) => void;
}

export function MicButton({
  disabled = false,
  transcribe,
  streamTranscribe,
  streamUrl,
  onPartialTranscribed,
  onTranscribed,
  onError,
  onStatusChange,
}: MicButtonProps) {
  // Dictation (speech → text) gates on its own flag only. The read-aloud flag
  // (cerebro_voice_output_enabled) is a separate, unrelated TTS feature — it
  // must not hide the mic.
  const enabled = useFeatureFlag("cerebro_voice_dictation_enabled");
  const defaultStreamTranscribe = useMemo(
    () => (streamUrl ? createWebSocketStreamingTranscriber(streamUrl) : undefined),
    [streamUrl],
  );
  const { status, error, isSupported, mediaStream, start, stop } = useDictation({
    transcribe: transcribe ?? backendNotDeployedTranscriber,
    streamTranscribe: streamTranscribe ?? defaultStreamTranscribe,
    onPartial: onPartialTranscribed,
    onTranscribed,
    onError,
  });

  // Bubble every status change to the host so a wrapper can render a
  // destination-side cue (the transcribing skeleton, Forslag B) — MicButton
  // stays the single owner of the dictation hook (FIR-1637).
  useEffect(() => {
    onStatusChange?.(status);
  }, [status, onStatusChange]);

  const isRecording = status === "recording";
  // The gap the user feels: from releasing the mic until the text lands
  // (the model transcribes for ~2-4s). Without a visible signal the button
  // just goes inert and it looks broken — so we show a spinner + label while
  // the clip is in flight (FIR-1637).
  const isTranscribing = status === "transcribing";
  const isBusy =
    status === "requesting-permission" || status === "transcribing";
  const isDisabled = disabled || !isSupported || isBusy;

  // Tap-to-toggle: one tap starts, the next stops. The old press-and-hold
  // model broke on touch — the OS long-press gesture fires `pointercancel`
  // and finger drift fires `pointerleave`, both of which aborted the
  // recording before any audio was captured, so nothing reached the field
  // (FIR-1678). A plain click sidesteps the whole pointer-gesture conflict.
  const handleClick = useCallback(() => {
    if (isBusy) return;
    if (isRecording) {
      void stop();
      return;
    }
    if (isDisabled) return;
    void start();
  }, [isBusy, isDisabled, isRecording, start, stop]);

  if (!enabled) {
    return null;
  }

  const tooltip = !isSupported
    ? "Dictation is not supported in this browser"
    : error
      ? error.message
      : isTranscribing
        ? "Transcribing…"
        : isRecording
          ? "Stop dictation"
          : "Start dictation";

  return (
    <div className="flex items-center gap-1.5">
      {/* Live level meter while recording, so the user can see the mic is
          actually listening — the feedback the old hold-to-talk button never
          gave. */}
      {isRecording && mediaStream && (
        <Waveform stream={mediaStream} className="w-16 text-brand sm:w-20" />
      )}
      {/* "Text is on its way" — the visible processing state between releasing
          the mic and the transcript arriving (FIR-1637). aria-live announces it
          to screen readers. */}
      {isTranscribing && (
        <span
          className="flex items-center gap-1 text-xs text-muted-foreground"
          aria-live="polite"
        >
          <Loader2 className="size-3 animate-spin" aria-hidden="true" />
          Transcribing…
        </span>
      )}
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              type="button"
              size="icon-sm"
              variant={isRecording ? "secondary" : "ghost"}
              className={cn(isRecording && "text-brand")}
              // While recording the button must stay clickable to stop, even
              // though `isDisabled` covers the busy/unsupported states.
              disabled={isDisabled && !isRecording}
              aria-label={isRecording ? "Stop dictation" : "Start dictation"}
              aria-pressed={isRecording}
              onClick={handleClick}
            >
              {isRecording ? (
                <Square className="fill-current" />
              ) : isTranscribing ? (
                <Loader2 className="animate-spin" />
              ) : (
                <Mic />
              )}
            </Button>
          }
        />
        <TooltipContent side="top">{tooltip}</TooltipContent>
      </Tooltip>
    </div>
  );
}
