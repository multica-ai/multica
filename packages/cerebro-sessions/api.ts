import { z } from "zod";
import { api, parseWithFallback } from "@multica/core/api";
import type { ContextUsage, HandoffBrief, Session, SessionStartFreshInput, SessionStatus } from "./types";

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
};

export async function getContextUsage(issueId: string): Promise<ContextUsage> {
  const path = `${base(issueId)}/context-usage`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(raw, ContextUsageSchema, CONTEXT_USAGE_FALLBACK, {
    endpoint: path,
  }) as ContextUsage;
}

export function listSessions(issueId: string): Promise<Session[]> {
  return api.cerebroRequest<Session[]>(base(issueId));
}

export function startFresh(issueId: string, input: SessionStartFreshInput): Promise<Session> {
  return api.cerebroRequest<Session>(`${base(issueId)}/start-fresh`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function updateSession(issueId: string, sessionId: string, input: {
  name?: string;
  status?: SessionStatus;
  handoff?: HandoffBrief;
}): Promise<Session> {
  return api.cerebroRequest<Session>(`${base(issueId)}/${sessionId}`, {
    method: "PATCH",
    body: JSON.stringify(input),
  });
}
