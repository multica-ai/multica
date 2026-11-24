import type { ReactNode } from "react";
import { AlertCircle, MicOff, RotateCcw } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Kbd } from "@multica/ui/components/ui/kbd";
import { cn } from "@multica/ui/lib/utils";
import type { Copy } from "./copy";
import type { VoiceError } from "./voice-session";
import "./voice.css";

export function formatElapsed(ms: number): string {
  const total = Math.floor(ms / 1000);
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}

/** Scrolling level history: newest sample on the right. */
export function Waveform({
  levels,
  bars = 40,
  className,
  barClassName,
  minHeight = 10,
}: {
  levels: number[];
  bars?: number;
  className?: string;
  barClassName?: string;
  minHeight?: number;
}) {
  const slice = levels.slice(-bars);
  return (
    <div aria-hidden className={cn("voice-wave-fade flex h-5 items-center justify-end gap-[2px]", className)}>
      {slice.map((v, i) => (
        <span
          key={i}
          className={cn("w-[2px] shrink-0 rounded-full bg-current", barClassName)}
          style={{ height: `${Math.max(minHeight, Math.min(100, v * 110))}%` }}
        />
      ))}
    </div>
  );
}

/** A compact three-bar meter that fits where an icon would be. */
export function LevelBars({ levels, className }: { levels: number[]; className?: string }) {
  const n = levels.length;
  const pick = [levels[n - 3] ?? 0, levels[n - 1] ?? 0, levels[n - 5] ?? 0];
  const shape = [0.7, 1, 0.8];
  return (
    <span aria-hidden className={cn("flex h-3.5 items-center gap-[2px]", className)}>
      {pick.map((v, i) => (
        <span
          key={i}
          className="w-[3px] rounded-full bg-current transition-[height] duration-75"
          style={{ height: `${Math.max(22, Math.min(100, v * shape[i]! * 130))}%` }}
        />
      ))}
    </span>
  );
}

export function RecDot({ className }: { className?: string }) {
  return <span aria-hidden className={cn("voice-rec-dot size-2 shrink-0 rounded-full bg-destructive", className)} />;
}

export function Elapsed({ ms, className }: { ms: number; className?: string }) {
  return <span className={cn("text-caption tabular-nums text-muted-foreground", className)}>{formatElapsed(ms)}</span>;
}

export function Shimmer({ children, className }: { children: ReactNode; className?: string }) {
  return <span className={cn("voice-shimmer text-caption font-medium", className)}>{children}</span>;
}

/** Committed words in full ink, words the recognizer may still revise in muted ink. */
export function LiveTranscript({
  finalText,
  interimText,
  caret = true,
  className,
}: {
  finalText: string;
  interimText: string;
  caret?: boolean;
  className?: string;
}) {
  return (
    <span className={cn("whitespace-pre-wrap break-words", className)}>
      {finalText}
      <span className="text-muted-foreground">{interimText}</span>
      {caret && <span className="voice-caret" />}
    </span>
  );
}

export function KeyHint({ keys, label }: { keys: string[]; label: string }) {
  return (
    <span className="inline-flex items-center gap-1 text-caption text-muted-foreground">
      {keys.map((k) => (
        <Kbd key={k}>{k}</Kbd>
      ))}
      <span>{label}</span>
    </span>
  );
}

export function errorMessage(error: VoiceError, copy: Copy): string {
  if (error === "permission-denied") return copy.deniedBody;
  if (error === "no-speech") return copy.noSpeech;
  return copy.network;
}

/** One-line error row that sits inside a composer, above its toolbar. */
export function InlineVoiceError({
  error,
  copy,
  onRetry,
  onDismiss,
}: {
  error: VoiceError;
  copy: Copy;
  onRetry?: () => void;
  onDismiss?: () => void;
}) {
  const Icon = error === "permission-denied" ? MicOff : AlertCircle;
  return (
    <div
      role="alert"
      className="flex items-center gap-2 rounded-md bg-destructive/8 px-2 py-1.5 text-caption text-destructive"
    >
      <Icon className="size-3.5 shrink-0" />
      <span className="min-w-0 flex-1">
        {error === "permission-denied" && <span className="font-medium">{copy.deniedTitle} </span>}
        {errorMessage(error, copy)}
      </span>
      {onRetry && error !== "permission-denied" && (
        <Button size="xs" variant="ghost" className="text-destructive hover:text-destructive" onClick={onRetry}>
          <RotateCcw />
          {copy.retry}
        </Button>
      )}
      {onDismiss && (
        <Button size="xs" variant="ghost" className="text-muted-foreground" onClick={onDismiss}>
          {copy.cancel}
        </Button>
      )}
    </div>
  );
}
