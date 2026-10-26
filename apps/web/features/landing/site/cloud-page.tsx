"use client";

import { useState, type ComponentType, type ReactNode } from "react";
import { ArrowUpRight, Check, GitPullRequest, Laptop, ListOrdered, MessageSquareReply, Pause, type LucideIcon } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import { ProviderLogo } from "@multica/views/runtimes/components/provider-logo";
import { useDashboardCtaHref } from "../utils/use-dashboard-cta";
import { docsHrefForLocale, useLocale } from "../i18n";
import { ActionLink, Container, Eyebrow, Section, SectionHeader, focusRing } from "./primitives";
import { AssignVisual, CloudScene, ConnectVisual, LinkAgentVisual } from "./demo/cloud-visuals";
import { CtaBand, SitePage } from "./site-page";

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
    <section className="pt-site-hero pb-site-section">
      <Container className="max-w-site-wide">
        <Eyebrow>Multica Cloud</Eyebrow>
        <h1 className="mt-4 font-site-display text-site-heading text-balance">
          <span className="block text-site-ink">Agents that run in the cloud.</span>
          <span className="block text-site-muted">Assigned like any teammate.</span>
        </h1>
        <p className="mt-3 max-w-[38rem] text-site-body text-site-muted text-pretty">
          Connect a cloud agent service as a runtime, and your agents take issues without a computer of yours staying
          on. Qoder Cloud Agents works today, with Cursor and Claude next.
        </p>
        <div className="mt-6 flex flex-wrap gap-2.5">
          <ActionLink href={ctaHref} size="lg">
            {user ? "Open dashboard" : "Get started"}
          </ActionLink>
          <ActionLink href={guideHref} size="lg" tone="secondary">
            Setup guide
            <ArrowUpRight className="size-4" aria-hidden />
          </ActionLink>
        </div>
      </Container>
      <Container className="mt-10 max-w-site-wide lg:mt-12">
        <div
          role="img"
          aria-label="The Runtimes page at multica.ai: Qoder Cloud Agents online with two runs going and one queued, Cursor Cloud Agents and Claude Managed Agents coming soon, and two of the team's machines. Over it, Backend Agent works an issue on Qoder Cloud Agents and opens a linked pull request."
          data-frost="cloud"
          className="site-frost overflow-hidden rounded-site-panel"
        >
          <CloudScene />
        </div>
      </Container>
    </section>
  );
}

const STEPS: { title: string; body: string; visual: ComponentType }[] = [
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
  const [active, setActive] = useState(0);
  return (
    <Section>
      <Container>
        <SectionHeader title="Set up in three steps" />
        <div className="mt-site-block grid gap-8 lg:grid-cols-[minmax(0,1fr)_minmax(0,22rem)] lg:gap-14">
          <div className="site-visual relative h-[23rem] overflow-hidden rounded-site-panel sm:h-[26rem] lg:h-[30rem]">
            {STEPS.map((step, index) => (
              <div
                key={step.title}
                aria-hidden={index !== active}
                inert={index !== active}
                className={cn(
                  "absolute inset-0 flex items-center justify-center text-foreground transition-[opacity,translate] duration-500 ease-[cubic-bezier(0.32,0.72,0,1)] motion-reduce:transition-none",
                  index === active ? "opacity-100" : "translate-y-2 opacity-0",
                )}
              >
                <div className="sm:scale-110 lg:scale-[1.2]">
                  <step.visual />
                </div>
              </div>
            ))}
          </div>
          <ol className="flex flex-col lg:justify-center">
            {STEPS.map((step, index) => {
              const selected = index === active;
              return (
                <li key={step.title} className="relative border-t border-site-line last:border-b">
                  <span
                    aria-hidden
                    className={cn(
                      "absolute -top-px left-0 h-px bg-site-ink transition-[width] duration-500 ease-out motion-reduce:transition-none",
                      selected ? "w-full" : "w-0",
                    )}
                  />
                  <button
                    type="button"
                    aria-pressed={selected}
                    onClick={() => setActive(index)}
                    className={cn("group flex w-full gap-4 py-5 text-left", focusRing)}
                  >
                    <span className="pt-1 font-site-mono text-site-label text-site-muted tabular-nums">
                      {String(index + 1).padStart(2, "0")}
                    </span>
                    <span className="min-w-0 flex-1">
                      <span
                        className={cn(
                          "block text-site-card transition-colors",
                          selected ? "text-site-ink" : "text-site-muted group-hover:text-site-ink",
                        )}
                      >
                        {step.title}
                      </span>
                      <span className="mt-site-stack block text-site-body text-site-muted text-pretty">{step.body}</span>
                    </span>
                  </button>
                </li>
              );
            })}
          </ol>
        </div>
      </Container>
    </Section>
  );
}

const UNATTENDED: { icon: LucideIcon; title: string; body: string }[] = [
  {
    icon: ListOrdered,
    title: "Runs queue up",
    body: "Run up to 10 at once on a connection. The rest wait in line and start as slots free up.",
  },
  {
    icon: MessageSquareReply,
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
        <ul className="mt-site-block grid border-y border-site-line sm:grid-cols-2">
          {UNATTENDED.map((item, index) => (
            <li
              key={item.title}
              className={cn(
                "flex gap-5 border-site-line py-8 sm:py-10",
                index > 0 && "max-sm:border-t",
                index > 1 && "sm:border-t",
                index % 2 === 0 ? "sm:pr-8 lg:pr-14" : "sm:border-l sm:pl-8 lg:pl-14",
              )}
            >
              <span className="flex size-10 shrink-0 items-center justify-center rounded-site-inner border border-site-line bg-site-surface text-site-ink-secondary">
                <item.icon className="size-5" aria-hidden />
              </span>
              <span className="min-w-0">
                <h3 className="text-site-card text-site-ink">{item.title}</h3>
                <p className="mt-site-stack max-w-[26rem] text-site-body text-site-muted text-pretty">{item.body}</p>
              </span>
            </li>
          ))}
        </ul>
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
              <ul className="mt-5 flex flex-col gap-3">
                {side.items.map((item) => (
                  <li key={item} className="flex gap-2.5 text-site-body text-site-ink-secondary">
                    <Check className="mt-1 size-4 shrink-0 text-site-muted" aria-hidden />
                    {item}
                  </li>
                ))}
              </ul>
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
      <span
        className={cn(
          "flex size-10 shrink-0 items-center justify-center rounded-site-inner border border-site-line bg-site-surface text-site-ink-secondary",
          !available && "opacity-60",
        )}
      >
        {icon}
      </span>
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

const COMPARE: { label: string; cloud: string; local: string }[] = [
  { label: "A computer left on", cloud: "Not needed", local: "Needed while agents run" },
  { label: "Model, tools, and skills", cloud: "Set up in the cloud service", local: "Set on the agent in Multica" },
  { label: "Code it can reach", cloud: "GitHub repositories you authorize", local: "Anything on that computer" },
  { label: "Chat with the agent", cloud: "Not yet", local: "Supported" },
  { label: "Local files and attachments", cloud: "Not supported", local: "Supported" },
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
          <div className="grid grid-cols-2 gap-x-4 border-b border-site-ink pb-3 text-site-small sm:grid-cols-3">
            <span className="max-sm:hidden" />
            <span className="text-site-ink">Cloud service</span>
            <span className="text-site-ink">Your computer</span>
          </div>
          <dl>
            {COMPARE.map((row) => (
              <div
                key={row.label}
                className="grid grid-cols-2 gap-x-4 gap-y-1.5 border-b border-site-line py-4 text-site-body sm:grid-cols-3"
              >
                <dt className="text-site-muted max-sm:col-span-2 max-sm:text-site-small">{row.label}</dt>
                <dd className="text-site-ink">{row.cloud}</dd>
                <dd className="text-site-ink">{row.local}</dd>
              </div>
            ))}
          </dl>
        </div>
        <ActionLink href={guideHref} tone="ghost" className="mt-8 -ml-3 md:mt-10">
          Read the setup guide
          <ArrowUpRight className="size-4" aria-hidden />
        </ActionLink>
      </Container>
    </Section>
  );
}
