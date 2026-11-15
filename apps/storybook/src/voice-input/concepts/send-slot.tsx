import { useRef, useState, type PointerEvent } from "react";
import { ArrowUp, ChevronLeft, ChevronUp, Loader2, Lock, Mic, Trash2 } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { cn } from "@multica/ui/lib/utils";
import { CHAT_SCRIPT } from "../copy";
import { useStoryEnv } from "../env";
import { AddButton, AgentPicker, ChatWindowFrame, ComposerSurface, DraftEditor, SendButton, useDemoChat } from "../chat-frame";
import { Elapsed, InlineVoiceError, RecDot, Shimmer, Waveform } from "../primitives";
import { useDictationCommit, useWindowKeys } from "../use-dictation";
import { useVoiceSession, type VoiceSession } from "../voice-session";

const CANCEL_DX = -110;
const LOCK_DY = -56;
const TAP_MS = 250;

/**
 * E. Hold the send button — the messenger pattern. With an empty draft the
 * send slot is a mic: hold to talk, release to send, slide left to cancel,
 * slide up to lock and keep talking hands-free.
 */
export function SendSlot({
  frozen,
  frozenDrag = { dx: 0, dy: 0 },
  frozenLocked = false,
  initialDraft = "",
}: {
  frozen?: VoiceSession;
  frozenDrag?: { dx: number; dy: number };
  frozenLocked?: boolean;
  initialDraft?: string;
}) {
  const { lang, source, copy } = useStoryEnv();
  const live = useVoiceSession({ source, lang, script: CHAT_SCRIPT[lang], polish: true, processingMs: 900 });
  const session = frozen ?? live;
  const chat = useDemoChat();
  const [draft, setDraft] = useState(initialDraft);
  const { stopAnd } = useDictationCommit(session, { lang, draft, setDraft, send: chat.send, enabled: !frozen });
  const [dragState, setDrag] = useState({ dx: 0, dy: 0 });
  const [lockedState, setLocked] = useState(false);
  const drag = frozen ? frozenDrag : dragState;
  const locked = frozen ? frozenLocked : lockedState;
  const pressRef = useRef<{ x: number; y: number; at: number } | null>(null);
  const { phase } = session;
  const recording = phase === "listening" || phase === "requesting";

  useWindowKeys(
    {
      down: (e) => {
        if (e.key === "Escape" && recording) {
          pressRef.current = null;
          setLocked(false);
          setDrag({ dx: 0, dy: 0 });
          session.cancel();
        }
      },
    },
    !frozen,
  );

  const onPointerDown = (e: PointerEvent<HTMLButtonElement>) => {
    if (phase !== "idle" && phase !== "error") return;
    e.currentTarget.setPointerCapture(e.pointerId);
    pressRef.current = { x: e.clientX, y: e.clientY, at: performance.now() };
    setLocked(false);
    session.start();
  };
  const onPointerMove = (e: PointerEvent<HTMLButtonElement>) => {
    const p = pressRef.current;
    if (!p || locked) return;
    const dx = Math.min(0, e.clientX - p.x);
    const dy = Math.min(0, e.clientY - p.y);
    if (dx < CANCEL_DX) {
      pressRef.current = null;
      setDrag({ dx: 0, dy: 0 });
      session.cancel();
      return;
    }
    if (dy < LOCK_DY) {
      pressRef.current = null;
      setDrag({ dx: 0, dy: 0 });
      setLocked(true);
      return;
    }
    // Commit to one axis, the way a thumb does.
    setDrag(Math.abs(dx) > Math.abs(dy) ? { dx, dy: 0 } : { dx: 0, dy });
  };
  const onPointerUp = () => {
    const p = pressRef.current;
    pressRef.current = null;
    setDrag({ dx: 0, dy: 0 });
    if (!p) return;
    if (performance.now() - p.at < TAP_MS) {
      setLocked(true);
      return;
    }
    stopAnd("send");
  };

  const send = () => {
    chat.send(draft);
    setDraft("");
  };

  const holding = recording && !locked;
  const cancelProgress = Math.min(1, drag.dx / CANCEL_DX);
  const lockProgress = Math.min(1, drag.dy / LOCK_DY);
  const level = session.level;

  let slot;
  if (draft.trim() && phase === "idle") {
    slot = <SendButton onClick={send} />;
  } else if (phase === "processing") {
    slot = (
      <span className="flex size-[var(--button-height-sm)] items-center justify-center rounded-full bg-primary text-primary-foreground">
        <Loader2 className="size-4 animate-spin" />
      </span>
    );
  } else if (recording && locked) {
    slot = (
      <button
        type="button"
        aria-label={copy.send}
        onClick={() => {
          setLocked(false);
          stopAnd("send");
        }}
        // eslint-disable-next-line no-restricted-syntax -- composer send button, the one round button
        className="flex size-[var(--button-height-sm)] cursor-pointer items-center justify-center rounded-full bg-primary text-primary-foreground transition-transform active:scale-95"
      >
        <ArrowUp className="size-4.5" />
      </button>
    );
  } else {
    slot = (
      <button
        type="button"
        aria-label={copy.holdToTalk}
        title={copy.holdToTalk}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        className={cn(
          // eslint-disable-next-line no-restricted-syntax -- composer send button, the one round button
          "relative flex size-[var(--button-height-sm)] touch-none cursor-pointer select-none items-center justify-center rounded-full bg-primary text-primary-foreground transition-transform duration-150",
          holding && "z-10 scale-[1.55] bg-brand text-brand-foreground",
        )}
        style={
          holding
            ? {
                translate: `${drag.dx}px ${drag.dy}px`,
                boxShadow: `0 0 0 ${2 + level * 9}px color-mix(in oklab, var(--brand) 22%, transparent)`,
              }
            : undefined
        }
      >
        <Mic className="size-4" />
      </button>
    );
  }

  let editorArea;
  let bottom;
  if (recording) {
    editorArea = (
      <div className="flex h-5 items-center gap-2 pr-10">
        <RecDot />
        <Elapsed ms={session.elapsedMs} className="text-foreground" />
        {locked ? (
          <span className="ml-auto flex items-center gap-1 text-caption text-muted-foreground">
            <Lock className="size-3" />
            {copy.locked}
          </span>
        ) : (
          <span
            className="ml-auto flex items-center gap-0.5 text-caption text-muted-foreground"
            style={{ translate: `${drag.dx * 0.6}px 0`, opacity: 1 - cancelProgress * 0.8 }}
          >
            <ChevronLeft className="size-3.5 animate-pulse" />
            {copy.slideToCancel}
          </span>
        )}
      </div>
    );
    bottom = (
      <>
        {locked ? (
          <IconButton
            label={copy.cancel}
            onClick={() => {
              setLocked(false);
              session.cancel();
            }}
            className="text-muted-foreground"
          >
            <Trash2 />
          </IconButton>
        ) : (
          <span className="w-1" />
        )}
        <Waveform
          levels={session.levels}
          bars={56}
          className={cn("min-w-0 flex-1 overflow-hidden", cancelProgress > 0.5 ? "text-destructive" : "text-foreground")}
        />
      </>
    );
  } else if (phase === "processing") {
    editorArea = <Shimmer className="text-body">{copy.transcribing}…</Shimmer>;
  } else {
    editorArea = <DraftEditor value={draft} onChange={setDraft} placeholder={copy.placeholder} onSubmit={send} />;
  }

  return (
    <ChatWindowFrame messages={chat.messages}>
      <div className="relative">
        {holding && (
          <div
            className="pointer-events-none absolute bottom-[62px] right-[22px] z-20 flex w-9 flex-col items-center gap-1 rounded-full bg-popover py-2 text-muted-foreground shadow-[var(--floating-shadow)] ring-1 ring-floating-border animate-in fade-in slide-in-from-bottom-2"
            style={{ translate: `0 ${drag.dy * 0.4}px` }}
          >
            <Lock className={cn("size-3.5 transition-colors", lockProgress > 0.6 && "text-brand")} />
            <ChevronUp className="size-3.5 animate-bounce" />
          </div>
        )}
        <ComposerSurface
          active={recording}
          above={
            phase === "error" && session.error ? (
              <InlineVoiceError error={session.error} copy={copy} onDismiss={session.cancel} />
            ) : undefined
          }
          bottom={recording ? bottom : undefined}
          left={
            <>
              <AddButton />
              <AgentPicker />
            </>
          }
          right={slot}
        >
          {editorArea}
        </ComposerSurface>
      </div>
    </ChatWindowFrame>
  );
}
