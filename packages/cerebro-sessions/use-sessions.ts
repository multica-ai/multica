"use client";

import { useCallback } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getContextTimeline, getContextUsage, getUsageBreakdown, listSessions, startFresh, updateSession } from "./api";
import type { HandoffActionInput } from "./types";
import type { SessionMode } from "./modes";

export const sessionKeys = {
  detail: (issueId: string) => ["cerebro-sessions", issueId] as const,
};

export function useSessions(issueId: string) {
  return useQuery({
    queryKey: sessionKeys.detail(issueId),
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

// FIR-1931: per-session cost/token/cache breakdown for the sheet the context bar
// opens. `enabled` lets the caller fetch only when the sheet is actually open.
export function useUsageBreakdown(issueId: string, enabled = true) {
  return useQuery({
    queryKey: ["cerebro-session-usage-breakdown", issueId],
    queryFn: () => getUsageBreakdown(issueId),
    enabled,
  });
}

// FIR-1931: the development curve over a session (one point per agent run).
// `enabled` lets the caller fetch only when the chart is actually shown.
export function useContextTimeline(issueId: string, sessionId: string, enabled = true) {
  return useQuery({
    queryKey: ["cerebro-session-context-timeline", issueId, sessionId],
    queryFn: () => getContextTimeline(issueId, sessionId),
    enabled: enabled && !!sessionId,
  });
}

export function useStartFresh(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: HandoffActionInput) => startFresh(issueId, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: sessionKeys.detail(issueId) }),
  });
}

// FIR-1874 point 2 (Jesper A): wrap a comment submit so that, when a handoff is
// armed, pressing Send first resolves the chosen thread + stores its summary
// (the handoff), then posts the new comment — which becomes the fresh session
// carrying that summary. The handoff runs first so it fails closed: if it
// errors, the comment is not posted and the draft is preserved. `onDone` clears
// the armed state after a successful send.
export function useSubmitWithHandoff(
  issueId: string,
  armed: { rootCommentId: string } | null,
  onDone: () => void,
  submit: (content: string, attachmentIds?: string[], mode?: SessionMode) => Promise<void>,
) {
  const handoff = useStartFresh(issueId);
  return useCallback(
    async (content: string, attachmentIds?: string[], mode?: SessionMode) => {
      if (armed) {
        await handoff.mutateAsync({ root_comment_id: armed.rootCommentId });
      }
      if (mode) await submit(content, attachmentIds, mode);
      else await submit(content, attachmentIds);
      if (armed) onDone();
    },
    [issueId, armed, handoff, onDone, submit],
  );
}

export function useUpdateSession(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ sessionId, input }: { sessionId: string; input: Parameters<typeof updateSession>[2] }) =>
      updateSession(issueId, sessionId, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: sessionKeys.detail(issueId) }),
  });
}
