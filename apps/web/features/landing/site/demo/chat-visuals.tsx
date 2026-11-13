"use client";

import type { ComponentType, ReactNode } from "react";
import { Hash, MessageSquare, Plus } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import { DingTalkMark } from "@multica/views/settings/components/dingtalk-mark";
import { LarkMark } from "@multica/views/settings/components/lark-mark";
import { SlackMark } from "@multica/views/settings/components/slack-mark";
import { TelegramMark } from "@multica/views/settings/components/telegram-mark";
import { WecomMark } from "@multica/views/settings/components/wecom-mark";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { Avatar, BACKEND, FLOATING, LEAD, LEO, MIA, SAM, TRIAGE, ZoomCanvas, type Actor } from "./mock-kit";

/*
 * Mockups for the Chat integrations page: a team chat where people talk to a
 * Multica agent through its Bot, and the Messaging settings where each Bot is
 * bound to one agent. The chat client is drawn plain so it reads as any of the
 * supported platforms; the Bot's replies follow the product's own copy. Data
 * is fictional.
 */

export type ChannelId = "slack" | "lark" | "dingtalk" | "wecom" | "telegram";

export const CHANNELS: { id: ChannelId; name: string; Mark: ComponentType<{ className?: string }> }[] = [
  { id: "slack", name: "Slack", Mark: SlackMark },
  { id: "lark", name: "Feishu", Mark: LarkMark },
  { id: "dingtalk", name: "DingTalk", Mark: DingTalkMark },
  { id: "wecom", name: "WeCom", Mark: WecomMark },
  { id: "telegram", name: "Telegram", Mark: TelegramMark },
];

const BOT = { name: "Multica", agent: BACKEND };

function Message({ actor, bot, time, children }: { actor: Actor; bot?: boolean; time: string; children: ReactNode }) {
  return (
    <div className="flex gap-2.5">
      <span className="pt-0.5">{bot ? <MulticaIcon shape="circle" size={24} /> : <Avatar actor={actor} size={24} />}</span>
      <div className="min-w-0 flex-1">
        <p className="flex items-center gap-1.5 text-caption">
          <span className="font-semibold">{bot ? BOT.name : actor.name}</span>
          {bot ? <span className="rounded-sm bg-muted px-1 text-micro font-medium text-muted-foreground">App</span> : null}
          <span className="text-muted-foreground tabular-nums">{time}</span>
        </p>
        <div className="mt-0.5 text-label leading-relaxed">{children}</div>
      </div>
    </div>
  );
}

function Mention({ children }: { children: ReactNode }) {
  return <span className="rounded-sm bg-brand/10 px-0.5 font-medium text-brand">{children}</span>;
}

function Slash({ children }: { children: ReactNode }) {
  return <span className="font-mono text-[12px]">{children}</span>;
}

function ChatWindow({
  className,
  title,
  icon: Icon,
  composer,
  children,
}: {
  className?: string;
  title: string;
  icon: ComponentType<{ className?: string }>;
  composer: string;
  children: ReactNode;
}) {
  return (
    <div className={cn("flex flex-col overflow-hidden rounded-xl border border-site-line-strong bg-site-surface shadow-site-float", className)}>
      <div className="flex h-11 shrink-0 items-center gap-2 border-b border-site-line px-4">
        <Icon className="size-4 text-muted-foreground" />
        <span className="text-label font-semibold">{title}</span>
      </div>
      <div className="flex min-h-0 flex-1 flex-col justify-end gap-4 overflow-hidden px-4 py-4">{children}</div>
      <div className="shrink-0 px-3 pb-3">
        <div className="flex h-9 items-center gap-2 rounded-lg border border-site-line bg-background px-3 text-label text-muted-foreground">
          <Plus className="size-3.5" />
          {composer}
        </div>
      </div>
    </div>
  );
}

function ThreadReplies({ count, children }: { count: number; children: ReactNode }) {
  return (
    <div className="ml-[34px] flex flex-col gap-3 border-l-2 border-border pl-3">
      <p className="text-caption font-medium text-brand">{count} replies</p>
      {children}
    </div>
  );
}

function ChannelConversation() {
  return (
    <>
      <Message actor={LEO} time="9:38">
        Export for the Northwind workspace failed again last night.
      </Message>
      <Message actor={MIA} time="9:41">
        <Mention>@Multica</Mention> why did last night’s export job fail?
      </Message>
      <ThreadReplies count={2}>
        <Message actor={BOT.agent} bot time="9:42">
          It hit the 15-minute timeout on a workspace with 2.1M rows. The job loads every row before writing, so it
          should write in batches of 10,000 instead.
        </Message>
        <Message actor={MIA} time="9:43">
          That matches the logs. Thanks!
        </Message>
      </ThreadReplies>
    </>
  );
}

const BOUND: { channel: ChannelId; agent: Actor | null }[] = [
  { channel: "slack", agent: BACKEND },
  { channel: "lark", agent: LEAD },
  { channel: "telegram", agent: TRIAGE },
  { channel: "dingtalk", agent: null },
  { channel: "wecom", agent: null },
];

/** Settings → Messaging: one Bot per agent, on each platform the team uses. */
function MessagingPanel({ className, rows = BOUND.length }: { className: string; rows?: number }) {
  return (
    <div className={cn(FLOATING, "absolute overflow-hidden", className)}>
      <div className="border-b border-border px-4 py-3">
        <p className="text-label font-semibold">Messaging</p>
        <p className="mt-0.5 text-caption text-muted-foreground">Connected bots</p>
      </div>
      {BOUND.slice(0, rows).map(({ channel, agent }) => {
        const { name, Mark } = CHANNELS.find((item) => item.id === channel)!;
        return (
          <div key={channel} className="flex h-[52px] items-center gap-3 border-b border-border px-4 last:border-b-0">
            <span className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-muted text-foreground">
              <Mark className="size-3.5" />
            </span>
            <span className="min-w-0 flex-1">
              <span className="block text-label font-medium">{name}</span>
              <span className="flex items-center gap-1.5 truncate text-caption text-muted-foreground">
                {agent ? (
                  <>
                    <Avatar actor={agent} size={16} />
                    {agent.name}
                  </>
                ) : (
                  "No bot yet"
                )}
              </span>
            </span>
            {agent ? (
              <span className="flex items-center gap-1.5 text-caption">
                <span className="size-1.5 rounded-full bg-success" />
                Connected
              </span>
            ) : (
              <span className="rounded-md px-2 py-0.5 text-caption font-medium ring-1 ring-border">Connect</span>
            )}
          </div>
        );
      })}
    </div>
  );
}

const SCENE = { width: 1100, height: 600 } as const;
const SCENE_NARROW = { width: 440, height: 440 } as const;

/** Hero: a team channel asking the Bot a question, and the Bots each agent has across platforms. */
export function ChatScene() {
  return (
    <>
      <div className="relative aspect-square sm:hidden">
        <ZoomCanvas {...SCENE_NARROW}>
          <ChatWindow title="eng-support" icon={Hash} composer="Message #eng-support" className="absolute inset-6">
            <ChannelConversation />
          </ChatWindow>
        </ZoomCanvas>
      </div>
      <div className="relative hidden aspect-[1100/600] sm:block">
        <ZoomCanvas {...SCENE}>
          <ChatWindow
            title="eng-support"
            icon={Hash}
            composer="Message #eng-support"
            className="absolute top-10 left-[6%] w-[56%] -bottom-12"
          >
            <ChannelConversation />
          </ChatWindow>
          <MessagingPanel className="top-[16%] right-[5%] w-[320px]" />
        </ZoomCanvas>
      </div>
    </>
  );
}

// --- Ways in -------------------------------------------------------------------

const PANEL = "h-[300px] w-[340px]";

export function DirectMessageVisual() {
  return (
    <ChatWindow title="Multica" icon={MessageSquare} composer="Message Multica" className={PANEL}>
      <Message actor={LEO} time="10:02">
        What changed in the billing API this week?
      </Message>
      <Message actor={BOT.agent} bot time="10:03">
        Three merged pull requests: invoices now paginate, refunds accept a reason, and the old /v1/charges route
        returns 410.
      </Message>
    </ChatWindow>
  );
}

export function MentionVisual() {
  return (
    <ChatWindow title="eng-support" icon={Hash} composer="Reply in thread" className={PANEL}>
      <Message actor={SAM} time="11:20">
        <Mention>@Multica</Mention> why is the checkout test flaky?
      </Message>
      <ThreadReplies count={1}>
        <Message actor={BOT.agent} bot time="11:21">
          It waits a fixed 500 ms for the payment form. On a slow runner the form isn’t there yet.
        </Message>
      </ThreadReplies>
    </ChatWindow>
  );
}

export function IssueCommandVisual() {
  return (
    <ChatWindow title="Multica" icon={MessageSquare} composer="Message Multica" className={PANEL}>
      <Message actor={MIA} time="9:43">
        <Slash>/issue</Slash> Batch the nightly export job
      </Message>
      <Message actor={BOT.agent} bot time="9:43">
        ✅ Created ACM-171 — Batch the nightly export job
        <span className="mt-2 flex items-center gap-2 rounded-lg border border-border bg-background px-3 py-2 text-caption">
          <StatusIcon status="todo" className="size-3.5" />
          <span className="text-muted-foreground tabular-nums">ACM-171</span>
          <span className="min-w-0 flex-1 truncate font-medium">Batch the nightly export job</span>
        </span>
      </Message>
    </ChatWindow>
  );
}
