export type SessionStatus = "todo" | "in_progress" | "done";

// The carry-over brief between sessions. In Model B it is a single nullable
// object on the session row (no per-comment field, no 4-field human form). An
// agent may author it via the API; otherwise the server auto-summarises.
export interface HandoffBrief {
  summary: string;
  done: string[];
  remaining: string[];
  plan_ref?: string | null;
}

export interface Session {
  id: string;
  issue_id: string;
  position: number;
  name: string;
  status: SessionStatus;
  handoff: HandoffBrief | null;
  created_at: string;
  updated_at: string;
}

// Starting a fresh session either carries the previous session's handoff
// forward ("handoff") or opens empty ("blank"). The closing session always
// keeps its handoff either way.
export type SessionStartMode = "handoff" | "blank";

export interface SessionStartFreshInput {
  mode: SessionStartMode;
  // Optional agent-authored brief; when omitted the server auto-summarises the
  // closing session.
  handoff?: HandoffBrief | null;
}
