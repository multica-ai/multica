import { z } from "zod";
import {
  ROUTINE_EXPECTED_OUTPUTS,
  ROUTINE_STATES,
  type RoutineExpectedOutput,
  type RoutineState,
} from "./charter-schema";

/**
 * The company digest (`GET /api/office/digest`): facts about the company since
 * a point in time, computed by SQL and bounded in size. Every collection
 * defaults to empty so an older or newer server never blanks the page.
 */

export interface DigestIssueRef {
  id: string;
  /** e.g. "MUL-12". */
  identifier: string;
  title: string;
  /** Ready-made markdown link: `[MUL-12](mention://issue/<id>)`. */
  mention: string;
}

export interface DigestProposalRef {
  id: string;
  title: string;
}

/** A routine of a chartered squad, as the digest lists it (the first ten). */
export interface DigestRoutine {
  autopilot_id: string;
  title: string;
  purpose: string;
  expected_output: RoutineExpectedOutput;
  required: boolean;
  state: RoutineState;
}

/** The charter section of a squad that has a charter. */
export interface DigestSquadCharter {
  /** Nothing has happened in the squad for more than seven days. */
  silent: boolean;
  routine_count: number;
  routines: DigestRoutine[];
}

/** A required routine that is not on time, for the workspace section. */
export interface DigestRoutineRef {
  autopilot_id: string;
  squad_id: string;
  squad_name: string;
  title: string;
  state: RoutineState;
}

export interface DigestSquad {
  id: string;
  name: string;
  floor_id: string | null;
  /** Issues done since the digest's `since`. */
  done: number;
  in_progress: number;
  /** Issues in the `blocked` status. */
  blocked: number;
  /** In progress with no activity for three days. */
  stale: number;
  failed_runs: number;
  /** Squad-leader evaluations that took no action since `since`. */
  leader_no_action: number;
  /** All pending proposals; `pending_proposals` lists the newest few. */
  pending_proposal_count: number;
  pending_proposals: DigestProposalRef[];
  /** Up to five, newest first. */
  recent_done: DigestIssueRef[];
  /** Null when the squad has no charter. */
  charter: DigestSquadCharter | null;
}

export interface DigestQuietSquad {
  id: string;
  name: string;
  in_progress: number;
}

export interface DigestList<T> {
  count: number;
  items: T[];
}

export interface DigestWorkspace {
  runs: number;
  failed_runs: number;
  input_tokens: number;
  output_tokens: number;
  /** Only runs whose provider reported a cost; other runs add tokens, not dollars. */
  reported_cost_usd: number;
  offline_runtimes: DigestList<{ id: string; name: string; provider: string }>;
  agents_with_failed_runs: DigestList<{ id: string; name: string; failed_runs: number }>;
  unseated_squads: DigestList<{ id: string; name: string }>;
  /** Required routines not on time, at most ten; `count` is the total. */
  routines_not_on_time: DigestList<DigestRoutineRef>;
}

export interface DigestKnowledgeDocument {
  /** The knowledge record id. */
  id: string;
  title: string;
  /** The knowledge base key, such as `market`. */
  knowledge_base: string;
  published_at: string;
}

/** A document Multica published without a person pressing publish (spec 3.3). */
export interface DigestAutoPublish {
  link_id: string;
  title: string;
  knowledge_base: string;
  kind: string;
  /** `auto_after_review` or `auto`. */
  mode: string;
  author_squad_id: string;
  critic_squad_id: string;
  published_at: string;
}

export interface DigestKnowledge {
  /** Published documents not republished for 90 days, oldest first, at most 10. */
  stale_published: DigestList<DigestKnowledgeDocument>;
  /** Drafts awaiting publishing across the knowledge bases. */
  pending_drafts: number;
  /** Automatic publishes since the last brief, newest first, at most 20. */
  auto_published: DigestList<DigestAutoPublish>;
}

/** A question the Secretary put to a squad leader (`multica brief ask`). */
export interface DigestInquiry {
  question: string;
  answered: boolean;
  /** The first characters of the leader's reply, once answered. */
  answer_excerpt: string | null;
}

/** One active or operating initiative's place in its pipeline (spec 3.7). */
export interface DigestPipelineInitiative {
  id: string;
  identifier: string;
  title: string;
  stage_key: string;
  stage_name: string;
  owner_squad_id: string | null;
  status: string;
  days_in_stage: number;
  sla_days: number;
  /** Past the stage's SLA. A flag for the brief, not an automatic move. */
  stalled: boolean;
  artifacts_required: number;
  artifacts_present: number;
  /** What the initiative waits for, in words. */
  blockers: string[];
  /** The stage's exit criteria when the stage is left by a gate, else empty. */
  next_gate: string;
  pending_gate_proposal_id: string | null;
}

/** One goal the brief reports: a change since the last brief, or at risk or off track (spec 4.1). */
export interface DigestGoal {
  id: string;
  title: string;
  scope_type: string;
  scope_id: string | null;
  status: string;
  /** The effective confidence: the owner's override when one is set. */
  confidence: string;
  overridden: boolean;
  /** 0 to 1, or null when no key result has a reading. */
  progress: number | null;
  /** The key result that holds the goal back, in words; empty when there is none to name. */
  cause: string;
  recommendation: string;
  changed_since: boolean;
}

/** A squad request its receiving squad has not answered in time. */
export interface DigestOverdueRequest {
  issue_id: string;
  identifier: string;
  title: string;
  squad_id: string;
  hours_overdue: number;
}

export interface Digest {
  since: string;
  until: string;
  workspace: DigestWorkspace;
  /** Squads with news, what needs attention first, at most 20. */
  squads: DigestSquad[];
  /** Squads with news ranked below the cut. */
  omitted_squad_count: number;
  quiet: { count: number; squads: DigestQuietSquad[] };
  knowledge: DigestKnowledge;
  /** The Secretary's questions asked today or yesterday, newest first. */
  inquiries: DigestInquiry[];
  pipeline: DigestPipelineInitiative[];
  /** Goals that changed since the last brief, and those at risk or off track. */
  goals: DigestGoal[];
  overdue_requests: DigestOverdueRequest[];
  /** Proposals a standing approval approved in the window; the brief's FYI names them (spec 4.4). */
  auto_approved: DigestAutoApproval[];
  /** The brief's Budget section: budgets at or above their soft line, and the holds in force (spec 4.2). */
  budget: DigestBudget;
}

/** One proposal a standing approval approved. */
export interface DigestAutoApproval {
  proposal_id: string;
  identifier: string;
  title: string;
  squad_id: string;
  policy_id: string;
  policy_name: string;
  decided_at: string;
}

/** One budget at or above its soft line in the current period. */
export interface DigestBudgetScope {
  budget_id: string;
  scope_type: string;
  scope_id: string;
  spent_micros: number;
  limit_micros: number;
  state: string;
  mode: string;
}

/** One run held for its budget. */
export interface DigestBudgetHold {
  task_id: string;
  run_class: string;
  scope_type: string;
  scope_id: string;
  held_at: string;
}

export interface DigestBudget {
  scopes: DigestBudgetScope[];
  holds_in_force: number;
  holds: DigestBudgetHold[];
}

export interface DigestParams {
  /** RFC 3339 timestamp. The server defaults to the last 24 hours and allows at most 35 days back. */
  since?: string;
}

const num = z.number().default(0);

const DigestIssueRefSchema = z
  .object({
    id: z.string().default(""),
    identifier: z.string().default(""),
    title: z.string().default(""),
    mention: z.string().default(""),
  })
  .loose();

const DigestProposalRefSchema = z
  .object({ id: z.string().default(""), title: z.string().default("") })
  .loose();

const DigestRoutineSchema = z
  .object({
    autopilot_id: z.string().default(""),
    title: z.string().default(""),
    purpose: z.string().default(""),
    expected_output: z.enum([...ROUTINE_EXPECTED_OUTPUTS, "unknown"] as const).catch("unknown"),
    required: z.boolean().default(true),
    state: z.enum([...ROUTINE_STATES, "unknown"] as const).catch("unknown"),
  })
  .loose();

const DigestSquadCharterSchema = z
  .object({
    silent: z.boolean().default(false),
    routine_count: num,
    routines: z.array(DigestRoutineSchema).nullable().default([]).transform((v) => v ?? []),
  })
  .loose();

const DigestRoutineRefSchema = z
  .object({
    autopilot_id: z.string().default(""),
    squad_id: z.string().default(""),
    squad_name: z.string().default(""),
    title: z.string().default(""),
    state: z.enum([...ROUTINE_STATES, "unknown"] as const).catch("unknown"),
  })
  .loose();

export const DigestSquadSchema = z
  .object({
    id: z.string().default(""),
    name: z.string().default(""),
    floor_id: z.string().nullable().default(null),
    done: num,
    in_progress: num,
    blocked: num,
    stale: num,
    failed_runs: num,
    leader_no_action: num,
    pending_proposal_count: num,
    pending_proposals: z.array(DigestProposalRefSchema).nullable().default([]).transform((v) => v ?? []),
    recent_done: z.array(DigestIssueRefSchema).nullable().default([]).transform((v) => v ?? []),
    // A malformed charter section drops only that section; the squad still shows.
    charter: DigestSquadCharterSchema.nullable().default(null).catch(null),
  })
  .loose();

const DigestQuietSquadSchema = z
  .object({ id: z.string().default(""), name: z.string().default(""), in_progress: num })
  .loose();

function listSchema<T extends z.ZodType>(item: T) {
  return z
    .object({
      count: num,
      items: z.array(item).nullable().default([]).transform((v) => v ?? []),
    })
    .loose()
    .default({ count: 0, items: [] });
}

const DigestWorkspaceSchema = z
  .object({
    runs: num,
    failed_runs: num,
    input_tokens: num,
    output_tokens: num,
    reported_cost_usd: num,
    offline_runtimes: listSchema(
      z.object({ id: z.string().default(""), name: z.string().default(""), provider: z.string().default("") }).loose(),
    ),
    agents_with_failed_runs: listSchema(
      z.object({ id: z.string().default(""), name: z.string().default(""), failed_runs: num }).loose(),
    ),
    unseated_squads: listSchema(z.object({ id: z.string().default(""), name: z.string().default("") }).loose()),
    routines_not_on_time: listSchema(DigestRoutineRefSchema),
  })
  .loose();

// Left without the DigestWorkspace annotation on purpose: an interface has no
// index signature, which the loose schema's default parameter needs.
const emptyDigestWorkspace = {
  runs: 0,
  failed_runs: 0,
  input_tokens: 0,
  output_tokens: 0,
  reported_cost_usd: 0,
  offline_runtimes: { count: 0, items: [] },
  agents_with_failed_runs: { count: 0, items: [] },
  unseated_squads: { count: 0, items: [] },
  routines_not_on_time: { count: 0, items: [] },
};
export const EMPTY_DIGEST_WORKSPACE: DigestWorkspace = emptyDigestWorkspace;

// An unreadable inquiry is dropped alone; the other inquiries and the digest still load.
const DigestInquirySchema = z
  .object({
    question: z.string().default(""),
    answered: z.boolean().catch(false).default(false),
    answer_excerpt: z.string().nullable().catch(null).default(null),
  })
  .loose();

const DigestKnowledgeDocumentSchema = z
  .object({
    id: z.string().default(""),
    title: z.string().default(""),
    knowledge_base: z.string().default(""),
    published_at: z.string().default(""),
  })
  .loose();

const DigestAutoPublishSchema = z
  .object({
    link_id: z.string().default(""),
    title: z.string().default(""),
    knowledge_base: z.string().default(""),
    kind: z.string().default(""),
    mode: z.string().default(""),
    author_squad_id: z.string().default(""),
    critic_squad_id: z.string().default(""),
    published_at: z.string().default(""),
  })
  .loose();

// Same reason as emptyDigestWorkspace: no DigestKnowledge annotation.
const emptyDigestKnowledge = {
  stale_published: { count: 0, items: [] },
  pending_drafts: 0,
  auto_published: { count: 0, items: [] },
};
export const EMPTY_DIGEST_KNOWLEDGE: DigestKnowledge = emptyDigestKnowledge;

const DigestKnowledgeSchema = z
  .object({
    stale_published: listSchema(DigestKnowledgeDocumentSchema),
    pending_drafts: num,
    auto_published: listSchema(DigestAutoPublishSchema),
  })
  .loose()
  .default(emptyDigestKnowledge);

const DigestBudgetScopeSchema = z
  .object({
    budget_id: z.string().default(""),
    scope_type: z.string().default(""),
    scope_id: z.string().default(""),
    spent_micros: num,
    limit_micros: num,
    state: z.string().default(""),
    mode: z.string().default(""),
  })
  .loose();

const DigestBudgetHoldSchema = z
  .object({
    task_id: z.string().default(""),
    run_class: z.string().default(""),
    scope_type: z.string().default(""),
    scope_id: z.string().default(""),
    held_at: z.string().default(""),
  })
  .loose();

// An unreadable budget section reads as no budgets and no holds.
const DigestBudgetSchema = z
  .object({
    scopes: listOfItems(DigestBudgetScopeSchema),
    holds_in_force: num,
    holds: listOfItems(DigestBudgetHoldSchema),
  })
  .loose()
  .catch({ scopes: [], holds_in_force: 0, holds: [] })
  .default({ scopes: [], holds_in_force: 0, holds: [] });

// An unreadable initiative is dropped alone; the others and the digest still load.
const DigestPipelineInitiativeSchema = z
  .object({
    id: z.string().default(""),
    identifier: z.string().default(""),
    title: z.string().default(""),
    stage_key: z.string().default(""),
    stage_name: z.string().default(""),
    owner_squad_id: z.string().nullable().catch(null).default(null),
    status: z.string().default(""),
    days_in_stage: num,
    sla_days: num,
    stalled: z.boolean().catch(false).default(false),
    artifacts_required: num,
    artifacts_present: num,
    blockers: z.array(z.string()).nullable().catch([]).default([]).transform((v) => v ?? []),
    next_gate: z.string().catch("").default(""),
    pending_gate_proposal_id: z.string().nullable().catch(null).default(null),
  })
  .loose();

const DigestGoalSchema = z
  .object({
    id: z.string().default(""),
    title: z.string().default(""),
    scope_type: z.string().default(""),
    scope_id: z.string().nullable().catch(null).default(null),
    status: z.string().default(""),
    confidence: z.string().catch("unknown").default("unknown"),
    overridden: z.boolean().catch(false).default(false),
    progress: z.number().nullable().catch(null).default(null),
    cause: z.string().catch("").default(""),
    recommendation: z.string().catch("").default(""),
    changed_since: z.boolean().catch(false).default(false),
  })
  .loose();

const DigestAutoApprovalSchema = z
  .object({
    proposal_id: z.string().default(""),
    identifier: z.string().default(""),
    title: z.string().default(""),
    squad_id: z.string().default(""),
    policy_id: z.string().default(""),
    policy_name: z.string().default(""),
    decided_at: z.string().default(""),
  })
  .loose();

const DigestOverdueRequestSchema = z
  .object({
    issue_id: z.string().default(""),
    identifier: z.string().default(""),
    title: z.string().default(""),
    squad_id: z.string().default(""),
    hours_overdue: num,
  })
  .loose();

/** Reads a list item by item: a missing or non-list value is none, a bad item is dropped. */
function listOfItems<T>(schema: z.ZodType<T>) {
  return z.unknown().transform((value) =>
    Array.isArray(value)
      ? value.flatMap((item) => {
          const parsed = schema.safeParse(item);
          return parsed.success ? [parsed.data] : [];
        })
      : [],
  );
}

export const DigestSchema = z
  .object({
    since: z.string().default(""),
    until: z.string().default(""),
    workspace: DigestWorkspaceSchema.default(emptyDigestWorkspace),
    squads: z.array(DigestSquadSchema).nullable().default([]).transform((v) => v ?? []),
    omitted_squad_count: num,
    quiet: z
      .object({
        count: num,
        squads: z.array(DigestQuietSquadSchema).nullable().default([]).transform((v) => v ?? []),
      })
      .loose()
      .default({ count: 0, squads: [] }),
    knowledge: DigestKnowledgeSchema,
    // Not a list (or missing) reads as none; the rest of the digest still loads.
    inquiries: listOfItems(DigestInquirySchema),
    // An older server sends neither list; both read as none.
    pipeline: listOfItems(DigestPipelineInitiativeSchema),
    // An older server sends no goals; that reads as none.
    goals: listOfItems(DigestGoalSchema),
    overdue_requests: listOfItems(DigestOverdueRequestSchema),
    // An older server sends no standing approvals; that reads as none.
    auto_approved: listOfItems(DigestAutoApprovalSchema),
    // An older server sends no budget section; it reads as none.
    budget: DigestBudgetSchema,
  })
  .loose();

/** What an unreadable digest response degrades to: an empty, quiet company. */
export const FALLBACK_DIGEST: Digest = {
  since: "",
  until: "",
  workspace: EMPTY_DIGEST_WORKSPACE,
  squads: [],
  omitted_squad_count: 0,
  quiet: { count: 0, squads: [] },
  knowledge: EMPTY_DIGEST_KNOWLEDGE,
  inquiries: [],
  pipeline: [],
  goals: [],
  overdue_requests: [],
  auto_approved: [],
  budget: { scopes: [], holds_in_force: 0, holds: [] },
};
