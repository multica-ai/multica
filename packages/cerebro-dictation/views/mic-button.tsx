"use client";

import { useCallback, useMemo } from "react";
import { Mic, Square } from "lucide-react";
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
import type { DictationError, StreamingTranscriber, Transcriber } from "../types";

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
}

export function MicButton({
  disabled = false,
  transcribe,
  streamTranscribe,
  streamUrl,
  onPartialTranscribed,
  onTranscribed,
  onError,
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

  const isRecording = status === "recording";
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
              {isRecording ? <Square className="fill-current" /> : <Mic />}
            </Button>
          }
        />
        <TooltipContent side="top">{tooltip}</TooltipContent>
      </Tooltip>
    </div>
  );
}
