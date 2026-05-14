"use client";

import { useCallback, useRef } from "react";
import { Mic, Square } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useDictation } from "../use-dictation";
import type { Transcriber } from "../types";

const backendNotDeployedTranscriber: Transcriber = async () => {
  throw new Error("Dictation backend is not deployed yet.");
};

export interface MicButtonProps {
  disabled?: boolean;
  transcribe?: Transcriber;
  onTranscribed: (text: string) => void;
}

export function MicButton({
  disabled = false,
  transcribe = backendNotDeployedTranscriber,
  onTranscribed,
}: MicButtonProps) {
  const enabled = useFeatureFlag("cerebro_voice_dictation_enabled");
  const pointerRecordingRef = useRef(false);
  const suppressClickRef = useRef(false);
  const { status, error, isSupported, start, stop } = useDictation({
    transcribe,
    onTranscribed,
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
