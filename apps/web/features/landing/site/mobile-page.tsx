"use client";

import { ArrowUpRight } from "lucide-react";
import { useDashboardCtaHref } from "../utils/use-dashboard-cta";
import { docsHrefForLocale, useLocale } from "../i18n";
import { ActionLink, Container, Section, SectionHeader } from "./primitives";
import {
  ChatTabVisual,
  InboxTabVisual,
  MobileScene,
  MoreTabVisual,
  MyIssuesTabVisual,
  RunsVisual,
} from "./demo/mobile-visuals";
import { CtaBand, SitePage } from "./site-page";
import { CheckList, CodeBlock, SurfaceHero, WalkthroughTabs, type Walkthrough } from "./surface-kit";

export function MobilePage() {
  return (
    <SitePage>
      <Hero />
      <div className="site-rails">
        <Tabs />
        <Runs />
        <Everything />
        <Install />
        <CtaBand title="Keep up with your agents from anywhere" lead="Build Multica for iPhone and sign in with the account you already use." />
      </div>
    </SitePage>
  );
}

function useMobileDocsHref() {
  const { locale } = useLocale();
  return `${docsHrefForLocale(locale)}/mobile-app`;
}

function Hero() {
  const ctaHref = useDashboardCtaHref();
  return (
    <SurfaceHero
      eyebrow="Multica for iPhone"
      title="Your team’s work, in your pocket."
      subtitle="Answer an agent from anywhere."
      lead="The iOS app opens the same workspace as the web and Desktop. Reply to an agent waiting on you, follow its runs, or start a chat, wherever you are."
      actions={
        <>
          <ActionLink href="#install" size="lg">
            Install on iPhone
          </ActionLink>
          <ActionLink href={ctaHref} size="lg" tone="secondary">
            Open the web app
          </ActionLink>
        </>
      }
      frost="mobile"
      label="Three iPhones running Multica. On the left, the inbox with Backend Agent’s question unread. In front, issue ACM-151, where Backend Agent asks to apply a migration and Leo Park says yes. On the right, a chat with Backend Agent about shipping the export fix."
    >
      <MobileScene />
    </SurfaceHero>
  );
}

const TABS: Walkthrough[] = [
  {
    title: "Inbox",
    body: "Mentions, assignments, status changes, and agents asking for you, in one list. Open one to answer on the spot.",
    visual: InboxTabVisual,
  },
  {
    title: "My Issues",
    body: "What’s assigned to you, what you created, and what your agents are on, filtered by status, priority, or project.",
    visual: MyIssuesTabVisual,
  },
  {
    title: "Chat",
    body: "Talk to any agent in the workspace, send it images and files, and pick up an earlier chat.",
    visual: ChatTabVisual,
  },
  {
    title: "More",
    body: "Pinned issues, every issue and project, account settings, and a switch to your other workspaces.",
    visual: MoreTabVisual,
  },
];

function Tabs() {
  return (
    <Section>
      <Container>
        <SectionHeader title="Four tabs, the whole workspace" />
        <div className="mt-site-block">
          <WalkthroughTabs steps={TABS} numbered={false} fill />
        </div>
      </Container>
    </Section>
  );
}

function Runs() {
  return (
    <Section>
      <Container className="grid items-center gap-site-block lg:grid-cols-2 lg:gap-16">
        <div>
          <h2 className="font-site-display text-site-heading text-balance">
            <span className="block text-site-ink">See every run on an issue.</span>
            <span className="block text-site-muted">Stop one that’s off track.</span>
          </h2>
          <p className="mt-3 max-w-[30rem] text-site-body text-site-muted text-pretty">
            When an agent is working, the issue says so. Tap through to its runs, active and past, and cancel one if the
            work has gone the wrong way: the agent stops after its current step.
          </p>
        </div>
        <div
          role="img"
          aria-label="Agent Runs for ACM-151 on iPhone: Backend Agent’s comment run is running and QA Agent’s run is queued, each with a Cancel button, above three past runs."
          data-frost="mobile"
          className="site-frost relative h-[26rem] overflow-hidden rounded-site-panel sm:h-[30rem]"
        >
          <div aria-hidden className="absolute inset-0">
            <RunsVisual />
          </div>
        </div>
      </Container>
    </Section>
  );
}

function Everything() {
  return (
    <Section>
      <Container className="grid gap-site-block lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] lg:gap-16">
        <SectionHeader
          title="More than a viewer"
          lead="Most of what you do in Multica during the day works from the phone."
        />
        <div className="grid gap-x-10 gap-y-3 sm:grid-cols-2">
          <CheckList
            items={[
              "Create issues and @-mention people, agents, or squads",
              "Comment, reply in threads, and react",
              "Change status, priority, assignee, and due date",
              "Resolve a thread once it’s settled",
            ]}
          />
          <CheckList
            items={[
              "Search issues and projects",
              "Pin the issues you keep coming back to",
              "Create projects and attach repositories",
              "Light or dark, in English or Simplified Chinese",
            ]}
          />
        </div>
      </Container>
    </Section>
  );
}

const BUILD_COMMANDS = `git clone https://github.com/multica-ai/multica.git
cd multica
pnpm install
pnpm ios:mobile:device:prod:release`;

function Install() {
  const docsHref = useMobileDocsHref();
  return (
    <Section id="install" className="scroll-mt-site-header">
      <Container className="grid items-start gap-site-block lg:grid-cols-2 lg:gap-16">
        <div>
          <h2 className="font-site-display text-site-heading text-balance">
            <span className="block text-site-ink">Not on the App Store yet.</span>
            <span className="block text-site-muted">Build it on your Mac today.</span>
          </h2>
          <p className="mt-3 max-w-[30rem] text-site-body text-site-muted text-pretty">
            With Xcode and a free Apple ID, one command builds the app and installs it on an iPhone plugged into your
            Mac. It connects to Multica Cloud, or to your own server.
          </p>
          <CheckList
            className="mt-6"
            items={[
              "A free Apple ID signs the app for 7 days. Rerun the command to renew it",
              "Your data lives on the server, so reinstalling never loses anything",
              "No Android app yet",
            ]}
          />
          <ActionLink href={docsHref} tone="ghost" className="mt-6 -ml-3">
            Installation guide
            <ArrowUpRight className="size-4" aria-hidden />
          </ActionLink>
        </div>
        <CodeBlock variants={[{ label: "Terminal", code: BUILD_COMMANDS }]} />
      </Container>
    </Section>
  );
}
