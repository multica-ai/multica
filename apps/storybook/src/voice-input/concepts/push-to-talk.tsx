import { useRef, useState } from "react";
import { Lock, Mic, Square } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Kbd, KbdGroup } from "@multica/ui/components/ui/kbd";
import { cn } from "@multica/ui/lib/utils";
import { CHAT_SCRIPT } from "../copy";
import { useStoryEnv } from "../env";
import { AddButton, AgentPicker, ChatWindowFrame, ComposerSurface, DraftEditor, SendButton, useDemoChat } from "../chat-frame";
import { InlineVoiceError, RecDot, Waveform, formatElapsed } from "../primitives";
import { useDictationCommit, useWindowKeys } from "../use-dictation";
import { useVoiceSession, type VoiceSession } from "../voice-session";

const HOLD_KEYS = (
  <KbdGroup>
    <Kbd>⌥</Kbd>
    <Kbd>Space</Kbd>
  </KbdGroup>
);

/** A short press is a tap (lock hands-free); a longer one is push-to-talk. */
const TAP_MS = 250;

/**
 * The HUD: always-dark ink, like a system indicator, so it reads the same over
 * any page and in either theme.
 */
export function TalkHud({
  session,
  locked,
  onStop,
  hint,
}: {
  session: VoiceSession;
  locked: boolean;
  onStop: () => void;
  hint: string;
}) {
  const { phase } = session;
  const tail = (session.finalText + session.interimText).slice(-46);
  return (
    <div className="pointer-events-none absolute inset-x-0 bottom-[132px] z-20 flex flex-col items-center gap-2">
      {tail && phase === "listening" && (
        <div className="max-w-[80%] truncate rounded-md bg-multica-ink/80 px-2.5 py-1 text-caption text-multica-paper/80 backdrop-blur animate-in fade-in">
          {(session.finalText + session.interimText).length > 46 ? "…" : ""}
          {tail}
        </div>
      )}
      <div
        className={cn(
          "pointer-events-auto flex h-10 items-center gap-2.5 rounded-full bg-multica-ink pl-3.5 text-multica-paper shadow-[var(--floating-shadow)] transition-[width] animate-in fade-in zoom-in-95 slide-in-from-bottom-2",
          locked && phase === "listening" ? "pr-1.5" : "pr-4",
        )}
      >
        {phase === "processing" ? (
          <>
            <Mic className="size-4 opacity-70" />
            <span className="voice-shimmer text-caption font-medium [--foreground:#fff] [--muted-foreground:rgb(255_255_255/0.5)]">
              {hint}
            </span>
          </>
        ) : phase === "requesting" ? (
          <>
            <Mic className="size-4 opacity-70" />
            <span className="text-caption opacity-80">{hint}</span>
          </>
        ) : (
          <>
            <RecDot className="bg-[#ff5a52]" />
            <Waveform levels={session.levels} bars={28} className="h-5 w-[86px]" />
            <span className="text-caption tabular-nums opacity-70">{formatElapsed(session.elapsedMs)}</span>
            {locked ? (
              <button
                type="button"
                aria-label="Stop"
                onClick={onStop}
                // eslint-disable-next-line no-restricted-syntax -- the HUD's stop control mirrors the composer send/stop button
                className="flex size-7 cursor-pointer items-center justify-center rounded-full bg-multica-paper text-multica-ink transition-transform active:scale-95"
              >
                <Square className="size-3 fill-current" />
              </button>
            ) : (
              <span className="text-caption opacity-70">{hint}</span>
            )}
          </>
        )}
      </div>
    </div>
  );
}

/**
 * C. Push-to-talk — hold ⌥Space (or press and hold the mic) and talk; let go
 * to drop the text into the draft. Tap instead of hold to lock it hands-free.
 */
export function PushToTalk({
  frozen,
  frozenLocked = false,
  initialDraft = "",
}: {
  frozen?: VoiceSession;
  frozenLocked?: boolean;
  initialDraft?: string;
}) {
  const { lang, source, copy } = useStoryEnv();
  const live = useVoiceSession({ source, lang, script: CHAT_SCRIPT[lang], processingMs: 700 });
  const session = frozen ?? live;
  const chat = useDemoChat();
  const [draft, setDraft] = useState(initialDraft);
  const { stopAnd, landed } = useDictationCommit(session, { lang, draft, setDraft, send: chat.send, enabled: !frozen });
  const [lockedState, setLocked] = useState(false);
  const locked = frozen ? frozenLocked : lockedState;
  const pressRef = useRef<{ at: number; via: "key" | "pointer" } | null>(null);
  const { phase } = session;

  const press = (via: "key" | "pointer") => {
    if (phase === "listening" && locked) {
      setLocked(false);
      stopAnd("insert");
      return;
    }
    if (phase !== "idle" && phase !== "error") return;
    pressRef.current = { at: performance.now(), via };
    setLocked(false);
    session.start();
  };

  const release = () => {
    const p = pressRef.current;
    pressRef.current = null;
    if (!p) return;
    if (performance.now() - p.at < TAP_MS) {
      setLocked(true);
      return;
    }
    stopAnd("insert");
  };

  useWindowKeys(
    {
      down: (e) => {
        if (e.code === "Space" && e.altKey && !e.repeat) {
          e.preventDefault();
          press("key");
        } else if (e.key === "Escape" && (phase === "listening" || phase === "requesting")) {
          pressRef.current = null;
          setLocked(false);
          session.cancel();
        }
      },
      up: (e) => {
        if (pressRef.current?.via === "key" && (e.code === "Space" || e.key === "Alt")) release();
      },
    },
    !frozen,
  );

  const hudVisible = phase === "listening" || phase === "processing" || phase === "requesting";
  const hudHint =
    phase === "processing" ? `${copy.transcribing}…` : phase === "requesting" ? `${copy.requestingTitle}…` : copy.releaseToInsert;

  const send = () => {
    chat.send(draft);
    setDraft("");
  };

  return (
    <ChatWindowFrame
      messages={chat.messages}
      overlay={
        hudVisible ? (
          <TalkHud
            session={session}
            locked={locked}
            hint={hudHint}
            onStop={() => {
              setLocked(false);
              stopAnd("insert");
            }}
          />
        ) : null
      }
    >
      <ComposerSurface
        above={
          phase === "error" && session.error ? (
            <InlineVoiceError error={session.error} copy={copy} onRetry={() => press("pointer")} onDismiss={session.cancel} />
          ) : undefined
        }
        left={
          <>
            <AddButton />
            <AgentPicker />
          </>
        }
        right={
          <>
            <IconButton
              label={phase === "listening" && locked ? copy.stop : copy.holdToTalk}
              shortcut={HOLD_KEYS}
              onPointerDown={(e) => {
                e.currentTarget.setPointerCapture(e.pointerId);
                press("pointer");
              }}
              onPointerUp={() => pressRef.current?.via === "pointer" && release()}
              className={cn(
                "touch-none text-muted-foreground",
                phase === "listening" && "bg-brand/10 text-brand hover:bg-brand/15 hover:text-brand",
              )}
            >
              {phase === "listening" && locked ? <Lock /> : <Mic />}
            </IconButton>
            <SendButton disabled={!draft.trim()} onClick={send} />
          </>
        }
      >
        <div key={landed} className={cn(landed > 0 && "voice-landed")}>
          <DraftEditor value={draft} onChange={setDraft} placeholder={copy.placeholder} onSubmit={send} />
        </div>
      </ComposerSurface>
      {phase === "idle" && !draft && (
        <div className="pointer-events-none absolute inset-x-0 bottom-[118px] flex justify-center">
          <span className="flex items-center gap-1.5 text-caption text-faint-foreground">
            {copy.holdToTalk}
            {HOLD_KEYS}
          </span>
        </div>
      )}
    </ChatWindowFrame>
  );
}
