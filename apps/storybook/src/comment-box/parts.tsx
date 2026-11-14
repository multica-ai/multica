import { useState, type CSSProperties, type FocusEvent, type ReactNode } from "react";
import { ArrowUp, Check, ChevronDown, Mic, Paperclip, X } from "@multica/ui/icons";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { Button } from "@multica/ui/components/ui/button";
import { Kbd } from "@multica/ui/components/ui/kbd";
import { cn } from "@multica/ui/lib/utils";
import type { Lang } from "../voice-input/copy";
import { useStoryEnv } from "../voice-input/env";
import { RecDot, formatElapsed } from "../voice-input/primitives";
import { LevelStreamProvider, useLevelSamples } from "../voice-input/waveform/stream";
import { VARIANT_BY_ID } from "../voice-input/waveform/variants";

export type Kind = "comment" | "reply";

/** A frozen starting point for a box, so the state grid can show every case at once. */
export interface Preset {
  text?: string;
  focused?: boolean;
  restart?: boolean;
  recording?: boolean;
}

type Copy = Record<Lang, string>;

export const TEXT = {
  placeholder: {
    comment: { zh: "留下评论...", en: "Leave a comment..." },
    reply: { zh: "回复...", en: "Leave a reply..." },
  },
  sample: {
    zh: "@Lambda 帮我看一下这个 PR 的 CI 为什么挂了",
    en: "@Lambda can you check why CI failed on this PR",
  },
  dictated: { zh: "，顺便看一下 daemon 的单元测试。", en: ", and look at the daemon unit tests too." },
  willStart: { zh: "发送后开始", en: "Will start when sent" },
  restartChip: { zh: "停止并重新开始", en: "Stop and start over" },
  restartSend: { zh: "停止并发送", en: "Stop and send" },
  send: { zh: "发送", en: "Send" },
  comment: { zh: "评论", en: "Comment" },
  reply: { zh: "回复", en: "Reply" },
  mic: { zh: "语音输入", en: "Dictate" },
  attach: { zh: "添加附件", en: "Attach files" },
  add: { zh: "添加", en: "Add" },
  uploadFile: { zh: "上传文件", en: "Upload a file" },
  mention: { zh: "提及成员或智能体", en: "Mention someone" },
  command: { zh: "快捷指令", en: "Quick actions" },
  cancel: { zh: "取消", en: "Cancel" },
  insert: { zh: "插入", en: "Insert" },
  to: { zh: "发给", en: "To" },
} satisfies Record<string, Copy | Record<Kind, Copy>>;

export function useCopy() {
  const { lang } = useStoryEnv();
  return { lang, t: (copy: Copy) => copy[lang] };
}

/** Draft, focus and recording state every direction shares. Typing `@` brings up the agent chip. */
export function useBox(kind: Kind, preset: Preset = {}) {
  const { lang } = useStoryEnv();
  const [text, setText] = useState(preset.text ?? "");
  const [hasFocus, setHasFocus] = useState(false);
  const [recording, setRecording] = useState(!!preset.recording);
  const empty = !text.trim();
  const mentions = text.includes("@");
  return {
    kind,
    text,
    setText,
    empty,
    focused: hasFocus || !!preset.focused,
    recording,
    chip: mentions ? (preset.restart ? ("restart" as const) : ("start" as const)) : null,
    restart: !!preset.restart && mentions,
    focusProps: {
      onFocus: () => setHasFocus(true),
      onBlur: (event: FocusEvent<HTMLElement>) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setHasFocus(false);
      },
    },
    startRecording: () => setRecording(true),
    stopRecording: (insert: boolean) => {
      setRecording(false);
      if (insert) setText((current) => (current.trim() ? current + TEXT.dictated[lang] : TEXT.dictated[lang].replace(/^[，,]\s*/, "")));
    },
    send: () => setText(""),
  };
}

export type Box = ReturnType<typeof useBox>;

export function Draft({ box, className }: { box: Box; className?: string }) {
  const { t } = useCopy();
  return (
    <textarea
      value={box.text}
      rows={1}
      onChange={(event) => box.setText(event.target.value)}
      onKeyDown={(event) => {
        if (event.key === "Enter" && (event.metaKey || event.ctrlKey) && !box.empty) {
          event.preventDefault();
          box.send();
        }
      }}
      placeholder={t(TEXT.placeholder[box.kind])}
      className={cn(
        "block w-full resize-none bg-transparent text-body leading-[1.625] text-foreground outline-none placeholder:text-muted-foreground",
        className,
      )}
      style={{ fieldSizing: "content" } as CSSProperties}
    />
  );
}

export function AgentAvatar() {
  return <ActorAvatar name="Lambda" initials="L" isAgent size="xs" />;
}

export function MemberAvatar({ size = "sm" }: { size?: "sm" | "md" }) {
  return <ActorAvatar name="Jiayuan" initials="J" size={size} className="bg-brand/15 text-brand" />;
}

/** The recipient chip as it ships (comment-trigger-chips.tsx); `named` adds the agent name. */
export function RecipientChip({ tone, named = false }: { tone: "start" | "restart"; named?: boolean }) {
  const { t } = useCopy();
  return (
    <button
      type="button"
      className={cn(
        "inline-flex h-6 min-w-0 max-w-full animate-in fade-in cursor-pointer items-center gap-1.5 rounded-md border border-transparent px-1.5 text-micro font-medium transition-colors",
        tone === "restart"
          ? "text-destructive hover:bg-destructive/10"
          : "text-muted-foreground hover:bg-muted hover:text-foreground",
      )}
    >
      <AgentAvatar />
      {named && <span className="shrink-0 text-foreground">Lambda</span>}
      <span className="truncate">{t(tone === "restart" ? TEXT.restartChip : TEXT.willStart)}</span>
      <ChevronDown aria-hidden className="size-3 shrink-0 text-faint-foreground" />
    </button>
  );
}

export function MicButton({ box, className }: { box: Box; className?: string }) {
  const { t } = useCopy();
  return (
    <IconButton label={t(TEXT.mic)} onClick={box.startRecording} className={cn("text-muted-foreground", className)}>
      <Mic />
    </IconButton>
  );
}

export function AttachButton({ small = false, className }: { small?: boolean; className?: string }) {
  const { t } = useCopy();
  return (
    <IconButton label={t(TEXT.attach)} size={small ? "icon-xs" : "icon-sm"} className={cn("text-muted-foreground", className)}>
      <Paperclip className={small ? "size-3.5" : "size-4"} />
    </IconButton>
  );
}

/** The send button as it ships: primary fill, dimmed to 50% while there is nothing to send. */
export function ShippedSend({ box }: { box: Box }) {
  const { t } = useCopy();
  return (
    <SubmitButton
      onClick={box.send}
      disabled={box.empty}
      ariaLabel={t(TEXT.send)}
      tooltip={
        <>
          {t(TEXT.send)} <Kbd>⌘↵</Kbd>
        </>
      }
    />
  );
}

/** Same 28 px circle as every other control; a quiet tint until there is something to send. */
export function QuietSend({ box, className }: { box: Box; className?: string }) {
  const { t } = useCopy();
  const ready = !box.empty || box.recording;
  return (
    <Button
      size="icon-sm"
      aria-label={t(TEXT.send)}
      disabled={!ready}
      onClick={box.send}
      className={cn(
        // eslint-disable-next-line no-restricted-syntax -- composer send button, the one round button
        "rounded-full transition-colors disabled:opacity-100",
        ready ? "bg-foreground text-background hover:bg-foreground/85" : "bg-muted text-faint-foreground",
        className,
      )}
    >
      <ArrowUp />
    </Button>
  );
}

function Elapsed() {
  const { elapsedMs } = useLevelSamples();
  return <span className="text-caption tabular-nums text-muted-foreground">{formatElapsed(elapsedMs)}</span>;
}

const DotsWave = VARIANT_BY_ID.dots.Wave;

/** The shipped recording strip (option C waveform); `send` is each direction's own send control. */
export function RecordingStrip({ box, send }: { box: Box; send: ReactNode }) {
  const { t } = useCopy();
  return (
    <LevelStreamProvider scenario="speech">
      <div className="flex min-w-0 flex-1 items-center gap-1">
        <IconButton label={t(TEXT.cancel)} onClick={() => box.stopRecording(false)} className="text-muted-foreground">
          <X />
        </IconButton>
        <span className="flex shrink-0 items-center gap-1.5 pl-1 pr-2">
          <RecDot />
          <Elapsed />
        </span>
        <div className="min-w-0 flex-1 text-foreground">
          <DotsWave density="strip" />
        </div>
        <IconButton label={t(TEXT.insert)} onClick={() => box.stopRecording(true)} className="ml-1 text-foreground">
          <Check />
        </IconButton>
        {send}
      </div>
    </LevelStreamProvider>
  );
}

const THREAD = {
  zh: "方案 C（静音成点）已经做进产品，评论、回复和 Chat 的录音条都换成了这一版。",
  en: "Option C (dots to bars) is in the product now, across the comment, reply and chat recording strips.",
};

/** A thread card from the issue timeline, with the reply row at its foot. */
export function ThreadCard({ children }: { children: ReactNode }) {
  const { t } = useCopy();
  return (
    <div className="overflow-hidden rounded-lg bg-card ring-1 ring-border">
      <div className="flex gap-2.5 px-4 py-3">
        <ActorAvatar name="Lambda" initials="L" isAgent size="md" className="mt-0.5" />
        <div className="min-w-0">
          <div className="flex items-baseline gap-2">
            <span className="text-body font-medium">Lambda</span>
            <span className="text-caption text-muted-foreground">25m</span>
          </div>
          <p className="mt-0.5 text-body text-foreground">{t(THREAD)}</p>
        </div>
      </div>
      <div className="border-t border-border/50 px-4 py-2.5">{children}</div>
    </div>
  );
}
