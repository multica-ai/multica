"use client";

import type { ReactNode } from "react";
import { ArrowUpRight, GitPullRequest, Laptop, ListOrdered, MessageSquareText, Pause } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import { ProviderLogo } from "@multica/views/runtimes/components/provider-logo";
import { useDashboardCtaHref } from "../utils/use-dashboard-cta";
import { docsHrefForLocale, useLocale } from "../i18n";
import { ActionLink, Container, Section, SectionHeader } from "./primitives";
import { AssignVisual, CloudScene, ConnectVisual, LinkAgentVisual } from "./demo/cloud-visuals";
import { CtaBand, SitePage } from "./site-page";
import {
  CheckList,
  CompareTable,
  FeatureGrid,
  IconTile,
  SurfaceHero,
  WalkthroughTabs,
  type CompareRow,
  type Feature,
  type Walkthrough,
} from "./surface-kit";

export function CloudPage() {
  return (
    <SitePage>
      <Hero />
      <div className="site-rails">
        <Steps />
        <Unattended />
        <Boundary />
        <Services />
        <Compare />
        <CtaBand title="Put an agent to work in the cloud" lead="Connect Qoder Cloud Agents and assign the first issue." />
      </div>
    </SitePage>
  );
}

function useSetupGuideHref() {
  const { locale } = useLocale();
  return `${docsHrefForLocale(locale)}/daemon-runtimes#qoder-cloud-agents-remote-runtime`;
}

function Hero() {
  const user = useAuthStore((s) => s.user);
  const ctaHref = useDashboardCtaHref();
  const guideHref = useSetupGuideHref();
  return (
    <SurfaceHero
      eyebrow="Multica Cloud"
      title="Agents that run in the cloud."
      subtitle="Assigned like any teammate."
      lead="Connect a cloud agent service as a runtime, and your agents take issues without a computer of yours staying on. Qoder Cloud Agents works today, with Cursor and Claude next."
      actions={
        <>
          <ActionLink href={ctaHref} size="lg">
            {user ? "Open dashboard" : "Get started"}
          </ActionLink>
          <ActionLink href={guideHref} size="lg" tone="secondary">
            Setup guide
            <ArrowUpRight className="size-4" aria-hidden />
          </ActionLink>
        </>
      }
      frost="cloud"
      label="The Runtimes page at multica.ai: Qoder Cloud Agents online with two runs going and one queued, Cursor Cloud Agents and Claude Managed Agents coming soon, and two of the team’s machines. Over it, Backend Agent works an issue on Qoder Cloud Agents and opens a linked pull request."
    >
      <CloudScene />
    </SurfaceHero>
  );
}

const STEPS: Walkthrough[] = [
  {
    title: "Connect Qoder",
    body: "On the Runtimes page, add Qoder Cloud Agents, paste a personal access token, and pick an environment.",
    visual: ConnectVisual,
  },
  {
    title: "Link an agent",
    body: "Create a Multica agent and choose the QCA Agent it runs as. Its model, tools, and skills stay set up in Qoder.",
    visual: LinkAgentVisual,
  },
  {
    title: "Assign it an issue",
    body: "It starts a cloud session, reports progress on the issue, and opens a pull request linked back to it.",
    visual: AssignVisual,
  },
];

function Steps() {
  return (
    <Section>
      <Container>
        <SectionHeader title="Set up in three steps" />
        <div className="mt-site-block">
          <WalkthroughTabs steps={STEPS} />
        </div>
      </Container>
    </Section>
  );
}

const UNATTENDED: Feature[] = [
  {
    icon: ListOrdered,
    title: "Runs queue up",
    body: "Run up to 10 at once on a connection. The rest wait in line and start as slots free up.",
  },
  {
    icon: MessageSquareText,
    title: "Follow-ups resume the session",
    body: "A new comment on the issue continues the agent’s previous Qoder session.",
  },
  {
    icon: GitPullRequest,
    title: "Pull requests link themselves",
    body: "Agents push a branch named after the issue and put its ID in the pull request title.",
  },
  {
    icon: Pause,
    title: "Pause without losing setup",
    body: "Stop new runs and keep credentials and agents, or disconnect to remove them.",
  },
];

function Unattended() {
  return (
    <Section>
      <Container>
        <SectionHeader title="Built for work nobody watches" />
        <div className="mt-site-block">
          <FeatureGrid items={UNATTENDED} />
        </div>
      </Container>
    </Section>
  );
}

const BOUNDARY: { title: string; items: string[] }[] = [
  {
    title: "Sent to Qoder for each run",
    items: ["The issue title and description", "Relevant comments", "Project context", "The agent’s instructions"],
  },
  {
    title: "Kept under your control",
    items: [
      "Tokens are encrypted and never shown again after saving",
      "GitHub tokens only mount the repository for a run",
      "Agents can clone only the repositories you authorize",
      "Admins manage the connection; members pick agents",
    ],
  },
];

function Boundary() {
  return (
    <Section>
      <Container>
        <h2 className="max-w-[44rem] font-site-display text-site-heading text-balance">
          <span className="block text-site-ink">You decide what the cloud sees.</span>
          <span className="block text-site-muted">And which code it can open.</span>
        </h2>
        <div className="mt-site-block grid overflow-hidden rounded-site-card border border-site-line bg-site-surface md:grid-cols-2">
          {BOUNDARY.map((side, index) => (
            <div key={side.title} className={cn("p-6 sm:p-8", index === 1 && "border-site-line max-md:border-t md:border-l")}>
              <h3 className="text-site-card text-site-ink">{side.title}</h3>
              <CheckList items={side.items} className="mt-5" />
            </div>
          ))}
        </div>
      </Container>
    </Section>
  );
}

const SERVICES: { provider: string; name: string; vendor: string; available: boolean }[] = [
  { provider: "qoder_cloud", name: "Qoder Cloud Agents", vendor: "By Qoder", available: true },
  { provider: "cursor", name: "Cursor Cloud Agents", vendor: "By Cursor", available: false },
  { provider: "claude", name: "Claude Managed Agents", vendor: "By Anthropic", available: false },
];

function Services() {
  const { locale } = useLocale();
  return (
    <Section>
      <Container className="grid gap-site-block lg:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)] lg:gap-16">
        <div>
          <SectionHeader
            title="More clouds on the way"
            lead="Cloud services sit beside your own machines on the Runtimes page, and you assign their agents the same way."
          />
          <ActionLink href={`${docsHrefForLocale(locale)}/providers`} tone="ghost" className="mt-6 -ml-3">
            Agents for your own machines
            <ArrowUpRight className="size-4" aria-hidden />
          </ActionLink>
        </div>
        <ul className="divide-y divide-site-line border-y border-site-line">
          {SERVICES.map((service) => (
            <ServiceRow
              key={service.provider}
              icon={<ProviderLogo provider={service.provider} className="size-5" />}
              name={service.name}
              detail={service.vendor}
              available={service.available}
            />
          ))}
          <ServiceRow icon={<Laptop className="size-5" aria-hidden />} name="Your own machines" detail="27 coding agents, through the daemon" available />
        </ul>
      </Container>
    </Section>
  );
}

function ServiceRow({ icon, name, detail, available }: { icon: ReactNode; name: string; detail: string; available: boolean }) {
  return (
    <li className="flex items-center gap-4 py-4">
      <IconTile muted={!available}>{icon}</IconTile>
      <span className="min-w-0 flex-1">
        <span className={cn("block text-site-body", available ? "text-site-ink" : "text-site-muted")}>{name}</span>
        <span className="block text-site-small text-site-muted">{detail}</span>
      </span>
      <Availability available={available} />
    </li>
  );
}

function Availability({ available }: { available: boolean }) {
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1.5 rounded-site-control bg-site-sunken px-2 py-0.5 text-site-small",
        available ? "text-site-ink" : "text-site-muted",
      )}
    >
      {available ? <span className="size-1.5 rounded-full bg-success" /> : null}
      {available ? "Available" : "Coming soon"}
    </span>
  );
}

const COMPARE: CompareRow[] = [
  { label: "A computer left on", values: ["Not needed", "Needed while agents run"] },
  { label: "Model, tools, and skills", values: ["Set up in the cloud service", "Set on the agent in Multica"] },
  { label: "Code it can reach", values: ["GitHub repositories you authorize", "Anything on that computer"] },
  { label: "Chat with the agent", values: ["Not yet", "Supported"] },
  { label: "Local files and attachments", values: ["Not supported", "Supported"] },
];

function Compare() {
  const guideHref = useSetupGuideHref();
  return (
    <Section>
      <Container>
        <SectionHeader
          title="A cloud service or your own computer"
          lead="Most teams use both: cloud agents for work that should run unattended, and their own machines for work that needs local files."
        />
        <div className="mt-site-block">
          <CompareTable columns={["Cloud service", "Your computer"]} rows={COMPARE} />
        </div>
        <ActionLink href={guideHref} tone="ghost" className="mt-8 -ml-3 md:mt-10">
          Read the setup guide
          <ArrowUpRight className="size-4" aria-hidden />
        </ActionLink>
      </Container>
    </Section>
  );
}
