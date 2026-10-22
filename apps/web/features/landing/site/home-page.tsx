"use client";

import type { ComponentType, CSSProperties } from "react";
import Link from "next/link";
import { ArrowRight, Check } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import { useDashboardCtaHref } from "../utils/use-dashboard-cta";
import { FAQSection } from "../components/faq-section";
import {
  ActionLink,
  Container,
  ProductFrame,
  Section,
  SectionHeader,
  focusRing,
} from "./primitives";
import { CUSTOMER_LOGOS } from "./customer-logos";
import {
  AutopilotsVisual,
  ChatVisual,
  RuntimesVisual,
  SkillsVisual,
  WorkflowVisual,
} from "./demo/feature-visuals";
import { HeroBoard } from "./demo/hero-board";
import { CtaBand, SitePage } from "./site-page";
import { PLACEHOLDER_PAGES, productHref } from "./sitemap";

export function HomePage() {
  return (
    <SitePage>
      <Hero />
      <LogoWall />
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
        <h1 className="mt-5 font-site-display text-site-heading text-site-ink text-balance">
          Your next ten hires won’t be human
        </h1>
        <p className="mt-3 max-w-[38rem] text-site-body text-site-muted text-pretty">
          Placeholder tagline. Assign issues to coding agents, review their work, and scale the team — on
          your machines or in the cloud.
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
        <div className="relative aspect-square sm:aspect-[16/10]">
          <HeroBoard />
        </div>
      </Container>
    </section>
  );
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

const FEATURES: { title: string; body: string; href: string; visual: ComponentType; alt: string }[] = [
  {
    title: "Runs on your machines, or in our cloud",
    body: "Connect a laptop or a server with one command and Multica finds the coding agents installed on it. Or run them in managed cloud sandboxes.",
    href: productHref("runtimes"),
    visual: RuntimesVisual,
    alt: "The Runtimes page listing a MacBook, a Linux server, Multica Cloud, and a Windows laptop, each with its coding agents",
  },
  {
    title: "Workflows hand off every step",
    body: "Lay out the steps a project’s issues follow and who owns each one. Moving an issue to the next step hands it to that step’s person or agent.",
    href: productHref("workflows"),
    visual: WorkflowVisual,
    alt: "A Feature delivery workflow with four steps, each with an owner, and an issue being handed from one step to the next",
  },
  {
    title: "Mention an agent in chat",
    body: "From Slack, Lark, DingTalk, WeCom, or Telegram. The agent reads the thread, files the issue, and gets to work.",
    href: productHref("chat"),
    visual: ChatVisual,
    alt: "A chat thread where a teammate mentions Backend Agent, which replies with the issue it filed",
  },
  {
    title: "Autopilots run on a schedule",
    body: "Upgrades, triage, and reports file themselves and land with an agent, every hour or every Monday.",
    href: productHref("autopilots"),
    visual: AutopilotsVisual,
    alt: "A list of autopilots with their schedules and the agent that runs each one",
  },
  {
    title: "Skills the whole team shares",
    body: "Write down how your team does a thing once, and every agent follows it.",
    href: productHref("skills"),
    visual: SkillsVisual,
    alt: "A list of team skills and the agents that use each one",
  },
];

function Features() {
  return (
    <Section className="border-t-0">
      <Container>
        <SectionHeader title="Built for teams of people and agents" />
        <div className="mt-site-block grid gap-x-site-grid-col gap-y-site-grid-row md:grid-cols-2">
          {FEATURES.slice(0, 2).map((feature) => (
            <FeatureCard key={feature.title} {...feature} aspect="aspect-[4/3]" />
          ))}
        </div>
        <div className="mt-site-grid-row grid gap-x-site-grid-col gap-y-site-grid-row md:grid-cols-3">
          {FEATURES.slice(2).map((feature) => (
            <FeatureCard key={feature.title} {...feature} aspect="aspect-[3/2]" />
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
  visual: Visual,
  alt,
  aspect,
}: (typeof FEATURES)[number] & { aspect: string }) {
  return (
    <Link href={href} className={cn("group block rounded-site-panel", focusRing)}>
      <div role="img" aria-label={alt} className={cn("relative overflow-hidden rounded-site-panel bg-site-sunken", aspect)}>
        <Visual />
      </div>
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
