"use client";

import Link from "next/link";
import { ArrowRight, Check } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import { docsHrefForLocale, useLocale } from "../i18n";
import { useDashboardCtaHref } from "../utils/use-dashboard-cta";
import { FAQSection } from "../components/faq-section";
import { HERO_PROVIDERS } from "../components/provider-marks";
import {
  ActionLink,
  Container,
  Eyebrow,
  ProductFrame,
  Section,
  SectionHeader,
  focusRing,
} from "./primitives";
import { CtaBand, SitePage } from "./site-page";
import { PLACEHOLDER_PAGES, productHref } from "./sitemap";

// Vector marks only: the raster marks in HERO_PROVIDERS are drawn white for a
// dark backdrop and disappear on the light canvas.
const STRIP_PROVIDERS = ["Claude Code", "Codex", "Cursor", "GitHub Copilot", "Qwen Code", "Kimi CLI"];

export function HomePage() {
  return (
    <SitePage>
      <Hero />
      <WorksWith />
      <Features />
      <HowItWorks />
      <Deploy />
      <Customers />
      <FAQSection />
      <CtaBand />
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
        <h1 className="mt-6 max-w-[20ch] font-site-display text-site-heading text-site-ink text-balance">
          Your next ten hires won’t be human
        </h1>
        <p className="mt-4 max-w-[30rem] text-site-body text-site-muted text-pretty">
          Placeholder tagline. Multica turns coding agents into teammates you can assign, review,
          and scale — on your machines or in the cloud.
        </p>
        <div className="mt-7 flex flex-wrap gap-2.5">
          <ActionLink href={ctaHref} size="lg">
            {user ? "Open dashboard" : "Get started"}
          </ActionLink>
          <ActionLink href="/contact-sales" size="lg" tone="secondary">
            Book a demo
          </ActionLink>
        </div>
      </Container>
      <Container className="mt-12 lg:mt-24">
        <ProductFrame label="Hero product visual" aspectRatio="16 / 9" />
      </Container>
    </section>
  );
}

function WorksWith() {
  const { locale } = useLocale();
  const providers = HERO_PROVIDERS.filter((p) => STRIP_PROVIDERS.includes(p.name));

  return (
    <Section>
      <Container>
        <div className="flex items-baseline justify-between gap-4">
          <Eyebrow>Works with the agents you already use</Eyebrow>
          <Link
            href={`${docsHrefForLocale(locale)}/providers`}
            className={cn("shrink-0 whitespace-nowrap text-site-small text-site-muted hover:text-site-ink", focusRing)}
          >
            All 27 agents
          </Link>
        </div>
        <ul className="mt-6 grid grid-cols-2 border-y border-site-line sm:grid-cols-3 lg:grid-cols-6">
          {providers.map(({ name, Mark }) => (
            <li
              key={name}
              className="flex h-24 items-center justify-center gap-2.5 border-site-line text-site-muted not-first:border-l"
            >
              <Mark className="size-5" />
              <span className="text-site-small">{name}</span>
            </li>
          ))}
        </ul>
      </Container>
    </Section>
  );
}

const FEATURES: { title: string; body: string; href: string }[] = [
  { title: "Assign work to agents like teammates", body: "Placeholder: agents sit in the same assignee picker as your people.", href: productHref("agents") },
  { title: "Bring any coding agent", body: "Placeholder: 27 coding tools register as runtimes on your machines.", href: productHref("runtimes") },
  { title: "Squads split big work", body: "Placeholder: a leader agent routes each piece to the right teammate.", href: productHref("squads") },
  { title: "Agents join the thread", body: "Placeholder: mention an agent in Slack or Lark and it gets to work.", href: productHref("chat") },
  { title: "Autopilots run on a schedule", body: "Placeholder: recurring reports, upgrades, and triage.", href: productHref("autopilots") },
];

function Features() {
  return (
    <Section>
      <Container>
        <SectionHeader title="Section headline placeholder" lead="Placeholder: one line framing the product pillars." />
        <div className="mt-site-block grid gap-x-site-grid-col gap-y-site-grid-row md:grid-cols-2">
          {FEATURES.slice(0, 2).map((feature) => (
            <FeatureCard key={feature.title} {...feature} aspectRatio="4 / 3" />
          ))}
        </div>
        <div className="mt-site-grid-row grid gap-x-site-grid-col gap-y-site-grid-row md:grid-cols-3">
          {FEATURES.slice(2).map((feature) => (
            <FeatureCard key={feature.title} {...feature} aspectRatio="5 / 4" />
          ))}
        </div>
      </Container>
    </Section>
  );
}

function FeatureCard({
  title,
  body,
  href,
  aspectRatio,
}: (typeof FEATURES)[number] & { aspectRatio: string }) {
  return (
    <Link href={href} className={cn("group block rounded-site-panel", focusRing)}>
      <ProductFrame label={`${title} visual`} aspectRatio={aspectRatio} />
      <h3 className="mt-4 text-site-card text-site-ink">{title}</h3>
      <p className="mt-site-stack max-w-[32rem] text-site-body text-site-muted">{body}</p>
    </Link>
  );
}

const STEPS: { title: string; body: string }[] = [
  { title: "Plan the work", body: "Placeholder: write issues, or let an agent break a spec into them." },
  { title: "Assign it to an agent", body: "Placeholder: the agent claims the task, works in your repo, and reports progress." },
  { title: "Review and ship", body: "Placeholder: review the PR, leave feedback, and merge." },
];

function HowItWorks() {
  return (
    <Section>
      <Container>
        <SectionHeader title="From issue to merged PR" lead="Placeholder: the core loop in three steps." />
        <ol className="mt-site-step flex flex-col gap-site-step">
          {STEPS.map((step, index) => (
            <li key={step.title} className="grid items-center gap-8 md:grid-cols-2 md:gap-16">
              <div className={cn(index % 2 === 1 && "md:order-2")}>
                <p className="font-site-mono text-site-label text-site-muted tabular-nums">
                  {String(index + 1).padStart(2, "0")}
                </p>
                <h3 className="mt-4 font-site-display text-site-title text-site-ink">{step.title}</h3>
                <p className="mt-1 max-w-[28rem] text-site-body text-site-muted">{step.body}</p>
              </div>
              <ProductFrame label={`Step ${index + 1} visual`} aspectRatio="4 / 3" />
            </li>
          ))}
        </ol>
      </Container>
    </Section>
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
