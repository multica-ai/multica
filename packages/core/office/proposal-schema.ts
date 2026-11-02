import { z } from "zod";

/**
 * A proposal is a squad's structured request for a decision by the boss,
 * backed by an ordinary issue (`issue_id`). `revising` means the boss asked for
 * changes and the squad has not resubmitted; `expired` means no decision came
 * within the window. `unknown` stands in for a state a newer server may add:
 * views must treat it as read-only.
 */
export type ProposalState =
  | "pending"
  | "approved"
  | "rejected"
  | "revising"
  | "expired"
  | "unknown";
export type ProposalDecision = "approve" | "reject" | "revise" | "snooze";

const KNOWN_STATES = ["pending", "approved", "rejected", "revising", "expired"] as const;

export interface ProposalHandoff {
  project_id: string;
  kickoff_issue_id: string;
}

/**
 * An earlier proposal or decision that looks related to a proposal. Proposal
 * entries carry the state and issue identifier; decision entries from the
 * knowledge base carry null for both.
 */
export interface ProposalRelated {
  kind: "proposal" | "decision" | "unknown";
  id: string;
  title: string;
  score: number;
  state: ProposalState | null;
  issue_identifier: string | null;
  created_at: string | null;
}

/** One version of a proposal's body. Revision 1 is the body as first filed. */
export interface ProposalRevision {
  revision: number;
  body: string;
  created_by_type: string;
  created_by_id: string;
  created_at: string;
}

export interface Proposal {
  id: string;
  workspace_id: string;
  squad_id: string;
  issue_id: string;
  /** e.g. "MUL-12". Empty when the server omitted it. */
  issue_identifier: string;
  issue_status: string;
  /** The backing issue's title. */
  title: string;
  handoff_squad_id: string | null;
  urgent: boolean;
  urgent_reason: string;
  state: ProposalState;
  created_by_type: string;
  created_by_id: string;
  decided_by: string | null;
  decided_at: string | null;
  decision_note: string;
  created_at: string;
  /** Starts at 1 and goes up each time the squad resubmits after a revise decision. */
  revision: number;
  /** End of the decision window. Kept after a decision as the record of the window. */
  expires_at: string | null;
  /** Set while the boss has put a pending proposal aside. A past time means the snooze has ended. */
  snoozed_until: string | null;
  /** Earlier proposals and decisions that look related. Empty when none do. */
  related: ProposalRelated[];
  /** Present only on the response to an approval that handed off to a squad. */
  handoff: ProposalHandoff | null;
}

export interface CreateProposalRequest {
  squad_id: string;
  title: string;
  summary: string;
  evidence: string[];
  recommendation: string;
  options: string[];
  cost_risk: string;
  ask: string;
  handoff_squad_id?: string;
  urgent?: boolean;
  urgent_reason?: string;
}

/**
 * A boss's decision. `revise` needs a note; `snooze` needs `until`, a date
 * (YYYY-MM-DD, midnight UTC) that the server checks is later than now and at
 * most 30 days away.
 */
export type DecideProposalRequest =
  | { decision: "approve" | "reject"; note?: string }
  | { decision: "revise"; note: string }
  | { decision: "snooze"; until: string };

export interface ListProposalsParams {
  state?: Exclude<ProposalState, "unknown">;
  squad_id?: string;
}

// Unknown states degrade to "unknown" instead of failing the whole row, so a
// newer server's extra state never blanks the decisions list.
function toProposalState(s: string): ProposalState {
  return (KNOWN_STATES as readonly string[]).includes(s) ? (s as ProposalState) : "unknown";
}

const ProposalStateSchema = z
  .string()
  .default("unknown")
  .transform(toProposalState);

const ProposalHandoffSchema = z
  .object({
    project_id: z.string().default(""),
    kickoff_issue_id: z.string().default(""),
  })
  .loose();

export const ProposalRelatedSchema = z
  .object({
    kind: z
      .string()
      .default("unknown")
      .transform((s): ProposalRelated["kind"] =>
        s === "proposal" || s === "decision" ? s : "unknown",
      ),
    id: z.string().default(""),
    title: z.string().default(""),
    score: z.number().default(0),
    state: z
      .string()
      .nullable()
      .default(null)
      .transform((s) => (s === null ? null : toProposalState(s))),
    issue_identifier: z.string().nullable().default(null),
    created_at: z.string().nullable().default(null),
  })
  .loose();

export const ProposalRevisionSchema = z
  .object({
    revision: z.number().default(1),
    body: z.string().default(""),
    created_by_type: z.string().default(""),
    created_by_id: z.string().default(""),
    created_at: z.string().default(""),
  })
  .loose();

export const ProposalRevisionListSchema = z.array(ProposalRevisionSchema);

export const ProposalSchema = z
  .object({
    id: z.string().default(""),
    workspace_id: z.string().default(""),
    squad_id: z.string().default(""),
    issue_id: z.string().default(""),
    issue_identifier: z.string().default(""),
    issue_status: z.string().default(""),
    title: z.string().default(""),
    handoff_squad_id: z.string().nullable().default(null),
    urgent: z.boolean().default(false),
    urgent_reason: z.string().default(""),
    state: ProposalStateSchema,
    created_by_type: z.string().default(""),
    created_by_id: z.string().default(""),
    decided_by: z.string().nullable().default(null),
    decided_at: z.string().nullable().default(null),
    decision_note: z.string().default(""),
    created_at: z.string().default(""),
    revision: z.number().default(1),
    expires_at: z.string().nullable().default(null),
    snoozed_until: z.string().nullable().default(null),
    related: z.array(ProposalRelatedSchema).default([]),
    handoff: ProposalHandoffSchema.nullable().default(null),
  })
  .loose();

export const ProposalListSchema = z.array(ProposalSchema);

/** What an unreadable single-proposal response degrades to: an `unknown`, id-less row. */
export const FALLBACK_PROPOSAL: Proposal = {
  id: "",
  workspace_id: "",
  squad_id: "",
  issue_id: "",
  issue_identifier: "",
  issue_status: "",
  title: "",
  handoff_squad_id: null,
  urgent: false,
  urgent_reason: "",
  state: "unknown",
  created_by_type: "",
  created_by_id: "",
  decided_by: null,
  decided_at: null,
  decision_note: "",
  created_at: "",
  revision: 1,
  expires_at: null,
  snoozed_until: null,
  related: [],
  handoff: null,
};
