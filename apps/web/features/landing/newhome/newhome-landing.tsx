"use client";

import Link from "next/link";
import dynamic from "next/dynamic";
import { Download, Star } from "lucide-react";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { cn } from "@multica/ui/lib/utils";
import { ProviderLogo } from "@multica/views/runtimes";
import { githubUrl } from "../components/shared";
import { DEMO_ZOOM } from "./demo/zoom";

// The interactive product demo is heavy (the whole issues board subsystem) and
// must stay client-only — it overrides the API singleton with a mock and uses
// browser-only providers, so it can't server-render. Lazy-load it so it never
// blocks the landing's first paint.
const DemoBoard = dynamic(
  () => import("./demo/demo-board").then((m) => m.DemoBoard),
  {
    ssr: false,
    loading: () => (
      <div className="flex h-full w-full items-center justify-center bg-white text-[14px] text-[#0a0d12]/40">
        Loading live demo…
      </div>
    ),
  },
);

// Value #1's auto-playing board micro-demo. Client-only for the same reason as
// DemoBoard (mock API singleton + browser-only providers).
const ValueBoardDemo = dynamic(
  () => import("./demo/value-board-demo").then((m) => m.ValueBoardDemo),
  {
    ssr: false,
    loading: () => <div className="h-[360px]" />,
  },
);

// GitHub Invertocat (official mark). lucide-react dropped its brand icons, so we
// inline the silhouette here rather than depend on a removed export.
function GitHubIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden className={className}>
      <path d="M12 1C5.9225 1 1 5.9225 1 12C1 16.8675 4.14875 20.9787 8.52125 22.4362C9.07125 22.5325 9.2775 22.2025 9.2775 21.9137C9.2775 21.6525 9.26375 20.7862 9.26375 19.865C6.5 20.3737 5.785 19.1912 5.565 18.5725C5.44125 18.2562 4.905 17.28 4.4375 17.0187C4.0525 16.8125 3.5025 16.3037 4.42375 16.29C5.29 16.2762 5.90875 17.0875 6.115 17.4175C7.105 19.0812 8.68625 18.6137 9.31875 18.325C9.415 17.61 9.70375 17.1287 10.02 16.8537C7.5725 16.5787 5.015 15.63 5.015 11.4225C5.015 10.2262 5.44125 9.23625 6.1425 8.46625C6.0325 8.19125 5.6475 7.06375 6.2525 5.55125C6.2525 5.55125 7.17375 5.2625 9.2775 6.67875C10.1575 6.43125 11.0925 6.3075 12.0275 6.3075C12.9625 6.3075 13.8975 6.43125 14.7775 6.67875C16.8813 5.24875 17.8025 5.55125 17.8025 5.55125C18.4075 7.06375 18.0225 8.19125 17.9125 8.46625C18.6138 9.23625 19.04 10.2125 19.04 11.4225C19.04 15.6437 16.4688 16.5787 14.0213 16.8537C14.42 17.1975 14.7638 17.8575 14.7638 18.8887C14.7638 20.36 14.75 21.5425 14.75 21.9137C14.75 22.2025 14.9563 22.5462 15.5063 22.4362C19.8513 20.9787 23 16.8537 23 12C23 5.9225 18.0775 1 12 1Z" />
    </svg>
  );
}

// Static for now — the repo currently has ~35k stars. Swap for a live
// GitHub API count later if we want it to self-update.
const GITHUB_STAR_COUNT = "35k";

// Embedded product demo: a slightly-shrunk window (~16:9). transform: scale on
// an inner box sized up by 1/scale, with the window height clamped so the
// un-scaled layout box doesn't leave dead space. (transform handles the board's
// drag better than zoom.) DEMO_ZOOM is shared with the value-section demos so
// every embedded board renders at one uniform scale.
const DEMO_WINDOW_H = 648;

/**
 * Multica Landing Page V2 (sandbox) — served at `/newhome`.
 *
 * Isolated rebuild of the landing hero. Shares nothing with the live landing
 * (`/`), so we can iterate freely here and only swap it in once the V2 design
 * is finalized.
 *
 * Hero copy follows the homepage positioning from MUL-2920:
 *   slogan   — "One board for all your agents."
 *   subtitle — assign work, track progress, automate execution.
 *
 * Layout follows the ElevenLabs reference: sans-serif headline on the left,
 * description on the right, a single pair of CTAs below, full-width product
 * preview (placeholder for now).
 */
export function NewHomeLanding() {
  return (
    <div className="min-h-screen bg-white font-sans text-[#0a0d12]">
      <NewHomeNav />
      <NewHomeHero />
    </div>
  );
}

// Top-level information architecture (see MUL-2932 menu IA):
// homepage · features · enterprise · pricing · resources(docs/changelog/…)
const NAV_LINKS = [
  { href: "#features", label: "Features" },
  { href: "#enterprise", label: "Enterprise" },
  { href: "#pricing", label: "Pricing" },
  { href: "#resources", label: "Resources" },
];

function NewHomeNav() {
  return (
    <header className="sticky top-0 z-30 bg-white/80 backdrop-blur-md">
      <div className="mx-auto flex h-[72px] max-w-[1200px] items-center justify-between px-5 sm:px-6 lg:px-8">
        <div className="flex items-center gap-8">
          <Link href="/newhome" className="flex shrink-0 items-center gap-2.5">
            <MulticaIcon className="size-5 text-[#0a0d12]" noSpin />
            <span className="text-[19px] font-semibold lowercase tracking-[0.04em]">
              multica
            </span>
          </Link>
          <nav aria-label="Primary" className="hidden items-center gap-1 md:flex">
            {NAV_LINKS.map((link) => (
              <Link
                key={link.href}
                href={link.href}
                className="inline-flex h-9 items-center rounded-[9px] px-3 text-[13.5px] font-medium text-[#0a0d12]/62 transition-colors hover:bg-[#0a0d12]/[0.05] hover:text-[#0a0d12]"
              >
                {link.label}
              </Link>
            ))}
          </nav>
        </div>

        <div className="flex items-center gap-1.5 sm:gap-2">
          <GitHubStars />
          <Link href="/login" className={navButton("ghost")}>
            Sign in
          </Link>
          <Link href="/download" className={navButton("solid")}>
            Download
          </Link>
        </div>
      </div>
    </header>
  );
}

function GitHubStars() {
  return (
    <Link
      href={githubUrl}
      target="_blank"
      rel="noreferrer"
      aria-label={`Star Multica on GitHub — ${GITHUB_STAR_COUNT} stars`}
      className="hidden items-center gap-2 rounded-[8px] px-2.5 py-1.5 text-[13px] font-semibold text-[#0a0d12]/70 transition-colors hover:bg-[#0a0d12]/[0.05] hover:text-[#0a0d12] sm:inline-flex"
    >
      <GitHubIcon className="size-[18px] text-[#0a0d12]" />
      <span className="inline-flex items-center gap-1">
        <Star className="size-3.5 fill-[#f5a623] text-[#f5a623]" aria-hidden />
        {GITHUB_STAR_COUNT}
      </span>
    </Link>
  );
}

function NewHomeHero() {
  return (
    <main>
      <section className="mx-auto max-w-[1200px] px-5 pb-14 pt-10 sm:px-6 sm:pt-12 lg:px-8 lg:pt-16">
        <div className="flex flex-col gap-8 lg:flex-row lg:items-end lg:justify-between lg:gap-16">
          <h1 className="max-w-[14ch] text-[2.4rem] font-semibold leading-[1.03] tracking-[-0.03em] sm:text-[3rem] lg:text-[3.55rem]">
            One board for all your agents.
          </h1>
          <p className="max-w-[440px] text-[16px] leading-7 text-[#0a0d12]/60 sm:text-[17px] lg:pb-2">
            Assign work, track progress, and automate execution across Claude
            Code, Codex, and every agent you run.
          </p>
        </div>

        <div className="mt-9 flex flex-wrap items-center gap-3">
          <Link href="/download" className={heroButton("solid")}>
            <Download className="size-4" aria-hidden />
            Download Desktop
          </Link>
          <Link href="/contact-sales" className={heroButton("ghost")}>
            Talk to sales
          </Link>
        </div>
      </section>

      <section className="mx-auto max-w-[1200px] px-4 pb-16 sm:px-5 lg:px-6">
        <ProductPreviewPlaceholder />
      </section>

      <SupportedAgents />
      <ValuesSection />
    </main>
  );
}

// The values section turns the hero's promise into concrete, watchable proof.
// Each value pairs a short From→To claim with a focused, auto-playing micro-demo
// built from the REAL product components. Value #1 ships first; #2–#4 follow.
function ValuesSection() {
  return (
    <section id="features" className="border-t border-[#0a0d12]/8 bg-[#0a0d12]/[0.015] py-20 sm:py-24">
      <div className="mx-auto max-w-[1200px] px-5 sm:px-6 lg:px-8">
        <h2 className="max-w-[20ch] text-[1.9rem] font-semibold leading-[1.08] tracking-[-0.025em] sm:text-[2.3rem]">
          From scattered agent runs to work you can actually manage.
        </h2>

        <ValueBlock
          eyebrow="Visibility"
          title="See every agent on one board"
          from="Agent work scattered across terminals, chats, repos, and scripts."
          to="Every task — queued, running, in review, done — on one board you can watch in real time."
        >
          <ValueBoardDemo />
        </ValueBlock>
      </div>
    </section>
  );
}

// Value layout: a compact text column (eyebrow + title + From→To) above a
// full-width canvas, so the board demo gets the room to render at the real
// product's size and read as the same board the hero shows.
function ValueBlock({
  eyebrow,
  title,
  from,
  to,
  children,
}: {
  eyebrow: string;
  title: string;
  from: string;
  to: string;
  children: React.ReactNode;
}) {
  return (
    <div className="mt-12 lg:mt-16">
      <div className="max-w-[760px]">
        <p className="text-[12.5px] font-semibold uppercase tracking-[0.08em] text-[#0a0d12]/40">
          {eyebrow}
        </p>
        <h3 className="mt-2.5 text-[1.55rem] font-semibold leading-[1.12] tracking-[-0.02em]">
          {title}
        </h3>
        <dl className="mt-4 flex flex-col gap-2.5 sm:flex-row sm:gap-8">
          <div className="flex gap-3">
            <dt className="mt-0.5 shrink-0 text-[11px] font-semibold uppercase tracking-[0.06em] text-[#0a0d12]/35">
              From
            </dt>
            <dd className="text-[14.5px] leading-6 text-[#0a0d12]/55">{from}</dd>
          </div>
          <div className="flex gap-3">
            <dt className="mt-0.5 shrink-0 text-[11px] font-semibold uppercase tracking-[0.06em] text-[var(--brand)]">
              To
            </dt>
            <dd className="text-[14.5px] font-medium leading-6 text-[#0a0d12]/80">{to}</dd>
          </div>
        </dl>
      </div>

      {/* Full-width demo canvas. landing-demo scopes the brand override +
          scrollbar hiding the product components expect. */}
      <div className="landing-demo mt-7 overflow-hidden rounded-[14px] border border-[#0a0d12]/10 bg-white p-3 shadow-[0_1px_3px_rgba(10,13,18,0.04)] sm:p-4">
        {children}
      </div>
    </div>
  );
}

// Until we have permission to display customer logos, the "logo wall" shows the
// coding agents Multica already supports instead — mirroring the reference
// social-proof band, one agent per card. Keys match the backend provider keys
// (server/pkg/agent/models.go) so ProviderLogo renders the right mark; any key
// without a logo falls back to a generic placeholder icon.
const SUPPORTED_AGENTS = [
  { key: "claude", name: "Claude Code" },
  { key: "codex", name: "Codex" },
  { key: "gemini", name: "Gemini CLI" },
  { key: "cursor", name: "Cursor" },
  { key: "copilot", name: "GitHub Copilot" },
  { key: "opencode", name: "OpenCode" },
  { key: "openclaw", name: "OpenClaw" },
  { key: "hermes", name: "Hermes" },
  { key: "kimi", name: "Kimi" },
  { key: "kiro", name: "Kiro" },
  { key: "pi", name: "Pi" },
  { key: "antigravity", name: "Antigravity" },
];

function SupportedAgents() {
  return (
    <section id="agents" className="pb-24">
      <p className="px-5 text-center text-[15px] text-[#0a0d12]/55 sm:px-6 lg:px-8">
        Works with the coding agents you already run
      </p>
      {/* Auto-scrolling marquee. overflow-hidden = no scrollbar; the track is two
          identical groups sliding left by one group width for a seamless loop. */}
      <div className="newhome-marquee mt-8 overflow-hidden">
        <div className="newhome-marquee-track flex w-max">
          <AgentTrackGroup />
          <AgentTrackGroup ariaHidden />
        </div>
      </div>
    </section>
  );
}

function AgentTrackGroup({ ariaHidden = false }: { ariaHidden?: boolean }) {
  return (
    <ul
      className="flex shrink-0 gap-3 pr-3"
      aria-hidden={ariaHidden || undefined}
    >
      {SUPPORTED_AGENTS.map(({ key, name }) => (
        <li
          key={key}
          className="group flex h-[84px] w-[172px] shrink-0 items-center justify-center gap-2.5 rounded-[8px] bg-[#0a0d12]/[0.03] text-[#0a0d12]/85"
        >
          {/* Grayscale by default; full brand color on hover of this card. */}
          <ProviderLogo
            provider={key}
            className="size-6 grayscale transition-[filter] duration-200 group-hover:grayscale-0"
          />
          <span className="text-[15px] font-semibold tracking-[-0.01em]">
            {name}
          </span>
        </li>
      ))}
    </ul>
  );
}

function ProductPreviewPlaceholder() {
  return (
    // Live, interactive product demo (mock data): browser tabs (Issues /
    // Agents / Skills), drag cards, click a card to open its issue page. The
    // browser chrome + tabs live inside DemoBoard. No drop shadow by request.
    // The demo is laid out on a larger canvas (1/scale) then scaled down so it
    // shows more content at a smaller size while still filling the 620px
    // window. transform: scale (not zoom) so it clips to its visual bounds and
    // never overflows the window.
    <div className="relative">
      <DemoLiveHint />
      <div
        className="overflow-hidden rounded-[12px] border border-[#0a0d12]/12 bg-white"
        style={{ height: DEMO_WINDOW_H }}
      >
        <div
          className="origin-top-left"
          style={{
            transform: `scale(${DEMO_ZOOM})`,
            width: `${100 / DEMO_ZOOM}%`,
            height: `${DEMO_WINDOW_H / DEMO_ZOOM}px`,
          }}
        >
          <DemoBoard />
        </div>
      </div>
    </div>
  );
}

// Playful "this is live, try it" annotation in the whitespace above the demo's
// top-right — so the interactive demo doesn't read as a static screenshot. The
// arrow draws itself in and the whole hint gently floats (see custom.css).
function DemoLiveHint() {
  return (
    <div
      aria-hidden
      className="newhome-hint pointer-events-none absolute -top-[58px] right-3 z-10 hidden items-end gap-1 lg:flex"
    >
      <span
        className="max-w-[280px] pb-2 text-right font-[family-name:var(--font-hand)] text-[22px] leading-[1.1] text-[#0a0d12]/55"
      >
        not a screenshot — it&rsquo;s live. drag a card, try it!
      </span>
      <svg
        viewBox="0 0 64 72"
        fill="none"
        className="newhome-hint-arrow size-[56px] shrink-0 text-[#0a0d12]/40"
      >
        {/* hand-drawn curve sweeping down into the demo's top-right */}
        <path
          d="M47 7c11 16 7 31-9 41"
          stroke="currentColor"
          strokeWidth="2.4"
          strokeLinecap="round"
        />
        {/* arrowhead pointing down-left */}
        <path
          d="M38 41l-3 9 10-1"
          stroke="currentColor"
          strokeWidth="2.4"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </div>
  );
}

function navButton(tone: "solid" | "ghost") {
  return cn(
    "inline-flex h-9 items-center justify-center rounded-[8px] px-3.5 text-[13.5px] font-semibold transition-colors",
    tone === "solid"
      ? "bg-[#0a0d12] text-white hover:bg-[#0a0d12]/90"
      : "border border-[#0a0d12]/14 bg-white text-[#0a0d12] hover:bg-[#0a0d12]/[0.04]",
  );
}

function heroButton(tone: "solid" | "ghost") {
  return cn(
    "inline-flex items-center justify-center gap-2 rounded-[8px] px-5 py-3 text-[14px] font-semibold transition-colors",
    tone === "solid"
      ? "bg-[#0a0d12] text-white hover:bg-[#0a0d12]/90"
      : "border border-[#0a0d12]/14 bg-white text-[#0a0d12] hover:bg-[#0a0d12]/[0.04]",
  );
}
