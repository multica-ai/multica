import { useEffect, useState } from "react";
import { Sparkles } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Kbd } from "@multica/ui/components/ui/kbd";
import { cn } from "@multica/ui/lib/utils";
import { CHAT_SCRIPT } from "../copy";
import { appendDictation, useStoryEnv } from "../env";
import { AddButton, AgentPicker, ChatWindowFrame, ComposerSurface, DraftEditor, SendButton, useDemoChat } from "../chat-frame";
import { Elapsed, InlineVoiceError, LevelBars, LiveTranscript, RecDot, Shimmer } from "../primitives";
import { isToggleChord, useDictationCommit, useWindowKeys } from "../use-dictation";
import { useVoiceSession, type VoiceSession } from "../voice-session";
import { MicButton, TOGGLE_KEYS } from "./inline-waveform";

/**
 * B. Live dictation — nothing changes shape. Words stream into the draft
 * where you are typing; uncertain words stay muted until the recognizer
 * commits them. A Polish chip offers an optional cleanup afterwards.
 */
export function LiveDictation({
  frozen,
  initialDraft = "",
  framed = true,
  showPolish,
}: {
  frozen?: VoiceSession;
  initialDraft?: string;
  framed?: boolean;
  /** Pin the post-dictation Polish chip, for the state gallery. */
  showPolish?: boolean;
}) {
  const { lang, source, copy } = useStoryEnv();
  const script = CHAT_SCRIPT[lang];
  const live = useVoiceSession({ source, lang, script, processingMs: 200 });
  const session = frozen ?? live;
  const chat = useDemoChat();
  const [draft, setDraft] = useState(initialDraft);
  const { stopAnd, lastInsert, clearLastInsert } = useDictationCommit(session, {
    lang,
    draft,
    setDraft,
    send: chat.send,
    enabled: !frozen,
  });
  const [polishing, setPolishing] = useState(false);

  // The chip is an offer, not a mode: it leaves once you type or after a while.
  useEffect(() => {
    if (!lastInsert) return;
    const id = window.setTimeout(clearLastInsert, 8000);
    return () => window.clearTimeout(id);
  }, [lastInsert, clearLastInsert]);

  const { phase } = session;
  const capturing = phase === "listening" || phase === "requesting";

  useWindowKeys(
    {
      down: (e) => {
        if (isToggleChord(e)) {
          e.preventDefault();
          if (phase === "listening") stopAnd("insert");
          else if (phase === "idle" || phase === "error") session.start();
        } else if (e.key === "Escape" && capturing) {
          stopAnd("insert");
        } else if (e.key === "Enter" && phase === "listening") {
          e.preventDefault();
          stopAnd("send");
        }
      },
    },
    !frozen,
  );

  const polish = () => {
    if (!lastInsert) return;
    setPolishing(true);
    window.setTimeout(() => {
      const raw = lastInsert.raw.trim();
      const polished = raw === script.phrases.join("").trim() ? script.polished : raw.replace(/^(um,?\s*|嗯，?)/i, "");
      setDraft(appendDictation(lastInsert.before, polished, lang));
      setPolishing(false);
      clearLastInsert();
    }, 900);
  };

  const showChip = showPolish || (!!lastInsert && phase === "idle");

  const send = () => {
    chat.send(draft);
    setDraft("");
    clearLastInsert();
  };

  const composer = (
    <ComposerSurface
      active={capturing}
      above={
        phase === "error" && session.error ? (
          <InlineVoiceError error={session.error} copy={copy} onRetry={session.retry} onDismiss={session.cancel} />
        ) : undefined
      }
      left={
        capturing ? (
          <span className="flex h-7 items-center gap-1.5 pl-1.5 text-caption">
            <RecDot />
            {phase === "requesting" ? (
              <Shimmer>{copy.requestingTitle}…</Shimmer>
            ) : (
              <>
                <span className="font-medium text-foreground">{copy.listening}</span>
                <Elapsed ms={session.elapsedMs} />
                <span className="pl-1 text-faint-foreground">·</span>
                <Kbd>Esc</Kbd>
                <span className="text-muted-foreground">{copy.done}</span>
              </>
            )}
          </span>
        ) : (
          <>
            <AddButton />
            <AgentPicker />
            {showChip && (
              <Button
                size="xs"
                variant="brandSubtle"
                className="ml-1 animate-in fade-in slide-in-from-left-1"
                onClick={polish}
                disabled={polishing}
              >
                <Sparkles />
                {polishing ? <Shimmer className="text-caption">{copy.polishing}…</Shimmer> : copy.polish}
              </Button>
            )}
          </>
        )
      }
      right={
        <>
          {capturing ? (
            <IconButton
              label={copy.stop}
              shortcut={TOGGLE_KEYS}
              onClick={() => (phase === "requesting" ? session.cancel() : stopAnd("insert"))}
              className="bg-brand/10 text-brand hover:bg-brand/15 hover:text-brand"
            >
              <LevelBars levels={session.levels} />
            </IconButton>
          ) : (
            <MicButton label={copy.dictate} onClick={session.start} />
          )}
          <SendButton disabled={!draft.trim() && !capturing} onClick={() => (capturing ? stopAnd("send") : send())} />
        </>
      }
    >
      {capturing || phase === "processing" ? (
        <div className="min-h-5 text-body">
          {draft}
          {draft && session.finalText + session.interimText && lang === "en" ? " " : ""}
          {phase === "requesting" && !draft ? (
            <span className="text-muted-foreground">{copy.speakNow}</span>
          ) : (
            <LiveTranscript finalText={session.finalText} interimText={session.interimText} />
          )}
        </div>
      ) : (
        <div className={cn(polishing && "opacity-60 transition-opacity")}>
          <DraftEditor value={draft} onChange={(v) => { setDraft(v); clearLastInsert(); }} placeholder={copy.placeholder} onSubmit={send} />
        </div>
      )}
    </ComposerSurface>
  );

  if (!framed) return <div className="w-[460px]">{composer}</div>;
  return <ChatWindowFrame messages={chat.messages}>{composer}</ChatWindowFrame>;
}
