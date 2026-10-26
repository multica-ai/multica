"use client";

import { useEffect, useRef, useState, type ComponentType, type CSSProperties } from "react";
import Link from "next/link";
import { ArrowRight, Check } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { BorderBeam } from "@multica/ui/components/common/border-beam";
import { useAuthStore } from "@multica/core/auth";
import { useDashboardCtaHref } from "../utils/use-dashboard-cta";
import { FAQSection } from "../components/faq-section";
import {
  ActionLink,
  Container,
  Section,
  SectionHeader,
  focusRing,
} from "./primitives";
import { CUSTOMER_LOGOS } from "./customer-logos";
import { AutopilotsVisual, ChatVisual, RuntimesVisual, SkillsVisual, WorkflowVisual } from "./demo/feature-visuals";
import { HeroDemo } from "./demo/hero-demo";
import { PROVIDER_MARKS, useStoryLoop } from "./demo/mock-kit";
import {
  DigestAfter,
  DigestBefore,
  PipelineAfter,
  PipelineBefore,
  TabsAfter,
  TabsBefore,
} from "./demo/before-after-visuals";
import { CtaBand, SitePage } from "./site-page";
import { PLACEHOLDER_PAGES, productHref } from "./sitemap";

export function HomePage() {
  return (
    <SitePage>
      <Hero />
      <LogoWall />
      <div className="site-rails">
        <BeforeAfter />
        <Features />
        <Deploy />
        <Customers />
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
      <Container>
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
          One workspace to assign, track, and review work across all your agents — you stay in control.
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
      <Container className="mt-10 lg:mt-12">
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
          Turn {HERO_AGENTS.slice(0, -1).join(", ")}, or {HERO_AGENTS.at(-1)} into a teammate. Build a team of
          humans and agents.
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
        <span aria-hidden className="hidden text-balance text-site-muted sm:block">
          Build a team of humans and agents.
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
      <Container>
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
    alt: "A chat channel where Mia mentions Backend Agent about failing exports and the agent replies with the issue it filed and is working on",
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

type Scene = { body: string; alt: string; visual: ComponentType };

const STORIES: { id: string; title: string; metric: [string, string]; before: Scene; after: Scene }[] = [
  {
    id: "board",
    title: "Thirty agents, thirty tabs",
    metric: ["30 tabs", "1 board"],
    before: {
      body: "Every agent runs in its own terminal tab. You spend the day cycling through them, and a question waits in the one you aren’t looking at.",
      alt: "A terminal with four agent tabs open and 26 more hidden, and a two-hour-old prompt in another tab asking whether to apply a migration",
      visual: TabsBefore,
    },
    after: {
      body: "Every agent’s work is an issue on one shared board. You see who is on what, and anything waiting on you stands out.",
      alt: "The Issues list grouped by status: one issue blocked and waiting for you, three in progress with agents working, one in review",
      visual: TabsAfter,
    },
  },
  {
    id: "digest",
    title: "The work nobody has time to do",
    metric: ["Spot checks", "every conversation"],
    before: {
      body: "Thousands of support conversations come in every day. Someone spot-checks a few dozen, and the rest are never read.",
      alt: "A spreadsheet of 2,418 support conversations where only 40 are marked reviewed",
      visual: DigestBefore,
    },
    after: {
      body: "An autopilot reads every conversation overnight and files what needs fixing. You review the digest in the morning.",
      alt: "A daily support digest where Content Agent read all 2,418 conversations, grouped them into three themes, and filed two issues, reviewed by Mia Chen at 09:12",
      visual: DigestAfter,
    },
  },
  {
    id: "pipeline",
    title: "A process that waits on people",
    metric: ["About 2 weeks", "about 3 days"],
    before: {
      body: "Analyze, fix, evaluate, release. Each step waits for someone to notice it’s their turn, so one fix takes about two weeks.",
      alt: "A chat thread from Monday to the next Thursday where each step of a fix waits for the next person to pick it up",
      visual: PipelineBefore,
    },
    after: {
      body: "Agents hand each step to the next, and people step in only to approve. The same fix ships in about three days.",
      alt: "An issue timeline where agents find the cause, open a pull request, and run 1,204 eval cases, then Mia Chen approves the release on day 3",
      visual: PipelineAfter,
    },
  },
];

function BeforeAfter() {
  return (
    <Section>
      <Container>
        <SectionHeader
          title="What changes when agents join your team"
          lead="Three stories from teams that moved their agent work into Multica."
        />
        <ol className="mt-site-block flex flex-col gap-site-step">
          {STORIES.map((story) => (
            <Story key={story.id} {...story} />
          ))}
        </ol>
      </Container>
    </Section>
  );
}

function Story({ id, title, metric: [from, to], before, after }: (typeof STORIES)[number]) {
  const ref = useRef<HTMLLIElement>(null);
  const [seen, setSeen] = useState(false);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const intersection = new IntersectionObserver(
      ([entry]) => {
        if (!entry?.isIntersecting) return;
        setSeen(true);
        intersection.disconnect();
      },
      { threshold: 0.3 },
    );
    intersection.observe(el);
    return () => intersection.disconnect();
  }, []);

  return (
    <li ref={ref}>
      <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
        <h3 className="font-site-display text-site-title text-site-ink">{title}</h3>
        <p className="text-site-small text-site-muted tabular-nums">
          {from} <span aria-hidden>→</span>
          <span className="sr-only">to</span> <span className="text-site-ink">{to}</span>
        </p>
      </div>
      <div className="mt-6 grid gap-x-site-grid-col gap-y-8 md:grid-cols-2">
        <StoryPanel label="Before" scene={before} />
        <StoryPanel label="With Multica" scene={after} frost={id} revealed={seen} />
      </div>
    </li>
  );
}

function StoryPanel({ label, scene, frost, revealed = true }: { label: string; scene: Scene; frost?: string; revealed?: boolean }) {
  const Visual = scene.visual;
  return (
    <figure>
      <div
        role="img"
        aria-label={scene.alt}
        data-frost={frost}
        className={cn("relative aspect-[16/10] overflow-hidden rounded-site-panel", frost ? "site-frost" : "bg-site-sunken")}
      >
        <div
          className={cn(
            "absolute inset-0 transition-[opacity,translate] delay-150 duration-700 ease-out motion-reduce:transition-none",
            !revealed && "translate-y-3 opacity-0 motion-reduce:translate-y-0 motion-reduce:opacity-100",
          )}
        >
          <Visual />
        </div>
      </div>
      <figcaption className="mt-4">
        <p className={cn("font-site-mono text-site-label uppercase", frost ? "text-site-ink" : "text-site-muted")}>{label}</p>
        <p className="mt-site-stack max-w-[32rem] text-site-body text-site-muted">{scene.body}</p>
      </figcaption>
    </figure>
  );
}

const DEPLOY_OPTIONS = [
  {
    title: "Multica Cloud",
    body: "Placeholder: hosted by us, ready in minutes.",
    points: ["Placeholder point", "Placeholder point", "Placeholder point"],
    href: productHref("cloud"),
  },
  {
    title: "Self-hosted",
    body: "Placeholder: run the whole platform on your own infrastructure.",
    points: ["Placeholder point", "Placeholder point", "Placeholder point"],
    href: productHref("self-hosting"),
  },
];

function Deploy() {
  return (
    <Section>
      <Container>
        <SectionHeader title="Run it where your code lives" lead="Placeholder: cloud or self-hosted, same product." />
        <div className="mt-site-block grid gap-site-grid-col md:grid-cols-2">
          {DEPLOY_OPTIONS.map((option) => (
            <div key={option.title} className="flex flex-col rounded-site-card border border-site-line bg-site-surface p-6 lg:p-8">
              <h3 className="text-site-card text-site-ink">{option.title}</h3>
              <p className="mt-site-stack text-site-body text-site-muted">{option.body}</p>
              <ul className="mt-6 flex flex-col gap-2.5">
                {option.points.map((point, index) => (
                  <li key={index} className="flex items-center gap-2.5 text-site-body text-site-ink-secondary">
                    <Check className="size-4 text-site-muted" aria-hidden />
                    {point}
                  </li>
                ))}
              </ul>
              <ActionLink href={option.href} tone="secondary" className="mt-8 self-start">
                Learn more
              </ActionLink>
            </div>
          ))}
        </div>
      </Container>
    </Section>
  );
}

function Customers() {
  return (
    <Section>
      <Container>
        <SectionHeader title="Teams shipping with Multica" lead="Placeholder: customer proof once logos and quotes are approved." />
        <div className="mt-site-block grid gap-site-grid-col md:grid-cols-3">
          {[0, 1, 2].map((index) => (
            <figure key={index} className="flex flex-col justify-between gap-10 rounded-site-card border border-site-line bg-site-surface p-6">
              <blockquote className="text-site-card text-site-ink">“Customer quote placeholder.”</blockquote>
              <figcaption className="flex items-center gap-3">
                <span aria-hidden className="size-9 rounded-site-control bg-site-sunken" />
                <span className="text-site-body">
                  <span className="block text-site-ink">Name placeholder</span>
                  <span className="block text-site-muted">Title, Company</span>
                </span>
              </figcaption>
            </figure>
          ))}
        </div>
        <ActionLink href="/customers" tone="ghost" className="mt-6">
          All customer stories
          <ArrowRight className="size-4" aria-hidden />
        </ActionLink>
      </Container>
    </Section>
  );
}
