import { useEffect, useState } from "react";
import { ArrowUp, Check, Mic, Pause, Play, Sparkles, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Switch } from "@multica/ui/components/ui/switch";
import { Tabs, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { Kbd } from "@multica/ui/components/ui/kbd";
import { cn } from "@multica/ui/lib/utils";
import { CHAT_SCRIPT } from "../copy";
import { appendDictation, useStoryEnv } from "../env";
import { AddButton, AgentPicker, ChatWindowFrame, ComposerSurface, DraftEditor, SendButton, useDemoChat } from "../chat-frame";
import { Elapsed, LiveTranscript, RecDot, Shimmer, errorMessage } from "../primitives";
import { isToggleChord, useWindowKeys } from "../use-dictation";
import { useVoiceSession, type VoiceSession } from "../voice-session";
import { MicButton } from "./inline-waveform";

export interface FocusReview {
  raw: string;
  polished: string;
}

/** Concentric rings driven by the level history: the newest sample in the core, older ones outward. */
function Orb({ levels, phase }: { levels: number[]; phase: VoiceSession["phase"] }) {
  const n = levels.length;
  const live = phase === "listening";
  const core = live ? (levels[n - 1] ?? 0) : 0;
  const mid = live ? (levels[n - 4] ?? 0) : 0;
  const outer = live ? (levels[n - 8] ?? 0) : 0;
  const shrink = phase === "processing";
  return (
    <div className={cn("relative flex size-40 items-center justify-center transition-transform duration-500", shrink && "scale-50")}>
      <span
        className="absolute size-20 rounded-full bg-brand/8 transition-transform duration-150 ease-out"
        style={{ transform: `scale(${1 + outer * 0.95})` }}
      />
      <span
        className="absolute size-20 rounded-full bg-brand/14 transition-transform duration-100 ease-out"
        style={{ transform: `scale(${1 + mid * 0.6})` }}
      />
      <span
        className={cn(
          "absolute flex size-20 items-center justify-center rounded-full text-brand-foreground transition-[transform,background-color] duration-75",
          phase === "paused" || phase === "error" ? "bg-muted-foreground/40" : "bg-brand",
        )}
        style={{ transform: `scale(${1 + core * 0.18})` }}
      >
        {phase === "paused" ? <Pause className="size-6" /> : <Mic className="size-7" />}
      </span>
    </div>
  );
}

function FocusPanel({
  session,
  review,
  polishOn,
  onPolishChange,
  onClose,
  onDone,
  onInsert,
  onSend,
}: {
  session: VoiceSession;
  review: FocusReview | null;
  polishOn: boolean;
  onPolishChange: (on: boolean) => void;
  onClose: () => void;
  onDone: () => void;
  onInsert: (text: string) => void;
  onSend: (text: string) => void;
}) {
  const { copy } = useStoryEnv();
  const { phase } = session;
  const [view, setView] = useState<"polished" | "raw">(polishOn ? "polished" : "raw");
  const [edited, setEdited] = useState<string | null>(null);
  const reviewText = edited ?? (review ? (view === "polished" ? review.polished : review.raw) : "");

  const status =
    review ? null : phase === "requesting" ? (
      <Shimmer>{copy.requestingTitle}…</Shimmer>
    ) : phase === "processing" ? (
      <Shimmer>{polishOn ? copy.polishing : copy.transcribing}…</Shimmer>
    ) : phase === "error" ? (
      <span className="text-caption font-medium text-destructive">{copy.stopped}</span>
    ) : phase === "paused" ? (
      <span className="text-caption font-medium text-muted-foreground">{copy.paused}</span>
    ) : (
      <span className="flex items-center gap-1.5 text-caption font-medium">
        <RecDot />
        {copy.listening}
      </span>
    );

  return (
    <div className="absolute inset-0 z-30 flex flex-col bg-surface-raised animate-in fade-in zoom-in-[0.98] duration-200">
      <div className="flex items-center justify-between gap-2 border-b px-4 py-2.5">
        <div className="flex h-7 items-center gap-2">
          {review ? <span className="text-body font-medium">{copy.dictate}</span> : status}
          {!review && phase !== "requesting" && phase !== "error" && <Elapsed ms={session.elapsedMs} />}
        </div>
        <IconButton label={copy.cancel} shortcut={<Kbd>Esc</Kbd>} onClick={onClose} className="text-muted-foreground">
          <X />
        </IconButton>
      </div>

      {review ? (
        <div className="flex min-h-0 flex-1 flex-col gap-3 p-4">
          {review.raw !== review.polished && (
            <Tabs value={view} onValueChange={(v) => { setView(v as "polished" | "raw"); setEdited(null); }}>
              <TabsList>
                <TabsTrigger value="polished">
                  <Sparkles />
                  {copy.polish}
                </TabsTrigger>
                <TabsTrigger value="raw">{copy.original}</TabsTrigger>
              </TabsList>
            </Tabs>
          )}
          <textarea
            value={reviewText}
            onChange={(e) => setEdited(e.target.value)}
            className="min-h-0 flex-1 resize-none rounded-lg border border-surface-border bg-surface p-3 text-body-lg leading-relaxed outline-none focus:border-brand focus:ring-2 focus:ring-ring/20"
          />
          <div className="flex items-center justify-end gap-2">
            <Button variant="outline" onClick={() => onInsert(reviewText)}>
              {copy.insert}
            </Button>
            <Button onClick={() => onSend(reviewText)}>
              <ArrowUp />
              {copy.send}
            </Button>
          </div>
        </div>
      ) : (
        <>
          <div className="flex flex-col items-center pt-6">
            <Orb levels={session.levels} phase={phase} />
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-4">
            {phase === "error" && session.error ? (
              <p className="text-center text-body text-destructive">{errorMessage(session.error, copy)}</p>
            ) : session.finalText || session.interimText ? (
              <p className="text-title-sm leading-relaxed">
                <LiveTranscript
                  finalText={session.finalText}
                  interimText={session.interimText}
                  caret={phase === "listening"}
                />
              </p>
            ) : (
              <p className="text-center text-title-sm text-faint-foreground">{copy.speakNow}</p>
            )}
          </div>
          <div className="flex items-center justify-between gap-2 border-t px-4 py-3">
            <label className="flex cursor-pointer items-center gap-2 text-caption text-muted-foreground">
              <Switch size="sm" checked={polishOn} onCheckedChange={onPolishChange} />
              <Sparkles className="size-3.5" />
              {copy.polish}
            </label>
            <div className="flex items-center gap-2">
              {phase === "error" ? (
                <Button variant="outline" onClick={session.start}>
                  {copy.retry}
                </Button>
              ) : (
                <IconButton
                  variant="outline"
                  size="icon"
                  label={phase === "paused" ? copy.resume : copy.pause}
                  disabled={phase !== "listening" && phase !== "paused"}
                  onClick={phase === "paused" ? session.resume : session.pause}
                >
                  {phase === "paused" ? <Play /> : <Pause />}
                </IconButton>
              )}
              <Button variant="brand" onClick={onDone} disabled={phase !== "listening" && phase !== "paused"}>
                <Check />
                {copy.done}
              </Button>
            </div>
          </div>
        </>
      )}
    </div>
  );
}

/**
 * D. Focus mode — for thinking out loud. The mic opens a full-panel listening
 * space: a level-reactive orb, the transcript in large type, pause, and a
 * review step that offers the polished and the original text side by side.
 */
export function FocusMode({
  frozen,
  frozenReview = null,
  initialOpen = false,
}: {
  frozen?: VoiceSession;
  frozenReview?: FocusReview | null;
  initialOpen?: boolean;
}) {
  const { lang, source, copy } = useStoryEnv();
  const [polishOn, setPolishOn] = useState(true);
  const live = useVoiceSession({ source, lang, script: CHAT_SCRIPT[lang], polish: true, processingMs: 1300 });
  const session = frozen ?? live;
  const chat = useDemoChat();
  const [draft, setDraft] = useState("");
  const [open, setOpen] = useState(initialOpen);
  const [liveReview, setReview] = useState<FocusReview | null>(null);
  const review = frozen ? frozenReview : liveReview;

  useEffect(() => {
    if (frozen || session.phase !== "done") return;
    setReview({ raw: session.finalText, polished: polishOn ? session.result : session.finalText });
    session.reset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session.phase]);

  const openPanel = () => {
    setOpen(true);
    setReview(null);
    session.start();
  };
  const close = () => {
    session.cancel();
    setReview(null);
    setOpen(false);
  };

  useWindowKeys(
    {
      down: (e) => {
        if (isToggleChord(e)) {
          e.preventDefault();
          if (!open) openPanel();
          else if (session.phase === "listening" || session.phase === "paused") session.stop();
        } else if (e.key === "Escape" && open) {
          close();
        } else if (e.code === "Space" && open && !review && !e.metaKey && !e.ctrlKey) {
          e.preventDefault();
          if (session.phase === "listening") session.pause();
          else if (session.phase === "paused") session.resume();
        }
      },
    },
    !frozen,
  );

  const send = (text: string, viaVoice = false) => {
    chat.send(text, viaVoice);
    setDraft("");
  };

  return (
    <ChatWindowFrame
      messages={chat.messages}
      overlay={
        open ? (
          <FocusPanel
            session={session}
            review={review}
            polishOn={polishOn}
            onPolishChange={setPolishOn}
            onClose={close}
            onDone={session.stop}
            onInsert={(text) => {
              setDraft(appendDictation(draft, text, lang));
              setReview(null);
              setOpen(false);
            }}
            onSend={(text) => {
              send(appendDictation(draft, text, lang), true);
              setReview(null);
              setOpen(false);
            }}
          />
        ) : null
      }
    >
      <ComposerSurface
        left={
          <>
            <AddButton />
            <AgentPicker />
          </>
        }
        right={
          <>
            <MicButton label={copy.dictate} onClick={openPanel} />
            <SendButton disabled={!draft.trim()} onClick={() => send(draft)} />
          </>
        }
      >
        <DraftEditor value={draft} onChange={setDraft} placeholder={copy.placeholder} onSubmit={() => send(draft)} />
      </ComposerSurface>
    </ChatWindowFrame>
  );
}
