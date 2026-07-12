import { z } from "zod";
import { api, parseWithFallback } from "@multica/core/api";
import type {
  ContextTimeline,
  ContextUsage,
  HandoffActionInput,
  HandoffBrief,
  Session,
  UsageBreakdown,
} from "./types";

const base = (issueId: string) => `/api/cerebro/issues/${issueId}/sessions`;

// Defensive boundary (see CLAUDE.md "API Response Compatibility"): an older
// desktop build must survive a newer/older server shape without white-screening.
// Every field defaults, so a missing one downgrades to "no data" rather than NaN.
const ContextUsageSchema = z.object({
  session_id: z.string().default(""),
  has_data: z.boolean().default(false),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  context_tokens: z.number().default(0),
  max_context_tokens: z.number().default(0),
  used_percent: z.number().default(0),
  cache_share_percent: z.number().default(0),
  approximate: z.boolean().default(false),
  // FIR-1960: an older server omits these — default to 0/false so the bar simply
  // shows no compaction marker rather than NaN.
  compactions: z.number().default(0),
  last_run_compaction: z.boolean().default(false),
  // FIR-2279: prompt-cache countdown anchor. Absent on older servers → null, so
  // the bar renders no timer rather than NaN.
  last_activity_at: z.string().nullish().default(null),
}).loose();

export const CONTEXT_USAGE_FALLBACK: ContextUsage = {
  session_id: "",
  has_data: false,
  model: "",
  input_tokens: 0,
  cache_read_tokens: 0,
  cache_write_tokens: 0,
  output_tokens: 0,
  context_tokens: 0,
  max_context_tokens: 0,
  used_percent: 0,
  cache_share_percent: 0,
  approximate: false,
  compactions: 0,
  last_run_compaction: false,
  last_activity_at: null,
};

export async function getContextUsage(issueId: string, sessionId?: string): Promise<ContextUsage> {
  // FIR-1870: scope the measurement to a specific session so each session shows
  // its own context window; omit for the active session.
  const query = sessionId && sessionId !== "default" ? `?session_id=${encodeURIComponent(sessionId)}` : "";
  const path = `${base(issueId)}/context-usage${query}`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(raw, ContextUsageSchema, CONTEXT_USAGE_FALLBACK, {
    endpoint: path,
  }) as ContextUsage;
}

// FIR-1931: per-session usage breakdown shown in the sheet the context bar opens.
// Every field defaults so an older/newer server shape downgrades to empty rather
// than throwing into the sheet (CLAUDE.md "API Response Compatibility").
const SessionUsageRowSchema = z.object({
  session_id: z.string().default(""),
  title: z.string().default(""),
  created_at: z.string().default(""),
  run_count: z.number().default(0),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  cost_cents: z.number().default(0),
  model: z.string().default(""),
  used_percent: z.number().default(0),
  context_tokens: z.number().default(0),
  max_context_tokens: z.number().default(0),
  cache_share_percent: z.number().default(0),
  approximate: z.boolean().default(false),
}).loose();

const UsageBreakdownSchema = z.object({
  sessions: z.array(SessionUsageRowSchema).default([]),
  total_cost_cents: z.number().default(0),
  total_input_tokens: z.number().default(0),
  total_output_tokens: z.number().default(0),
  total_cache_read_tokens: z.number().default(0),
  total_cache_write_tokens: z.number().default(0),
}).loose();

export const USAGE_BREAKDOWN_FALLBACK: UsageBreakdown = {
  sessions: [],
  total_cost_cents: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_read_tokens: 0,
  total_cache_write_tokens: 0,
};

export async function getUsageBreakdown(issueId: string): Promise<UsageBreakdown> {
  const path = `${base(issueId)}/usage-breakdown`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(raw, UsageBreakdownSchema, USAGE_BREAKDOWN_FALLBACK, {
    endpoint: path,
  }) as UsageBreakdown;
}

// FIR-1931: the development curve over a session (one point per agent run).
const ContextTimelinePointSchema = z.object({
  observed_at: z.string().default(""),
  context_tokens: z.number().default(0),
  max_context_tokens: z.number().default(0),
  used_percent: z.number().default(0),
  cache_share_percent: z.number().default(0),
  is_compaction: z.boolean().default(false),
}).loose();

const ContextTimelineSchema = z.object({
  session_id: z.string().default(""),
  has_data: z.boolean().default(false),
  points: z.array(ContextTimelinePointSchema).default([]),
}).loose();

export const CONTEXT_TIMELINE_FALLBACK: ContextTimeline = {
  session_id: "",
  has_data: false,
  points: [],
};

export async function getContextTimeline(issueId: string, sessionId: string): Promise<ContextTimeline> {
  const path = `${base(issueId)}/${encodeURIComponent(sessionId)}/context-timeline`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(raw, ContextTimelineSchema, CONTEXT_TIMELINE_FALLBACK, {
    endpoint: path,
  }) as ContextTimeline;
}

export function listSessions(issueId: string): Promise<Session[]> {
  return api.cerebroRequest<Session[]>(base(issueId));
}

// FIR-1874: the Send-button Handoff action. Closes (resolves) the chosen thread
// and stores a handoff on its row. Endpoint kept as /start-fresh to avoid an
// upstream router edit.
export function startFresh(issueId: string, input: HandoffActionInput): Promise<Session> {
  return api.cerebroRequest<Session>(`${base(issueId)}/start-fresh`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function updateSession(issueId: string, sessionId: string, input: {
  name?: string;
  mode?: "default" | "plan";
  handoff?: HandoffBrief;
}): Promise<Session> {
  return api.cerebroRequest<Session>(`${base(issueId)}/${sessionId}`, {
    method: "PATCH",
    body: JSON.stringify(input),
  });
}
