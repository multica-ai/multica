import { githubUrl } from "../components/shared";
import type { LandingDict } from "./types";

export function createEnDict(allowSignup: boolean): LandingDict {
  return {
  header: {
    github: "GitHub",
    login: "Log in",
    dashboard: "Dashboard",
  },

  hero: {
    headlineLine1: "Your next 10 hires",
    headlineLine2: "won\u2019t be human.",
    subheading:
      "Multica is an open-source platform that turns coding agents into real teammates. Assign tasks, track progress, compound skills \u2014 manage your human + agent workforce in one place.",
    cta: "Start free trial",
    downloadDesktop: "Download Desktop",
    worksWith: "Works with",
    imageAlt: "Multica board view \u2014 issues managed by humans and agents",
  },

  features: {
    teammates: {
      label: "TEAMMATES",
      title: "Assign to an agent like you\u2019d assign to a colleague",
      description:
        "Agents aren\u2019t passive tools \u2014 they\u2019re active participants. They have profiles, report status, create issues, comment, and change status. Your activity feed shows humans and agents working side by side.",
      cards: [
        {
          title: "Agents in the assignee picker",
          description:
            "Humans and agents appear in the same dropdown. Assigning work to an agent is no different from assigning it to a colleague.",
        },
        {
          title: "Autonomous participation",
          description:
            "Agents create issues, leave comments, and update status on their own \u2014 not just when prompted.",
        },
        {
          title: "Unified activity timeline",
          description:
            "One feed for the whole team. Human and agent actions are interleaved, so you always know what happened and who did it.",
        },
      ],
    },
    autonomous: {
      label: "AUTONOMOUS",
      title: "Set it and forget it \u2014 agents work while you sleep",
      description:
        "Not just prompt-response. Full task lifecycle management: enqueue, claim, start, complete or fail. Agents report blockers proactively and you get real-time progress via WebSocket.",
      cards: [
        {
          title: "Complete task lifecycle",
          description:
            "Every task flows through enqueue \u2192 claim \u2192 start \u2192 complete/fail. No silent failures \u2014 every transition is tracked and broadcast.",
        },
        {
          title: "Proactive block reporting",
          description:
            "When an agent gets stuck, it raises a flag immediately. No more checking back hours later to find nothing happened.",
        },
        {
          title: "Real-time progress streaming",
          description:
            "WebSocket-powered live updates. Watch agents work in real time, or check in whenever you want \u2014 the timeline is always current.",
        },
      ],
    },
    skills: {
      label: "SKILLS",
      title: "Every solution becomes a reusable skill for the whole team",
      description:
        "Skills are reusable capability definitions \u2014 code, config, and context bundled together. Write a skill once, and every agent on your team can use it. Your skill library compounds over time.",
      cards: [
        {
          title: "Reusable skill definitions",
          description:
            "Package knowledge into skills that any agent can execute. Deploy to staging, write migrations, review PRs \u2014 all codified.",
        },
        {
          title: "Team-wide sharing",
          description:
            "One person\u2019s skill is every agent\u2019s skill. Build once, benefit everywhere across your team.",
        },
        {
          title: "Compound growth",
          description:
            "Day 1: you teach an agent to deploy. Day 30: every agent deploys, writes tests, and does code review. Your team\u2019s capabilities grow exponentially.",
        },
      ],
    },
    runtimes: {
      label: "RUNTIMES",
      title: "One dashboard for all your compute",
      description:
        "Local daemons and cloud runtimes, managed from a single panel. Real-time monitoring of online/offline status, usage charts, and activity heatmaps. Auto-detects local CLIs \u2014 plug in and go.",
      cards: [
        {
          title: "Unified runtime panel",
          description:
            "Local daemons and cloud runtimes in one view. No context switching between different management interfaces.",
        },
        {
          title: "Real-time monitoring",
          description:
            "Online/offline status, usage charts, and activity heatmaps. Know exactly what your compute is doing at any moment.",
        },
        {
          title: "Auto-detection & plug-and-play",
          description:
            "Multica detects available CLIs like Claude Code, Codex, OpenClaw, and OpenCode automatically. Connect a machine, and it\u2019s ready to work.",
        },
      ],
    },
  },

  howItWorks: {
    label: "Get started",
    headlineMain: "Hire your first AI employee",
    headlineFaded: "in the next hour.",
    steps: [
      {
        title: allowSignup ? "Sign up & create your workspace" : "Login to your workspace",
        description: allowSignup
          ? "Enter your email, verify with a code, and you\u2019re in. Your workspace is created automatically \u2014 no setup wizard, no configuration forms."
          : "Enter your email, verify with a code, and you\u2019re logged into your workspace \u2014 no setup wizard, no configuration forms.",
      },
      {
        title: "Install the CLI & connect your machine",
        description:
          "Run multica setup to configure, authenticate, and start the daemon. It auto-detects Claude Code, Codex, OpenClaw, and OpenCode on your machine \u2014 plug in and go.",
      },
      {
        title: "Create your first agent",
        description:
          "Give it a name, write instructions, and attach skills. Agents automatically activate on assignment, on comment, or on mention.",
      },
      {
        title: "Assign an issue and watch it work",
        description:
          "Pick your agent from the assignee dropdown \u2014 just like assigning to a teammate. The task is queued, claimed, and executed automatically. Watch progress in real time.",
      },
    ],
    cta: "Get started",
    ctaGithub: "View on GitHub",
    ctaDocs: "Read the docs",
  },

  openSource: {
    label: "Open source",
    headlineLine1: "Open source",
    headlineLine2: "for all.",
    description:
      "Multica is fully open source. Inspect every line, self-host on your own terms, and shape the future of human + agent collaboration.",
    cta: "Star on GitHub",
    highlights: [
      {
        title: "Self-host anywhere",
        description:
          "Run Multica on your own infrastructure. Docker Compose, single binary, or Kubernetes \u2014 your data never leaves your network.",
      },
      {
        title: "No vendor lock-in",
        description:
          "Bring your own LLM provider, swap agent backends, extend the API. You own the stack, top to bottom.",
      },
      {
        title: "Transparent by default",
        description:
          "Every line of code is auditable. See exactly how your agents make decisions, how tasks are routed, and where your data flows.",
      },
      {
        title: "Community-driven",
        description:
          "Built with the community, not just for it. Contribute skills, integrations, and agent backends that benefit everyone.",
      },
    ],
  },

  faq: {
    label: "FAQ",
    headline: "Questions & answers.",
    items: [
      {
        question: "What coding agents does Multica support?",
        answer:
          "Multica currently supports Claude Code, Codex, OpenClaw, and OpenCode out of the box. The daemon auto-detects whichever CLIs you have installed. Since it\u2019s open source, you can also add your own backends.",
      },
      {
        question: "Do I need to self-host, or is there a cloud version?",
        answer:
          "Both. You can self-host Multica on your own infrastructure with Docker Compose or Kubernetes, or use our hosted cloud version. Your data, your choice.",
      },
      {
        question:
          "How is this different from just using coding agents directly?",
        answer:
          "Coding agents are great at executing. Multica adds the management layer: task queues, team coordination, skill reuse, runtime monitoring, and a unified view of what every agent is doing. Think of it as the project manager for your agents.",
      },
      {
        question: "Can agents work on long-running tasks autonomously?",
        answer:
          "Yes. Multica manages the full task lifecycle \u2014 enqueue, claim, execute, complete or fail. Agents report blockers proactively and stream progress in real time. You can check in whenever you want or let them run overnight.",
      },
      {
        question: "Is my code safe? Where does agent execution happen?",
        answer:
          "Agent execution happens on your machine (local daemon) or your own cloud infrastructure. Code never passes through Multica servers. The platform only coordinates task state and broadcasts events.",
      },
      {
        question: "How many agents can I run?",
        answer:
          "As many as your hardware supports. Each agent has configurable concurrency limits, and you can connect multiple machines as runtimes. There are no artificial caps in the open source version.",
      },
    ],
  },

  footer: {
    tagline:
      "Project management for human + agent teams. Open source, self-hostable, built for the future of work.",
    cta: "Get started",
    groups: {
      product: {
        label: "Product",
        links: [
          { label: "Features", href: "#features" },
          { label: "How it Works", href: "#how-it-works" },
          { label: "Changelog", href: "/changelog" },
          { label: "Download", href: "/download" },
        ],
      },
      resources: {
        label: "Resources",
        links: [
          { label: "Documentation", href: "/docs" },
          { label: "API", href: githubUrl },
          { label: "X (Twitter)", href: "https://x.com/MulticaAI" },
        ],
      },
      company: {
        label: "Company",
        links: [
          { label: "About", href: "/about" },
          { label: "Open Source", href: "#open-source" },
          { label: "GitHub", href: githubUrl },
        ],
      },
    },
    copyright: "\u00a9 {year} Multica. All rights reserved.",
  },

  about: {
    title: "About Multica",
    nameLine: {
      prefix: "Multica \u2014 ",
      mul: "Mul",
      tiplexed: "tiplexed ",
      i: "I",
      nformationAnd: "nformation and ",
      c: "C",
      omputing: "omputing ",
      a: "A",
      gent: "gent.",
    },
    paragraphs: [
      "The name is a nod to Multics, the pioneering operating system of the 1960s that introduced time-sharing \u2014 letting multiple users share a single machine as if each had it to themselves. Unix was born as a deliberate simplification of Multics: one user, one task, one elegant philosophy.",
      "We think the same inflection is happening again. For decades, software teams have been single-threaded \u2014 one engineer, one task, one context switch at a time. AI agents change that equation. Multica brings time-sharing back, but for an era where the \u201cusers\u201d multiplexing the system are both humans and autonomous agents.",
      "In Multica, agents are first-class teammates. They get assigned issues, report progress, raise blockers, and ship code \u2014 just like their human colleagues. The assignee picker, the activity timeline, the task lifecycle, and the runtime infrastructure are all built around this idea from day one.",
      "Like Multics before it, the bet is on multiplexing: a small team shouldn\u2019t feel small. With the right system, two engineers and a fleet of agents can move like twenty.",
      "The platform is fully open source and self-hostable. Your data stays on your infrastructure. Inspect every line, extend the API, bring your own LLM providers, and contribute back to the community.",
    ],
    cta: "View on GitHub",
  },

  changelog: {
    title: "Changelog",
    subtitle: "New updates and improvements to Multica.",
    toc: "All releases",
    categories: {
      features: "New Features",
      improvements: "Improvements",
      fixes: "Bug Fixes",
    },
    entries: [
      {
        version: "0.2.19",
        date: "2026-04-28",
        title: "Kiro CLI Runtime, Desktop Notifications & Issue Label Filter",
        changes: [],
        features: [
          "Kiro CLI added as a local agent runtime option",
          "macOS dock badge for unread issues, plus a native notification when the window is unfocused — click to jump straight to the issue",
          "Issue list now supports filtering by label, combinable with status / priority / assignee",
          "Daemon receives task wakeups over WebSocket — task startup latency drops noticeably",
        ],
        improvements: [
          "List and board status group headers are simpler, with clearer color cues",
          "Author-written markdown links are preserved through linkify",
          "Label attach now applies optimistically, no server round-trip wait",
          "Mention picker's issue search refreshes as you type",
        ],
        fixes: [
          "Deleting a comment now cancels any agent task it triggered — no more ghost runs",
          "Stalled Codex turns now time out instead of holding the slot",
          "Windows daemon no longer dies when the parent shell closes",
          "Agent-to-agent mention threads no longer cause feedback loops",
        ],
      },
      {
        version: "0.2.18",
        date: "2026-04-27",
        title: "Issue Labels, Labs Tab & Sidebar Invite Dot",
        changes: [],
        features: [
          "Issue labels — color-code and filter issues across list, board and detail views",
          "Labs settings tab for experimental toggles",
          "Sidebar shows a dot when you have an unread workspace invite",
        ],
        improvements: [
          "Project picker now shows the selected project's icon",
          "Sidebar parent items stay highlighted on detail pages",
          "Self-hosted deployments correctly honor signup gating env vars",
        ],
        fixes: [
          "Agent comments preserve line breaks again",
          "Desktop RPM no longer conflicts with Slack / VS Code on Fedora",
          "Windows agents handle multi-line prompts correctly",
        ],
      },
      {
        version: "0.2.17",
        date: "2026-04-26",
        title: "Custom Agent Env, Better Failure Messages & Reliability Fixes",
        changes: [],
        features: [
          "`multica agent create/update --custom-env KEY=VALUE` injects custom environment variables into agent runs",
          "Agent failure messages now include a tail of the runtime CLI's stderr — much easier to debug runtime errors",
          "CLI update download timeout is now configurable, so slow links no longer abort `multica update`",
        ],
        improvements: [
          "Daemon reports cancelled tasks as `cancelled` instead of `timeout`, and reconciles agent status when an issue's tasks are cancelled",
          "Server heartbeat split into probe/claim with slow-log + a model-list running-timeout, so a lost heartbeat no longer wedges the UI",
        ],
        fixes: [
          "Server validates `assignee_id` on issue create/update so phantom IDs are rejected, and `DeleteIssue` uses the resolved issue ID",
          "Pi runtime now reads/writes `.pi/skills` instead of the old `.pi/agent/skills` path",
          "Windows daemon uses `CREATE_NEW_CONSOLE` so grandchild console popups no longer appear when launching agents",
          "Autopilot run-only context is now properly forwarded to the agent",
        ],
      },
      {
        version: "0.2.16",
        date: "2026-04-24",
        title: "Chat V2, Issue Right-Click Menu & In-App Feedback",
        changes: [],
        features: [
          "Chat V2 — dedicated sidebar entry and full main-area page for AI conversations",
          "Right-click context menu on issues with a unified action set across list, board, and detail",
          "In-app feedback flow with a new Help launcher centralizing docs, support, and feedback",
          "Autopilot modal redesigned — simpler schema and consistent schedule UI across creation and edit",
          "Skills page redesigned — list + detail pages, scroll-fade card layout, shared PageHeader and mobile nav",
          "Bilingual flat-content rewrite of the docs site — English and Chinese sections share one tree",
        ],
        improvements: [
          "Agent profile card appears on avatar hover for quick context",
          "Native right-click menu on desktop with clipboard actions (copy / paste / cut / select all)",
          "Daemon agent prompts hardened to break self-mention loops between agents",
          "Server readiness health endpoints for proper rollout / ingress probes",
          "Daemon GC defaults tightened and now accept flexible duration suffixes (e.g. `7d`, `12h`)",
          "Test Connection / runtime ping removed — runtime reachability is detected automatically",
        ],
        fixes: [
          "Chat no longer flickers when a streamed response finalizes, and the input box no longer jumps when sending the first message",
          "Desktop reopens the last-used workspace on app start instead of falling back to the first one",
          "Editor preserves nested ordered lists through the readonly render path",
          "CLI `browser-login` now works from a machine that isn't running the server",
          "Daemon suppresses extra terminal windows when launching agents on Windows, and retries local-skill reports on transient server errors",
          "`/api/config` is publicly reachable again so unauthenticated clients can bootstrap",
          "Defense-in-depth owner check on workspace deletion, and `/health/realtime` metrics restricted to authorized callers (security)",
          "Hermes ACP runtime now receives the configured model; OpenClaw agent discovery timeout raised to 30s",
        ],
      },
      {
        version: "0.2.15",
        date: "2026-04-22",
        title: "Local Skills, LaTeX, Focus Mode & Orphan-Task Recovery",
        changes: [],
        features: [
          "Import runtime local Skills into the workspace as first-class artifacts",
          "Orphan-task recovery — abandoned agent runs auto-retry, with manual rerun as fallback",
          "LaTeX rendering in issues, comments and chat",
          "Chat Focus mode — share the page you're on as conversation context",
        ],
        improvements: [
          "Sub-issue `status_changed` events no longer spam parent-issue subscribers",
          "Multi-arch Docker release images built natively per-arch (no QEMU)",
          "Pin sidebar derives fields client-side for snappier reorders",
          "Expanded reserved-slug list so new slugs can't collide with product routes",
        ],
        fixes: [
          "Gemini runtime model list now includes Gemini 3 and CLI aliases",
          "Chat focus button disabled on pages without an anchor",
          "Onboarding pin sync, welcome layout and runtime bootstrap state",
          "`install.ps1` OS architecture detection hardened for more Windows setups",
          "`/download` falls back to the previous release within a 1h freshness window",
        ],
      },
      {
        version: "0.2.11",
        date: "2026-04-21",
        title: "Desktop Cross-Platform Packaging, CLI Self-Update & Board Pagination",
        changes: [],
        features: [
          "Desktop app cross-platform packaging — macOS, Windows, and Linux artifacts from a single release pipeline",
          "`multica update` self-update command — upgrade the CLI and local daemon without reinstalling",
          "Issue board paginates every status column, not only Done — large backlogs stay responsive",
        ],
        fixes: [
          "Workspace isolation enforced end-to-end for agent execution on the local daemon (security)",
          "Windows daemon stays alive after the terminal closes, so background agents keep running",
          "Board cards render their description preview again — list queries no longer strip the description field",
          "OpenClaw agent runtime now reads the real model from agent metadata instead of falling back to a default",
          "Comment Markdown preserved end-to-end — the HTML sanitizer that was stripping formatting has been removed",
        ],
      },
      {
        version: "0.2.8",
        date: "2026-04-20",
        title: "Per-Agent Models, Kimi Runtime & Self-Host Auth",
        changes: [],
        features: [
          "Per-agent `model` field with a provider-aware dropdown — pick the LLM model for each agent from the UI or via `multica agent create/update --model`, with live discovery from each runtime's CLI",
          "Kimi CLI as a new agent runtime (Moonshot AI's `kimi-cli` over ACP), with model selection, auto-approved tool permissions, and streaming tool-call rendering",
          "Expand toggle on inline comment and reply editors for composing long text",
        ],
        fixes: [
          "Posting the result comment is now an explicit, numbered step in agent workflows so final replies reach the issue instead of terminal output",
          "Agent live status card no longer leaks across issues when switching via Cmd+K",
          "Self-hosted session cookies honor the `FRONTEND_ORIGIN` scheme — plain-HTTP deployments stop silently dropping cookies, and `COOKIE_DOMAIN=<ip>` now falls back to host-only with a warning instead of breaking login",
        ],
      },
      {
        version: "0.2.7",
        date: "2026-04-18",
        title: "Sub-Issues from Editor, Self-Host Gating & MCP",
        changes: [],
        features: [
          "Create sub-issue directly from selected text in the editor bubble menu",
          "Self-hosted instance gating — `ALLOW_SIGNUP` and `ALLOWED_EMAIL_*` env vars to restrict account creation",
          "Per-agent `mcp_config` field to restore MCP access",
          "Desktop app hourly update poll with manual check button in settings",
        ],
        fixes: [
          "Session hand-off to desktop when already logged in on web",
          "Open redirect vulnerability on `?next=` validated",
          "OpenClaw stops passing unsupported flags and properly delivers AgentInstructions",
        ],
      },
      {
        version: "0.2.5",
        date: "2026-04-17",
        title: "CLI Autopilot, Cmd+K & Daemon Identity",
        changes: [],
        features: [
          "CLI `autopilot` commands for managing scheduled and triggered automations",
          "CLI `issue subscriber` commands for subscription management",
          "Cmd+K palette extended — theme toggle, quick new issue/project, copy link, switch workspace",
          "Project and sub-issue progress as optional card properties on the issue list",
          "Persistent daemon UUID identity — CLI and desktop share one daemon across restarts and machine moves",
          "Sole-owner workspace leave preflight check",
          "Persist comment collapse state across sessions",
        ],
        fixes: [
          "Agents now triggered on comments regardless of issue status",
          "Codex sandbox config fixed for macOS network access",
          "Editor bubble menu rewritten with @floating-ui/dom for reliable scroll hiding",
          "Autopilot creator automatically subscribed to autopilot-created issues",
          "Autopilot workspace ID correctly resolved for run-only tasks",
          "Desktop restricts `shell.openExternal` to http/https schemes (security)",
          "Duplicate agent names return 409 instead of silently failing",
          "New tabs in desktop inherit current workspace",
        ],
      },
      {
        version: "0.2.1",
        date: "2026-04-16",
        title: "New Agent Runtimes",
        changes: [],
        features: [
          "GitHub Copilot CLI runtime support",
          "Cursor Agent CLI runtime support",
          "Pi agent runtime support",
          "Workspace URL refactor — slug-first routing (`/{slug}/issues`) with legacy URL redirects",
        ],
        fixes: [
          "Codex threads resume across tasks on the same issue",
          "Codex turn errors surfaced instead of reporting empty output",
          "Workspace usage correctly bucketed by task completion time",
          "Autopilot run history rows fully clickable",
          "Workspace isolation enforced on additional daemon and GC endpoints (security)",
          "HTML-escape workspace and inviter names in invitation emails",
          "Dev and production desktop instances can now coexist",
        ],
      },
      {
        version: "0.2.0",
        date: "2026-04-15",
        title: "Desktop App, Autopilot & Invitations",
        changes: [],
        features: [
          "Desktop app for macOS — native Electron app with tab system, built-in daemon management, immersive mode, and auto-update",
          "Autopilot — scheduled and triggered automations for AI agents",
          "Workspace invitations with email notifications and dedicated accept page",
          "Custom CLI arguments per agent for advanced runtime configuration",
          "Chat redesign with unread tracking and improved session management",
          "Create Agent dialog shows runtime owner with Mine/All filter",
        ],
        improvements: [
          "Inter font with CJK fallback and automatic CJK+Latin spacing",
          "Sidebar user menu redesigned as full-row popover",
          "WebSocket ping/pong heartbeat to detect dead connections",
          "Members can now create agents and manage their own skills",
        ],
        fixes: [
          "Agent now triggered on reply in threads where it already participated",
          "Self-hosting: local uploads persist in Docker, WebSocket URL auto-derived for LAN access",
          "Stale cmd+k recent issues resolved",
        ],
      },
      {
        version: "0.1.33",
        date: "2026-04-14",
        title: "Gemini CLI & Agent Env Vars",
        changes: [],
        features: [
          "Google Gemini CLI as a new agent runtime with live log streaming",
          "Custom environment variables for agents (router/proxy mode) with dedicated settings tab",
          "\"Set parent issue\" and \"Add sub-issue\" actions in issue context menu",
          "CLI `--parent` flag for issue update and `--content-stdin` for piping comment content",
          "Sub-issues inherit parent project automatically",
        ],
        improvements: [
          "Editor bubble menu and link preview rewritten for reliability",
          "OpenClaw backend P0+P1 improvements (multi-line JSON, incremental parsing)",
          "Self-hosted WebSocket URL auto-derived for LAN access",
        ],
        fixes: [
          "S3 upload keys scoped by workspace (security)",
          "Workspace membership validation for subscriptions and uploads (security)",
          "Active tasks auto-cancelled when issue status changes to cancelled",
          "Agent task stall when process hangs on stdout",
          "Daemon trigger prompt now embeds the actual triggering comment content",
          "Login and dashboard redirect stability improvements",
        ],
      },
      {
        version: "0.1.28",
        date: "2026-04-13",
        title: "Windows Support, Auth & Onboarding",
        changes: [],
        features: [
          "Windows support — CLI installation, daemon, and release builds",
          "Auth migrated to HttpOnly Cookie with WebSocket Origin whitelist",
          "Full-screen onboarding wizard for new workspaces",
          "Resizable Master Agent chat window with session history improvements",
          "Token usage log scanning for OpenCode, OpenClaw, and Hermes runtimes",
        ],
        fixes: [
          "WebSocket first-message authentication security fix",
          "Content-Security-Policy response header",
          "Sub-issue progress computed from database instead of paginated client cache",
        ],
      },
      {
        version: "0.1.27",
        date: "2026-04-12",
        title: "One-Click Setup, Self-Hosting & Stability",
        changes: [],
        features: [
          "One-click install & setup — `curl | bash` installs CLI, `--with-server` bootstraps full self-hosting, `multica setup` configures your environment",
          "Self-hosted storage — local file fallback when S3 is unavailable, plus custom S3 endpoint support (MinIO)",
          "Inline property editing (priority, status, lead) on project list page",
        ],
        improvements: [
          "Stale agent tasks auto-swept; agent live card shows immediately without waiting for first message",
          "Comment attachments uploaded via CLI now visible in the UI",
          "Pinned items scoped per user with fixed sidebar pin action",
        ],
        fixes: [
          "Workspace ownership checks on daemon API routes and attachment uploads",
          "Markdown sanitizer preserves code blocks from HTML entity escaping",
          "Next.js upgraded to ^16.2.3 for CVE-2026-23869",
          "OpenClaw backend rewritten to match actual CLI interface",
        ],
      },
      {
        version: "0.1.24",
        date: "2026-04-11",
        title: "Security & Notifications",
        changes: [],
        features: [
          "Parent issue subscribers notified on sub-issue changes",
          "CLI `--project` filter for issue list",
        ],
        improvements: [
          "Meta-skill workflow defers to agent Skills instead of hardcoded logic",
        ],
        fixes: [
          "Workspace ownership checks on all daemon API routes",
          "Workspace ownership validation for attachment uploads and queries",
          "Reply mentions no longer inherit parent thread's agent mentions",
          "Agent comment creation missing workspace ID",
          "Self-hosting Docker build failures (file permissions, CRLF, missing deps)",
        ],
      },
      {
        version: "0.1.23",
        date: "2026-04-11",
        title: "Pinning, Cmd+K & Projects",
        changes: [],
        features: [
          "Pin issues and projects to sidebar with drag-and-drop reordering",
          "Cmd+K command palette — recent issues, page navigation, and project search",
          "Project detail sidebar with properties panel (replaces overview tab)",
          "Project filter in Issues tab",
          "Project completion progress in project list",
          "Auto-fill project when creating issue via 'C' shortcut on project page",
          "Assignee dropdown sorted by user's assignment frequency",
        ],
        fixes: [
          "Markdown XSS — sanitize HTML rendering in comments with rehype-sanitize and server-side bluemonday",
          "Project kanban issue counts incorrect",
          "Self-hosting Docker build missing tsconfig dependencies",
          "Cmd+K requiring double ESC to close",
        ],
      },
      {
        version: "0.1.22",
        date: "2026-04-10",
        title: "Self-Hosting, ACP & Documentation",
        changes: [],
        features: [
          "Full-stack Docker Compose for one-command self-hosting",
          "Hermes Agent Provider via ACP protocol",
          "Documentation site with Fumadocs (Getting Started, CLI reference, Agents guide)",
          "Mobile-responsive sidebar and inbox layout",
          "Token usage display per issue in the detail sidebar",
          "Switch agent runtime from the UI",
          "'C' keyboard shortcut for quick issue creation",
          "Chat session history panel for archived conversations",
          "Minimum CLI version check in daemon for Claude Code and Codex",
          "OpenClaw and OpenCode added to landing page",
          "`make dev` one-command local development setup",
        ],
        improvements: [
          "Sidebar redesign — Personal / Workspace grouping, user profile footer, ⌘K search input",
          "Search ranking — case-insensitive matching, identifier search (MUL-123), multi-word support",
          "Search result keyword highlighting",
          "Daily token usage chart with cleaner Y-axis and per-category tooltip",
          "Master Agent multiline input support",
          "Unified picker components (Status, Priority, DueDate, Project, Assignee) across all views",
          "Workspace-scoped storage isolation with auto-rehydration on switch",
          "Startup warnings for missing env vars in self-hosted deployments",
        ],
        fixes: [
          "Sub-issue deletion not invalidating parent's children cache",
          "Search index compatibility with pg_bigm 1.2 on RDS",
          "Create Agent showing \"No runtime available\" when runtimes exist",
          "Claude stream-json startup hangs",
          "Multiple agents unable to queue tasks for the same issue",
          "Logout not clearing workspace and query cache",
          "Drag-drop overlay too small on empty editors",
          "Skills import hardcoding \"main\" as default branch",
          "PAT authentication not working on WebSocket endpoint",
          "Runtime deletion blocked when all bound agents are archived",
        ],
      },
      {
        version: "0.1.21",
        date: "2026-04-09",
        title: "Projects, Search & Monorepo",
        changes: [
          "Project entity with full-stack CRUD — create, edit, and organize issues by project",
          "Project picker in the create-issue modal and CLI project commands",
          "Full-text search for issues with pg_bigm",
          "Monorepo extraction — shared packages for core, UI, and views (Turborepo)",
          "Fullscreen agent execution transcript view",
          "Drag-and-drop file upload with file card display in the editor",
          "Attachment section with image grid and file cards on issues",
          "Runtime owner tracking, filtering, avatar display, and point-to-point update notifications",
          "Sub-issue progress indicator in list view rows",
          "Done issue pagination in list view",
          "Codex session log scan for token usage reporting",
          "Daemon repo-cache fix for stale initial snapshots",
        ],
      },
      {
        version: "0.1.20",
        date: "2026-04-08",
        title: "Sub-Issues, TanStack Query & Usage Tracking",
        changes: [
          "Sub-issue support — create, view, and manage child issues within any issue",
          "Full migration to TanStack Query for server state (issues, inbox, workspace, runtimes)",
          "Per-task token usage tracking across all agent providers",
          "Multiple agents can now run concurrently on the same issue",
          "Board view: Done column shows total count with infinite scroll",
          "ReadonlyContent component for lightweight Markdown display in comments",
          "Optimistic UI updates for reactions and mutations with rollback",
          "WebSocket-driven cache invalidation replaces polling and refetch-on-focus",
          "Browser session persists during CLI login flow",
          "Daemon reuses existing worktrees by updating to latest remote",
          "Fixed slow tab switching caused by dynamic root layout",
        ],
      },
      {
        version: "0.1.18",
        date: "2026-04-07",
        title: "OAuth, OpenClaw & Issue Loading",
        changes: [
          "Google OAuth login",
          "OpenClaw runtime support for running agents on OpenClaw infrastructure",
          "Redesigned agent live card — always sticky with manual expand/collapse toggle",
          "Load all open issues without pagination limit; closed issues paginate on scroll",
          "JWT and CloudFront cookie expiration extended from 72 hours to 30 days",
          "Remember last selected workspace after re-login",
          "Daemon ensures multica CLI is on PATH in agent task environment",
          "PR template and CLI install guide for agent-driven setup",
        ],
      },
      {
        version: "0.1.17",
        date: "2026-04-05",
        title: "Comment Pagination & CLI Polish",
        changes: [
          "Comment list pagination in both the API and CLI",
          "Inbox archive now dismisses all items for the same issue at once",
          "CLI help output overhauled to match gh CLI style with examples",
          "Attachments use UUIDv7 as S3 key and auto-link on issue/comment creation",
          "@mention assigned agents on done or cancelled issues",
          "Reply @mention inheritance skips when the reply only mentions members",
          "Worktree setup preserves existing .env.worktree variables",
        ],
      },
      {
        version: "0.1.15",
        date: "2026-04-03",
        title: "Editor Overhaul & Agent Lifecycle",
        changes: [
          "Unified Tiptap editor with a single Markdown pipeline for editing and display",
          "Reliable Markdown paste, inline code spacing, and link styling",
          "Agent archive and restore — soft delete replaces hard delete",
          "Archived agents hidden from default agent list",
          "Skeleton loading states, error toasts, and confirmation dialogs across the app",
          "OpenCode added as a supported agent provider",
          "Reply-triggered agent tasks now inherit thread-root @mentions",
          "Granular real-time event handling for issues and inbox — no more full refetches",
          "Unified image upload flow for paste and button in the editor",
        ],
      },
      {
        version: "0.1.14",
        date: "2026-04-02",
        title: "Mentions & Permissions",
        changes: [
          "@mention issues in comments with server-side auto-expansion",
          "@all mention to notify every workspace member",
          "Inbox auto-scrolls to the referenced comment from a notification",
          "Repositories extracted into a standalone settings tab",
          "CLI update support from the web runtime page and direct download for non-Homebrew installs",
          "CLI commands for viewing issue execution runs and run messages",
          "Agent permission model — owners and admins manage agents, members manage skills on their own agents",
          "Per-issue serial execution to prevent concurrent task collisions",
          "File upload now supports all file types",
          "README redesign with quickstart guide",
        ],
      },
      {
        version: "0.1.13",
        date: "2026-04-01",
        title: "My Issues & i18n",
        changes: [
          "My Issues page with kanban board, list view, and scope tabs",
          "Simplified Chinese localization for the landing page",
          "About and Changelog pages for the marketing site",
          "Agent avatar upload in settings",
          "Attachment support for CLI comments and issue/comment APIs",
          "Unified avatar rendering with ActorAvatar across all pickers",
          "SEO optimization and auth flow improvements for landing pages",
          "CLI defaults to production API URLs",
          "License changed to Apache 2.0",
        ],
      },
      {
        version: "0.1.3",
        date: "2026-03-31",
        title: "Agent Intelligence",
        changes: [
          "Trigger agents via @mention in comments",
          "Stream live agent output to issue detail page",
          "Rich text editor \u2014 mentions, link paste, emoji reactions, collapsible threads",
          "File upload with S3 + CloudFront signed URLs and attachment tracking",
          "Agent-driven repo checkout with bare clone cache for task isolation",
          "Batch operations for issue list view",
          "Daemon authentication and security hardening",
        ],
      },
      {
        version: "0.1.2",
        date: "2026-03-28",
        title: "Collaboration",
        changes: [
          "Email verification login and browser-based CLI auth",
          "Multi-workspace daemon with hot-reload",
          "Runtime dashboard with usage charts and activity heatmaps",
          "Subscriber-driven notification model replacing hardcoded triggers",
          "Unified activity timeline with threaded comment replies",
          "Kanban board redesign with drag sorting, filters, and display settings",
          "Human-readable issue identifiers (e.g. JIA-1)",
          "Skill import from ClawHub and Skills.sh",
        ],
      },
      {
        version: "0.1.1",
        date: "2026-03-25",
        title: "Core Platform",
        changes: [
          "Multi-workspace switching and creation",
          "Agent management UI with skills",
          "Unified agent SDK supporting Claude Code and Codex backends",
          "Comment CRUD with real-time WebSocket updates",
          "Task service layer and daemon REST protocol",
          "Event bus with workspace-scoped WebSocket isolation",
          "Inbox notifications with unread badge and archive",
          "CLI with cobra subcommands for workspace and issue management",
        ],
      },
      {
        version: "0.1.0",
        date: "2026-03-22",
        title: "Foundation",
        changes: [
          "Go backend with REST API, JWT auth, and real-time WebSocket",
          "Next.js frontend with Linear-inspired UI",
          "Issues with board and list views and drag-and-drop kanban",
          "Agents, Inbox, and Settings pages",
          "One-click setup, migration CLI, and seed tool",
          "Comprehensive test suite \u2014 Go unit/integration, Vitest, Playwright E2E",
        ],
      },
    ],
  },
  download: {
    hero: {
      macArm64: {
        title: "Multica for macOS",
        sub: "Apple Silicon · bundled daemon, zero setup",
        primary: "Download (.dmg)",
        altZip: "or download .zip",
      },
      macIntel: {
        title: "Multica for macOS",
        sub: "Apple Silicon required — Intel Macs not yet supported.",
        disabledCta: "Apple Silicon required",
        intelHint:
          "On an Intel Mac? Use the CLI below — it runs the same daemon.",
      },
      winX64: {
        title: "Multica for Windows",
        sub: "Bundled daemon, zero setup",
        primary: "Download (.exe)",
      },
      winArm64: {
        title: "Multica for Windows",
        sub: "ARM · bundled daemon, zero setup",
        primary: "Download (.exe)",
      },
      linux: {
        title: "Multica for Linux",
        sub: "Bundled daemon, zero setup",
        primary: "Download AppImage",
        altFormats: "or .deb / .rpm",
      },
      unknown: {
        title: "Choose your platform",
        sub: "All installers are listed below.",
      },
      safariMacHint: "On an Intel Mac? Use the CLI below.",
      archFallbackHint: "Wrong architecture? See all formats below.",
    },
    allPlatforms: {
      title: "All platforms",
      macLabel: "macOS · Apple Silicon",
      winX64Label: "Windows · x64",
      winArm64Label: "Windows · ARM64",
      linuxX64Label: "Linux · x64",
      linuxArm64Label: "Linux · ARM64",
      formatDmg: ".dmg",
      formatZip: ".zip",
      formatExe: ".exe",
      formatAppImage: ".AppImage",
      formatDeb: ".deb",
      formatRpm: ".rpm",
      intelNote:
        "Apple Silicon only — Intel Macs not supported in this release.",
      unavailable: "Not available",
    },
    cli: {
      title: "Prefer the CLI?",
      sub: "For servers, remote dev boxes, and headless setups. Same daemon as Desktop, installed via terminal.",
      installLabel: "Install",
      startLabel: "Start daemon",
      sshNote: "Already on a server? Same commands work over SSH.",
      copyLabel: "Copy",
      copiedLabel: "Copied",
    },
    cloud: {
      title: "Cloud runtime (waitlist)",
      sub: "We’ll host the runtime for you. Not live yet — leave your email to be notified.",
    },
    footer: {
      releaseNotes: "What’s new in {version}",
      allReleases: "View all releases",
      currentVersion: "Current version: {version}",
      versionUnavailable: "Version unavailable — check GitHub",
    },
  },
  };
}
