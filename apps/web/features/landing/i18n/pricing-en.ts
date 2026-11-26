import type { PricingDict } from "./types";

export const enPricingCopy: PricingDict = {
  eyebrow: "Pricing",
  title: "Pay for your team. Run as many agents as you like.",
  lead: "Every plan includes unlimited agents on your own runtimes, and runs you start yourself are never counted. Bring Claude Code, Codex, or any coding agent you already use.",
  launchOffer: { badge: "Launch offer", text: "30% off your first year on annual plans, until {date}" },
  billing: { label: "Billing period", yearly: "Yearly", monthly: "Monthly", save: "Save 20%" },
  recommended: "Recommended",
  soon: "Soon",
  included: "Included",
  notIncluded: "Not included",
  yearlyNote: "{price} if billed monthly",
  monthlyNote: "{price} / month if billed yearly",
  firstYear: "{price} in your first year",
  plans: {
    free: {
      name: "Free",
      audience: "For individuals and small teams getting started.",
      cadence: "forever",
      cta: "Get started",
      ctaNote: "No credit card required",
      includes: "Includes",
      features: [
        { label: "Up to 5 members" },
        { label: "Unlimited agents, runtimes, squads, and skills" },
        { label: "Unlimited issues, projects, and run history" },
        { label: "200 autopilot runs / month" },
        { label: "1 GB file storage" },
        { label: "30-day usage dashboard" },
      ],
    },
    pro: {
      name: "Pro",
      audience: "Remove the limits for teams that ship every day.",
      cadence: "per member / month",
      cta: "Get started",
      ctaNote: "Cancel anytime",
      includes: "Everything in Free, plus",
      features: [
        { label: "Unlimited members" },
        { label: "Free guests who don't take a seat" },
        { label: "1,000 autopilot runs / member / month" },
        { label: "10 GB storage per member" },
        { label: "90-day usage dashboard" },
        { label: "Multica Cloud credits", soon: true },
      ],
    },
    business: {
      name: "Business",
      audience: "Control who sees and runs what as your team grows.",
      cadence: "per member / month",
      cta: "Start 14-day trial",
      ctaNote: "No credit card ·",
      ctaNoteLink: "Talk to sales",
      includes: "Everything in Pro, plus",
      features: [
        { label: "Private projects" },
        { label: "Agent and runtime access by member or team" },
        { label: "Team app accounts and approval rules" },
        { label: "Google sign-in enforcement and domain auto-join" },
        { label: "Per-member usage and cost reports" },
        { label: "3,000 autopilot runs / member / month" },
        { label: "100 GB storage per member" },
        { label: "365-day usage dashboard" },
      ],
    },
    enterprise: {
      name: "Enterprise",
      audience: "For security, compliance, and self-hosting needs.",
      cadence: "annual contract",
      cta: "Contact sales",
      ctaNote: "Multica Cloud or self-hosted",
      includes: "Everything in Business, plus",
      features: [
        { label: "SAML SSO and SCIM" },
        { label: "Audit log" },
        { label: "Data retention and deletion policies" },
        { label: "IP allowlist" },
        { label: "Custom branding and domain" },
        { label: "Self-hosted deployment" },
        { label: "Invoice billing and dedicated support" },
      ],
    },
  },
  customPrice: "Custom",
  cloudCredits: {
    title: "Multica Cloud credits",
    body: "Run agents on hosted runtimes and models without your own machine. Credits are priced close to cost, shared across the workspace, and self-hosted instances can buy them too.",
  },
  selfHost: {
    title: "Self-hosting Multica?",
    body: "The Community edition is free for internal use in your organization. Enterprise adds SSO, audit log, custom branding, and support for your own deployment.",
    contactSales: "Contact sales",
    licensing: "Licensing",
  },
  compare: {
    title: "Compare plans",
    lead: "Agents are unlimited everywhere. Pro removes the limits; Business adds access and control.",
    groups: [
      {
        title: "Team",
        rows: [
          { label: "Members", cells: ["Up to 5", "Unlimited", "Unlimited", "Unlimited"] },
          { label: "Guests", hint: "View and comment, free, no seat", cells: [false, true, true, true] },
          { label: "Issues, projects, and run history", cells: ["Unlimited", "Unlimited", "Unlimited", "Unlimited"] },
          { label: "File storage", cells: ["1 GB", "10 GB / member", "100 GB / member", "Custom"] },
        ],
      },
      {
        title: "Agents and automation",
        rows: [
          { label: "Agents, runtimes, squads, and skills", cells: ["Unlimited", "Unlimited", "Unlimited", "Unlimited"] },
          { label: "Runs you start yourself", cells: ["Unlimited", "Unlimited", "Unlimited", "Unlimited"] },
          {
            label: "Autopilot runs",
            hint: "Shared across the workspace",
            cells: ["200 / month", "1,000 / member / mo", "3,000 / member / mo", "Custom"],
          },
          { label: "Extra autopilot runs", cells: [false, "$10 per 1,000", "$10 per 1,000", "Custom"] },
          { label: "Multica Cloud credits", soon: true, cells: [false, "Included", "More included", "Custom"] },
        ],
      },
      {
        title: "Insights",
        rows: [
          { label: "Usage dashboard history", cells: ["30 days", "90 days", "365 days", "Custom"] },
          { label: "Per-member usage and cost, CSV export", cells: [false, false, true, true] },
        ],
      },
      {
        title: "Access and control",
        rows: [
          { label: "Private projects", cells: [false, false, true, true] },
          { label: "Agent access for specific members or teams", cells: [false, false, true, true] },
          { label: "Runtime sharing with specific members", cells: [false, false, true, true] },
          { label: "Team app accounts and approval rules", cells: [false, false, true, true] },
        ],
      },
      {
        title: "Security and admin",
        rows: [
          { label: "Admin roles", cells: [true, true, true, true] },
          { label: "Google sign-in enforcement", cells: [false, false, true, true] },
          { label: "Domain auto-join", cells: [false, false, true, true] },
          { label: "SAML SSO and SCIM", cells: [false, false, false, true] },
          { label: "Audit log", cells: [false, false, false, true] },
          { label: "Data retention, deletion, and IP allowlist", cells: [false, false, false, true] },
          { label: "Custom branding and domain", cells: [false, false, false, true] },
          { label: "Self-hosted deployment", cells: [false, false, false, true] },
        ],
      },
      {
        title: "Billing and support",
        rows: [
          { label: "Payment", cells: ["—", "Card", "Card", "Invoice and PO"] },
          { label: "Support", cells: ["Community", "Email", "Priority email", "Dedicated"] },
        ],
      },
    ],
  },
  faq: {
    title: "Questions",
    items: [
      {
        question: "Do agents count as members?",
        answer:
          "No. You pay for the people on your team. Agents, runtimes, squads, and skills are unlimited on every plan.",
      },
      {
        question: "What counts as an autopilot run?",
        answer:
          "Each run an autopilot starts on a schedule or trigger. Runs you start yourself, by assigning an issue, chatting, or mentioning an agent, are never counted. Runs are pooled across the workspace, and Pro and Business can add 1,000 more for $10.",
      },
      {
        question: "Who are guests?",
        answer:
          "People who only need to follow along. Guests can view and comment on the projects you share with them, don't take a seat, and are free on Pro and Business.",
      },
      {
        question: "What happens when the trial ends?",
        answer:
          "New workspaces get 14 days of Business without a credit card. When it ends, you pick a plan or move to Free. Nothing is deleted, and Business settings you already set up keep working; you can change them again after upgrading.",
      },
      {
        question: "How does the launch offer work?",
        answer:
          "Subscribe to Pro or Business with annual billing by {date} and your first year is 30% off. Renewals are at the regular annual price.",
      },
      {
        question: "Can I self-host Multica?",
        answer:
          "Yes. The Community edition is free for internal use in your organization. Enterprise adds SSO, audit log, custom branding, and support for self-hosted deployments.",
      },
    ],
  },
};
