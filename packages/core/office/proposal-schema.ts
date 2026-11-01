import { z } from "zod";

/**
 * A proposal is a squad's structured request for a decision by the boss,
 * backed by an ordinary issue (`issue_id`). `unknown` stands in for a state a
 * newer server may add: views must treat it as read-only.
 */
export type ProposalState = "pending" | "approved" | "rejected" | "unknown";
export type ProposalDecision = "approve" | "reject";

const KNOWN_STATES = ["pending", "approved", "rejected"] as const;

export interface ProposalHandoff {
  project_id: string;
  kickoff_issue_id: string;
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

export interface DecideProposalRequest {
  decision: ProposalDecision;
  note?: string;
}

export interface ListProposalsParams {
  state?: Exclude<ProposalState, "unknown">;
  squad_id?: string;
}

// Unknown states degrade to "unknown" instead of failing the whole row, so a
// newer server's extra state never blanks the decisions list.
const ProposalStateSchema = z
  .string()
  .default("unknown")
  .transform((s): ProposalState =>
    (KNOWN_STATES as readonly string[]).includes(s) ? (s as ProposalState) : "unknown",
  );

const ProposalHandoffSchema = z
  .object({
    project_id: z.string().default(""),
    kickoff_issue_id: z.string().default(""),
  })
  .loose();

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
  handoff: null,
};
