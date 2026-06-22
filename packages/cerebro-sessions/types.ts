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

// The truthful context-window measurement for a session's active context, read
// from real per-run token usage (task_usage) — across runtimes and models, not
// a char-count guess. has_data is false until a run with recorded usage has
// happened in the active session. max_context_tokens is the active run's model
// window, so used_percent stays meaningful when the model changes between runs.
export interface ContextUsage {
  session_id: string;
  has_data: boolean;
  model: string;
  input_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  output_tokens: number;
  // The whole prompt the model last read (input + cache_read + cache_write).
  // This — not input_tokens alone — drives used_percent and the token count the
  // indicator shows; a warm cache makes input_tokens a tiny fraction of it.
  context_tokens: number;
  max_context_tokens: number;
  used_percent: number;
  cache_share_percent: number;
}
