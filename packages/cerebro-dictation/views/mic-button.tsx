"use client";

import { useCallback, useMemo, useRef } from "react";
import { Mic, Square } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useDictation } from "../use-dictation";
import { createWebSocketStreamingTranscriber } from "../streaming-transcriber";
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
  const dictationEnabled = useFeatureFlag("cerebro_voice_dictation_enabled");
  const voiceEnabled = useFeatureFlag("cerebro_voice_output_enabled");
  const enabled = dictationEnabled && voiceEnabled;
  const pointerRecordingRef = useRef(false);
  const suppressClickRef = useRef(false);
  const defaultStreamTranscribe = useMemo(
    () => (streamUrl ? createWebSocketStreamingTranscriber(streamUrl) : undefined),
    [streamUrl],
  );
  const { status, error, isSupported, start, stop } = useDictation({
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

  const handlePointerDown = useCallback(() => {
    if (isDisabled || isRecording) return;
    pointerRecordingRef.current = true;
    void start();
  }, [isDisabled, isRecording, start]);

  const handlePointerUp = useCallback(() => {
    if (!pointerRecordingRef.current) return;
    pointerRecordingRef.current = false;
    suppressClickRef.current = true;
    void stop();
  }, [stop]);

  const handleClick = useCallback(() => {
    if (suppressClickRef.current) {
      suppressClickRef.current = false;
      return;
    }
    if (isDisabled) return;
    if (pointerRecordingRef.current) return;
    if (isRecording) {
      void stop();
      return;
    }
    void start();
  }, [isDisabled, isRecording, start, stop]);

  if (!enabled) {
    return null;
  }

  const tooltip = !isSupported
    ? "Dictation is not supported in this browser"
    : error
      ? error.message
      : isRecording
        ? "Release to stop dictation"
        : "Hold to dictate";

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type="button"
            size="icon-sm"
            variant={isRecording ? "secondary" : "ghost"}
            disabled={isDisabled}
            aria-label={isRecording ? "Stop dictation" : "Start dictation"}
            aria-pressed={isRecording}
            onPointerDown={handlePointerDown}
            onPointerUp={handlePointerUp}
            onPointerCancel={handlePointerUp}
            onPointerLeave={handlePointerUp}
            onClick={handleClick}
          >
            {isRecording ? <Square className="fill-current" /> : <Mic />}
          </Button>
        }
      />
      <TooltipContent side="top">{tooltip}</TooltipContent>
    </Tooltip>
  );
}
