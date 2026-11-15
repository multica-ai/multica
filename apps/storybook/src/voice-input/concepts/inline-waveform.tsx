import { useState } from "react";
import { ArrowUp, Check, Mic, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Kbd, KbdGroup } from "@multica/ui/components/ui/kbd";
import { cn } from "@multica/ui/lib/utils";
import { CHAT_SCRIPT } from "../copy";
import { useStoryEnv } from "../env";
import { AddButton, AgentPicker, ChatWindowFrame, ComposerSurface, DraftEditor, SendButton, useDemoChat } from "../chat-frame";
import { Elapsed, InlineVoiceError, RecDot, Shimmer, Waveform } from "../primitives";
import { isToggleChord, useDictationCommit, useWindowKeys } from "../use-dictation";
import { useVoiceSession, type VoiceSession, type VoiceSessionOptions } from "../voice-session";

export const TOGGLE_KEYS = (
  <KbdGroup>
    <Kbd>⌘</Kbd>
    <Kbd>⇧</Kbd>
    <Kbd>Space</Kbd>
  </KbdGroup>
);

export function MicButton({ onClick, label }: { onClick?: () => void; label: string }) {
  return (
    <IconButton label={label} shortcut={TOGGLE_KEYS} onClick={onClick} className="text-muted-foreground">
      <Mic />
    </IconButton>
  );
}

/**
 * A. Inline waveform — the toolbar becomes a recording strip. Stop with ✓ to
 * review the text in the draft, or ↑ to send it as is.
 */
export function InlineWaveform({
  frozen,
  initialDraft = "",
  framed = true,
  simulate,
}: {
  frozen?: VoiceSession;
  initialDraft?: string;
  framed?: boolean;
  /** Walk through permission and failure paths with the simulated speaker. */
  simulate?: Pick<VoiceSessionOptions, "failWith" | "permission">;
}) {
  const { lang, source, copy } = useStoryEnv();
  const live = useVoiceSession({ source, lang, script: CHAT_SCRIPT[lang], processingMs: 1100, ...simulate });
  const session = frozen ?? live;
  const chat = useDemoChat();
  const [draft, setDraft] = useState(initialDraft);
  const { stopAnd, landed } = useDictationCommit(session, {
    lang,
    draft,
    setDraft,
    send: chat.send,
    enabled: !frozen,
  });

  const { phase } = session;
  const recording = phase === "listening";

  useWindowKeys(
    {
      down: (e) => {
        if (isToggleChord(e)) {
          e.preventDefault();
          if (recording) stopAnd("insert");
          else if (phase === "idle" || phase === "error") session.start();
        } else if (e.key === "Escape" && (recording || phase === "requesting")) {
          session.cancel();
        } else if (e.key === "Enter" && recording) {
          e.preventDefault();
          stopAnd("send");
        }
      },
    },
    !frozen,
  );

  let bottom = null;
  if (phase === "requesting") {
    bottom = (
      <>
        <IconButton label={copy.cancel} onClick={session.cancel} className="text-muted-foreground">
          <X />
        </IconButton>
        <Shimmer className="flex-1 pl-1">{copy.requestingTitle}…</Shimmer>
      </>
    );
  } else if (recording) {
    bottom = (
      <>
        <IconButton label={copy.cancel} shortcut={<Kbd>Esc</Kbd>} onClick={session.cancel} className="text-muted-foreground">
          <X />
        </IconButton>
        <span className="flex items-center gap-1.5 pl-1 pr-2">
          <RecDot />
          <Elapsed ms={session.elapsedMs} />
        </span>
        <Waveform levels={session.levels} bars={64} className="min-w-0 flex-1 overflow-hidden text-foreground" />
        <IconButton label={copy.done} shortcut={TOGGLE_KEYS} onClick={() => stopAnd("insert")} className="ml-1 text-foreground">
          <Check />
        </IconButton>
        <Button
          size="icon-sm"
          // eslint-disable-next-line no-restricted-syntax -- composer send button, the one round button
          className="rounded-full"
          aria-label={copy.send}
          onClick={() => stopAnd("send")}
        >
          <ArrowUp />
        </Button>
      </>
    );
  } else if (phase === "processing") {
    bottom = (
      <>
        <span className="flex size-[var(--button-height-sm)] items-center justify-center">
          <Mic className="size-4 text-muted-foreground" />
        </span>
        <Shimmer className="flex-1 pl-1">{copy.transcribing}…</Shimmer>
      </>
    );
  }

  const composer = (
    <ComposerSurface
      active={recording}
      above={
        phase === "error" && session.error ? (
          <InlineVoiceError error={session.error} copy={copy} onRetry={session.retry} onDismiss={session.cancel} />
        ) : undefined
      }
      bottom={bottom}
      left={
        <>
          <AddButton />
          <AgentPicker />
        </>
      }
      right={
        bottom ? undefined : <>
          <MicButton label={copy.dictate} onClick={session.start} />
          <SendButton
            disabled={!draft.trim()}
            onClick={() => {
              chat.send(draft);
              setDraft("");
            }}
          />
        </>
      }
    >
      <div key={landed} className={cn(landed > 0 && "voice-landed")}>
        <DraftEditor
          value={draft}
          onChange={setDraft}
          placeholder={copy.placeholder}
          onSubmit={() => {
            chat.send(draft);
            setDraft("");
          }}
        />
      </div>
    </ComposerSurface>
  );

  if (!framed) return <div className="w-[460px]">{composer}</div>;
  return <ChatWindowFrame messages={chat.messages}>{composer}</ChatWindowFrame>;
}
