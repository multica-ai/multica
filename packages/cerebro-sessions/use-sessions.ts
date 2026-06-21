"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { listSessions, startFresh, updateSession } from "./api";
import type { SessionStartFreshInput } from "./types";

const key = (issueId: string) => ["cerebro-sessions", issueId] as const;

export function useSessions(issueId: string) {
  return useQuery({
    queryKey: key(issueId),
    queryFn: () => listSessions(issueId),
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
