"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { CircleCheck, Loader2, Mic, RotateCcw, Square } from "lucide-react";
import { toast } from "sonner";
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

/** mm:ss for the recording timer. */
function formatElapsed(totalSeconds: number): string {
  const m = Math.floor(totalSeconds / 60);
  const s = totalSeconds % 60;
  return `${m}:${s.toString().padStart(2, "0")}`;
}

/**
 * A user-facing message for a dictation failure. Until now a failed
 * transcription was silent — the button just went inert and nothing reached the
 * field, so it looked like dictation "did nothing" (Jesper, FIR-1637). We now
 * surface it as a toast. `aborted` is the user cancelling on purpose, so it is
 * intentionally excluded by the caller.
 */
function errorToastMessage(error: DictationError): string {
  switch (error.kind) {
    case "permission-denied":
      return "Microphone access was blocked. Allow it in your browser to dictate.";
    case "no-microphone":
      return "No microphone was found.";
    case "unsupported":
      return "Dictation isn't supported in this browser.";
    case "warming-up":
      // A cold start, not a failure: the engine is booting (~30s). Keep it calm
      // and point at the kept recording (FIR-2048).
      return "The speech engine is warming up — your recording is saved. Tap Re-send in a moment.";
    case "transcription-failed":
    case "streaming-failed":
      // Append the concrete cause so a failure on a device we can't reproduce
      // (e.g. iPhone) is self-reporting in the screenshot, not a dead end.
      return error.message
        ? `Couldn't transcribe that — ${error.message}`
        : "Couldn't transcribe that — the speech service didn't respond. Please try again.";
    case "recording-failed":
      return error.message
        ? `Recording failed — ${error.message}`
        : "Recording failed. Please try again.";
    default:
      return error.message || "Dictation failed. Please try again.";
  }
}

export interface MicButtonProps {
  disabled?: boolean;
  transcribe?: Transcriber;
  streamTranscribe?: StreamingTranscriber;
  streamUrl?: string;
  /**
   * Fired the instant the user presses the mic, before recording starts, to
   * pre-boot the speech engine while they speak (Jesper, FIR-1637). Best-effort
   * and side-effect-free for the UI — see `createWarmup`.
   */
  warmup?: () => void;
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
  /**
   * Visual treatment. "inline" (default) is the ghost icon button mounted in a
   * composer action cluster next to the attach button. "floating" is the round,
   * filled grey record button (white icon) that overlays a note / document /
   * quick-note editor (FIR-1748, Jesper) — a distinct round button on those
   * surfaces so dictation reads as the primary action, not a corner affordance.
   * Turns red while recording.
   */
  appearance?: "inline" | "floating";
  /**
   * Label for the brief green success cue shown next to the mic once a
   * transcript lands. Defaults to "Inserted" for composer inputs; note /
   * document surfaces pass "Added" (FIR-1748, Jesper).
   */
  successLabel?: string;
}

export function MicButton({
  disabled = false,
  transcribe,
  streamTranscribe,
  streamUrl,
  warmup,
  onPartialTranscribed,
  onTranscribed,
  onError,
  onStatusChange,
  appearance = "inline",
  successLabel = "Inserted",
}: MicButtonProps) {
  const floating = appearance === "floating";
  // Dictation (speech → text) gates on its own flag only. The read-aloud flag
  // (cerebro_voice_output_enabled) is a separate, unrelated TTS feature — it
  // must not hide the mic.
  const enabled = useFeatureFlag("cerebro_voice_dictation_enabled");
  const defaultStreamTranscribe = useMemo(
    () => (streamUrl ? createWebSocketStreamingTranscriber(streamUrl) : undefined),
    [streamUrl],
  );

  // Brief green "Inserted" confirmation next to the mic the moment the
  // transcript lands in the field (Jesper, FIR-1637). Without it the recording
  // → transcribing → (silently done) flow gave no closing signal, so it wasn't
  // obvious the text had actually been inserted. Auto-clears after a beat.
  const [showInserted, setShowInserted] = useState(false);
  const handleTranscribed = useCallback(
    (text: string) => {
      onTranscribed(text);
      setShowInserted(true);
    },
    [onTranscribed],
  );

  const { status, error, isSupported, mediaStream, start, stop, canResend, resend } =
    useDictation({
      transcribe: transcribe ?? backendNotDeployedTranscriber,
      streamTranscribe: streamTranscribe ?? defaultStreamTranscribe,
      onPartial: onPartialTranscribed,
      onTranscribed: handleTranscribed,
      onError,
    });

  // Bubble every status change to the host so a wrapper can render a
  // destination-side cue (the transcribing skeleton, Forslag B) — MicButton
  // stays the single owner of the dictation hook (FIR-1637).
  useEffect(() => {
    onStatusChange?.(status);
  }, [status, onStatusChange]);

  const isRecording = status === "recording";

  // Recording timer (FIR-1637, Jesper): a blue mm:ss counter next to the
  // waveform so it's obvious the mic is actually capturing, not just animating.
  // Ticks once a second only while recording; resets the moment it stops.
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    if (!isRecording) {
      setElapsed(0);
      return;
    }
    setElapsed(0);
    const startedAt = performance.now();
    const id = setInterval(() => {
      setElapsed(Math.floor((performance.now() - startedAt) / 1000));
    }, 250);
    return () => clearInterval(id);
  }, [isRecording]);

  // Surface failures (FIR-1637, Jesper): a failed transcription used to be
  // silent — nothing landed in the field and it looked like dictation "did
  // nothing". Toast every non-aborted error so the user knows what happened.
  useEffect(() => {
    if (!error || error.kind === "aborted") return;
    // A warming-up cold start is expected, not a failure — show it as a neutral
    // message, not a red error toast, since the recording is safe and re-sendable
    // (FIR-2048).
    if (error.kind === "warming-up") {
      toast.message(errorToastMessage(error));
    } else {
      toast.error(errorToastMessage(error));
    }
  }, [error]);

  // Hold the "Inserted" confirmation for a beat, then fade it. Starting a new
  // recording clears it immediately so the live meter isn't crowded by a stale
  // success label.
  useEffect(() => {
    if (status === "recording" || status === "transcribing") {
      setShowInserted(false);
      return;
    }
  }, [status]);
  useEffect(() => {
    if (!showInserted) return;
    const id = setTimeout(() => setShowInserted(false), 2500);
    return () => clearTimeout(id);
  }, [showInserted]);

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
    // Pre-boot the engine the moment the user commits to recording, so a cold
    // start overlaps their speech instead of stalling the transcribe (FIR-1637).
    warmup?.();
    void start();
  }, [isBusy, isDisabled, isRecording, start, stop, warmup]);

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
      {/* Blue recording timer next to the wave (Jesper, FIR-1637) — proof the
          mic is actually capturing. tabular-nums keeps it from jittering. */}
      {isRecording && (
        <span
          className="text-xs font-medium tabular-nums text-blue-500"
          aria-live="off"
        >
          {formatElapsed(elapsed)}
        </span>
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
      {/* Green "Inserted" confirmation, sitting to the LEFT of the mic so it
          reads as a result of the dictation just finished (Jesper, FIR-1637). */}
      {showInserted && !isRecording && !isTranscribing && (
        <span
          className="flex items-center gap-1 text-xs font-semibold text-green-600"
          aria-live="polite"
        >
          <CircleCheck className="size-3.5" aria-hidden="true" />
          {successLabel}
        </span>
      )}
      {/* Re-send the kept recording after a failure / cold start, without
          speaking again (FIR-2048). Hidden while recording or transcribing. */}
      {canResend && !isRecording && !isTranscribing && (
        <Button
          type="button"
          size="xs"
          variant="outline"
          className="gap-1"
          onClick={() => void resend()}
          aria-label="Re-send recording for transcription"
        >
          <RotateCcw className="size-3" aria-hidden="true" />
          Re-send
        </Button>
      )}
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              type="button"
              size={floating ? "icon" : "icon-sm"}
              variant={!floating && isRecording ? "secondary" : "ghost"}
              className={cn(
                floating
                  ? cn(
                      // sm:size-11 too — the Button preset's `sm:size-8` would
                      // otherwise win over an unprefixed size on desktop and
                      // shrink the round button back to 32px.
                      "size-11 sm:size-11 rounded-full text-white shadow-md hover:text-white",
                      isRecording
                        ? "animate-pulse bg-red-500 hover:bg-red-600"
                        : "bg-zinc-500 hover:bg-zinc-600",
                    )
                  : isRecording && "text-brand",
              )}
              // While recording the button must stay clickable to stop, even
              // though `isDisabled` covers the busy/unsupported states.
              disabled={isDisabled && !isRecording}
              aria-label={isRecording ? "Stop dictation" : "Start dictation"}
              aria-pressed={isRecording}
              onClick={handleClick}
            >
              {isRecording ? (
                <Square className={cn("fill-current", floating && "size-4")} />
              ) : isTranscribing ? (
                <Loader2 className={cn("animate-spin", floating && "size-5")} />
              ) : (
                <Mic className={cn(floating && "size-5")} />
              )}
            </Button>
          }
        />
        <TooltipContent side="top">{tooltip}</TooltipContent>
      </Tooltip>
    </div>
  );
}
