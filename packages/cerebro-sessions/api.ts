import { api } from "@multica/core/api";
import type { HandoffBrief, Session, SessionStartFreshInput, SessionStatus } from "./types";

const base = (issueId: string) => `/api/cerebro/issues/${issueId}/sessions`;

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
