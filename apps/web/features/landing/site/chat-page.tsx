"use client";

import { ArrowUpRight, Layers, Link2, ShieldCheck, UserPen } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import { useDashboardCtaHref } from "../utils/use-dashboard-cta";
import { docsHrefForLocale, useLocale } from "../i18n";
import { ActionLink, Container, Section, SectionHeader } from "./primitives";
import { CHANNELS, ChatScene, DirectMessageVisual, IssueCommandVisual, MentionVisual, type ChannelId } from "./demo/chat-visuals";
import { CtaBand, SitePage } from "./site-page";
import { FeatureGrid, IconTile, SurfaceHero, WalkthroughTabs, type Feature, type Walkthrough } from "./surface-kit";

export function ChatPage() {
  return (
    <SitePage>
      <Hero />
      <div className="site-rails">
        <WaysIn />
        <Journey />
        <Commands />
        <Platforms />
        <Access />
        <CtaBand title="Bring an agent into your team chat" lead="Bind an agent to a Bot and say hello from Slack, Feishu, DingTalk, WeCom, or Telegram." />
      </div>
    </SitePage>
  );
}

function useChatDocsHref() {
  const { locale } = useLocale();
  return `${docsHrefForLocale(locale)}/channels`;
}

function Hero() {
  const user = useAuthStore((s) => s.user);
  const ctaHref = useDashboardCtaHref();
  const docsHref = useChatDocsHref();
  return (
    <SurfaceHero
      eyebrow="Chat integrations"
      title="Your agents, in your team chat."
      subtitle="No need to open Multica."
      lead="Bind an agent to a Bot in Slack, Feishu, DingTalk, WeCom, or Telegram. Message it directly, mention it in a group, or turn a message into an issue."
      actions={
        <>
          <ActionLink href={ctaHref} size="lg">
            {user ? "Open dashboard" : "Get started"}
          </ActionLink>
          <ActionLink href={docsHref} size="lg" tone="secondary">
            Setup guides
            <ArrowUpRight className="size-4" aria-hidden />
          </ActionLink>
        </>
      }
      frost="chat-page"
      label="A team channel, eng-support: Mia mentions @Multica to ask why last night’s export job failed, and the Bot answers in the thread. Beside it, the Messaging settings show Bots for Backend Agent on Slack, Lead Agent on Feishu, and Triage Agent on Telegram."
    >
      <ChatScene />
    </SurfaceHero>
  );
}

const WAYS: Walkthrough[] = [
  {
    title: "Message it directly",
    body: "Ask a question in a direct message. No mention needed, and the conversation picks up where you left it.",
    visual: DirectMessageVisual,
  },
  {
    title: "Mention it in a group",
    body: "@-mention the Bot in a group or channel. It answers in the thread and only sees messages addressed to it.",
    visual: MentionVisual,
  },
  {
    title: "Turn it into an issue",
    body: "Type /issue and a title. The issue lands in Multica, ready to assign to an agent or a teammate.",
    visual: IssueCommandVisual,
  },
];

function WaysIn() {
  return (
    <Section>
      <Container>
        <SectionHeader title="Three ways to reach an agent" />
        <div className="mt-site-block">
          <WalkthroughTabs steps={WAYS} side="left" numbered={false} />
        </div>
      </Container>
    </Section>
  );
}

const JOURNEY: { title: string; body: string }[] = [
  { title: "You message the Bot", body: "In a direct message, or with an @-mention in a group." },
  { title: "Multica finds the agent", body: "Each Bot belongs to one agent in one workspace." },
  { title: "It checks who you are", body: "Your chat account must be linked to a member of that workspace." },
  { title: "The agent runs", body: "The message joins an agent chat, the same as one sent in Multica." },
  { title: "The reply comes back", body: "In the same direct message or thread you wrote in." },
];

function Journey() {
  return (
    <Section>
      <Container>
        <SectionHeader
          title="What happens to a message"
          lead="Every Bot follows the same path, whichever platform it lives on."
        />
        <ol className="mt-site-block grid gap-8 lg:grid-cols-5 lg:gap-6">
          {JOURNEY.map((step, index) => (
            <li key={step.title} className="relative flex gap-5 lg:flex-col lg:gap-6">
              {index < JOURNEY.length - 1 ? (
                <span
                  aria-hidden
                  className="absolute top-[25px] -bottom-[26px] left-[7px] w-px bg-site-line lg:top-[7px] lg:right-[-1rem] lg:bottom-auto lg:left-[23px] lg:h-px lg:w-auto"
                />
              ) : null}
              <span
                aria-hidden
                className={cn(
                  "relative mt-0.5 size-[15px] shrink-0 rounded-full border-2 bg-site-canvas lg:mt-0",
                  index === JOURNEY.length - 1 ? "border-site-ink" : "border-site-line-strong",
                )}
              />
              <span>
                <span className="block font-site-mono text-site-label text-site-muted tabular-nums">
                  {String(index + 1).padStart(2, "0")}
                </span>
                <h3 className="mt-2 text-site-body text-site-ink">{step.title}</h3>
                <p className="mt-1.5 text-site-body text-site-muted text-pretty">{step.body}</p>
              </span>
            </li>
          ))}
        </ol>
      </Container>
    </Section>
  );
}

const COMMANDS: { command: string; body: string }[] = [
  { command: "/issue <title>", body: "Create an issue in the agent’s workspace." },
  { command: "/new [message]", body: "Start a fresh Multica chat. The old one stays in Multica." },
  { command: "/clear [message]", body: "Keep the chat, but start the agent on a clean context." },
];

function Commands() {
  const docsHref = useChatDocsHref();
  return (
    <Section>
      <Container className="grid items-center gap-site-block lg:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)] lg:gap-16">
        <div>
          <h2 className="font-site-display text-site-heading text-balance">
            <span className="block text-site-ink">Steer the conversation</span>
            <span className="block text-site-muted">without leaving it.</span>
          </h2>
          <p className="mt-3 max-w-[30rem] text-site-body text-site-muted text-pretty">
            The same three commands work on every platform. Each conversation keeps its own session, so a thread in one
            channel never bleeds into another.
          </p>
          <ActionLink href={`${docsHref}#conversation-controls`} tone="ghost" className="mt-6 -ml-3">
            Conversation controls
            <ArrowUpRight className="size-4" aria-hidden />
          </ActionLink>
        </div>
        <dl className="overflow-hidden rounded-site-card border border-site-line bg-site-surface">
          {COMMANDS.map((item, index) => (
            <div
              key={item.command}
              className={cn("grid gap-2 px-6 py-5 sm:grid-cols-[11rem_minmax(0,1fr)] sm:gap-6", index > 0 && "border-t border-site-line")}
            >
              <dt className="font-site-mono text-site-body text-site-ink">{item.command}</dt>
              <dd className="text-site-body text-site-muted text-pretty">{item.body}</dd>
            </div>
          ))}
        </dl>
      </Container>
    </Section>
  );
}

const PLATFORMS: { id: ChannelId; setup: string; groups: string; connection: string; community?: boolean }[] = [
  { id: "slack", setup: "Create a Slack app and paste two tokens", groups: "@-mention the Bot", connection: "Socket Mode" },
  { id: "lark", setup: "Scan a QR code from Multica", groups: "@-mention the Bot", connection: "Long connection" },
  {
    id: "dingtalk",
    setup: "Create a Stream-mode robot and paste its AppKey and AppSecret",
    groups: "@-mention the Bot",
    connection: "Stream mode",
    community: true,
  },
  {
    id: "wecom",
    setup: "Create a smart bot and paste its Bot ID and Secret",
    groups: "@-mention the Bot",
    connection: "Long connection",
    community: true,
  },
  {
    id: "telegram",
    setup: "Create a Bot with @BotFather and paste its token",
    groups: "@-mention or reply to the Bot",
    connection: "Long polling",
    community: true,
  },
];

function Platforms() {
  return (
    <Section>
      <Container>
        <SectionHeader
          title="Pick your platform"
          lead="Each one installs a little differently, but none needs a public webhook: Multica keeps the connection open itself."
        />
        <div className="mt-site-block">
          <div className="grid grid-cols-[minmax(0,12rem)_minmax(0,1.6fr)_minmax(0,1fr)_minmax(0,9rem)] gap-x-6 border-b border-site-ink pb-3 text-site-small text-site-ink max-lg:hidden">
            <span>Platform</span>
            <span>Setup</span>
            <span>In groups</span>
            <span>Connection</span>
          </div>
          <ul>
            {PLATFORMS.map((platform) => {
              const { name, Mark } = CHANNELS.find((item) => item.id === platform.id)!;
              return (
                <li
                  key={platform.id}
                  className="grid gap-x-6 gap-y-2 border-b border-site-line py-5 text-site-body max-lg:first:border-t lg:grid-cols-[minmax(0,12rem)_minmax(0,1.6fr)_minmax(0,1fr)_minmax(0,9rem)] lg:items-center"
                >
                  <span className="flex items-center gap-3">
                    <IconTile>
                      <Mark className="size-4" />
                    </IconTile>
                    <span>
                      <span className="block text-site-ink">{name}</span>
                      {platform.community ? <span className="block text-site-small text-site-muted">Community</span> : null}
                    </span>
                  </span>
                  <span className="text-site-ink-secondary max-lg:mt-2">{platform.setup}</span>
                  <span className="text-site-muted lg:text-site-ink-secondary">
                    <span className="lg:hidden">In groups: </span>
                    {platform.groups}
                  </span>
                  <span className="text-site-muted lg:text-site-ink-secondary">
                    <span className="lg:hidden">Connection: </span>
                    {platform.connection}
                  </span>
                </li>
              );
            })}
          </ul>
          <p className="mt-4 max-w-[48rem] text-site-small text-site-muted text-pretty">
            New Feishu connections are open in mainland China; existing Lark connections keep working. Community platforms
            ship in every release, without an official support commitment.
          </p>
        </div>
      </Container>
    </Section>
  );
}

const ACCESS: Feature[] = [
  {
    icon: Link2,
    title: "Linked accounts only",
    body: "The first time someone messages a Bot, they get a link to sign in and connect their chat account to Multica.",
  },
  {
    icon: ShieldCheck,
    title: "Checked on every message",
    body: "Membership is confirmed each time. Leave the workspace and the Bot no longer answers.",
  },
  {
    icon: Layers,
    title: "Only what’s addressed to it",
    body: "In a group the agent reads messages that mention it, never the rest of the channel.",
  },
  {
    icon: UserPen,
    title: "Admins decide which Bots exist",
    body: "Owners and admins connect and disconnect Bots. Conversations and runs are kept after a disconnect.",
  },
];

function Access() {
  return (
    <Section>
      <Container>
        <h2 className="max-w-[44rem] font-site-display text-site-heading text-balance">
          <span className="block text-site-ink">Open to your team.</span>
          <span className="block text-site-muted">Closed to everyone else in the chat.</span>
        </h2>
        <div className="mt-site-block">
          <FeatureGrid items={ACCESS} />
        </div>
      </Container>
    </Section>
  );
}
