import { z } from "zod";
import {
  AGENT_INSTRUCTION_TEMPLATES,
  type AgentInstructionTemplate,
} from "../agents/instruction-templates";

/**
 * The starter company (spec 3.4): one squad card per squad, the pipeline's
 * stage owners, and the arithmetic the wizard shows before anything is
 * created. Cards are data. The apply service on the server matches squads and
 * agents by these keys and names, so changing a card changes what a rerun does.
 */

export const COMPANY_FLOOR_KEYS = [
  "executive",
  "research",
  "product",
  "build",
  "launch",
  "governance",
] as const;
export type CompanyFloorKey = (typeof COMPANY_FLOOR_KEYS)[number];

/**
 * The floor names the apply service creates when a workspace lacks them. They
 * stay English, like the squad and agent names, so a rerun finds them.
 */
export const COMPANY_FLOOR_NAMES: Record<CompanyFloorKey, string> = {
  executive: "Executive",
  research: "Research",
  product: "Product",
  build: "Build",
  launch: "Launch",
  governance: "Governance",
};

export const COMPANY_SQUAD_KEYS = [
  "executive_office",
  "market_research",
  "opportunity_lab",
  "competitor_intelligence",
  "customer_voice",
  "product_discovery",
  "product_analysis",
  "planning",
  "build",
  "release_ops",
  "growth_gtm",
  "support",
  "finance_risk",
  "people_performance",
] as const;
export type CompanySquadKey = (typeof COMPANY_SQUAD_KEYS)[number];

export const COMPANY_STAGE_KEYS = [
  "validate",
  "define",
  "plan",
  "build",
  "launch",
  "operate",
  "learn",
] as const;
export type CompanyStageKey = (typeof COMPANY_STAGE_KEYS)[number];

export interface CompanyStageOwner {
  key: CompanyStageKey;
  name: string;
  /**
   * The squad the stage's `owner` names in the pipeline. Stages a second squad
   * co-owns list it in that squad's card, not here. Learn is owned by whichever
   * squad owned Build; the starter company gives Build to the Build squad.
   */
  owner: CompanySquadKey;
}

export const COMPANY_STAGE_OWNERS: readonly CompanyStageOwner[] = [
  { key: "validate", name: "Validate", owner: "product_discovery" },
  { key: "define", name: "Define", owner: "product_analysis" },
  { key: "plan", name: "Plan", owner: "planning" },
  { key: "build", name: "Build", owner: "build" },
  { key: "launch", name: "Launch", owner: "release_ops" },
  { key: "operate", name: "Operate", owner: "support" },
  { key: "learn", name: "Learn", owner: "build" },
];

/**
 * How the wizard starts routines. "all" activates every routine of the chosen
 * squads. "per_floor" activates the routines created active plus each floor's
 * lead routine. "leave_paused" keeps the creation default: only the Secretary's
 * brief and the market scan run.
 */
export const COMPANY_START_OPTIONS = ["all", "per_floor", "leave_paused"] as const;
export type CompanyStart = (typeof COMPANY_START_OPTIONS)[number];

const KEY_PATTERN = /^[a-z][a-z0-9_]*$/;
const CRON_PATTERN = /^\S+( \S+){4}$/;

const CompanyRoleSchema = z.object({
  key: z.string().regex(KEY_PATTERN),
  /** Name of the agent the apply service creates. A non-archived agent with this name is reused. */
  name: z.string().min(1).max(64),
  description: z.string().min(1).max(200),
  agentTemplate: z.custom<AgentInstructionTemplate["id"]>(
    (value) => AGENT_INSTRUCTION_TEMPLATES.some((t) => t.id === value),
    { message: "unknown instruction template" },
  ),
  /**
   * "secretary" means the workspace's existing Secretary agent is the role;
   * the apply service links it and does not create a second one.
   */
  runtime: z.enum(["worker", "secretary"]),
  /** Roster text the leader reads; context only. */
  role: z.string().min(1),
});

const CompanyRoutineSchema = z.object({
  key: z.string().regex(KEY_PATTERN),
  /** Name of the autopilot. Reused by name, as the squad quickstart does. */
  title: z.string().min(1).max(120),
  cadence: z.enum(["daily", "weekly", "monthly"]),
  /** Five-field cron, in the timezone the wizard sends. */
  cron: z.string().regex(CRON_PATTERN),
  /** The issue body each run starts from. */
  description: z.string().min(1),
  /** Created active. Every other routine is created paused (spec 3.4: no surprise spend). */
  activeAtCreate: z.boolean(),
  /** Activated by "Start per floor": the lead routine of its floor. */
  floorStart: z.boolean(),
});

const CompanyCharterSchema = z.object({
  mission: z.string().min(1),
  responsibilities: z
    .array(z.object({ key: z.string().min(1), text: z.string().min(1) }))
    .min(1),
  outputs: z.array(
    z.object({
      key: z.string().min(1),
      kind: z.enum(["issue", "proposal", "kb_document", "report"]),
      cadenceHint: z.string(),
    }),
  ),
  boundaries: z.string().min(1),
  escalation: z.string().min(1),
  intake: z.string().min(1),
});

/**
 * Phase 4 stubs. Phase 3 installs squads at autonomy level 1, with no goals and
 * no budget share, so these values change nothing yet. They sit on the card so
 * phase 4 fills them in one place.
 */
const GovernanceStubSchema = z.object({
  /** Stub until phase 4 (spec 4.4): the autonomy level a squad starts at. */
  autonomyLevel: z.literal(1),
  /** Stub until phase 4 (spec 4.6): the squad's default share of the budget, in percent. */
  budgetSharePct: z.number().min(0).max(100),
  /** Stub until phase 4 (spec 4.1): goals the company scaffolds for the squad. Phase 3 scaffolds none. */
  goalsScaffold: z.array(z.string()),
});

export const CompanySquadCardSchema = z
  .object({
    key: z.enum(COMPANY_SQUAD_KEYS),
    /** Name of the squad. A squad with this name is reused. */
    name: z.string().min(1).max(64),
    description: z.string().min(1).max(200),
    floor: z.enum(COMPANY_FLOOR_KEYS),
    leader: z.string().regex(KEY_PATTERN),
    roles: z.array(CompanyRoleSchema).min(1),
    routines: z.array(CompanyRoutineSchema),
    /** Pipeline stages this squad owns or co-owns. The stage's primary owner is in COMPANY_STAGE_OWNERS. */
    stages: z.array(z.enum(COMPANY_STAGE_KEYS)),
    charter: CompanyCharterSchema,
    governance: GovernanceStubSchema,
  })
  .superRefine((card, ctx) => {
    const roleKeys = card.roles.map((role) => role.key);
    if (new Set(roleKeys).size !== roleKeys.length) {
      ctx.addIssue({ code: "custom", message: "role keys must be unique", path: ["roles"] });
    }
    if (!roleKeys.includes(card.leader)) {
      ctx.addIssue({ code: "custom", message: "leader must be one of the roles", path: ["leader"] });
    }
    const titles = card.routines.map((routine) => routine.title);
    if (new Set(titles).size !== titles.length) {
      ctx.addIssue({ code: "custom", message: "routine titles must be unique", path: ["routines"] });
    }
    const routineKeys = card.routines.map((routine) => routine.key);
    if (new Set(routineKeys).size !== routineKeys.length) {
      ctx.addIssue({ code: "custom", message: "routine keys must be unique", path: ["routines"] });
    }
  });

export type CompanySquadCard = z.infer<typeof CompanySquadCardSchema>;
export type CompanyRole = CompanySquadCard["roles"][number];
export type CompanyRoutine = CompanySquadCard["routines"][number];
export type CompanyRoutineCadence = CompanyRoutine["cadence"];

export const COMPANY_SQUAD_CARDS: readonly CompanySquadCard[] = [
  {
    key: "executive_office",
    name: "Executive Office",
    description: "The Secretary and the boss, who run the company as a whole.",
    floor: "executive",
    leader: "secretary",
    roles: [
      {
        key: "secretary",
        name: "Secretary",
        description: "Reads the company and writes the boss's brief.",
        agentTemplate: "secretary",
        runtime: "secretary",
        role: "Secretary: the boss's brief and single point of contact",
      },
      {
        key: "librarian",
        name: "Librarian",
        description: "Keeps the knowledge base accurate, findable, and current.",
        agentTemplate: "knowledge_curator",
        runtime: "worker",
        role: "Knowledge curation: triage, freshness, retrieval checks, taxonomy audit",
      },
    ],
    routines: [
      {
        key: "daily_brief",
        title: "Daily brief",
        cadence: "daily",
        cron: "0 8 * * *",
        description: "Daily brief for the boss. Read the digest, write the brief in the order of your instructions, and publish it.",
        activeAtCreate: true,
        floorStart: false,
      },
      {
        key: "weekly_review",
        title: "Weekly review",
        cadence: "weekly",
        cron: "0 17 * * 5",
        description: "Weekly review for the boss, Friday afternoon. Look back over the week, not the last day.",
        activeAtCreate: false,
        floorStart: false,
      },
      {
        key: "librarian_triage",
        title: "Librarian daily triage",
        cadence: "daily",
        cron: "0 7 * * *",
        description: "Daily draft triage: list the new drafts, summarise each for the brief, flag duplicates, and check required headings.",
        activeAtCreate: false,
        floorStart: true,
      },
      {
        key: "librarian_freshness",
        title: "Librarian weekly freshness review",
        cadence: "weekly",
        cron: "0 9 * * 1",
        description: "Find published documents past their valid_until date or review interval, and open review requests with the owning squads.",
        activeAtCreate: false,
        floorStart: false,
      },
      {
        key: "librarian_retrieval",
        title: "Librarian weekly retrieval check",
        cadence: "weekly",
        cron: "0 9 * * 3",
        description: "Run the evaluation cases through knowledge search and record the hit rate and each miss.",
        activeAtCreate: false,
        floorStart: false,
      },
      {
        key: "librarian_taxonomy",
        title: "Librarian monthly taxonomy audit",
        cadence: "monthly",
        cron: "0 9 1 * *",
        description: "Audit the taxonomy: documents with no kind, and knowledge links whose document has gone.",
        activeAtCreate: false,
        floorStart: false,
      },
    ],
    stages: [],
    charter: {
      mission: "Be the boss's single point of contact, consolidate what the company is doing, and keep its knowledge base true.",
      responsibilities: [
        { key: "brief", text: "Publish the daily brief and the weekly review, decisions first." },
        { key: "routing", text: "Route each request to the squad that owns the work." },
        { key: "knowledge", text: "Keep the knowledge base accurate, findable, and current." },
      ],
      outputs: [
        { key: "daily_brief", kind: "report", cadenceHint: "daily" },
        { key: "knowledge_audits", kind: "report", cadenceHint: "weekly and monthly" },
      ],
      boundaries: "Does not decide proposals, change issues or agents, or publish knowledge that no publish policy allows.",
      escalation: "A flag the digest cannot explain goes to the squad leader as an inquiry; the boss decides the rest.",
      intake: "The boss's questions, and squads' questions about what the company knows.",
    },
    governance: { autonomyLevel: 1, budgetSharePct: 0, goalsScaffold: [] },
  },
  {
    key: "market_research",
    name: "Market Research",
    description: "Scans markets each week, has a critic challenge the evidence, and files only the proposals it supports.",
    floor: "research",
    leader: "research_lead",
    roles: [
      {
        key: "research_lead",
        name: "Research Lead",
        description: "Leads market research: sets the scan, delegates, and files only evidenced proposals.",
        agentTemplate: "research_lead",
        runtime: "worker",
        role: "Squad leader",
      },
      {
        key: "scout",
        name: "Market Scout",
        description: "Finds and reads market, customer, and trend sources, with a date and link for each claim.",
        agentTemplate: "market_scout",
        runtime: "worker",
        role: "Market scan",
      },
      {
        key: "critic",
        name: "Research Critic",
        description: "Challenges the evidence before anything is filed, and writes no findings of its own.",
        agentTemplate: "critic",
        runtime: "worker",
        role: "Challenges the evidence",
      },
    ],
    routines: [
      {
        key: "market_scan",
        title: "Market scan",
        cadence: "weekly",
        cron: "0 8 * * 1",
        description: "Weekly market scan. Scope: the markets, customer segments, and named competitors in the company knowledge base; if none are named, say which markets you chose. Look for changes in the last seven days: pricing, launches, wins and complaints, regulation, funding. File zero to three proposals; none is a valid result.",
        activeAtCreate: true,
        floorStart: false,
      },
    ],
    stages: [],
    charter: {
      mission: "Scan markets and customers each week, and file only the proposals the evidence supports.",
      responsibilities: [
        { key: "market_scan", text: "Watch markets and customers for changes in the last seven days, with a dated source for each claim." },
        { key: "challenge", text: "Have the critic challenge the evidence before anything is filed." },
      ],
      outputs: [
        { key: "market_report", kind: "kb_document", cadenceHint: "weekly" },
        { key: "market_proposals", kind: "proposal", cadenceHint: "0 to 3 a week" },
      ],
      boundaries: "Does not write product code, make product decisions, or contact anyone outside the workspace.",
      escalation: "Questions for the boss go in a proposal, not in a chat.",
      intake: "The weekly market scan routine opens each week's issue.",
    },
    governance: { autonomyLevel: 1, budgetSharePct: 0, goalsScaffold: [] },
  },
  {
    key: "opportunity_lab",
    name: "Opportunity Lab",
    description: "Generates product opportunities each week, sizes them, and brings the boss only the ones the evidence supports.",
    floor: "research",
    leader: "opportunity_lead",
    roles: [
      {
        key: "opportunity_lead",
        name: "Opportunity Lead",
        description: "Leads the Opportunity Lab: sets the week's question and files opportunities the evidence supports.",
        agentTemplate: "opportunity_lead",
        runtime: "worker",
        role: "Squad leader",
      },
      {
        key: "opportunity_analyst",
        name: "Opportunity Analyst",
        description: "Sizes an opportunity as a range, with the evidence for and against it.",
        agentTemplate: "opportunity_analyst",
        runtime: "worker",
        role: "Sizing and evidence for candidate opportunities",
      },
      {
        key: "critic",
        name: "Opportunity Critic",
        description: "Challenges each candidate's evidence before an opportunity is filed.",
        agentTemplate: "critic",
        runtime: "worker",
        role: "Challenges the evidence",
      },
    ],
    routines: [
      {
        key: "opportunity_generation",
        title: "Opportunity generation",
        cadence: "weekly",
        cron: "0 8 * * 2",
        description: "Weekly opportunity generation. Use the market and customer documents in the knowledge base to find up to three candidates. Each candidate needs a named problem, the people who have it, and evidence. Drop what you cannot support, and say why.",
        activeAtCreate: false,
        floorStart: true,
      },
    ],
    stages: [],
    charter: {
      mission: "Generate and size product opportunities, and bring the boss only the ones the evidence supports.",
      responsibilities: [
        { key: "generate", text: "Find candidate opportunities from the market and customer evidence each week." },
        { key: "size", text: "Size each candidate as a range, with its assumptions and the evidence against it." },
      ],
      outputs: [
        { key: "opportunity_proposals", kind: "proposal", cadenceHint: "0 to 3 a week" },
        { key: "opportunity_briefs", kind: "kb_document", cadenceHint: "per candidate" },
      ],
      boundaries: "Does not start an initiative; the boss decides which opportunities become initiatives.",
      escalation: "Candidates the critic blocks are dropped, and the drop is recorded in the report.",
      intake: "Opportunities come from the weekly routine; other squads may send a candidate as a request issue.",
    },
    governance: { autonomyLevel: 1, budgetSharePct: 0, goalsScaffold: [] },
  },
  {
    key: "competitor_intelligence",
    name: "Competitor Intelligence",
    description: "Watches the company's competitors each week and keeps a deep profile of each one each month.",
    floor: "research",
    leader: "competitor_lead",
    roles: [
      {
        key: "competitor_lead",
        name: "Competitor Lead",
        description: "Leads competitor intelligence: the weekly watch and the monthly profiles.",
        agentTemplate: "competitor_lead",
        runtime: "worker",
        role: "Squad leader",
      },
      {
        key: "competitor_analyst",
        name: "Competitor Analyst",
        description: "Compares competitors' products, pricing, and moves from primary sources.",
        agentTemplate: "competitor_analyst",
        runtime: "worker",
        role: "Competitor analysis",
      },
    ],
    routines: [
      {
        key: "weekly_watch",
        title: "Weekly competitor watch",
        cadence: "weekly",
        cron: "0 8 * * 3",
        description: "Weekly watch on the tracked competitors: what changed in the last seven days, with a dated primary source for each change.",
        activeAtCreate: false,
        floorStart: false,
      },
      {
        key: "monthly_profile",
        title: "Monthly competitor profile",
        cadence: "monthly",
        cron: "0 9 1 * *",
        description: "Monthly deep profile of each tracked competitor: pricing, positioning, customers, and product moves, filed as a competitor_profile draft.",
        activeAtCreate: false,
        floorStart: false,
      },
    ],
    stages: [],
    charter: {
      mission: "Watch the company's competitors, and keep a current, sourced profile of each.",
      responsibilities: [
        { key: "watch", text: "Track the tracked competitors' products, pricing, and moves each week." },
        { key: "profiles", text: "Keep a sourced competitor profile of each competitor, refreshed each month." },
      ],
      outputs: [
        { key: "competitor_profiles", kind: "kb_document", cadenceHint: "monthly" },
        { key: "watch_report", kind: "report", cadenceHint: "weekly" },
      ],
      boundaries: "Reports what competitors publicly do and say; does not recommend a company move.",
      escalation: "A change that should alter a decision goes to the boss as a proposal.",
      intake: "Requests for a competitor check come as request issues assigned to this squad.",
    },
    governance: { autonomyLevel: 1, budgetSharePct: 0, goalsScaffold: [] },
  },
  {
    key: "customer_voice",
    name: "Customer Voice",
    description: "Turns the support requests and feedback the company already has into customer insight.",
    floor: "research",
    leader: "customer_lead",
    roles: [
      {
        key: "customer_lead",
        name: "Customer Lead",
        description: "Leads Customer Voice: the weekly support-signal scan and the monthly customer synthesis.",
        agentTemplate: "customer_lead",
        runtime: "worker",
        role: "Customer evidence: scans and synthesis",
      },
    ],
    routines: [
      {
        key: "support_signal_scan",
        title: "Support-signal scan",
        cadence: "weekly",
        cron: "0 9 * * 4",
        description: "Weekly scan of the support requests and feedback logged this week. Group them by theme, with counts and linked examples.",
        activeAtCreate: false,
        floorStart: false,
      },
      {
        key: "customer_synthesis",
        title: "Monthly customer synthesis",
        cadence: "monthly",
        cron: "0 9 2 * *",
        description: "Monthly customer synthesis: combine the month's scans and existing insight into one customer_insight draft.",
        activeAtCreate: false,
        floorStart: false,
      },
    ],
    stages: [],
    charter: {
      mission: "Hear customers through the company's own records, and turn what they say into evidence the other squads can use.",
      responsibilities: [
        { key: "scan", text: "Group the week's support signals by theme, with their evidence." },
        { key: "synthesis", text: "Keep a current customer insight: segments, jobs, and ranked pains." },
      ],
      outputs: [
        { key: "customer_insight", kind: "kb_document", cadenceHint: "monthly" },
        { key: "signal_report", kind: "report", cadenceHint: "weekly" },
      ],
      boundaries: "Reads what customers already said; does not contact customers.",
      escalation: "A theme that should change a decision goes to the boss as a proposal.",
      intake: "Requests for customer evidence come as request issues assigned to this squad.",
    },
    governance: { autonomyLevel: 1, budgetSharePct: 0, goalsScaffold: [] },
  },
  {
    key: "product_discovery",
    name: "Product Discovery",
    description: "Owns the Validate stage: decides with evidence whether an opportunity is worth a product.",
    floor: "product",
    leader: "discovery_lead",
    roles: [
      {
        key: "discovery_lead",
        name: "Discovery Lead",
        description: "Leads Product Discovery: validates opportunities and recommends go or no-go.",
        agentTemplate: "discovery_lead",
        runtime: "worker",
        role: "Squad leader; owns the Validate stage",
      },
      {
        key: "validation_analyst",
        name: "Validation Analyst",
        description: "Tests an opportunity's thesis against customer and market evidence.",
        agentTemplate: "validation_analyst",
        runtime: "worker",
        role: "Validation evidence",
      },
    ],
    routines: [
      {
        key: "discovery_review",
        title: "Operating review",
        cadence: "weekly",
        cron: "30 10 * * 1",
        description: "Weekly operating review of the initiatives in Validate: what is in, what is past its SLA, and the next step.",
        activeAtCreate: false,
        floorStart: true,
      },
    ],
    stages: ["validate"],
    charter: {
      mission: "Validate opportunities with evidence, and recommend which are worth a product.",
      responsibilities: [
        { key: "validate", text: "Produce the validation report, with a competitor profile and a customer insight." },
        { key: "recommend", text: "Recommend go or no-go, with the evidence for and against." },
      ],
      outputs: [
        { key: "validation_report", kind: "kb_document", cadenceHint: "per initiative" },
        { key: "go_no_go", kind: "proposal", cadenceHint: "per initiative" },
      ],
      boundaries: "Does not commit to a product. The boss makes the go or no-go decision.",
      escalation: "A missing competitor profile or customer insight is requested from its squad.",
      intake: "Initiatives arrive at Validate; other squads may ask for validation as a request issue.",
    },
    governance: { autonomyLevel: 1, budgetSharePct: 0, goalsScaffold: [] },
  },
  {
    key: "product_analysis",
    name: "Product Analysis",
    description: "Owns the Define stage: the product brief, the economics and feasibility, and the risk register.",
    floor: "product",
    leader: "product_analysis_lead",
    roles: [
      {
        key: "product_analysis_lead",
        name: "Product Analysis Lead",
        description: "Leads Product Analysis: writes the product brief and checks the analysis.",
        agentTemplate: "product_analysis_lead",
        runtime: "worker",
        role: "Squad leader; owns the Define stage",
      },
      {
        key: "feasibility_analyst",
        name: "Feasibility Analyst",
        description: "Works out cost, return, feasibility, and risk for an initiative, with each assumption stated.",
        agentTemplate: "feasibility_analyst",
        runtime: "worker",
        role: "Economics and feasibility",
      },
    ],
    routines: [
      {
        key: "analysis_review",
        title: "Operating review",
        cadence: "weekly",
        cron: "30 10 * * 2",
        description: "Weekly operating review of the initiatives in Define: what is in, what is past its SLA, and the next step.",
        activeAtCreate: false,
        floorStart: false,
      },
    ],
    stages: ["define"],
    charter: {
      mission: "Turn a validated initiative into a product brief, an economics and feasibility analysis, and a risk register.",
      responsibilities: [
        { key: "brief", text: "Write the product brief for each initiative in Define." },
        { key: "analysis", text: "Work out economics and feasibility, with every number's assumption stated." },
        { key: "risks", text: "Keep the risk register for each initiative: likelihood, impact, owner, mitigation." },
      ],
      outputs: [
        { key: "product_brief", kind: "kb_document", cadenceHint: "per initiative" },
        { key: "analysis", kind: "kb_document", cadenceHint: "per initiative" },
        { key: "risk_register", kind: "kb_document", cadenceHint: "per initiative" },
      ],
      boundaries: "Does not approve spending or commit resources; costs go into the Plan stage's budget request.",
      escalation: "Open questions that block the brief go to the boss as a proposal.",
      intake: "Initiatives arrive at Define once the boss commits them to a product.",
    },
    governance: { autonomyLevel: 1, budgetSharePct: 0, goalsScaffold: [] },
  },
  {
    key: "planning",
    name: "Planning",
    description: "Owns the Plan stage: the PRD, the roadmap, a launch plan outline, and the budget request.",
    floor: "product",
    leader: "planning_lead",
    roles: [
      {
        key: "planning_lead",
        name: "Planning Lead",
        description: "Leads Planning: the roadmap, the launch plan outline, and the budget request for the boss.",
        agentTemplate: "planning_lead",
        runtime: "worker",
        role: "Squad leader; owns the Plan stage",
      },
      {
        key: "prd_writer",
        name: "PRD Writer",
        description: "Writes requirements with an acceptance criterion for each one.",
        agentTemplate: "prd_writer",
        runtime: "worker",
        role: "Product requirements",
      },
    ],
    routines: [
      {
        key: "planning_review",
        title: "Operating review",
        cadence: "weekly",
        cron: "30 10 * * 3",
        description: "Weekly operating review of the initiatives in Plan: what is in, what is past its SLA, and the next step.",
        activeAtCreate: false,
        floorStart: false,
      },
    ],
    stages: ["plan"],
    charter: {
      mission: "Plan each initiative: its requirements, roadmap, launch outline, and the budget it needs.",
      responsibilities: [
        { key: "requirements", text: "Write the PRD, with an acceptance criterion for each requirement." },
        { key: "roadmap", text: "Write the roadmap and the launch plan outline." },
        { key: "budget", text: "Write the budget request: amount, purpose, period, and what refusal means." },
      ],
      outputs: [
        { key: "prd", kind: "kb_document", cadenceHint: "per initiative" },
        { key: "roadmap", kind: "kb_document", cadenceHint: "per initiative" },
        { key: "budget_request", kind: "kb_document", cadenceHint: "per initiative" },
      ],
      boundaries: "Does not approve the plan or the budget; the boss decides at the Plan gate.",
      escalation: "Open questions that block the plan go to the boss as a proposal.",
      intake: "Initiatives arrive at Plan once their analysis is attested.",
    },
    governance: { autonomyLevel: 1, budgetSharePct: 0, goalsScaffold: [] },
  },
  {
    key: "build",
    name: "Build",
    description: "Builds and verifies what the Plan stage asks for, through the SDLC workflow, and owns the Learn stage.",
    floor: "build",
    leader: "lead",
    roles: [
      {
        key: "lead",
        name: "SDLC Lead",
        description: "Leads SDLC squads: delegates, verifies evidence, and reports.",
        agentTemplate: "tech_lead",
        runtime: "worker",
        role: "Squad leader",
      },
      {
        key: "analyst",
        name: "SDLC Product Analyst",
        description: "Turns requests and sources into briefs, PRDs, epics, and stories.",
        agentTemplate: "product_analyst",
        runtime: "worker",
        role: "Briefs, PRD, epics and stories, requirement changes, build queue",
      },
      {
        key: "ux",
        name: "SDLC UX Designer",
        description: "Designs screens, states, and the design system for the requirements.",
        agentTemplate: "ux_designer",
        runtime: "worker",
        role: "UX design, screen specs, mockups",
      },
      {
        key: "architect",
        name: "SDLC Architect",
        description: "Designs the simplest system that fits, records decisions, and analyses impact.",
        agentTemplate: "architect",
        runtime: "worker",
        role: "Architecture, impact analysis, project context",
      },
      {
        key: "developer",
        name: "SDLC Developer",
        description: "Builds stories end to end and verifies them on the running app.",
        agentTemplate: "fullstack_developer",
        runtime: "worker",
        role: "Implementation of epics, stories, and features",
      },
      {
        key: "fixer",
        name: "SDLC Bug Fixer",
        description: "Reproduces a defect and fixes it with the smallest change.",
        agentTemplate: "bug_fixer",
        runtime: "worker",
        role: "Defects and small changes in one component",
      },
      {
        key: "reviewer",
        name: "SDLC Code Reviewer",
        description: "Independent review of changes and plans, with a verdict first.",
        agentTemplate: "code_reviewer",
        runtime: "worker",
        role: "Independent review, planning readiness check",
      },
      {
        key: "qa",
        name: "SDLC QA Tester",
        description: "Designs tests from acceptance criteria and runs them on the real system.",
        agentTemplate: "qa_tester",
        runtime: "worker",
        role: "Test design, regression, final verification",
      },
      {
        key: "security",
        name: "SDLC Security Reviewer",
        description: "Gates a change or release on verified security controls.",
        agentTemplate: "security_reviewer",
        runtime: "worker",
        role: "Security review of changes and of the whole product",
      },
      {
        key: "devops",
        name: "SDLC DevOps",
        description: "Packages and releases one verified digest; production needs approval.",
        agentTemplate: "devops_engineer",
        runtime: "worker",
        role: "Packaging, run instructions, release notes",
      },
    ],
    routines: [],
    stages: ["build", "learn"],
    charter: {
      mission: "Build and verify each initiative's product through the SDLC workflow, then run its retrospective.",
      responsibilities: [
        { key: "build", text: "Build the release candidate from the PRD, verified on the running system." },
        { key: "verify", text: "Record the QA verdict and the test report for each build." },
        { key: "learn", text: "Write the retrospective when an initiative closes." },
      ],
      outputs: [
        { key: "release_candidate", kind: "kb_document", cadenceHint: "per initiative" },
        { key: "test_report", kind: "kb_document", cadenceHint: "per build" },
        { key: "retrospective", kind: "kb_document", cadenceHint: "per initiative" },
      ],
      boundaries: "No production deploys and no deletions without the boss. Spending money needs the boss.",
      escalation: "A blocked build goes to the requester, named on the request.",
      intake: "Build work arrives as issues in the build project, or as stages of an initiative.",
    },
    governance: { autonomyLevel: 1, budgetSharePct: 0, goalsScaffold: [] },
  },
  {
    key: "release_ops",
    name: "Release & Ops",
    description: "Owns the release side of the Launch stage: the checklist, the release notes, and the go-live readiness.",
    floor: "launch",
    leader: "release_lead",
    roles: [
      {
        key: "release_lead",
        name: "Release Lead",
        description: "Leads Release & Ops: the launch checklist, the release notes, and readiness for the boss's go-live gate.",
        agentTemplate: "release_lead",
        runtime: "worker",
        role: "Squad leader; owns the release side of Launch",
      },
      {
        key: "release_engineer",
        name: "Release Engineer",
        description: "Checks deployment, rollback, and monitoring for a release candidate.",
        agentTemplate: "devops_engineer",
        runtime: "worker",
        role: "Deployment, rollback and monitoring checks",
      },
    ],
    routines: [
      {
        key: "release_checklist_audit",
        title: "Release checklist audit",
        cadence: "weekly",
        cron: "0 10 * * 4",
        description: "Weekly audit of the launch checklist for each initiative in Launch, checked against its evidence. Report what is open.",
        activeAtCreate: false,
        floorStart: true,
      },
    ],
    stages: ["launch"],
    charter: {
      mission: "Ship each initiative safely: a checked launch checklist, clear release notes, and a rollback that has been checked.",
      responsibilities: [
        { key: "checklist", text: "Keep the launch checklist, each item checked against evidence." },
        { key: "notes", text: "Write the release notes for the people who use the product." },
      ],
      outputs: [
        { key: "launch_checklist", kind: "kb_document", cadenceHint: "per initiative" },
        { key: "release_notes", kind: "kb_document", cadenceHint: "per release" },
      ],
      boundaries: "Publishes nothing outside the company. The boss decides when the product goes live.",
      escalation: "A checklist item that cannot be checked is reported as not verified, and goes to the boss at the Launch gate.",
      intake: "Initiatives arrive at Launch once the build is verified.",
    },
    governance: { autonomyLevel: 1, budgetSharePct: 0, goalsScaffold: [] },
  },
  {
    key: "growth_gtm",
    name: "Growth & GTM",
    description: "Owns the go-to-market plan for Launch and the metrics for Operate.",
    floor: "launch",
    leader: "growth_lead",
    roles: [
      {
        key: "growth_lead",
        name: "Growth Lead",
        description: "Leads Growth & GTM: the go-to-market plan, the metrics reports, and the weekly growth review.",
        agentTemplate: "growth_lead",
        runtime: "worker",
        role: "Squad leader; go to market and growth metrics",
      },
    ],
    routines: [
      {
        key: "growth_review",
        title: "Weekly growth review",
        cadence: "weekly",
        cron: "0 10 * * 5",
        description: "Weekly growth review: channels, funnel, and experiments in flight, each number linked to its data.",
        activeAtCreate: false,
        floorStart: false,
      },
    ],
    stages: ["launch", "operate"],
    charter: {
      mission: "Reach the market with each launched initiative, and show with numbers whether it works.",
      responsibilities: [
        { key: "gtm", text: "Write the go-to-market plan: channels, audience, messages, and spend." },
        { key: "metrics", text: "Write the weekly metrics report for each initiative in Operate." },
      ],
      outputs: [
        { key: "gtm_plan", kind: "kb_document", cadenceHint: "per initiative" },
        { key: "metrics_report", kind: "kb_document", cadenceHint: "weekly" },
      ],
      boundaries: "Any spend, price, or public announcement is the boss's decision.",
      escalation: "A metric that moves the wrong way goes into the weekly review and, if it needs a decision, to the boss as a proposal.",
      intake: "Initiatives arrive at Launch for the go-to-market plan, and at Operate for the metrics.",
    },
    governance: { autonomyLevel: 1, budgetSharePct: 0, goalsScaffold: [] },
  },
  {
    key: "support",
    name: "Support",
    description: "Owns the customer side of Operate: triage, answers from the knowledge base, and write-back.",
    floor: "launch",
    leader: "support_lead",
    roles: [
      {
        key: "support_lead",
        name: "Support Lead",
        description: "Leads Support: triage, escalations to the build squad, and knowledge write-back.",
        agentTemplate: "support_lead",
        runtime: "worker",
        role: "Squad leader; owns the customer side of Operate",
      },
      {
        key: "support_agent",
        name: "Support Agent",
        description: "Answers customer requests from the company's knowledge, and escalates what it cannot answer.",
        agentTemplate: "support_agent",
        runtime: "worker",
        role: "Customer requests",
      },
    ],
    routines: [
      {
        key: "daily_triage",
        title: "Daily triage",
        cadence: "daily",
        cron: "30 8 * * *",
        description: "Daily triage of the support requests: answer from the knowledge base, escalate bugs, and mark each request's state.",
        activeAtCreate: false,
        floorStart: false,
      },
      {
        key: "knowledge_write_back",
        title: "Weekly knowledge write-back",
        cadence: "weekly",
        cron: "0 16 * * 5",
        description: "Weekly write-back: the questions that came up three or more times, filed as playbook or reference drafts after a search for duplicates.",
        activeAtCreate: false,
        floorStart: false,
      },
    ],
    stages: ["operate"],
    charter: {
      mission: "Keep customers after launch: answer them correctly, escalate what needs the build squad, and keep the knowledge base current.",
      responsibilities: [
        { key: "triage", text: "Triage each support request each day, with its state." },
        { key: "write_back", text: "Turn repeated questions into playbook or reference drafts each week." },
      ],
      outputs: [
        { key: "support_queue", kind: "report", cadenceHint: "daily" },
        { key: "playbooks", kind: "kb_document", cadenceHint: "weekly" },
      ],
      boundaries: "Refunds, credits, price changes, and exceptions are the boss's decision; nothing is promised to a customer.",
      escalation: "A bug goes to the build squad as a request with its steps to reproduce.",
      intake: "Customer requests arrive as issues assigned to this squad.",
    },
    governance: { autonomyLevel: 1, budgetSharePct: 0, goalsScaffold: [] },
  },
  {
    key: "finance_risk",
    name: "Finance & Risk",
    description: "The company's check on money and risk: reviews spending proposals and keeps the risk register current.",
    floor: "governance",
    leader: "finance_lead",
    roles: [
      {
        key: "finance_lead",
        name: "Finance Lead",
        description: "Leads Finance & Risk: reviews spending proposals and keeps the risk register current.",
        agentTemplate: "finance_lead",
        runtime: "worker",
        role: "Squad leader; money and risk review",
      },
    ],
    // The weekly budget review is phase 4 (spec 4.6); phase 3 installs no routine here.
    routines: [],
    stages: [],
    charter: {
      mission: "Check the company's money and risk, and put each decision the boss must make in front of them with its evidence.",
      responsibilities: [
        { key: "review", text: "Review each spending proposal with its assumptions." },
        { key: "risks", text: "Keep the risk register current, with an owner for each risk." },
      ],
      outputs: [
        { key: "risk_register", kind: "kb_document", cadenceHint: "weekly" },
        { key: "review_proposals", kind: "proposal", cadenceHint: "as needed" },
      ],
      boundaries: "Approves nothing and commits nothing. Its review is advice for the boss's decision.",
      escalation: "A risk with no owner is flagged, and an owner is proposed.",
      intake: "Spending proposals and risk questions come to this squad from the boss and other squads.",
    },
    governance: { autonomyLevel: 1, budgetSharePct: 0, goalsScaffold: [] },
  },
  // Added by plan 10.18d, not by the spec 3.4 table. Its phase 4 governance
  // fields stay at the stubs above until goals and budgets exist.
  {
    key: "people_performance",
    name: "People & Performance",
    description: "Reviews each squad's record against its charter each week, and reports what is late or silent.",
    floor: "governance",
    leader: "people_lead",
    roles: [
      {
        key: "people_lead",
        name: "People Lead",
        description: "Leads People & Performance: reads each squad's record against its charter and reports what is late or silent.",
        agentTemplate: "people_performance",
        runtime: "worker",
        role: "Squad leader; squad record against charter",
      },
    ],
    routines: [],
    stages: [],
    charter: {
      mission: "Keep the boss informed of how each squad performs against its charter, with the evidence.",
      responsibilities: [
        { key: "record", text: "Compare each squad's outputs with its charter every week." },
        { key: "report", text: "Report what is late, missing, or silent, with a link to each." },
      ],
      outputs: [
        { key: "performance_report", kind: "report", cadenceHint: "weekly" },
      ],
      boundaries: "Reports on squads, not on individuals. Sets no targets and changes no agent. Goals and scorecards come in a later release.",
      escalation: "A squad silent for a week is raised with its leader; the boss hears of it if it persists.",
      intake: "Requests for a squad's record come as request issues assigned to this squad.",
    },
    governance: { autonomyLevel: 1, budgetSharePct: 0, goalsScaffold: [] },
  },
];

/** The card for a squad key. Throws for a key that has no card, which the type prevents. */
export function companySquadCard(key: CompanySquadKey): CompanySquadCard {
  const card = COMPANY_SQUAD_CARDS.find((c) => c.key === key);
  if (!card) throw new Error(`no company squad card "${key}"`);
  return card;
}

/**
 * The stages with no owner among the selected squads. A stage is unowned when
 * the squad that owns it in the pipeline is not selected.
 */
export function unownedStages(selected: readonly CompanySquadKey[]): CompanyStageOwner[] {
  const chosen = new Set(selected);
  return COMPANY_STAGE_OWNERS.filter((stage) => !chosen.has(stage.owner));
}

/** The routines `start` activates for the selected squads, each with its squad. */
export function routinesStartedBy(
  selected: readonly CompanySquadKey[],
  start: CompanyStart,
): { squad: CompanySquadKey; routine: CompanyRoutine }[] {
  const started: { squad: CompanySquadKey; routine: CompanyRoutine }[] = [];
  for (const key of selected) {
    for (const routine of companySquadCard(key).routines) {
      const on =
        start === "all" ||
        (start === "per_floor" && (routine.activeAtCreate || routine.floorStart)) ||
        (start === "leave_paused" && routine.activeAtCreate);
      if (on) started.push({ squad: key, routine });
    }
  }
  return started;
}

/** Runs per week for each cadence. A month is 52 / 12 weeks. */
export const RUNS_PER_WEEK: Record<CompanyRoutineCadence, number> = {
  daily: 7,
  weekly: 1,
  monthly: 12 / 52,
};

/**
 * The cost of one agent run, stated as a default. The wizard uses the
 * workspace's measured average once the server reports one; until then this
 * figure is shown and labelled as a default.
 */
export const DEFAULT_RUN_COST_USD = 0.5;

export interface CompanyEstimate {
  runsPerWeek: number;
  costPerWeekUsd: number;
  /** True when the cost uses DEFAULT_RUN_COST_USD rather than a measured average. */
  costIsDefault: boolean;
}

/** Runs and cost per week that the chosen start option adds for the selected squads. */
export function estimateCompanyRuns(
  selected: readonly CompanySquadKey[],
  start: CompanyStart,
  measuredRunCostUsd: number | null = null,
): CompanyEstimate {
  const runsPerWeek = routinesStartedBy(selected, start).reduce(
    (sum, { routine }) => sum + RUNS_PER_WEEK[routine.cadence],
    0,
  );
  const costIsDefault = measuredRunCostUsd === null;
  const perRun = measuredRunCostUsd ?? DEFAULT_RUN_COST_USD;
  return { runsPerWeek, costPerWeekUsd: runsPerWeek * perRun, costIsDefault };
}
