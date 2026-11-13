import { ArrowUp, Check, Mic, Sparkles, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Kbd, KbdGroup } from "@multica/ui/components/ui/kbd";
import { cn } from "@multica/ui/lib/utils";
import { useStoryEnv } from "../env";
import { Elapsed, InlineVoiceError, LevelBars, RecDot, Shimmer, Waveform, formatElapsed } from "../primitives";
import { useVoiceSettings } from "../settings/store";
import type { VoiceTarget } from "./voice-provider";

export function ShortcutKeys({ keys }: { keys: string[] }) {
  return (
    <KbdGroup>
      {keys.map((k) => (
        <Kbd key={k}>{k}</Kbd>
      ))}
    </KbdGroup>
  );
}

/**
 * The one mic control every composer carries. Tap to dictate into the
 * toolbar strip; press and hold to talk, release to insert.
 */
export function VoiceMicButton({ target }: { target: VoiceTarget }) {
  const { copy } = useStoryEnv();
  const { settings } = useVoiceSettings();
  const { layer, active, mode, phase } = target;
  const live = active && (mode === "pending" || mode === "hud") && phase !== "error";
  const tooltip =
    settings.activation === "auto" ? copy.micTooltip : settings.activation === "hold" ? copy.holdToTalk : copy.dictate;
  return (
    <IconButton
      label={tooltip}
      shortcut={<ShortcutKeys keys={settings.shortcut} />}
      onPointerDown={(e) => {
        if (e.button !== 0) return;
        e.currentTarget.setPointerCapture(e.pointerId);
        target.onFocus();
        layer.press(target.id, "pointer");
      }}
      onPointerUp={() => layer.release()}
      onPointerCancel={() => layer.release()}
      className={cn("touch-none text-muted-foreground", live && "bg-brand/10 text-brand hover:bg-brand/15 hover:text-brand")}
    >
      {live && phase === "listening" ? <LevelBars levels={target.session.levels} /> : <Mic />}
    </IconButton>
  );
}

/**
 * A's recording strip. Replaces the composer's bottom row while a tapped
 * recording runs: cancel, timer, waveform, insert, send.
 */
export function VoiceStrip({ target, compact }: { target: VoiceTarget; compact?: boolean }) {
  const { copy } = useStoryEnv();
  const { settings } = useVoiceSettings();
  const { layer, session, phase } = target;
  if (phase === "requesting") {
    return (
      <>
        <IconButton label={copy.cancel} onClick={layer.cancel} className="text-muted-foreground">
          <X />
        </IconButton>
        <Shimmer className="flex-1 pl-1">{copy.requestingTitle}…</Shimmer>
      </>
    );
  }
  if (phase === "processing") {
    return (
      <>
        <span className="flex size-[var(--button-height-sm)] shrink-0 items-center justify-center">
          <Mic className="size-4 text-muted-foreground" />
        </span>
        <Shimmer className="flex-1 pl-1">{settings.polish === "auto" ? copy.polishing : copy.transcribing}…</Shimmer>
      </>
    );
  }
  return (
    <>
      <IconButton label={copy.cancel} shortcut={<Kbd>Esc</Kbd>} onClick={layer.cancel} className="text-muted-foreground">
        <X />
      </IconButton>
      <span className="flex shrink-0 items-center gap-1.5 pl-1 pr-2">
        <RecDot />
        <Elapsed ms={session.elapsedMs} />
      </span>
      <Waveform levels={session.levels} bars={compact ? 36 : 64} className="min-w-0 flex-1 overflow-hidden text-foreground" />
      <IconButton
        label={copy.insert}
        shortcut={<ShortcutKeys keys={settings.shortcut} />}
        onClick={() => layer.stopAnd("insert")}
        className="ml-1 text-foreground"
      >
        <Check />
      </IconButton>
      <Button
        size="icon-sm"
        // eslint-disable-next-line no-restricted-syntax -- composer send button, the one round button
        className="shrink-0 rounded-full"
        aria-label={copy.send}
        onClick={() => layer.stopAnd("send")}
      >
        <ArrowUp />
      </Button>
    </>
  );
}

/**
 * C's floating pill, anchored above whichever composer receives the text.
 * Always dark ink, like a system indicator, so it reads over any page.
 */
export function VoiceHud({ target, className }: { target: VoiceTarget; className?: string }) {
  const { copy } = useStoryEnv();
  const { settings, activeModel } = useVoiceSettings();
  const { session, phase, mode, active } = target;
  if (!active || mode !== "hud" || phase === "error" || phase === "idle") return null;
  const text = session.finalText + session.interimText;
  const showText = phase === "listening" && text && (settings.engine === "cloud" || activeModel?.streaming);
  return (
    <div
      className={cn(
        "pointer-events-none absolute bottom-full left-1/2 z-30 mb-3 flex -translate-x-1/2 flex-col items-center gap-2",
        className,
      )}
    >
      {showText && (
        <div className="max-w-[340px] truncate rounded-md bg-multica-ink/80 px-2.5 py-1 text-caption text-multica-paper/80 backdrop-blur animate-in fade-in [direction:rtl]">
          <span className="[direction:ltr] [unicode-bidi:embed]">{text}</span>
        </div>
      )}
      <div className="flex h-10 items-center gap-2.5 whitespace-nowrap rounded-full bg-multica-ink pl-3.5 pr-4 text-multica-paper shadow-[var(--floating-shadow)] animate-in fade-in zoom-in-95 slide-in-from-bottom-2">
        {phase === "processing" ? (
          <>
            <Mic className="size-4 opacity-70" />
            <span className="voice-shimmer text-caption font-medium [--foreground:#fff] [--muted-foreground:rgb(255_255_255/0.5)]">
              {copy.transcribing}…
            </span>
          </>
        ) : phase === "requesting" ? (
          <>
            <Mic className="size-4 opacity-70" />
            <span className="text-caption opacity-80">{copy.requestingTitle}…</span>
          </>
        ) : (
          <>
            <RecDot className="bg-[#ff5a52]" />
            <Waveform levels={session.levels} bars={28} className="h-5 w-[86px]" />
            <span className="text-caption tabular-nums opacity-70">{formatElapsed(session.elapsedMs)}</span>
            <span className="text-caption opacity-70">{copy.releaseToInsert}</span>
          </>
        )}
      </div>
    </div>
  );
}

export function VoiceErrorRow({ target }: { target: VoiceTarget }) {
  const { copy } = useStoryEnv();
  const { layer, session, phase } = target;
  if (phase !== "error" || !session.error) return null;
  return <InlineVoiceError error={session.error} copy={copy} onRetry={layer.retry} onDismiss={layer.cancel} />;
}

/** After an insert, one click turns the spoken text into written text. */
export function PolishChip({ target }: { target: VoiceTarget }) {
  const { copy } = useStoryEnv();
  const { layer, offered } = target;
  if (!offered) return null;
  return (
    <Button
      size="xs"
      variant="brandSubtle"
      onClick={layer.polish}
      disabled={layer.polishing}
      className="animate-in fade-in slide-in-from-left-1"
    >
      <Sparkles />
      {layer.polishing ? <Shimmer className="text-caption">{copy.polishing}…</Shimmer> : copy.polish}
    </Button>
  );
}
