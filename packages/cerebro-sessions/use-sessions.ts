"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getContextUsage, listSessions, startFresh, updateSession } from "./api";
import type { SessionStartFreshInput } from "./types";

const key = (issueId: string) => ["cerebro-sessions", issueId] as const;

export function useSessions(issueId: string) {
  return useQuery({
    queryKey: key(issueId),
    queryFn: () => listSessions(issueId),
  });
}

// Context-window usage from real per-run tokens (cross-runtime, cross-model).
// `sessionId` scopes it to one session so each session shows its own window
// (FIR-1870); omit/empty for the issue's active session. `enabled` lets the
// caller fetch only when the bar is actually shown.
export function useContextUsage(issueId: string, sessionId?: string, enabled = true) {
  return useQuery({
    queryKey: ["cerebro-session-context-usage", issueId, sessionId ?? ""],
    queryFn: () => getContextUsage(issueId, sessionId),
    enabled,
  });
}

export function useStartFresh(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: SessionStartFreshInput) => startFresh(issueId, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: key(issueId) }),
  });
}

export function useUpdateSession(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ sessionId, input }: { sessionId: string; input: Parameters<typeof updateSession>[2] }) =>
      updateSession(issueId, sessionId, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: key(issueId) }),
  });
}
