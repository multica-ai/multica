import type { LandingDict } from "./types";

export const enSiteCopy: Pick<LandingDict, "site" | "home"> = {
  site: {
    nav: {
      product: "Product",
      solutions: "Solutions",
      resources: "Resources",
      customers: "Customers",
      pricing: "Pricing",
      documentation: "Documentation",
      changelog: "Changelog",
      useCases: "Use cases",
      blog: "Blog",
      download: "Download",
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
      download: "Download",
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
    logoWall: { title: "Used by people at these companies and universities" },
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
