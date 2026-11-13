"use client";

import type { PointerEvent, SyntheticEvent } from "react";
import { AlertCircle, Check, Mic, MicOff, RotateCcw, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { ShimmerText } from "@multica/ui/components/common/shimmer-text";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { cn } from "@multica/ui/lib/utils";
import { createShortcutChord } from "@multica/core/shortcuts";
import {
  cancelVoice,
  noteVoiceFocus,
  pressVoice,
  releaseVoice,
  retryVoice,
  stopVoice,
  toggleVoice,
  useVoiceMeter,
  useVoiceSettingsStore,
  type VoiceErrorKind,
} from "@multica/core/voice";
import { ActionShortcutKeycaps, ShortcutKeycaps } from "../common/shortcut-keycaps";
import { useT } from "../i18n";
import type { ComposerVoice } from "./use-composer-voice";

const ESCAPE = createShortcutChord("Escape");
const ENTER = createShortcutChord("Enter");

/** Keep the caret in the composer when a voice control is clicked. */
function keepFocusInComposer(event: SyntheticEvent) {
  event.preventDefault();
}

function formatElapsed(ms: number): string {
  const total = Math.floor(ms / 1000);
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}

/** Scrolling level history, newest sample on the right. */
function Waveform({ bars, className }: { bars: number; className?: string }) {
  const { levels } = useVoiceMeter();
  return (
    <div
      aria-hidden
      className={cn("flex h-5 items-center justify-end gap-[2px]", className)}
      style={{ maskImage: "linear-gradient(90deg, transparent 0, #000 24px)" }}
    >
      {levels.slice(-bars).map((level, index) => (
        <span
          // Bars are positional: the history scrolls left one slot per sample.
          key={index}
          className="w-[2px] shrink-0 rounded-full bg-current"
          style={{ height: `${Math.max(10, Math.min(100, level * 110))}%` }}
        />
      ))}
    </div>
  );
}

/** Three bars that fit where the mic icon was. */
function LevelBars() {
  const { levels } = useVoiceMeter();
  const n = levels.length;
  const picks = [levels[n - 3] ?? 0, levels[n - 1] ?? 0, levels[n - 5] ?? 0];
  const shape = [0.7, 1, 0.8];
  return (
    <span aria-hidden className="flex h-3.5 items-center gap-[2px]">
      {picks.map((level, index) => (
        <span
          key={index}
          className="w-[3px] rounded-full bg-current transition-[height] duration-75"
          style={{ height: `${Math.max(22, Math.min(100, level * (shape[index] ?? 1) * 130))}%` }}
        />
      ))}
    </span>
  );
}

function Elapsed({ className }: { className?: string }) {
  const { elapsedMs } = useVoiceMeter();
  return <span className={cn("text-caption tabular-nums", className)}>{formatElapsed(elapsedMs)}</span>;
}

function RecDot({ className }: { className?: string }) {
  return (
    <span
      aria-hidden
      className={cn("size-2 shrink-0 rounded-full bg-destructive motion-safe:animate-pulse", className)}
    />
  );
}

/**
 * The mic every composer carries. Tap to dictate into the toolbar strip;
 * press and hold to talk, release to insert.
 */
export function VoiceMicButton({ voice, className }: { voice: ComposerVoice; className?: string }) {
  const { t } = useT("editor");
  const activation = useVoiceSettingsStore((state) => state.activation);
  if (!voice.available) return null;
  const { session } = voice;
  const live =
    session !== null &&
    (session.phase === "requesting" || session.phase === "recording") &&
    session.mode !== "strip";
  const label =
    activation === "hold"
      ? t(($) => $.voice.hold_to_talk)
      : activation === "toggle"
        ? t(($) => $.voice.dictate)
        : t(($) => $.voice.tap_or_hold);
  return (
    <IconButton
      label={label}
      shortcut={<ActionShortcutKeycaps action="voiceInput" />}
      disabled={voice.busyElsewhere}
      aria-pressed={live}
      onMouseDown={keepFocusInComposer}
      onPointerDown={(event: PointerEvent<HTMLButtonElement>) => {
        if (event.button !== 0) return;
        event.preventDefault();
        event.currentTarget.setPointerCapture(event.pointerId);
        noteVoiceFocus(voice.id);
        pressVoice(voice.id);
      }}
      onPointerUp={releaseVoice}
      onPointerCancel={releaseVoice}
      onClick={(event) => {
        // Keyboard activation (Enter/Space on the focused button) has no
        // pointer phase and cannot be held.
        if (event.detail === 0) toggleVoice(voice.id);
      }}
      className={cn(
        "touch-none text-muted-foreground",
        live && "bg-brand/10 text-brand hover:bg-brand/15 hover:text-brand",
        className,
      )}
    >
      {live && session.phase === "recording" ? <LevelBars /> : <Mic />}
    </IconButton>
  );
}

/**
 * The recording strip. Replaces the composer's toolbar row while a tapped
 * recording runs: cancel, timer, waveform, insert, send.
 */
export function VoiceStrip({ voice, bars = 64 }: { voice: ComposerVoice; bars?: number }) {
  const { t } = useT("editor");
  const phase = voice.session?.phase;
  if (!voice.showStrip) return null;
  if (phase === "transcribing") {
    return (
      <div className="flex min-w-0 flex-1 items-center gap-1" aria-live="polite">
        <span className="flex size-[var(--button-height-sm)] shrink-0 items-center justify-center">
          <Mic className="size-4 text-muted-foreground" />
        </span>
        <ShimmerText className="truncate pl-1 text-caption font-medium text-muted-foreground">
          {t(($) => $.voice.transcribing)}
        </ShimmerText>
      </div>
    );
  }
  return (
    <div className="flex min-w-0 flex-1 items-center gap-1">
      <IconButton
        label={t(($) => $.voice.cancel)}
        shortcut={<ShortcutKeycaps shortcut={ESCAPE} decorative />}
        onMouseDown={keepFocusInComposer}
        onClick={cancelVoice}
        className="text-muted-foreground"
      >
        <X />
      </IconButton>
      {phase === "requesting" ? (
        <ShimmerText className="min-w-0 flex-1 truncate pl-1 text-caption font-medium text-muted-foreground">
          {t(($) => $.voice.requesting)}
        </ShimmerText>
      ) : (
        <>
          <span className="flex shrink-0 items-center gap-1.5 pl-1 pr-2" role="status">
            <RecDot />
            <span className="sr-only">{t(($) => $.voice.recording)}</span>
            <Elapsed className="text-muted-foreground" />
          </span>
          <Waveform bars={bars} className="min-w-0 flex-1 overflow-hidden text-foreground" />
          <IconButton
            label={t(($) => $.voice.insert)}
            shortcut={<ActionShortcutKeycaps action="voiceInput" />}
            onMouseDown={keepFocusInComposer}
            onClick={() => stopVoice(false)}
            className="ml-1 text-foreground"
          >
            <Check />
          </IconButton>
          {voice.canSubmit && (
            <SubmitButton
              onClick={() => stopVoice(true)}
              ariaLabel={t(($) => $.voice.send)}
              tooltip={
                <>
                  {t(($) => $.voice.send)}
                  <ShortcutKeycaps shortcut={ENTER} decorative />
                </>
              }
            />
          )}
        </>
      )}
    </div>
  );
}

/**
 * The push-to-talk pill, anchored above the composer that receives the text.
 * Always dark ink, like a system indicator, so it reads over any page. The
 * composer root must be `relative`.
 */
export function VoiceHud({ voice }: { voice: ComposerVoice }) {
  const { t } = useT("editor");
  const phase = voice.session?.phase;
  if (!voice.showHud) return null;
  return (
    <div className="pointer-events-none absolute bottom-full left-1/2 z-30 mb-3 -translate-x-1/2">
      <div
        role="status"
        className="flex h-10 items-center gap-2.5 whitespace-nowrap rounded-full bg-multica-ink pl-3.5 pr-4 text-multica-paper shadow-[var(--floating-shadow)] animate-in fade-in zoom-in-95 slide-in-from-bottom-2"
      >
        {phase === "recording" ? (
          <>
            <RecDot className="bg-[#ff5a52]" />
            <span className="sr-only">{t(($) => $.voice.recording)}</span>
            <Waveform bars={28} className="w-[86px]" />
            <Elapsed className="opacity-70" />
            <span className="text-caption opacity-70">{t(($) => $.voice.release_to_insert)}</span>
          </>
        ) : (
          <>
            <Mic className="size-4 opacity-70" />
            <span className="text-caption opacity-80">
              {phase === "transcribing" ? t(($) => $.voice.transcribing) : t(($) => $.voice.requesting)}
            </span>
          </>
        )}
      </div>
    </div>
  );
}

function useErrorMessage(error: VoiceErrorKind): string {
  const { t } = useT("editor");
  switch (error) {
    case "permission-denied":
      return t(($) => $.voice.errors.permission_denied);
    case "no-microphone":
      return t(($) => $.voice.errors.no_microphone);
    case "no-speech":
      return t(($) => $.voice.errors.no_speech);
    case "unavailable":
      return t(($) => $.voice.errors.unavailable);
    case "transcription-failed":
      return t(($) => $.voice.errors.transcription_failed);
    case "failed":
      return t(($) => $.voice.errors.microphone_failed);
  }
}

/** One-line error inside the composer, above its toolbar. */
export function VoiceErrorRow({ voice, className }: { voice: ComposerVoice; className?: string }) {
  const error = voice.session?.phase === "error" ? voice.session.error : null;
  if (!error) return null;
  return <VoiceErrorContent error={error} className={className} />;
}

function VoiceErrorContent({ error, className }: { error: VoiceErrorKind; className?: string }) {
  const { t } = useT("editor");
  const message = useErrorMessage(error);
  const Icon = error === "permission-denied" || error === "no-microphone" ? MicOff : AlertCircle;
  return (
    <div
      role="alert"
      className={cn(
        "flex items-center gap-2 rounded-md bg-destructive/8 px-2 py-1 text-caption text-destructive",
        className,
      )}
    >
      <Icon className="size-3.5 shrink-0" aria-hidden />
      <span className="min-w-0 flex-1">{message}</span>
      {error !== "unavailable" && (
        <Button
          size="xs"
          variant="ghost"
          className="text-destructive hover:text-destructive"
          onMouseDown={keepFocusInComposer}
          onClick={retryVoice}
        >
          <RotateCcw />
          {t(($) => $.voice.retry)}
        </Button>
      )}
      <Button
        size="xs"
        variant="ghost"
        className="text-muted-foreground"
        onMouseDown={keepFocusInComposer}
        onClick={cancelVoice}
      >
        {t(($) => $.voice.dismiss)}
      </Button>
    </div>
  );
}
