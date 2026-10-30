"use client";

import { useEffect, useId } from "react";
import {
  useCommandBarContextStore,
  useRecentVisitsStore,
  type CommandBarContext,
  type RecentVisitType,
} from "@multica/core/command-bar";

function recentVisitOf(
  context: CommandBarContext | null,
): { wsId: string; type: RecentVisitType; id: string } | null {
  switch (context?.kind) {
    case "project":
      return { wsId: context.project.workspace_id, type: "project", id: context.project.id };
    case "agent":
      return { wsId: context.agent.workspace_id, type: "agent", id: context.agent.id };
    case "autopilot":
      return { wsId: context.autopilot.workspace_id, type: "autopilot", id: context.autopilot.id };
    default:
      return null;
  }
}

/**
 * Tell the command bar what this page is about. Pass `null` while the object
 * is still loading. The context must be memoized: a new object re-registers.
 *
 * Project, agent and autopilot pages also land in the bar's Recent group.
 */
export function useRegisterCommandBarContext(context: CommandBarContext | null) {
  const token = useId();

  useEffect(() => {
    const store = useCommandBarContextStore.getState();
    if (context) store.register(token, context);
    else store.unregister(token);
  }, [token, context]);

  useEffect(
    () => () => useCommandBarContextStore.getState().unregister(token),
    [token],
  );

  const visit = recentVisitOf(context);
  const wsId = visit?.wsId ?? null;
  const visitType = visit?.type ?? null;
  const visitId = visit?.id ?? null;
  useEffect(() => {
    if (!wsId || !visitType || !visitId) return;
    useRecentVisitsStore.getState().recordVisit(wsId, { type: visitType, id: visitId });
  }, [wsId, visitType, visitId]);
}
