import type { AgentInstructionTemplate } from "../agents/instruction-templates";

/**
 * Ready-made squads a user can start from in the Squads quickstart. A template
 * names the roles it needs, the instructions its leader follows, and the
 * workflow its build project uses. Card names and descriptions are translated
 * in the views package, keyed by `id`; the agents it creates keep the English
 * names below so that running the quickstart twice reuses them.
 */

export type SquadTemplateId = "sdlc_greenfield" | "sdlc_maintenance" | "market_research";

export type SquadRoleKey =
  | "lead"
  | "analyst"
  | "ux"
  | "architect"
  | "developer"
  | "fixer"
  | "reviewer"
  | "qa"
  | "security"
  | "devops"
  | "research_lead"
  | "scout"
  | "competitor"
  | "critic";

export interface SquadRole {
  key: SquadRoleKey;
  /** The agent instruction template the created agent starts from. */
  agentTemplate: AgentInstructionTemplate["id"];
  /** Name of the created agent. A non-archived agent with this name is reused. */
  name: string;
  description: string;
  /** Roster text the leader reads; context only, it schedules nothing. */
  role: string;
}

export const SQUAD_ROLES: Record<SquadRoleKey, SquadRole> = {
  lead: {
    key: "lead",
    agentTemplate: "tech_lead",
    name: "SDLC Lead",
    description: "Leads SDLC squads: delegates, verifies evidence, and reports.",
    role: "Squad leader",
  },
  analyst: {
    key: "analyst",
    agentTemplate: "product_analyst",
    name: "SDLC Product Analyst",
    description: "Turns requests and sources into briefs, PRDs, epics, and stories.",
    role: "Briefs, PRD, epics and stories, requirement changes, build queue",
  },
  ux: {
    key: "ux",
    agentTemplate: "ux_designer",
    name: "SDLC UX Designer",
    description: "Designs screens, states, and the design system for the requirements.",
    role: "UX design, screen specs, mockups",
  },
  architect: {
    key: "architect",
    agentTemplate: "architect",
    name: "SDLC Architect",
    description: "Designs the simplest system that fits, records decisions, and analyses impact.",
    role: "Architecture, impact analysis, project context",
  },
  developer: {
    key: "developer",
    agentTemplate: "fullstack_developer",
    name: "SDLC Developer",
    description: "Builds stories end to end and verifies them on the running app.",
    role: "Implementation of epics, stories, and features",
  },
  fixer: {
    key: "fixer",
    agentTemplate: "bug_fixer",
    name: "SDLC Bug Fixer",
    description: "Reproduces a defect and fixes it with the smallest change.",
    role: "Defects and small changes in one component",
  },
  reviewer: {
    key: "reviewer",
    agentTemplate: "code_reviewer",
    name: "SDLC Code Reviewer",
    description: "Independent review of changes and plans, with a verdict first.",
    role: "Independent review, planning readiness check",
  },
  qa: {
    key: "qa",
    agentTemplate: "qa_tester",
    name: "SDLC QA Tester",
    description: "Designs tests from acceptance criteria and runs them on the real system.",
    role: "Test design, regression, final verification",
  },
  security: {
    key: "security",
    agentTemplate: "security_reviewer",
    name: "SDLC Security Reviewer",
    description: "Gates a change or release on verified security controls.",
    role: "Security review of changes and of the whole product",
  },
  devops: {
    key: "devops",
    agentTemplate: "devops_engineer",
    name: "SDLC DevOps",
    description: "Packages and releases one verified digest; production needs approval.",
    role: "Packaging, run instructions, release notes",
  },
  research_lead: {
    key: "research_lead",
    agentTemplate: "research_lead",
    name: "Research Lead",
    description: "Leads market research: sets the scan, delegates, and files only evidenced proposals.",
    role: "Squad leader",
  },
  scout: {
    key: "scout",
    agentTemplate: "market_scout",
    name: "Market Scout",
    description: "Finds and reads market, customer, and trend sources, with a date and link for each claim.",
    role: "Market scan",
  },
  competitor: {
    key: "competitor",
    agentTemplate: "competitor_analyst",
    name: "Competitor Analyst",
    description: "Compares competitors' products, pricing, and moves from primary sources.",
    role: "Competitor analysis",
  },
  critic: {
    key: "critic",
    agentTemplate: "critic",
    name: "Research Critic",
    description: "Challenges the evidence before anything is filed, and writes no findings of its own.",
    role: "Challenges the evidence",
  },
};

export interface SquadWorkflowStep {
  /** An existing status key, or a status to create under this name. */
  status: { key: string } | { name: string };
  /** The role that takes the issue when it enters the step; null keeps the assignee. */
  handler: SquadRoleKey | null;
  instructions: string;
  back?: string;
}

export interface SquadWorkflowTemplate {
  name: string;
  description: string;
  steps: SquadWorkflowStep[];
}

/**
 * The workflow of a build project: the developer builds, an independent
 * reviewer checks, QA verifies, and QA's step is the last one before done, so
 * an agent run can close the issue and the run queue moves on.
 */
export const SDLC_AUTO_WORKFLOW: SquadWorkflowTemplate = {
  name: "SDLC Auto",
  description:
    "Build, independent review, then QA, all by agents. A rejected step goes back to the developer.",
  steps: [
    {
      status: { key: "todo" },
      handler: "developer",
      instructions:
        "Build everything this issue asks for, story by story, following the story flow in your instructions. Hand it to review only when every story is implemented and verified on the running app. Record what you verified in the spec.",
    },
    {
      status: { key: "in_review" },
      handler: "reviewer",
      back: "todo",
      instructions:
        "Review the whole change for this issue against its acceptance criteria. Send it back with the blocking findings, or pass it on with your verdict.",
    },
    {
      status: { name: "QA" },
      handler: "qa",
      back: "todo",
      instructions:
        "Verify the change on the running system against the acceptance criteria. NO GO sends it back with the defects. GO or CONDITIONAL GO finishes the issue.",
    },
  ],
};

/** A recurring autopilot the template creates for its squad. */
export interface SquadRoutineTemplate {
  /** Name of the autopilot. A non-archived autopilot with this name on the squad is reused. */
  title: string;
  /** Title of each issue the routine opens. Supports {{date}}. */
  issueTitleTemplate: string;
  /** Five-field cron expression, in the timezone the quickstart passes. */
  cron: string;
  /** The issue body each run starts from: the scan scope and the bar for a proposal. */
  description: string;
}

/** The charter a template saves for its squad on creation. */
export interface SquadCharterTemplate {
  mission: string;
  responsibilities: { key: string; text: string }[];
  outputs: { key: string; kind: "issue" | "proposal" | "kb_document" | "report"; cadenceHint: string }[];
  boundaries: string;
  escalation: string;
  intake: string;
}

export interface SquadTemplate {
  id: SquadTemplateId;
  /** Name of the created squad. */
  name: string;
  description: string;
  /** Roles in the squad; the leader leads and the others are members. */
  roles: SquadRoleKey[];
  /** The role that leads. It is created first and is the squad's leader. */
  leader: SquadRoleKey;
  /**
   * Floor name passed as the squad's floor_name. The server uses it only when
   * it has to make a new floor, so the squad may land on an existing floor.
   */
  floorName: string | null;
  /** The recurring autopilot the template creates for its squad, when it has one. */
  routine: SquadRoutineTemplate | null;
  /** The charter saved on creation. Saving it also creates the squad's operating review. */
  charter: SquadCharterTemplate;
  /** Squad instructions: what the leader follows. */
  instructions: string;
  /** The workflow its build project needs, when it has one. */
  workflow: SquadWorkflowTemplate | null;
}

const SDLC_CHARTER_BOUNDARIES =
  "No production deploys and no deletions without the boss. Spending money needs the boss.";
const SDLC_CHARTER_ESCALATION =
  "A blocked request or an open decision goes to the requester, named on the request.";

const COMMON_INSTRUCTIONS = `You lead an SDLC squad. A request arrives as an issue assigned to this squad. You carry it to the end without asking the requester again. You coordinate and verify; the members do the work.

These instructions decide how you delegate here, in place of @mentioning members in a comment.

How you delegate:
- Create one sub-issue of the request per piece of work: multica issue create --title "..." --parent <request> --assignee "<member name>" --stage <N> --status todo|backlog --description-file <file>. Create later stages up front as backlog, and promote a stage to todo only when every sub-issue of the earlier stage is done.
- Each description holds: the goal; the input paths or links; the output paths; the skill to run; the acceptance check; the words "This run is unattended".
- Post at most one short comment per turn that links the sub-issues. Do not @mention a member in it: a mention and an assignment would start the same member twice.

How you verify:
- When a sub-issue reaches in_review, read its report and open the files and evidence it names. If it meets the acceptance check, set it done: multica issue status <sub-issue> done. If not, comment the specific gap on the sub-issue and set it back to todo so its assignee reworks it. A report is a claim until the evidence supports it.
- Treat an empty, malformed, or failed report as no progress. Retry the same piece at most twice, then change the member or the approach, or split it. Never set a sub-issue done to get past a stage.

Unattended:
- Members take the recommended default for every open question and record it as an assumption (id, decision, why, who can reverse it) in docs/sdlc/assumptions.md. You add any default you took yourself.
- Stop only for what cannot be undone: deleting data, breaking a released contract, a production release, or spending money. Then set the request to blocked, comment what you need, and mention the requester.
- Never deploy to production, and never delete anything, on your own.

Records: every turn, run multica squad activity on the issue this turn runs on. Keep the request in progress while work remains. Do not do a member's work yourself.

Final report, one comment on the request when everything is verified:
- Outcome in one line, then what was built or changed and where.
- Evidence: the verification results with links, and anything not verified.
- Decisions and assumptions you or the members took, with ids.
- Open risks and follow-ups, and how to run and roll back.
Then set the request to in_review. Leave done to a person.`;

const GREENFIELD_INSTRUCTIONS = `${COMMON_INSTRUCTIONS}

Greenfield: build a new product from the request.

Intake:
- The request should name the Build project: a project with the repository and the "SDLC Auto" workflow. Check with multica project get <id> --output json that workflow_id is set. If there is no Build project or it has no workflow, comment once, set the request to blocked, and stop.
- Find or create the Planning project "<Build project title> · Planning" with the same repository (multica project create --title "..." --repo <url>) and keep the Default workflow. Move the request issue into it (multica issue update <request> --project <planning>). Your runs then never hold up the build queue.
- Put every sub-issue of stages 1 to 6 and of the final verification in the Planning project.
- Fill anything the request leaves out with a default and add it to the assumptions.

Planning stages (create all up front; stage 2 and later as backlog):
1. Product Analyst: product brief (bmad-product-brief; add bmad-prfaq when the idea is vague).
2. Product Analyst: PRD (bmad-prd).
3. UX Designer: UX design (bmad-ux). Skip when the product has no UI.
4. Architect: architecture (bmad-architecture), then project context (bmad-project-context).
5. Product Analyst: epics and stories (bmad-create-epics-and-stories).
6. Code Reviewer: planning readiness check (the readiness gate of bmad-sprint-planning). On CONCERNS or FAIL, send each named gap to the member who owns that document, then run stage 6 again. At most twice.
7. Product Analyst: publish the build queue. In the Build project, create one issue per epic (an issue, not a sub-issue) assigned to "SDLC Developer", status backlog, describing the epic and linking its stories, then number it: multica issue queue-order <issue> --number <N>. N follows the epic order and its dependencies. Give two epics the same number only when they touch different code. An epic with more than eight stories becomes one issue per story, numbered in order. Create no other unnumbered issue in the Build project while the queue runs: one with a running agent holds the queue.

Build: the queue starts by itself. Do not run anything yourself. Register two wakeups on the request with multica issue wakeup create: one when the last epic is done (--until-issue <last epic> --until-issue-state done --on-timeout wake --expires-in 336h), and a check every 2 hours (--kind every --every 2h --expires-in 336h). At each check, list the Build project (multica issue list --project <id> --output json). An issue that is blocked or whose run failed: multica issue rerun once, then comment, and if it is still stuck set the request to blocked and mention the requester. End a check with nothing to report using multica issue wakeup checkin.

Final verification, once the last epic is done: stage 8 QA Tester (full regression and a runtime check from a clean clone), stage 9 Security Reviewer (the whole product), stage 10 DevOps (how to run and deploy, packaging, release notes; no production deploy). On NO GO, FAIL, or a blocking finding, create a fix issue in the Build project for "SDLC Bug Fixer", wait for it to finish, and run that stage again. At most twice.

Then post the final report.`;

const MAINTENANCE_INSTRUCTIONS = `${COMMON_INSTRUCTIONS}

Maintenance: change an existing product (bug, feature, optimization, upgrade, upkeep).

Triage first, as one comment on the request: the type, the risk tier (Lightweight, Standard, High-risk), the route below, and the assumptions you took. Work in the request's project and repository; its workflow stays as it is. High-risk means auth, money, data migration or deletion, a public contract, a security control, or a production release.

Routes:
A. Hotfix, or a bug that is Trivial or Small: stage 1 Bug Fixer, stage 2 Code Reviewer, stage 3 QA Tester.
B. A bug with an unclear cause or a wide effect: stage 1 Architect (root cause and impact; add Security Reviewer when auth or data is involved), stage 2 one implementation sub-issue per component for Bug Fixer or Developer, then review, QA, and security stages.
C. Feature or enhancement: stage 1 Product Analyst (requirements and acceptance criteria; add UX Designer when the UI changes), stage 2 Architect (design delta; skip when there is none), stage 3 one implementation sub-issue per story for Developer, in dependency order, then review, QA, and security stages.
D. Optimization: stage 1 Architect (measure the baseline and set a target), stage 2 Developer, then QA compares against the baseline.
E. Upgrade or upkeep: stage 1 Architect (risks and rollback plan), stage 2 Developer or DevOps, then QA runs the regression.

Rules:
- Run sub-issues of a stage in parallel only when they touch different files.
- Every change gets an independent Code Reviewer sub-issue and a QA Tester sub-issue after it. High-risk changes also get a Security Reviewer sub-issue.
- A Medium-sized or larger bug fix, or a changed requirement, goes through bmad-correct-course before anyone builds it.
- A review or QA verdict that blocks sends the work back: create a fix sub-issue for the owner, then run the check again. At most twice, then set the request to blocked and mention the requester.

Then post the final report.`;

const MARKET_RESEARCH_INSTRUCTIONS = `${COMMON_INSTRUCTIONS}

Market research: a weekly scan of markets, customers, and competitors. The routine opens one issue each week; the leader carries it to a report and proposals.

Plan:
- Restate the scan's scope: the markets, the customer segments, and the named competitors. Take the scope from the issue, or else from the company's knowledge base (multica knowledge search "<query>"). Record any scope you choose as an assumption.
- In the first round, create two sub-issues for research members: the market scout (sources and changes in the window) and the competitor analyst (named competitors, their moves and pricing). Give each the question, the start sources, where to post the report, and the acceptance check: every claim has a dated source link.
- When both reports are verified, create one sub-issue for the research critic. It challenges each claim in both reports and in the draft proposals. Each blocking objection is answered with evidence or the claim is dropped.

Proposals:
- File zero to three proposals with multica proposal create --squad "Market Research", after the critic has reviewed them. Each one states the recommendation, the evidence links, the options considered, the cost and risk, and the decision the boss needs. Propose nothing you cannot support with evidence. "No proposal this week" is a valid result.
- Do not write product code, make product decisions, or contact anyone outside the workspace. A proposal is a request for the boss's decision.

Final report, one comment on the request: the window scanned, the proposals filed with their ids, what the critic objected to, the assumptions you took, and what is not verified.`;

export const SQUAD_TEMPLATES: readonly SquadTemplate[] = [
  {
    id: "sdlc_greenfield",
    name: "SDLC Greenfield",
    description:
      "Builds a new product from a request: brief, PRD, UX, architecture, epics, a queued build, final verification, and a report.",
    roles: [
      "lead",
      "analyst",
      "ux",
      "architect",
      "developer",
      "reviewer",
      "qa",
      "security",
      "devops",
    ],
    leader: "lead",
    floorName: null,
    routine: null,
    charter: {
      mission: "Take a product request from brief to a verified build, with a report the boss can act on.",
      responsibilities: [
        { key: "analysis", text: "Turn each request into a brief, PRD, epics and stories the build can follow." },
        { key: "build", text: "Build the queued epics and verify them on the running app." },
        { key: "verification", text: "Run the final regression, security and packaging checks before anyone calls it done." },
      ],
      outputs: [{ key: "final_report", kind: "report", cadenceHint: "per request" }],
      boundaries: SDLC_CHARTER_BOUNDARIES,
      escalation: SDLC_CHARTER_ESCALATION,
      intake: "Requests arrive as issues assigned to this squad.",
    },
    instructions: GREENFIELD_INSTRUCTIONS,
    workflow: SDLC_AUTO_WORKFLOW,
  },
  {
    id: "sdlc_maintenance",
    name: "SDLC Maintenance",
    description:
      "Changes an existing product: triages a bug, feature, optimization, or upgrade, routes it, verifies it, and reports.",
    roles: [
      "lead",
      "analyst",
      "ux",
      "architect",
      "developer",
      "fixer",
      "reviewer",
      "qa",
      "security",
      "devops",
    ],
    leader: "lead",
    floorName: null,
    routine: null,
    charter: {
      mission: "Keep an existing product correct, safe and current: fix bugs, ship changes, and keep it up to date.",
      responsibilities: [
        { key: "triage", text: "Triage each bug or change: type, risk tier, and the route it takes." },
        { key: "delivery", text: "Deliver the change through review and QA, with evidence for each claim." },
        { key: "upkeep", text: "Keep dependencies, the build and the documentation current." },
      ],
      outputs: [{ key: "change_report", kind: "report", cadenceHint: "per change" }],
      boundaries: SDLC_CHARTER_BOUNDARIES,
      escalation: SDLC_CHARTER_ESCALATION,
      intake: "Bugs and changes arrive as issues assigned to this squad.",
    },
    instructions: MAINTENANCE_INSTRUCTIONS,
    workflow: null,
  },
  {
    id: "market_research",
    name: "Market Research",
    description:
      "Scans markets and competitors each week, has a critic challenge the evidence, and files only the proposals it supports.",
    roles: ["research_lead", "scout", "competitor", "critic"],
    leader: "research_lead",
    floorName: "Research",
    routine: {
      title: "Market scan",
      issueTitleTemplate: "Market scan, week of {{date}}",
      cron: "0 8 * * 1",
      description:
        "Weekly market scan. Scope: the markets, customer segments, and named competitors in the research notes; if none are named, start from the company knowledge base (multica knowledge search) and say which markets you chose. Look for changes in the last seven days: pricing, product launches, customer wins and complaints, regulation, and funding. Bar for a proposal: a change that touches a decision the company can make this quarter, backed by at least two dated sources, with its cost and risk stated. File zero to three proposals; no proposal this week is a valid result. Write the report in English.",
    },
    charter: {
      mission: "Scan markets, customers and competitors each week, and file only the proposals the evidence supports.",
      responsibilities: [
        { key: "market_scan", text: "Watch markets and customers for changes in the last seven days, with a dated source for each claim." },
        { key: "competitors", text: "Track named competitors' products, pricing and moves from primary sources." },
        { key: "challenge", text: "Have the critic challenge the evidence before anything is filed." },
      ],
      outputs: [
        { key: "weekly_report", kind: "report", cadenceHint: "weekly" },
        { key: "market_proposals", kind: "proposal", cadenceHint: "0 to 3 a week" },
      ],
      boundaries:
        "Does not write product code, make product decisions, or contact anyone outside the workspace.",
      escalation: "Questions for the boss go in a proposal, not in a chat.",
      intake: "The weekly market scan routine opens each week's issue.",
    },
    instructions: MARKET_RESEARCH_INSTRUCTIONS,
    workflow: null,
  },
];
