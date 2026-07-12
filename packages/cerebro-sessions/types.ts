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
  mode?: "default" | "plan";
  // FIR-2283 followup: the workflow phase this session (thread) belongs to,
  // when it was started by an Issue workflow — "plan" | "build" | "review".
  // Null/absent for ordinary (non-workflow) sessions.
  phase: string | null;
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
  // True when the server fell back to the cumulative task_usage sum (no last-turn
  // footprint). The figure is then clamped to the window and the bar prefixes "~"
  // (FIR-1931): a heavy issue must never display an impossible 6986k / 1000k.
  approximate: boolean;
  // FIR-1960: compaction tracking surfaced in the measurement itself, not only the
  // development-curve sheet. compactions counts the sharp context drops in this
  // session's run history; last_run_compaction is true when the most recent run
  // was itself a drop — the window was just compacted. Zero/false for sessions
  // that predate footprint history (an older server omits them; default to 0).
  compactions: number;
  last_run_compaction: boolean;
  // FIR-2279: RFC3339 timestamp of when the latest run in this session finished.
  // Anthropic keeps the prompt cache warm ~5 minutes past the last call, so the
  // bar counts down from here to a "cache cold" state (after which resuming
  // re-pays the full cache-write). Null/absent when there is no run yet or an
  // older server omits it — the bar then shows no timer.
  last_activity_at?: string | null;
}

// FIR-1931: one row of the per-session usage breakdown the context bar opens in a
// sheet — summed cost/tokens/cache for a session (= a thread) plus its current
// context fullness. Built from data we already have (task_usage + footprint).
export interface SessionUsageRow {
  session_id: string;
  title: string;
  created_at: string;
  run_count: number;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  cost_cents: number;
  model: string;
  used_percent: number;
  context_tokens: number;
  max_context_tokens: number;
  cache_share_percent: number;
  approximate: boolean;
}

// The whole breakdown: one row per session + the issue totals.
export interface UsageBreakdown {
  sessions: SessionUsageRow[];
  total_cost_cents: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_cache_read_tokens: number;
  total_cache_write_tokens: number;
}

// FIR-1931: one point on a session's development curve — one agent run's context
// footprint. is_compaction marks a sharp drop from a fuller previous run.
export interface ContextTimelinePoint {
  observed_at: string;
  context_tokens: number;
  max_context_tokens: number;
  used_percent: number;
  cache_share_percent: number;
  is_compaction: boolean;
}

// The development curve over a session (one point per run). has_data is false for
// sessions that ran before the history feature deployed — show an empty state.
export interface ContextTimeline {
  session_id: string;
  has_data: boolean;
  points: ContextTimelinePoint[];
}
