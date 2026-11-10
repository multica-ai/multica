"use client";

import { ArrowUpRight } from "@multica/ui/icons";
import { docsHrefForLocale, useLocale } from "../i18n";
import { ActionLink, Container, Section, SectionHeader } from "./primitives";
import { AgentSessionVisual, CliScene } from "./demo/cli-visuals";
import { CtaBand, SitePage } from "./site-page";
import { CheckList, CodeBlock, SurfaceHero, type CodeVariant } from "./surface-kit";

export function CliPage() {
  return (
    <SitePage>
      <Hero />
      <div className="site-rails">
        <Install />
        <Commands />
        <AgentDriven />
        <CtaBand title="Connect your first computer" lead="Install the CLI, run multica setup, and assign an issue to an agent on that machine." />
      </div>
    </SitePage>
  );
}

const SKILL_URL = "https://github.com/multica-ai/multica-cli";

function useCliDocsHref() {
  const { locale } = useLocale();
  return `${docsHrefForLocale(locale)}/cli`;
}

function Hero() {
  const docsHref = useCliDocsHref();
  return (
    <SurfaceHero
      eyebrow="Multica CLI"
      title="Multica from your terminal."
      subtitle="And from every agent you run."
      lead="The multica command connects a computer as a runtime, then lets you, a script, or another coding agent work with issues, comments, and agents from the shell."
      actions={
        <>
          <ActionLink href="#install" size="lg">
            Install the CLI
          </ActionLink>
          <ActionLink href={`${docsHref}#command-reference`} size="lg" tone="secondary">
            Command reference
            <ArrowUpRight className="size-4" aria-hidden />
          </ActionLink>
        </>
      }
      frost="cli"
      label="A terminal on build-01 runs multica setup: it signs Leo Park in, starts the daemon, and multica daemon status reports Claude Code, Codex, and OpenCode. Beside it, build-01 appears on the Runtimes page as a new machine."
    >
      <CliScene />
    </SurfaceHero>
  );
}

const INSTALL: { title: string; body: string; code: CodeVariant[] }[] = [
  {
    title: "Install it",
    body: "One line in a terminal on macOS and Linux, or in PowerShell on Windows. Homebrew works too.",
    code: [
      { label: "macOS / Linux", code: "curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash" },
      { label: "Windows", code: "irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex" },
      { label: "Homebrew", code: "brew install multica-ai/tap/multica" },
    ],
  },
  {
    title: "Connect the computer",
    body: "Setup saves the server, signs you in through the browser, and starts the daemon. Running your own Multica? Give it your server’s addresses.",
    code: [
      { label: "Multica Cloud", code: "multica setup" },
      {
        label: "Self-hosted",
        code: "multica setup self-host \\\n  --server-url https://api.example.com \\\n  --app-url https://app.example.com",
      },
    ],
  },
  {
    title: "Check it’s running",
    body: "See who you’re signed in as and which coding tools the daemon found. Your computer is now a runtime you can assign agents to.",
    code: [{ label: "Terminal", code: "multica auth status\nmultica daemon status" }],
  },
];

function Install() {
  return (
    <Section id="install" className="scroll-mt-site-header">
      <Container>
        <SectionHeader title="A runtime in three commands" />
        <ol className="mt-site-block flex flex-col">
          {INSTALL.map((step, index) => (
            <li
              key={step.title}
              className="grid gap-6 border-t border-site-line py-8 last:border-b lg:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)] lg:gap-16 lg:py-10"
            >
              <div className="flex gap-4">
                <span className="pt-1 font-site-mono text-site-label text-site-muted tabular-nums">
                  {String(index + 1).padStart(2, "0")}
                </span>
                <span>
                  <h3 className="text-site-card text-site-ink">{step.title}</h3>
                  <p className="mt-site-stack max-w-[26rem] text-site-body text-site-muted text-pretty">{step.body}</p>
                </span>
              </div>
              <CodeBlock variants={step.code} />
            </li>
          ))}
        </ol>
      </Container>
    </Section>
  );
}

const COMMANDS: { name: string; body: string }[] = [
  { name: "issue", body: "Create, assign, search, and comment on issues, and follow their runs" },
  { name: "project", body: "Manage projects and the repositories attached to them" },
  { name: "workflow", body: "Set each status’s owner and handoff, and pick the workflow a project uses" },
  { name: "property", body: "Define the custom properties issues carry across the workspace" },
  { name: "agent", body: "Create agents, set their instructions, skills, and MCP servers" },
  { name: "squad", body: "Group agents under a leader that routes the work" },
  { name: "autopilot", body: "Schedule recurring work and trigger runs by hand" },
  { name: "skill", body: "Import, search, and edit the skills agents share" },
  { name: "repo", body: "List the workspace’s repositories and check one out" },
  { name: "workspace", body: "Switch workspaces and invite members" },
  { name: "daemon", body: "Start, stop, and read the logs of the daemon on this computer" },
  { name: "runtime", body: "List the workspace’s runtimes, their usage, and their activity" },
];

function Commands() {
  return (
    <Section>
      <Container className="grid gap-site-block lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] lg:gap-16">
        <SectionHeader
          title="The whole workspace, scriptable"
          lead="What you can do in the app, you can do from the shell. Add --output json to any command for output a script can read."
        />
        <dl className="grid border-t border-site-line sm:grid-cols-2 sm:gap-x-10">
          {COMMANDS.map((command) => (
            <div key={command.name} className="border-b border-site-line py-4">
              <dt className="font-site-mono text-site-small text-site-ink">multica {command.name}</dt>
              <dd className="mt-1 text-site-small text-site-muted text-pretty">{command.body}</dd>
            </div>
          ))}
        </dl>
      </Container>
    </Section>
  );
}

function AgentDriven() {
  return (
    <Section>
      <Container className="grid items-center gap-site-block lg:grid-cols-2 lg:gap-16">
        <div
          role="img"
          aria-label="A Claude Code session asked to pick up ACM-163: it reads the issue and its comment threads with multica, finds a fixed timeout in the checkout test, writes a reply to a file, and posts it as a comment."
          data-frost="cli"
          className="site-frost flex h-[26rem] items-center justify-center overflow-hidden rounded-site-panel max-lg:order-2"
        >
          <div aria-hidden className="relative text-foreground max-sm:scale-[0.8] lg:max-xl:scale-[0.84]">
            <AgentSessionVisual />
          </div>
        </div>
        <div>
          <h2 className="font-site-display text-site-heading text-balance">
            <span className="block text-site-ink">Let your coding agent drive it.</span>
            <span className="block text-site-muted">Without leaving its session.</span>
          </h2>
          <p className="mt-3 max-w-[30rem] text-site-body text-site-muted text-pretty">
            The Multica CLI skill teaches Codex, Claude Code, Cursor, and other agents to work with Multica through the
            commands on this page.
          </p>
          <CheckList
            className="mt-6"
            items={[
              "Reads long threads a page at a time, not all at once",
              "Writes comments through a file, never a mangled one-liner",
              "Knows which actions start runs or notify people",
              "Acts with your login and permissions, nothing more",
            ]}
          />
          <ActionLink href={SKILL_URL} tone="ghost" className="mt-6 -ml-3">
            Get the CLI skill
            <ArrowUpRight className="size-4" aria-hidden />
          </ActionLink>
        </div>
      </Container>
    </Section>
  );
}
