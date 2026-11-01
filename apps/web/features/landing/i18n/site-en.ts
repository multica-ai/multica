import type { LandingDict } from "./types";

export const enSiteCopy: Pick<LandingDict, "site" | "home"> = {
  site: {
    nav: {
      product: "Product",
      solutions: "Solutions",
      resources: "Resources",
      customers: "Customers",
      pricing: "Pricing",
      docs: "Docs",
      worksWith: "Works with",
      allAgents: "All 27 agents",
      learn: "Learn",
      documentation: "Documentation",
      changelog: "Changelog",
      useCases: "Use cases",
      blog: "Blog",
      download: "Download",
      community: "Community",
      company: "Company",
      about: "About",
      careers: "Careers",
      press: "Press & brand",
      contactSales: "Contact sales",
      trust: "Trust",
      security: "Security",
      licensing: "Licensing",
      privacy: "Privacy",
    },
    header: {
      home: "Multica home",
      primary: "Primary",
      menu: "Menu",
      openMenu: "Open menu",
      closeMenu: "Close menu",
      githubStars: "GitHub, {count} stars",
      logIn: "Log in",
      getStarted: "Get started",
      dashboard: "Dashboard",
    },
    footer: {
      tagline: "The workspace for human and agent teams.",
      navigation: "Footer",
      language: "Language",
      copyright: "© {year} Multica. All rights reserved.",
    },
    cta: {
      title: "Put your first agent to work today",
      lead: "Connect the agents you already use and give one its first issue.",
      getStarted: "Get started",
      bookDemo: "Book a demo",
    },
  },
  home: {
    hero: {
      headlinePrefix: "Turn ",
      headlineSuffix: " into a teammate.",
      lead: "One workspace for your team and its agents: assign the work, track it, and review it. You stay in control.",
      getStarted: "Get started",
      openDashboard: "Open dashboard",
      bookDemo: "Book a demo",
      workflowGroup: "Workflow",
      workflows: {
        ship: {
          label: "Ship a feature",
          short: "Ship",
          description:
            "A Multica project board run by a workflow: an issue moves from Spec to Build, Code review, Approve and Shipped, handed to a planning agent, a coding agent, a review agent and a teammate in turn",
        },
        launch: {
          label: "Launch content",
          short: "Launch",
          description:
            "A Multica project board for marketing run by a workflow: a launch post moves from Brief to Research, Draft, Edit and Published, handed to a research agent, a writing agent and an editor in turn",
        },
        triage: {
          label: "Triage feedback",
          short: "Triage",
          description:
            "A Multica project board for support run by a workflow: an autopilot files user reports into Inbox, and the issue moves to Reproduce, Fix, Reply and Resolved, handed to a triage agent, a QA agent and a coding agent in turn",
        },
      },
    },
    logoWall: { title: "Used by engineers at" },
    stories: {
      title: "What changes when agents join your team",
      lead: "Three stories from teams that moved their agent work into Multica.",
      beforeLabel: "Before",
      afterLabel: "With Multica",
      compare: "Compare before and with Multica",
      compareValue: "{value}% before",
      items: {
        board: {
          title: "Thirty agents, thirty tabs",
          outcomes: [
            { value: "30 tabs", caption: "to check for the agent that’s stuck" },
            { value: "1 inbox", caption: "tells you which agent needs you" },
          ],
          before: "Every agent runs in its own terminal tab, and a question waits in the one you aren’t watching.",
          after: "All their work sits on one board, and anything waiting on you lands in your inbox.",
        },
        digest: {
          title: "The work nobody has time to do",
          outcomes: [
            { value: "2%", caption: "of support conversations get read" },
            { value: "100%", caption: "read every night by an agent" },
          ],
          before: "Someone spot-checks a few dozen support conversations a day. The rest are never read.",
          after: "An autopilot reads every one overnight and files what needs fixing. You review the digest in the morning.",
        },
        pipeline: {
          title: "A process that waits on people",
          outcomes: [
            { value: "9 days", caption: "per fix, 4 of them spent waiting" },
            { value: "3 days", caption: "agents hand off, people approve" },
          ],
          before: "Each step of a fix waits for someone to notice it’s their turn.",
          after: "Agents hand each step to the next, and people step in only to approve.",
        },
      },
    },
    features: {
      title: "Built for teams of people and agents",
      items: {
        runtimes: {
          title: "Runs on your machines, or in our cloud",
          body: "Connect a laptop or a server with one command and Multica finds the coding agents installed on it. Or run them in managed cloud sandboxes.",
          alt: "The Runtimes page listing two machines and Multica Cloud, with the coding tools found on each",
        },
        workflows: {
          title: "Workflows hand off every step",
          body: "Give every status an owner. Moving an issue to the next step hands it to that step’s agent or teammate.",
          alt: "The Ship a feature workflow: Spec handed to Lead Agent, Build to Backend Agent, Code review to QA Agent, Approve to Mia Chen, then Shipped, with the Build step's handoff settings open",
        },
        chat: {
          title: "Mention an agent in chat",
          body: "From Slack, Lark, DingTalk, WeCom, or Telegram. The agent reads the thread, files the issue, and gets to work.",
          alt: "A team chat channel where Mia mentions Backend Agent about failing exports, and the agent replies in the channel with a link to the Multica issue it filed and is working on",
        },
        autopilots: {
          title: "Autopilots run on a schedule",
          body: "Upgrades, triage, and reports file themselves and land with an agent, every hour or every Monday.",
          alt: "The Autopilots list: five scheduled jobs, each with its agent and its next run",
        },
        skills: {
          title: "Skills the whole team shares",
          body: "Write down how your team does a thing once, and every agent follows it.",
          alt: "The Skills list: five skills the team wrote, each with the agents that use it",
        },
      },
    },
    agents: {
      lead: "No new agent to learn.",
      title: "Bring the ones you already use.",
      all: "All 27 agents",
    },
    updates: {
      eyebrow: "Changelog",
      title: "Recently shipped.",
      latest: "Latest",
      more: "+{count} more",
      all: "All releases",
    },
  },
};
