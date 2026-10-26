"use client";

import { useEffect, useRef, useState, type ComponentType, type CSSProperties } from "react";
import Link from "next/link";
import { ArrowRight, ArrowUpRight, ChevronRight, ChevronsLeftRight } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { BorderBeam } from "@multica/ui/components/common/border-beam";
import { useAuthStore } from "@multica/core/auth";
import { useDashboardCtaHref } from "../utils/use-dashboard-cta";
import { FAQSection } from "../components/faq-section";
import { anchorId } from "../components/changelog-page-client";
import {
  ActionLink,
  Container,
  Eyebrow,
  Section,
  SectionHeader,
  focusRing,
} from "./primitives";
import { HERO_PROVIDERS } from "../components/provider-marks";
import { docsHrefForLocale, useLocale } from "../i18n";
import { CUSTOMER_LOGOS } from "./customer-logos";
import { AutopilotsVisual, ChatVisual, RuntimesVisual, SkillsVisual, WorkflowVisual } from "./demo/feature-visuals";
import { HeroDemo } from "./demo/hero-demo";
import { PROVIDER_MARKS, ZoomCanvas, usePrefersReducedMotion, useStoryLoop } from "./demo/mock-kit";
import {
  DigestAfter,
  DigestBefore,
  PipelineAfter,
  PipelineBefore,
  STAGE_CANVAS,
  TabsAfter,
  TabsBefore,
} from "./demo/before-after-visuals";
import { CtaBand, SitePage } from "./site-page";
import { AdvanceLine, useAutoAdvance } from "./auto-advance";
import { PLACEHOLDER_PAGES } from "./sitemap";

export function HomePage() {
  return (
    <SitePage>
      <Hero />
      <LogoWall />
      <div className="site-rails">
        <BeforeAfter />
        <Features />
        <Agents />
        <Updates />
        <FAQSection />
        <CtaBand />
      </div>
    </SitePage>
  );
}

function Hero() {
  const user = useAuthStore((s) => s.user);
  const ctaHref = useDashboardCtaHref();

  return (
    <section className="pt-site-hero pb-site-section">
      <Container className="max-w-site-wide">
        <Link
          href={`/${PLACEHOLDER_PAGES.blog.slug}`}
          className={cn(
            "inline-flex items-center gap-2 rounded-site-control border border-site-line bg-site-surface py-1 pr-3 pl-1 text-site-small text-site-ink-secondary transition-colors hover:border-site-line-strong",
            focusRing,
          )}
        >
          <span className="rounded-site-control bg-site-sunken px-2 py-0.5 text-site-ink">New</span>
          Announcement placeholder
          <ArrowRight className="size-3.5 text-site-muted" aria-hidden />
        </Link>
        <HeroHeadline />
        <p className="mt-3 max-w-[38rem] text-site-body text-site-muted text-pretty">
          One workspace for your team and its agents: assign the work, track it, and review it. You stay in control.
        </p>
        <div className="mt-6 flex flex-wrap gap-2.5">
          <ActionLink href={ctaHref} size="lg">
            {user ? "Open dashboard" : "Get started"}
          </ActionLink>
          <ActionLink href="/contact-sales" size="lg" tone="secondary">
            Book a demo
          </ActionLink>
        </div>
      </Container>
      <Container className="mt-10 max-w-site-wide lg:mt-12">
        <HeroDemo />
      </Container>
    </section>
  );
}

const HERO_AGENTS = ["Claude Code", "Codex", "Hermes", "OpenCode"] as const;
const HERO_AGENT_HOLDS = HERO_AGENTS.map(() => 3600);

function HeroHeadline() {
  const { ref, step } = useStoryLoop(HERO_AGENT_HOLDS, 0);
  const measures = useRef<(HTMLSpanElement | null)[]>([]);
  const [widths, setWidths] = useState<number[] | null>(null);

  useEffect(() => {
    const items = measures.current.filter((el): el is HTMLSpanElement => el !== null);
    const measure = () => setWidths(items.map((el) => el.getBoundingClientRect().width));
    const resize = new ResizeObserver(measure);
    items.forEach((el) => resize.observe(el));
    return () => resize.disconnect();
  }, []);

  return (
    <div ref={ref}>
      <h1 className="mt-5 font-site-display text-site-heading text-site-ink">
        <span className="sr-only">
          Turn {HERO_AGENTS.slice(0, -1).join(", ")}, or {HERO_AGENTS.at(-1)} into a teammate.
        </span>
        <span aria-hidden className="block text-balance">
          Turn{" "}
          <span className="relative inline-flex rounded-full bg-brand/5 px-[0.36em] [--border-beam-width:1.5px]">
            <span
              className="inline-grid overflow-hidden transition-[width] duration-500 ease-[cubic-bezier(0.22,1,0.36,1)] motion-reduce:transition-none"
              style={{ width: widths?.[step] }}
            >
              {HERO_AGENTS.map((name, index) => (
                <span
                  key={name}
                  ref={(el) => {
                    measures.current[index] = el;
                  }}
                  className={cn(
                    "col-start-1 row-start-1 inline-flex w-max items-center gap-[0.2em] whitespace-nowrap transition-[opacity,filter] ease-out motion-reduce:transition-none",
                    index === step ? "opacity-100 blur-none delay-150 duration-300" : "opacity-0 blur-[3px] duration-150",
                  )}
                >
                  <ProviderMark name={name} />
                  {name}
                </span>
              ))}
            </span>
            <BorderBeam />
          </span>{" "}
          into a teammate.
        </span>
      </h1>
    </div>
  );
}

function ProviderMark({ name }: { name: string }) {
  const Mark = PROVIDER_MARKS.get(name);
  return Mark ? <Mark className="size-[0.72em] shrink-0" /> : null;
}

function LogoWall() {
  return (
    <section aria-labelledby="logo-wall-title" className="pb-site-section">
      <Container className="max-w-site-wide">
        <h2 id="logo-wall-title" className="text-center text-site-small text-site-muted">
          Used by engineers at
        </h2>
        <div className="group/wall mt-6 overflow-hidden mask-x-from-85% mask-x-to-100% motion-reduce:mask-none">
          <div className="flex w-max animate-site-marquee group-hover/wall:[animation-play-state:paused] motion-reduce:w-auto motion-reduce:animate-none">
            {[0, 1].map((copy) => (
              <ul
                key={copy}
                aria-hidden={copy === 1 || undefined}
                className={cn(
                  "flex shrink-0 items-center gap-x-8 pr-8 sm:gap-x-12 sm:pr-12 motion-reduce:w-full motion-reduce:flex-wrap motion-reduce:justify-center motion-reduce:gap-y-4 motion-reduce:pr-0 sm:motion-reduce:gap-y-6",
                  copy === 1 && "motion-reduce:hidden",
                )}
              >
                {CUSTOMER_LOGOS.map((logo) => (
                  <li
                    key={logo.name}
                    style={{ "--logo-color": logo.color } as CSSProperties}
                    className="group/logo flex h-10 items-center gap-2 text-site-muted transition-colors duration-200 hover:text-site-ink"
                  >
                    <svg
                      viewBox="0 0 24 24"
                      aria-hidden
                      className="size-5 fill-current sm:size-6 transition-colors duration-200 group-hover/logo:fill-(--logo-color)"
                    >
                      <path d={logo.path} />
                    </svg>
                    <span className="text-[0.9375rem] font-semibold sm:text-[1.0625rem] tracking-tight whitespace-nowrap">{logo.name}</span>
                  </li>
                ))}
              </ul>
            ))}
          </div>
        </div>
      </Container>
    </section>
  );
}

const FEATURES: { id: string; title: string; body: string; alt: string; visual: ComponentType }[] = [
  {
    id: "runtimes",
    title: "Runs on your machines, or in our cloud",
    body: "Connect a laptop or a server with one command and Multica finds the coding agents installed on it. Or run them in managed cloud sandboxes.",
    alt: "The Runtimes page listing three machines and Multica Cloud, all online, with the coding agents on each and what they are running",
    visual: RuntimesVisual,
  },
  {
    id: "workflows",
    title: "Workflows hand off every step",
    body: "Give every status an owner. Moving an issue to the next step hands it to that step’s agent or teammate.",
    alt: "The Ship a feature workflow: Spec handed to Lead Agent, Build to Backend Agent, Code review to QA Agent, Approve to Mia Chen, then Shipped, with the Build step's handoff settings open",
    visual: WorkflowVisual,
  },
  {
    id: "chat",
    title: "Mention an agent in chat",
    body: "From Slack, Lark, DingTalk, WeCom, or Telegram. The agent reads the thread, files the issue, and gets to work.",
    alt: "A team chat channel where Mia mentions Backend Agent about failing exports, and the agent replies in the channel with a link to the Multica issue it filed and is working on",
    visual: ChatVisual,
  },
  {
    id: "autopilots",
    title: "Autopilots run on a schedule",
    body: "Upgrades, triage, and reports file themselves and land with an agent, every hour or every Monday.",
    alt: "The Autopilots list: four scheduled jobs, each with its schedule, its agent, and its next run",
    visual: AutopilotsVisual,
  },
  {
    id: "skills",
    title: "Skills the whole team shares",
    body: "Write down how your team does a thing once, and every agent follows it.",
    alt: "A Write release notes skill with its rules, used by four agents",
    visual: SkillsVisual,
  },
];

function Features() {
  return (
    <Section>
      <Container>
        <SectionHeader title="Built for teams of people and agents" />
        <div className="mt-site-block grid gap-x-site-grid-col gap-y-site-grid-row md:grid-cols-2">
          {FEATURES.slice(0, 2).map((feature) => (
            <FeatureCard key={feature.id} {...feature} />
          ))}
        </div>
        <div className="mt-site-grid-row grid gap-x-site-grid-col gap-y-site-grid-row md:grid-cols-3">
          {FEATURES.slice(2).map((feature) => (
            <FeatureCard key={feature.id} {...feature} />
          ))}
        </div>
      </Container>
    </Section>
  );
}

const POPULAR_AGENTS = ["Claude Code", "Codex", "Cursor", "GitHub Copilot", "OpenCode", "Hermes", "Antigravity", "Qwen Code"];

function Agents() {
  const agents = POPULAR_AGENTS.map((name) => HERO_PROVIDERS.find((provider) => provider.name === name)!);
  const { locale } = useLocale();
  const docsHref = docsHrefForLocale(locale);
  return (
    <Section>
      <Container>
        <h2 className="max-w-[44rem] font-site-display text-site-heading text-balance">
          <span className="block text-site-muted">No new agent to learn.</span>
          <span className="block text-site-ink">Bring the ones you already use.</span>
        </h2>
        <ul className="mt-site-block grid grid-cols-2 gap-y-8 md:grid-cols-4 md:gap-y-10">
          {agents.map(({ name, Mark, size }, index) => (
            <li
              key={name}
              className={cn(
                "flex flex-col gap-8 border-site-line px-4 md:gap-10 md:px-5",
                index % 2 === 1 && "border-l",
                index % 4 !== 0 && "md:border-l",
                index % 4 === 2 && "max-md:border-l-0",
              )}
            >
              <span className="flex size-7 items-center text-site-muted">
                <Mark className={size ?? "size-6"} />
              </span>
              <span className="text-site-body text-site-muted">{name}</span>
            </li>
          ))}
        </ul>
        <ActionLink href={`${docsHref}/providers`} tone="ghost" className="mt-8 ml-1 md:mt-10 md:ml-2">
          All 27 agents
          <ArrowUpRight className="size-4" aria-hidden />
        </ActionLink>
      </Container>
    </Section>
  );
}

const UPDATE_COUNT = 7;

/** Release titles list their changes ("A, B, and C"); the home page leads with the first and counts the rest. */
function releaseHeadline(title: string) {
  const [headline = title, ...rest] = title.split(/, (?:and )?/);
  return { headline, more: rest.length };
}

function Updates() {
  const { t, locale } = useLocale();
  const releases = t.changelog.entries.slice(0, UPDATE_COUNT);
  const latest = releases[0];
  const day = new Intl.DateTimeFormat(locale, { month: "short", day: "numeric", timeZone: "UTC" });
  const dateLabel = (date: string) => day.format(new Date(`${date}T00:00:00Z`));
  if (!latest) return null;

  return (
    <Section>
      <Container className="grid gap-site-block lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] lg:gap-16">
        <div className="flex flex-col justify-between gap-site-block">
          <SectionHeader eyebrow="Changelog" title="Recently shipped." />
          <Link
            href={`/changelog#${anchorId(latest.version)}`}
            data-frost="changelog"
            className={cn(
              "site-frost group hidden max-w-[20rem] flex-col gap-8 rounded-site-card p-5 lg:flex",
              focusRing,
            )}
          >
            <span className="relative text-site-small text-site-ink-secondary">Latest · {dateLabel(latest.date)}</span>
            <span className="relative">
              <span className="block font-site-display text-site-heading text-site-ink">v{latest.version}</span>
              <span className="mt-1 block text-site-body text-site-ink-secondary">{releaseHeadline(latest.title).headline}</span>
            </span>
          </Link>
        </div>
        <div>
          <ul className="divide-y divide-site-line border-y border-site-line">
            {releases.map((release) => (
              <li key={release.version}>
                <Link
                  href={`/changelog#${anchorId(release.version)}`}
                  className={cn(
                    "group grid grid-cols-[4rem_minmax(0,1fr)_auto] items-baseline gap-4 py-4 text-site-body sm:grid-cols-[5rem_minmax(0,1fr)_4rem_auto]",
                    focusRing,
                  )}
                >
                  <span className="text-site-muted tabular-nums">{dateLabel(release.date)}</span>
                  <span className="line-clamp-2 text-site-ink sm:truncate">
                    {releaseHeadline(release.title).headline}
                    {releaseHeadline(release.title).more > 0 ? (
                      <span className="text-site-muted"> +{releaseHeadline(release.title).more} more</span>
                    ) : null}
                  </span>
                  <span className="text-site-small text-site-muted tabular-nums max-sm:hidden">v{release.version}</span>
                  <ChevronRight
                    aria-hidden
                    className="size-4 self-center text-site-muted transition-transform group-hover:translate-x-0.5"
                  />
                </Link>
              </li>
            ))}
          </ul>
          <ActionLink href="/changelog" tone="ghost" className="mt-6 -ml-3">
            All releases
            <ArrowRight className="size-4" aria-hidden />
          </ActionLink>
        </div>
      </Container>
    </Section>
  );
}

function FeatureCard({ id, title, body, visual: Visual, alt }: (typeof FEATURES)[number]) {
  return (
    <div>
      <div role="img" aria-label={alt} data-frost={id} className="site-frost aspect-[4/3] rounded-site-panel">
        <Visual />
      </div>
      <h3 className="mt-4 text-site-card text-site-ink">{title}</h3>
      <p className="mt-site-stack max-w-[32rem] text-site-body text-site-muted">{body}</p>
    </div>
  );
}

type Outcome = { value: string; caption: string };

const STORIES: {
  id: string;
  title: string;
  outcomes: [Outcome, Outcome];
  before: string;
  after: string;
  scenes: [ComponentType, ComponentType];
}[] = [
  {
    id: "board",
    title: "Thirty agents, thirty tabs",
    outcomes: [
      { value: "30 tabs", caption: "to check for the agent that’s stuck" },
      { value: "1 inbox", caption: "tells you which agent needs you" },
    ],
    before: "Every agent runs in its own terminal tab, and a question waits in the one you aren’t watching.",
    after: "All their work sits on one board, and anything waiting on you lands in your inbox.",
    scenes: [TabsBefore, TabsAfter],
  },
  {
    id: "digest",
    title: "The work nobody has time to do",
    outcomes: [
      { value: "2%", caption: "of support conversations get read" },
      { value: "100%", caption: "read every night by an agent" },
    ],
    before: "Someone spot-checks a few dozen support conversations a day. The rest are never read.",
    after: "An autopilot reads every one overnight and files what needs fixing. You review the digest in the morning.",
    scenes: [DigestBefore, DigestAfter],
  },
  {
    id: "pipeline",
    title: "A process that waits on people",
    outcomes: [
      { value: "9 days", caption: "per fix, 4 of them spent waiting" },
      { value: "3 days", caption: "agents hand off, people approve" },
    ],
    before: "Each step of a fix waits for someone to notice it’s their turn.",
    after: "Agents hand each step to the next, and people step in only to approve.",
    scenes: [PipelineBefore, PipelineAfter],
  },
];

const OUTCOME_LABELS = ["Before", "With Multica"] as const;

const STORY_ENTER = "animate-in fade-in duration-500 motion-reduce:animate-none";

const STORY_HOLD = 8000;

function BeforeAfter() {
  const cycle = useAutoAdvance(STORIES.length);
  const story = STORIES[cycle.active]!;

  return (
    <Section>
      <Container>
        <SectionHeader
          title="What changes when agents join your team"
          lead="Three stories from teams that moved their agent work into Multica."
        />
        <div
          ref={cycle.ref}
          className="mt-site-block grid gap-8 xl:grid-cols-[minmax(0,19rem)_minmax(0,1fr)] xl:gap-14"
        >
          <ol {...cycle.holdProps} className="order-2 flex flex-col xl:order-1">
            {STORIES.map((item, index) => {
              const selected = index === cycle.active;
              return (
                <li key={item.id} className="relative border-t border-site-line last:border-b">
                  <AdvanceLine
                    selected={selected}
                    playing={cycle.playing}
                    running={cycle.running}
                    duration={STORY_HOLD}
                    onDone={cycle.advance}
                  />
                  <button
                    type="button"
                    aria-pressed={selected}
                    onClick={() => cycle.select(index)}
                    className={cn(
                      "w-full py-5 text-left text-site-card transition-colors",
                      selected ? "text-site-ink" : "text-site-muted hover:text-site-ink",
                      focusRing,
                    )}
                  >
                    {item.title}
                  </button>
                </li>
              );
            })}
          </ol>
          <div key={story.id} className={cn("order-1 xl:order-2", STORY_ENTER)}>
            <dl className="grid grid-cols-2">
              {story.outcomes.map((outcome, index) => (
                <div key={OUTCOME_LABELS[index]} className={index === 0 ? "pr-4 sm:pr-6" : "border-l border-site-line pl-4 sm:pl-6"}>
                  <dt className="text-site-small text-site-muted">{OUTCOME_LABELS[index]}</dt>
                  <dd
                    className={cn(
                      "mt-2 font-site-display text-site-heading tabular-nums",
                      index === 0 ? "text-site-muted" : "text-site-ink",
                    )}
                  >
                    {outcome.value}
                  </dd>
                  <dd className="mt-1.5 text-site-small text-site-muted text-pretty">{outcome.caption}</dd>
                </div>
              ))}
            </dl>
            <CompareStage story={story} onDrag={cycle.stop} className="mt-6 sm:mt-8" />
          </div>
        </div>
      </Container>
    </Section>
  );
}

function CompareStage({ story, onDrag, className }: { story: (typeof STORIES)[number]; onDrag: () => void; className?: string }) {
  const [Before, After] = story.scenes;
  const ref = useRef<HTMLDivElement>(null);
  const [split, setSplit] = useState(100);
  const [sweeping, setSweeping] = useState(false);
  const reducedMotion = usePrefersReducedMotion();

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const intersection = new IntersectionObserver(
      ([entry]) => {
        if (!entry?.isIntersecting) return;
        intersection.disconnect();
        if (reducedMotion) {
          setSplit(50);
          return;
        }
        timer = setTimeout(() => {
          setSweeping(true);
          setSplit(50);
        }, 500);
      },
      { threshold: 0.5 },
    );
    intersection.observe(el);
    return () => {
      intersection.disconnect();
      clearTimeout(timer);
    };
  }, [reducedMotion]);

  const motion = sweeping && "transition-[clip-path,left] duration-[1400ms] ease-[cubic-bezier(0.65,0,0.35,1)]";

  return (
    <div
      ref={ref}
      role="group"
      aria-label={story.title}
      className={cn("relative aspect-[4/3] overflow-hidden rounded-site-panel select-none sm:aspect-[3/2]", className)}
    >
      <p className="sr-only">
        Before: {story.before} With Multica: {story.after}
      </p>
      <div className="absolute inset-0">
        <div data-frost={story.id} className="site-frost size-full">
          <ZoomCanvas {...STAGE_CANVAS}>
            <After />
          </ZoomCanvas>
        </div>
      </div>
      <div
        className={cn("absolute inset-0", motion)}
        style={{ clipPath: `inset(0 ${100 - split}% 0 0)` }}
        onTransitionEnd={() => setSweeping(false)}
      >
        <div className="site-visual size-full">
          <ZoomCanvas {...STAGE_CANVAS}>
            <Before />
          </ZoomCanvas>
        </div>
      </div>
      <input
        type="range"
        min={0}
        max={100}
        value={split}
        aria-label="Compare before and with Multica"
        aria-valuetext={`${split}% before`}
        onChange={(event) => {
          onDrag();
          setSweeping(false);
          setSplit(Number(event.target.value));
        }}
        className="peer absolute inset-0 size-full cursor-ew-resize appearance-none opacity-0"
      />
      <div
        aria-hidden
        className={cn(
          "pointer-events-none absolute inset-y-0 w-px bg-site-surface",
          "peer-focus-visible:*:outline-2 peer-focus-visible:*:outline-offset-2 peer-focus-visible:*:outline-site-accent",
          motion,
        )}
        style={{ left: `${split}%` }}
      >
        <span className="absolute top-1/2 left-1/2 flex size-9 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full bg-site-surface text-site-ink shadow-site-float ring-1 ring-site-line">
          <ChevronsLeftRight className="size-4" />
        </span>
      </div>
    </div>
  );
}
