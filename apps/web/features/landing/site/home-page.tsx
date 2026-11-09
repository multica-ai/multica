"use client";

import { useEffect, useRef, useState, type ComponentType, type CSSProperties } from "react";
import Link from "next/link";
import { ArrowRight, ArrowUpRight, ChevronRight } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { BorderBeam } from "@multica/ui/components/common/border-beam";
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
import { docsHrefForLocale, toLandingDictionaryLocale, useLocale } from "../i18n";
import type { HomeDict } from "../i18n/types";
import { CUSTOMER_LOGOS, type CustomerLogo } from "./customer-logos";
import { AutopilotsVisual, ChatVisual, RuntimesVisual, SkillsVisual, WorkflowVisual } from "./demo/feature-visuals";
import { HeroDemo } from "./demo/hero-demo";
import { PROVIDER_MARKS, useStoryLoop } from "./demo/mock-kit";
import { CtaBand, SitePage } from "./site-page";
import { SITE_PREVIEW } from "./sitemap";

export function HomePage() {
  return (
    <SitePage>
      <Hero />
      <LogoWall />
      <div className="site-rails">
        <Features />
        <Agents />
        <Updates />
        <FAQSection />
        <CtaBand />
      </div>
    </SitePage>
  );
}

/** The pill above the headline; it shows only while there is something to announce. */
const ANNOUNCEMENT: { label: string; href: string } | null = SITE_PREVIEW
  ? { label: "Announcement placeholder", href: "/blog" }
  : null;

function Hero() {
  const copy = useLocale().t.home.hero;

  return (
    <section className="pt-site-hero pb-site-section">
      <Container className="max-w-site-wide">
        {ANNOUNCEMENT ? (
          <Link
            href={ANNOUNCEMENT.href}
            className={cn(
              "mb-5 inline-flex items-center gap-2 rounded-site-control border border-site-line bg-site-surface py-1 pr-3 pl-1 text-site-small text-site-ink-secondary transition-colors hover:border-site-line-strong",
              focusRing,
            )}
          >
            <span className="rounded-site-control bg-site-sunken px-2 py-0.5 text-site-ink">New</span>
            {ANNOUNCEMENT.label}
            <ArrowRight className="size-3.5 text-site-muted" aria-hidden />
          </Link>
        ) : null}
        <HeroHeadline />
        <p className="mt-3 max-w-[38rem] text-site-body text-site-muted text-pretty">{copy.lead}</p>
        <div className="mt-6 flex flex-wrap gap-2.5">
          <ActionLink href="/download" size="lg">
            {copy.download}
          </ActionLink>
          <ActionLink href="/contact-sales" size="lg" tone="secondary">
            {copy.bookDemo}
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
  const { t, locale } = useLocale();
  const { headlinePrefix, headlineSuffix } = t.home.hero;
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
      <h1 className="font-site-display text-site-heading text-site-ink">
        <span className="sr-only">
          {headlinePrefix}
          {new Intl.ListFormat(locale, { type: "disjunction" }).format(HERO_AGENTS)}
          {headlineSuffix}
        </span>
        <span aria-hidden className="block text-balance">
          {headlinePrefix}
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
          </span>
          {headlineSuffix}
        </span>
      </h1>
    </div>
  );
}

function ProviderMark({ name }: { name: string }) {
  const Mark = PROVIDER_MARKS.get(name);
  return Mark ? <Mark className="size-[0.72em] shrink-0" /> : null;
}

/** Logos share one visual area, so wide wordmarks and square seals read at the same weight. */
const LOGO_AREA = 2600;

function logoMaskStyle({ logo, ratio }: CustomerLogo): CSSProperties {
  const height = Math.min(40, Math.max(18, Math.sqrt(LOGO_AREA / ratio)));
  const mask = `url(/images/logos/${logo}.webp) center / 100% 100% no-repeat`;
  return {
    "--logo-h": `calc(${height.toFixed(1)}px * var(--logo-scale))`,
    aspectRatio: ratio,
    mask,
    WebkitMask: mask,
  } as CSSProperties;
}

function LogoWall() {
  const { t, locale } = useLocale();
  const logos = CUSTOMER_LOGOS[toLandingDictionaryLocale(locale)];
  return (
    <section aria-labelledby="logo-wall-title" className="pb-site-section">
      <Container className="max-w-site-wide">
        <h2 id="logo-wall-title" className="text-center text-site-small text-site-muted">
          {t.home.logoWall.title}
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
                {logos.map((logo) => (
                  <li key={logo.name} className="flex h-10 items-center">
                    <span
                      role="img"
                      aria-label={logo.name}
                      style={logoMaskStyle(logo)}
                      className="block h-(--logo-h) bg-site-muted transition-colors duration-200 [--logo-scale:0.8] hover:bg-site-ink sm:[--logo-scale:1]"
                    />
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

type FeatureId = keyof HomeDict["features"]["items"];

const FEATURES: { id: FeatureId; visual: ComponentType }[] = [
  { id: "runtimes", visual: RuntimesVisual },
  { id: "workflows", visual: WorkflowVisual },
  { id: "chat", visual: ChatVisual },
  { id: "autopilots", visual: AutopilotsVisual },
  { id: "skills", visual: SkillsVisual },
];

function Features() {
  const { t } = useLocale();
  return (
    <Section>
      <Container>
        <SectionHeader title={t.home.features.title} />
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
  const { locale, t } = useLocale();
  const copy = t.home.agents;
  const docsHref = docsHrefForLocale(locale);
  return (
    <Section>
      <Container>
        <h2 className="max-w-[44rem] font-site-display text-site-heading text-balance">
          <span className="block text-site-muted">{copy.lead}</span>
          <span className="block text-site-ink">{copy.title}</span>
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
          {copy.all}
          <ArrowUpRight className="size-4" aria-hidden />
        </ActionLink>
      </Container>
    </Section>
  );
}

const UPDATE_COUNT = 7;

/**
 * Release titles list their changes ("A, B, and C", or "A、B、C"); the home
 * page leads with the first and counts the rest.
 */
function releaseHeadline(title: string) {
  const [headline = title, ...rest] = title.split(/, (?:and )?|、/);
  return { headline, more: rest.length };
}

function Updates() {
  const { t, locale } = useLocale();
  const copy = t.home.updates;
  const releases = t.changelog.entries.slice(0, UPDATE_COUNT);
  const latest = releases[0];
  const day = new Intl.DateTimeFormat(locale, { month: "short", day: "numeric", timeZone: "UTC" });
  const dateLabel = (date: string) => day.format(new Date(`${date}T00:00:00Z`));
  if (!latest) return null;

  return (
    <Section>
      <Container className="grid gap-site-block lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] lg:gap-16">
        <div className="flex flex-col justify-between gap-site-block">
          <SectionHeader eyebrow={copy.eyebrow} title={copy.title} />
          <Link
            href={`/changelog#${anchorId(latest.version)}`}
            data-frost="changelog"
            className={cn(
              "site-frost group hidden max-w-[20rem] flex-col gap-8 rounded-site-card p-5 lg:flex",
              focusRing,
            )}
          >
            <span className="relative text-site-small text-site-ink-secondary">{copy.latest} · {dateLabel(latest.date)}</span>
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
                      <span className="text-site-muted">
                        {" "}
                        {copy.more.replace("{count}", String(releaseHeadline(release.title).more))}
                      </span>
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
            {copy.all}
            <ArrowRight className="size-4" aria-hidden />
          </ActionLink>
        </div>
      </Container>
    </Section>
  );
}

function FeatureCard({ id, visual: Visual }: (typeof FEATURES)[number]) {
  const { title, body, alt } = useLocale().t.home.features.items[id];
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
