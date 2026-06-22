// The carry-over brief a session keeps when it is handed off. A single nullable
// object on the session row. An agent may author it via the API; otherwise the
// server auto-summarises the thread.
export interface HandoffBrief {
  summary: string;
  done: string[];
  remaining: string[];
  plan_ref?: string | null;
}

// FIR-1874: a Session row is OPTIONAL metadata (name/handoff) keyed to a thread
// root via root_comment_id. A session's open/closed state is NOT stored here —
// it is the thread root's resolved_at — so there is no status field.
export interface Session {
  id: string;
  issue_id: string;
  root_comment_id: string | null;
  position: number;
  name: string;
  handoff: HandoffBrief | null;
  created_at: string;
  updated_at: string;
}

// The Send-button "Start new session with Handoff" action: it closes the chosen
// open thread (root_comment_id) by resolving its root and stores a handoff on
// that thread's row. An optional agent-authored brief overrides the auto-summary.
export interface HandoffActionInput {
  root_comment_id: string;
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
