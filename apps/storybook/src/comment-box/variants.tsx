import type { ComponentType, ReactNode } from "react";
import { AtSign, Paperclip, Plus } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import type { Lang } from "../voice-input/copy";
import {
  AttachButton,
  Draft,
  MemberAvatar,
  MicButton,
  QuietSend,
  RecipientChip,
  RecordingStrip,
  ShippedSend,
  TEXT,
  useBox,
  useCopy,
  type Box,
  type Preset,
} from "./parts";

export interface BoxProps {
  preset?: Preset;
}

function ReplyRow({ children, avatarClassName }: { children: ReactNode; avatarClassName?: string }) {
  return (
    <div className="flex items-start gap-2.5">
      <span className={cn("shrink-0", avatarClassName)}>
        <MemberAvatar />
      </span>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}

/* A · Current: comment-input.tsx and reply-input.tsx as they ship. */

function ShippedRestart({ box }: { box: Box }) {
  const { t } = useCopy();
  return (
    <Button
      size="sm"
      variant="destructive"
      // eslint-disable-next-line no-restricted-syntax -- composer send button, the one round button
      className="rounded-full"
      onClick={box.send}
    >
      {t(TEXT.restartSend)}
    </Button>
  );
}

function ShippedActions({ box }: { box: Box }) {
  return (
    <>
      <MicButton box={box} />
      <AttachButton small />
      {box.restart ? <ShippedRestart box={box} /> : <ShippedSend box={box} />}
    </>
  );
}

function CurrentComment({ preset }: BoxProps) {
  const box = useBox("comment", preset);
  return (
    <div {...box.focusProps} className="relative flex flex-col rounded-lg bg-card pb-8 ring-1 ring-border">
      <div className="px-3 py-2">
        <Draft box={box} />
      </div>
      {box.recording ? (
        <div className="absolute inset-x-1.5 bottom-1 flex items-center">
          <RecordingStrip box={box} send={<ShippedSend box={{ ...box, empty: false }} />} />
        </div>
      ) : (
        <>
          <div className="absolute bottom-1 left-2 right-36 min-w-0">{box.chip && <RecipientChip tone={box.chip} />}</div>
          <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
            <ShippedActions box={box} />
          </div>
        </>
      )}
    </div>
  );
}

function CurrentReply({ preset }: BoxProps) {
  const box = useBox("reply", preset);
  return (
    <ReplyRow avatarClassName="mt-0.5">
      <div
        {...box.focusProps}
        className={cn("relative flex flex-col", (!box.empty || box.chip || box.recording) && "pb-9")}
      >
        <Draft box={box} />
        {box.recording ? (
          <div className="absolute inset-x-0 bottom-0 flex items-center">
            <RecordingStrip box={box} send={<ShippedSend box={{ ...box, empty: false }} />} />
          </div>
        ) : (
          <>
            <div className="absolute bottom-0 left-0 right-32 min-w-0">{box.chip && <RecipientChip tone={box.chip} />}</div>
            <div className="absolute bottom-0 right-0 flex items-center gap-1">
              <ShippedActions box={box} />
            </div>
          </>
        )}
      </div>
    </ReplyRow>
  );
}

/* Shared by the redesigns. */

function RestartPill({ box }: { box: Box }) {
  const { t } = useCopy();
  return (
    <Button
      size="sm"
      variant="destructive"
      // eslint-disable-next-line no-restricted-syntax -- composer send button, the one round button
      className="h-7 rounded-full px-3"
      onClick={box.send}
    >
      {t(TEXT.restartSend)}
    </Button>
  );
}

function TidyActions({ box }: { box: Box }) {
  return (
    <>
      <MicButton box={box} />
      <AttachButton />
      {box.restart ? <RestartPill box={box} /> : <QuietSend box={box} />}
    </>
  );
}

function focusRing(box: Box) {
  return box.focused ? "ring-foreground/25" : "ring-border";
}

/* B · Tidy: same layout, one control size, a quiet send, the toolbar in normal flow. */

function TidyComment({ preset }: BoxProps) {
  const box = useBox("comment", preset);
  return (
    <div {...box.focusProps} className={cn("flex flex-col rounded-lg bg-card ring-1 transition-shadow", focusRing(box))}>
      <div className="px-3 pt-2.5">
        <Draft box={box} />
      </div>
      <div className="flex h-10 items-center gap-1 px-1.5">
        {box.recording ? (
          <RecordingStrip box={box} send={<QuietSend box={box} />} />
        ) : (
          <>
            <div className="min-w-0 flex-1 overflow-hidden pl-0.5">{box.chip && <RecipientChip tone={box.chip} named />}</div>
            <TidyActions box={box} />
          </>
        )}
      </div>
    </div>
  );
}

function TidyReply({ preset }: BoxProps) {
  const box = useBox("reply", preset);
  const toolbar = !box.empty || !!box.chip || box.recording;
  return (
    <ReplyRow avatarClassName="mt-1">
      <div {...box.focusProps}>
        <div className="flex items-start gap-1">
          <Draft box={box} className="min-w-0 flex-1 py-px" />
          {!toolbar && (
            <div className="-my-px flex shrink-0 items-center gap-1">
              <TidyActions box={box} />
            </div>
          )}
        </div>
        {toolbar && (
          <div className="mt-1 flex h-8 items-center gap-1">
            {box.recording ? (
              <RecordingStrip box={box} send={<QuietSend box={box} />} />
            ) : (
              <>
                <div className="min-w-0 flex-1 overflow-hidden">{box.chip && <RecipientChip tone={box.chip} named />}</div>
                <TidyActions box={box} />
              </>
            )}
          </div>
        )}
      </div>
    </ReplyRow>
  );
}

/* C · Grows on focus: one line at rest, the toolbar and send appear once you are writing. */

function GrowingBox({ box, compact }: { box: Box; compact?: boolean }) {
  const expanded = box.focused || !box.empty || box.recording;
  return (
    <div
      {...box.focusProps}
      className={cn(
        "bg-card ring-1 transition-shadow",
        compact ? "rounded-md" : "rounded-lg",
        expanded ? "ring-foreground/25 shadow-[0_1px_3px_rgb(0_0_0/0.05)]" : "ring-border",
      )}
    >
      <div className="flex items-start">
        <Draft box={box} className={cn("min-w-0 flex-1 px-3", compact ? "py-1" : "py-2")} />
        {!expanded && (
          <div className={cn("flex shrink-0 items-center gap-0.5 pr-1", compact ? "py-0.5" : "py-1.5")}>
            <MicButton box={box} />
            <AttachButton />
          </div>
        )}
      </div>
      {expanded && (
        <div className="flex h-9 animate-in fade-in slide-in-from-top-1 items-center gap-1 px-1.5 pb-1 duration-150">
          {box.recording ? (
            <RecordingStrip box={box} send={<QuietSend box={box} />} />
          ) : (
            <>
              <div className="min-w-0 flex-1 overflow-hidden pl-0.5">{box.chip && <RecipientChip tone={box.chip} named />}</div>
              <TidyActions box={box} />
            </>
          )}
        </div>
      )}
    </div>
  );
}

function GrowingComment({ preset }: BoxProps) {
  return <GrowingBox box={useBox("comment", preset)} />;
}

function GrowingReply({ preset }: BoxProps) {
  const box = useBox("reply", preset);
  return (
    <ReplyRow avatarClassName="mt-1.5">
      <GrowingBox box={box} compact />
    </ReplyRow>
  );
}

/* D · Same as chat: a "+" menu on the left, mic and send on the right, recipients on a "To" line. */

function AddMenu() {
  const { t } = useCopy();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <IconButton label={t(TEXT.add)} className="text-muted-foreground">
            <Plus />
          </IconButton>
        }
      />
      <DropdownMenuContent align="start" side="top" sideOffset={6} className="min-w-48">
        <DropdownMenuItem>
          <Paperclip />
          {t(TEXT.uploadFile)}
        </DropdownMenuItem>
        <DropdownMenuItem>
          <AtSign />
          {t(TEXT.mention)}
          <DropdownMenuShortcut>@</DropdownMenuShortcut>
        </DropdownMenuItem>
        <DropdownMenuItem>
          <span className="flex size-4 items-center justify-center text-caption font-medium">/</span>
          {t(TEXT.command)}
          <DropdownMenuShortcut>/</DropdownMenuShortcut>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function ChatBox({ box, compact }: { box: Box; compact?: boolean }) {
  const { t } = useCopy();
  return (
    <div
      {...box.focusProps}
      className={cn("flex flex-col overflow-hidden bg-card ring-1 transition-shadow", compact ? "rounded-lg" : "rounded-xl", focusRing(box))}
    >
      {box.chip && (
        <div className="flex animate-in fade-in items-center gap-1 border-b border-border/60 px-2 py-1">
          <span className="pl-1 text-caption text-muted-foreground">{t(TEXT.to)}</span>
          <RecipientChip tone={box.chip} named />
        </div>
      )}
      <div className={cn("px-3", compact ? "pt-1.5" : "pt-2.5")}>
        <Draft box={box} />
      </div>
      <div className={cn("flex items-center gap-1 px-1.5", compact ? "h-9" : "h-10")}>
        {box.recording ? (
          <RecordingStrip box={box} send={<QuietSend box={box} />} />
        ) : (
          <>
            <AddMenu />
            <div className="flex-1" />
            <MicButton box={box} />
            {box.restart ? <RestartPill box={box} /> : <QuietSend box={box} />}
          </>
        )}
      </div>
    </div>
  );
}

function ChatComment({ preset }: BoxProps) {
  return <ChatBox box={useBox("comment", preset)} />;
}

function ChatReply({ preset }: BoxProps) {
  const box = useBox("reply", preset);
  return (
    <ReplyRow avatarClassName="mt-2">
      <ChatBox box={box} compact />
    </ReplyRow>
  );
}

/* E · Labelled send: the button names the action; restart is the same button in red. */

function LabelSend({ box }: { box: Box }) {
  const { t } = useCopy();
  const ready = !box.empty || box.recording;
  const label = box.restart ? TEXT.restartSend : box.kind === "reply" ? TEXT.reply : TEXT.comment;
  return (
    <Button
      size="sm"
      variant={box.restart ? "destructive" : "default"}
      disabled={!ready}
      onClick={box.send}
      className={cn("gap-2 disabled:opacity-100", !ready && "bg-muted text-faint-foreground")}
    >
      {t(label)}
      <span className={cn("text-micro font-normal", ready ? "opacity-60" : "opacity-80")}>⌘↵</span>
    </Button>
  );
}

function LabelBox({ box, compact }: { box: Box; compact?: boolean }) {
  const toolbar = !compact || box.focused || !box.empty || box.recording;
  return (
    <div {...box.focusProps} className={cn("bg-card ring-1 transition-shadow", compact ? "rounded-md" : "rounded-lg", focusRing(box))}>
      <Draft box={box} className={cn("px-3", compact ? "py-1" : "min-h-[52px] pt-2.5 pb-1")} />
      {toolbar && (
        <div className={cn("flex items-center gap-1 px-1.5 pb-1.5", compact && "animate-in fade-in duration-150")}>
          {box.recording ? (
            <RecordingStrip box={box} send={<LabelSend box={box} />} />
          ) : (
            <>
              <AttachButton />
              <MicButton box={box} />
              <div className="flex min-w-0 flex-1 justify-end overflow-hidden">{box.chip && <RecipientChip tone={box.chip} named />}</div>
              <LabelSend box={box} />
            </>
          )}
        </div>
      )}
    </div>
  );
}

function LabelComment({ preset }: BoxProps) {
  return <LabelBox box={useBox("comment", preset)} />;
}

function LabelReply({ preset }: BoxProps) {
  const box = useBox("reply", preset);
  return (
    <ReplyRow avatarClassName="mt-1.5">
      <LabelBox box={box} compact />
    </ReplyRow>
  );
}

export type DirectionId = "current" | "tidy" | "growing" | "chat" | "label";

export interface Direction {
  id: DirectionId;
  letter: string;
  name: Record<Lang, string>;
  pitch: Record<Lang, string>;
  notes: Record<Lang, string[]>;
  Comment: ComponentType<BoxProps>;
  Reply: ComponentType<BoxProps>;
}

export const DIRECTIONS: Direction[] = [
  {
    id: "current",
    letter: "A",
    name: { zh: "现状", en: "Current" },
    pitch: {
      zh: "线上的评论框和回复框，照着 comment-input.tsx 和 reply-input.tsx 复刻。",
      en: "The comment and reply boxes as they ship, rebuilt from comment-input.tsx and reply-input.tsx.",
    },
    notes: {
      zh: [
        "三个按钮三种规格：麦克风 28px 配 16px 图标，附件 24px 配 14px 图标，发送是 28px 实心圆",
        "没内容时发送键是一块半透明的实心灰圆，反而是框里最重的东西",
        "空的评论框也预留了一整行工具栏，又高又空",
        "评论是带边框的卡片，回复是没有边框的裸文本；回复一输入，工具栏冒出来，高度会跳",
        "「停止并发送」是文字胶囊，和圆形发送键不是一个形状",
        "智能体提示只有头像，看不出是谁会被叫起来",
      ],
      en: [
        "Three buttons, three specs: a 28 px mic with a 16 px icon, a 24 px paperclip with a 14 px icon, a filled 28 px send circle",
        "With nothing to send, the send button is a half-transparent grey disc, the heaviest thing in the box",
        "The empty comment box reserves a whole toolbar row, so it looks tall and empty",
        "Comments are a bordered card and replies are bare text; typing a reply pops the toolbar in and the height jumps",
        "Stop and send is a text pill, a different shape from the round send button",
        "The agent chip shows only an avatar, not who will start",
      ],
    },
    Comment: CurrentComment,
    Reply: CurrentReply,
  },
  {
    id: "tidy",
    letter: "B",
    name: { zh: "统一规格", en: "Tidy" },
    pitch: {
      zh: "布局不动，只把按钮统一：都是 28px、16px 图标、同一种样式；发送键没内容时是浅色底，能发送时才变实心。",
      en: "Same layout with the controls unified: all 28 px with 16 px icons in one style; send stays a light tint until there is something to send.",
    },
    notes: {
      zh: [
        "改动最小，基本只动两个文件",
        "工具栏改成正常排版，不再靠绝对定位和写死的右边距",
        "聚焦时边框加深，知道光标在哪个框里",
        "「停止并发送」也是 28px 高，和圆形按钮在同一条线上",
        "智能体提示带上名字：「Lambda 发送后开始」",
      ],
      en: [
        "The smallest change, essentially two files",
        "The toolbar is laid out in normal flow, no absolute positioning or hard-coded right offsets",
        "The border darkens on focus, so you can see which box has the caret",
        "Stop and send is 28 px tall too, on one line with the round buttons",
        "The agent chip names the agent: “Lambda will start when sent”",
      ],
    },
    Comment: TidyComment,
    Reply: TidyReply,
  },
  {
    id: "growing",
    letter: "C",
    name: { zh: "单行展开", en: "Grows on focus" },
    pitch: {
      zh: "空着时只有一行，右边放麦克风和附件；点进去或开始输入才展开工具栏和发送键。评论框和回复框是同一个组件。",
      en: "One line at rest with the mic and paperclip on the right; focus or type and it grows a toolbar with send. Comments and replies are the same component.",
    },
    notes: {
      zh: [
        "时间线底部和每个讨论串都省下一行高度",
        "没东西可发时不出现发送键",
        "评论和回复长得一样，只是尺寸不同",
        "代价：展开时高度会变一次（带过渡）",
      ],
      en: [
        "Saves a row at the foot of the timeline and in every thread",
        "No send button until there is something to send",
        "Comments and replies look the same, just at two sizes",
        "The trade-off: the height changes once on focus (animated)",
      ],
    },
    Comment: GrowingComment,
    Reply: GrowingReply,
  },
  {
    id: "chat",
    letter: "D",
    name: { zh: "和 Chat 统一", en: "Same as chat" },
    pitch: {
      zh: "照搬 Chat 输入框：左边一个「+」收纳附件、提及和快捷指令，右边是麦克风和发送。会被叫起来的智能体放到顶部「发给」一行。",
      en: "Borrowed from the chat composer: a “+” on the left holds attach, mention and quick actions; mic and send sit on the right. Agents that will start move to a “To” line on top.",
    },
    notes: {
      zh: [
        "Chat、评论、回复三处同一套输入框，学一次就会",
        "「发给 Lambda」放在最上面，像邮件收件人，发送前一眼看清谁会被叫起来",
        "附件收进「+」，多一次点击（拖拽和粘贴不受影响）",
        "回复框空着也是两行高，讨论串会变长",
      ],
      en: [
        "Chat, comments and replies share one composer, so it is learned once",
        "“To Lambda” sits on top like an email recipient, so you see who will start before you send",
        "Attach moves into “+”, one extra click (drag and paste still work)",
        "An empty reply box is two rows tall, so threads get longer",
      ],
    },
    Comment: ChatComment,
    Reply: ChatReply,
  },
  {
    id: "label",
    letter: "E",
    name: { zh: "文字发送键", en: "Labelled send" },
    pitch: {
      zh: "发送键写明动作：「评论」「回复」；会重启运行时，同一个按钮变成红色「停止并发送」。工具放左边，智能体提示紧挨着发送键。",
      en: "The send button names the action, Comment or Reply; when it would restart a run, the same button turns red and reads Stop and send. Tools sit on the left, the agent chip next to send.",
    },
    notes: {
      zh: [
        "动作一目了然，不用猜圆圈箭头是什么",
        "正常和重启是同一个形状，只换颜色和文案",
        "提示和按钮连成一句话：「Lambda 发送后开始 · 评论 ⌘↵」",
        "回复框收起时只有一行，聚焦后才出现工具栏",
        "文字按钮比圆形图标更占宽度",
      ],
      en: [
        "The action is spelled out; no guessing what the arrow circle does",
        "Normal and restart share one shape, only colour and label change",
        "Chip and button read as one sentence: “Lambda will start when sent · Comment ⌘↵”",
        "The reply box is one line until focused, then grows its toolbar",
        "A text button takes more width than a round icon",
      ],
    },
    Comment: LabelComment,
    Reply: LabelReply,
  },
];

export const DIRECTION_BY_ID = Object.fromEntries(DIRECTIONS.map((d) => [d.id, d])) as Record<DirectionId, Direction>;
