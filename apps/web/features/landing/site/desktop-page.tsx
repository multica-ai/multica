"use client";

import type { ComponentType } from "react";
import { ArrowRight, ArrowUpRight } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { docsHrefForLocale, useLocale } from "../i18n";
import { ActionLink, Container, Section, SectionHeader } from "./primitives";
import { DaemonVisual, DesktopScene, TabsVisual } from "./demo/desktop-visuals";
import { CtaBand, SitePage } from "./site-page";
import { CheckList, CodeBlock, CompareTable, IconTile, SurfaceHero, type CompareRow } from "./surface-kit";
import { AppleIcon, LinuxIcon, WindowsIcon } from "../components/download/os-icons";

export function DesktopPage() {
  return (
    <SitePage>
      <Hero />
      <div className="site-rails">
        <Highlights />
        <Compare />
        <Downloads />
        <SelfHosted />
        <CtaBand title="Put your computer to work" lead="Download Multica Desktop, sign in, and assign an issue to an agent on your machine." />
      </div>
    </SitePage>
  );
}

function useDesktopDocsHref() {
  const { locale } = useLocale();
  return `${docsHrefForLocale(locale)}/desktop-app`;
}

function Hero() {
  const docsHref = useDesktopDocsHref();
  return (
    <SurfaceHero
      eyebrow="Multica Desktop"
      title="Multica, as a native app."
      subtitle="Your computer becomes a runtime."
      lead="Multica Desktop for macOS, Windows, and Linux starts the daemon for you after sign-in, so the coding tools on your computer can take issues. Each workspace keeps its own tabs."
      actions={
        <>
          <ActionLink href="/download" size="lg">
            Download
          </ActionLink>
          <ActionLink href={docsHref} size="lg" tone="secondary">
            Desktop guide
            <ArrowUpRight className="size-4" aria-hidden />
          </ActionLink>
        </>
      }
      frost="desktop"
      label="Multica Desktop with four tabs open in the title bar. The active tab shows issue ACM-152, which Backend Agent is working on Mia’s MacBook Pro. Beside it, that computer appears as a runtime managed by Desktop, with Claude Code, Codex, and Cursor detected."
    >
      <DesktopScene />
    </SurfaceHero>
  );
}

const HIGHLIGHTS: { title: string; body: string; points: string[]; visual: ComponentType; label: string }[] = [
  {
    title: "The daemon starts itself",
    body: "Sign in and Desktop starts its own bundled CLI, finds the coding tools already installed, and puts your computer on the Runtimes page. No terminal needed.",
    points: [
      "Runs on its own profile, apart from any daemon you start in a terminal",
      "Status, logs, and restart live in Settings",
      "Updates with the app, so the CLI never falls behind",
    ],
    visual: DaemonVisual,
    label: "Desktop’s Daemon settings: running on the desktop-multica.ai profile, CLI managed by Desktop, with Claude Code, Codex, and Cursor detected and ready.",
  },
  {
    title: "Tabs for every workspace",
    body: "Open issues, chats, and projects in tabs, and switch workspaces without losing them. Come back to a workspace and its tabs are where you left them.",
    points: [
      "Pin the tabs you keep coming back to",
      "Each tab keeps its own history and scroll position",
      "Multica links open in a tab instead of the browser",
    ],
    visual: TabsVisual,
    label: "Two workspaces, each keeping its own tabs: Acme Labs with its pinned Inbox, issue ACM-152, and a chat with Backend Agent; Mia’s workspace with its Inbox and MIA-12.",
  },
];

function Highlights() {
  return (
    <Section>
      <Container className="flex flex-col gap-site-section">
        {HIGHLIGHTS.map((item, index) => (
          <div key={item.title} className="grid items-center gap-site-block lg:grid-cols-2 lg:gap-16">
            <div className={cn(index % 2 === 1 && "lg:order-2")}>
              <h2 className="font-site-display text-site-heading text-site-ink text-balance">{item.title}</h2>
              <p className="mt-3 max-w-[30rem] text-site-body text-site-muted text-pretty">{item.body}</p>
              <CheckList items={item.points} className="mt-6" />
            </div>
            <div
              role="img"
              aria-label={item.label}
              className={cn(
                "site-visual flex h-[23rem] items-center justify-center overflow-hidden rounded-site-panel sm:h-[26rem]",
                index % 2 === 1 && "lg:order-1",
              )}
            >
              <div aria-hidden className="text-foreground max-sm:scale-75 lg:max-xl:scale-[0.85] xl:scale-110">
                <item.visual />
              </div>
            </div>
          </div>
        ))}
      </Container>
    </Section>
  );
}

const COMPARE: CompareRow[] = [
  { label: "How you open it", values: ["Install the app", "Any browser"] },
  { label: "Your computer as a runtime", values: ["Started for you after sign-in", "Install and start the CLI yourself"] },
  { label: "Tabs", values: ["Saved for each workspace", "Your browser’s tabs"] },
  { label: "Updates", values: ["In the background, installed on restart", "Refresh the page"] },
];

function Compare() {
  return (
    <Section>
      <Container>
        <SectionHeader
          title="Desktop or the web app"
          lead="Both show the same workspace, and you can be signed in to both at once. The web app suits a quick look or a shared computer."
        />
        <div className="mt-site-block">
          <CompareTable columns={["Desktop", "Web"]} rows={COMPARE} />
        </div>
      </Container>
    </Section>
  );
}

const PLATFORMS: { name: string; detail: string; formats: string[]; icon: ComponentType<{ size?: number }> }[] = [
  { name: "macOS", detail: "Apple silicon and Intel", formats: [".dmg"], icon: AppleIcon },
  { name: "Windows", detail: "x64 and Arm", formats: [".exe"], icon: WindowsIcon },
  { name: "Linux", detail: "x64 and Arm", formats: [".AppImage", ".deb", ".rpm"], icon: LinuxIcon },
];

function Downloads() {
  return (
    <Section>
      <Container className="grid gap-site-block lg:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)] lg:gap-16">
        <SectionHeader
          title="For the computer you have"
          lead="Desktop updates itself in the background. Restart to install a new version, or let it install the next time you quit."
        />
        <div>
          <ul className="divide-y divide-site-line border-y border-site-line">
            {PLATFORMS.map((platform) => (
              <li key={platform.name} className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-4 py-5">
                <IconTile>
                  <platform.icon size={18} />
                </IconTile>
                <span>
                  <span className="block text-site-card text-site-ink">{platform.name}</span>
                  <span className="mt-1 block text-site-small text-site-muted">{platform.detail}</span>
                </span>
                <span className="flex flex-wrap justify-end gap-1.5">
                  {platform.formats.map((format) => (
                    <span
                      key={format}
                      className="rounded-site-control bg-site-sunken px-2 py-0.5 font-site-mono text-site-small text-site-ink-secondary"
                    >
                      {format}
                    </span>
                  ))}
                </span>
              </li>
            ))}
          </ul>
          <p className="mt-4 text-site-small text-site-muted">
            On Linux, automatic updates work with the .AppImage. Update a .deb or .rpm by installing the new package.
          </p>
          <ActionLink href="/download" tone="ghost" className="mt-6 -ml-3">
            All downloads
            <ArrowRight className="size-4" aria-hidden />
          </ActionLink>
        </div>
      </Container>
    </Section>
  );
}

const DESKTOP_CONFIG = `{
  "schemaVersion": 1,
  "apiUrl": "https://api.example.com"
}`;

function SelfHosted() {
  const docsHref = useDesktopDocsHref();
  return (
    <Section>
      <Container className="grid items-start gap-site-block lg:grid-cols-2 lg:gap-16">
        <div>
          <h2 className="font-site-display text-site-heading text-balance">
            <span className="block text-site-ink">Running your own Multica?</span>
            <span className="block text-site-muted">Point Desktop at it.</span>
          </h2>
          <p className="mt-3 max-w-[30rem] text-site-body text-site-muted text-pretty">
            Desktop connects to Multica Cloud by default. Add a small file with your server’s address and restart the app.
          </p>
          <ActionLink href={`${docsHref}#connecting-to-a-self-hosted-instance`} tone="ghost" className="mt-6 -ml-3">
            Self-hosted setup
            <ArrowUpRight className="size-4" aria-hidden />
          </ActionLink>
        </div>
        <CodeBlock variants={[{ label: "~/.multica/desktop.json", code: DESKTOP_CONFIG }]} />
      </Container>
    </Section>
  );
}
