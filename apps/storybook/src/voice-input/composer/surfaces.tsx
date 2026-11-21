import { useRef, useState, type ReactNode } from "react";
import { Bot, ChevronRight, Mic, Paperclip } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { cn } from "@multica/ui/lib/utils";
import { CHAT_SCRIPT, COMMENT_SCRIPT, REPLY_SCRIPT, type VoiceScript } from "../copy";
import { useStoryEnv } from "../env";
import { AddButton, AgentPicker, ComposerSurface, DraftEditor, SendButton } from "../chat-frame";
import { useVoiceTarget, type VoiceTarget } from "./voice-provider";
import { PolishChip, VoiceErrorRow, VoiceHud, VoiceMicButton, VoiceStrip } from "./voice-ui";

export type SendHandler = (text: string, viaVoice: boolean) => void;

/** Draft state plus the voice target, the part every composer shares. */
function useVoiceComposer(id: string, script: VoiceScript, onSend: SendHandler, initialDraft = "") {
  const [draft, setDraft] = useState(initialDraft);
  const draftRef = useRef(draft);
  draftRef.current = draft;
  const target = useVoiceTarget({
    id,
    script,
    getDraft: () => draftRef.current,
    setDraft,
    send: (text) => onSend(text, true),
  });
  const recording = target.phase === "listening" || target.phase === "requesting";
  const strip = target.active && target.mode === "strip" && (recording || target.phase === "processing");
  const submit = () => {
    if (!draft.trim()) return;
    onSend(draft, false);
    setDraft("");
  };
  return { draft, setDraft, target, recording, strip, submit };
}

function Landed({ target, children }: { target: VoiceTarget; children: ReactNode }) {
  return (
    <div key={target.landed} className={cn(target.landed > 0 && "voice-landed")}>
      {children}
    </div>
  );
}

/** The floating chat window's composer (packages/views/chat/components/chat-input.tsx). */
export function ChatComposer({ id = "chat", onSend, initialDraft }: { id?: string; onSend: SendHandler; initialDraft?: string }) {
  const { lang, copy } = useStoryEnv();
  const c = useVoiceComposer(id, CHAT_SCRIPT[lang], onSend, initialDraft);
  return (
    <div className="relative" onFocusCapture={c.target.onFocus}>
      <VoiceHud target={c.target} />
      <ComposerSurface
        active={c.target.active && c.recording}
        above={c.target.phase === "error" ? <VoiceErrorRow target={c.target} /> : undefined}
        bottom={c.strip ? <VoiceStrip target={c.target} /> : undefined}
        left={
          <>
            <AddButton />
            <AgentPicker />
            <PolishChip target={c.target} />
          </>
        }
        right={
          c.strip ? undefined : (
            <>
              <VoiceMicButton target={c.target} />
              <SendButton disabled={!c.draft.trim()} onClick={c.submit} />
            </>
          )
        }
      >
        <Landed target={c.target}>
          <DraftEditor value={c.draft} onChange={c.setDraft} placeholder={copy.placeholder} onSubmit={c.submit} />
        </Landed>
      </ComposerSurface>
    </div>
  );
}

function TriggerChip() {
  const { copy } = useStoryEnv();
  return (
    <span className="inline-flex h-6 min-w-0 items-center gap-1.5 rounded-md px-1.5 text-micro font-medium text-muted-foreground">
      <span className="flex size-4 items-center justify-center rounded-full bg-brand/12 text-brand">
        <Bot className="size-2.5" />
      </span>
      <span className="truncate">
        {copy.agentName} · {copy.willStart}
      </span>
    </span>
  );
}

/** The issue's comment box (packages/views/issues/components/comment-input.tsx). */
export function CommentComposer({
  id = "comment",
  onSend,
  initialDraft,
  trigger = true,
}: {
  id?: string;
  onSend: SendHandler;
  initialDraft?: string;
  /** The "agent starts after sending" chip; off outside an issue. */
  trigger?: boolean;
}) {
  const { lang, copy } = useStoryEnv();
  const c = useVoiceComposer(id, COMMENT_SCRIPT[lang], onSend, initialDraft);
  const ring = c.target.active && c.recording;
  return (
    <div className="relative" onFocusCapture={c.target.onFocus}>
      <VoiceHud target={c.target} />
      <div
        data-mc="comment-composer"
        className={cn(
          "relative flex flex-col rounded-lg bg-card pb-8 ring-1 ring-border transition-shadow",
          ring && "ring-2 ring-brand/70",
        )}
      >
        <div className="px-3 py-2">
          <Landed target={c.target}>
            <DraftEditor value={c.draft} onChange={c.setDraft} placeholder={copy.commentPlaceholder} onSubmit={c.submit} />
          </Landed>
        </div>
        <div className="px-2 pb-1 empty:hidden">
          <VoiceErrorRow target={c.target} />
        </div>
        {c.strip ? (
          <div className="absolute bottom-1 left-1.5 right-1.5 flex h-8 items-center gap-1">
            <VoiceStrip target={c.target} />
          </div>
        ) : (
          <>
            <div className="absolute bottom-1 left-2 right-36 flex min-w-0 items-center gap-1.5">
              {trigger && <TriggerChip />}
              <PolishChip target={c.target} />
            </div>
            <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
              <IconButton label={copy.attach} className="text-muted-foreground">
                <Paperclip />
              </IconButton>
              <VoiceMicButton target={c.target} />
              <SendButton disabled={!c.draft.trim()} onClick={c.submit} />
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function Avatar({ kind, className }: { kind: "agent" | "human"; className?: string }) {
  return kind === "agent" ? (
    <span className={cn("flex size-6 shrink-0 items-center justify-center rounded-full bg-brand/12 text-brand", className)}>
      <Bot className="size-3.5" />
    </span>
  ) : (
    <span
      className={cn(
        "flex size-6 shrink-0 items-center justify-center rounded-full bg-multica-ink text-micro font-semibold text-multica-paper",
        className,
      )}
    >
      J
    </span>
  );
}

/** A reply inside a comment thread (packages/views/issues/components/reply-input.tsx). */
export function ReplyComposer({ id = "reply", onSend }: { id?: string; onSend: SendHandler }) {
  const { lang, copy } = useStoryEnv();
  const c = useVoiceComposer(id, REPLY_SCRIPT[lang], onSend);
  const ring = c.target.active && c.recording;
  const tall = !!c.draft || c.strip || c.target.offered || c.target.phase === "error";
  return (
    <div className="flex items-start gap-2.5" onFocusCapture={c.target.onFocus}>
      <Avatar kind="human" className="mt-0.5 size-5" />
      <div className={cn("relative flex min-w-0 flex-1 flex-col rounded-md transition-shadow", tall && "pb-9", ring && "ring-2 ring-brand/70 ring-offset-4 ring-offset-card")}>
        <VoiceHud target={c.target} />
        <Landed target={c.target}>
          <DraftEditor value={c.draft} onChange={c.setDraft} placeholder={copy.replyPlaceholder} onSubmit={c.submit} className="pr-20" />
        </Landed>
        <div className="pt-1 empty:hidden">
          <VoiceErrorRow target={c.target} />
        </div>
        {c.strip ? (
          <div className="absolute bottom-0 left-0 right-0 flex h-8 items-center gap-1">
            <VoiceStrip target={c.target} compact />
          </div>
        ) : (
          <>
            <div className="absolute bottom-0 left-0 flex items-center">
              <PolishChip target={c.target} />
            </div>
            <div className="absolute bottom-0 right-0 flex items-center gap-1">
              <VoiceMicButton target={c.target} />
              <SendButton disabled={!c.draft.trim()} onClick={c.submit} />
            </div>
          </>
        )}
      </div>
    </div>
  );
}

export interface PostedComment {
  id: number;
  text: string;
  viaVoice?: boolean;
}

function VoiceMeta() {
  const { copy } = useStoryEnv();
  return (
    <span className="inline-flex items-center gap-1 text-micro text-faint-foreground">
      <Mic className="size-3" />
      {copy.dictate}
    </span>
  );
}

function CommentRow({ who, name, time, text, viaVoice }: { who: "agent" | "human"; name: string; time: string; text: string; viaVoice?: boolean }) {
  return (
    <div className="flex gap-2.5 px-4 py-3">
      <Avatar kind={who} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 text-caption">
          <span className="font-medium text-foreground">{name}</span>
          <span className="text-faint-foreground">{time}</span>
          {viaVoice && <VoiceMeta />}
        </div>
        <p className="mt-1 whitespace-pre-wrap text-body leading-relaxed">{text}</p>
      </div>
    </div>
  );
}

/**
 * An issue page cut down to the activity column: a thread with a reply box and
 * the comment box under it — two more composers on the same voice layer.
 */
export function IssuePage({ width = 720, height = 760, className }: { width?: number; height?: number; className?: string }) {
  const { copy } = useStoryEnv();
  const [replies, setReplies] = useState<PostedComment[]>([]);
  const [comments, setComments] = useState<PostedComment[]>([]);
  const post = (set: typeof setComments): SendHandler => (text, viaVoice) => {
    if (text.trim()) set((list) => [...list, { id: Date.now(), text: text.trim(), viaVoice }]);
  };
  return (
    <div
      className={cn("flex flex-col overflow-hidden rounded-xl bg-background ring-1 ring-border", className)}
      style={{ width, height }}
    >
      <div className="flex items-center gap-1.5 border-b px-6 py-2.5 text-caption text-muted-foreground">
        <span>Multica</span>
        <ChevronRight className="size-3" />
        <span className="font-medium text-foreground">{copy.issueKey}</span>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-8 pb-6 pt-6">
        <h1 className="text-title-lg font-semibold tracking-tight">{copy.sessionTitle}</h1>
        <p className="mt-3 text-body leading-relaxed text-muted-foreground">{copy.issueBody}</p>
        <h2 className="mt-8 mb-3 text-body font-semibold">{copy.activity}</h2>
        <div className="space-y-4">
          <div className="overflow-hidden rounded-xl border bg-card">
            <CommentRow who="human" name={copy.humanName} time={copy.minutesAgo} text={copy.humanComment} />
            <div className="border-t border-border/50">
              <CommentRow who="agent" name={copy.agentName} time={copy.minutesAgo} text={copy.agentReply} />
            </div>
            {replies.map((r) => (
              <div key={r.id} className="border-t border-border/50">
                <CommentRow who="human" name={copy.humanName} time={copy.justNow} text={r.text} viaVoice={r.viaVoice} />
              </div>
            ))}
            <div className="border-t border-border/50 px-4 py-2.5">
              <ReplyComposer onSend={post(setReplies)} />
            </div>
          </div>
          {comments.map((c) => (
            <div key={c.id} className="rounded-xl border bg-card">
              <CommentRow who="human" name={copy.humanName} time={copy.justNow} text={c.text} viaVoice={c.viaVoice} />
            </div>
          ))}
          <CommentComposer onSend={post(setComments)} />
        </div>
      </div>
    </div>
  );
}
