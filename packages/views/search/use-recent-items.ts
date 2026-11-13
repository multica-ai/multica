"use client";

import { useMemo } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { selectRecentVisits, useRecentVisitsStore } from "@multica/core/command-bar";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { selectRecentIssues, useRecentIssuesStore } from "@multica/core/issues/stores";
import { projectListOptions } from "@multica/core/projects/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { Agent, Autopilot, Issue, Project } from "@multica/core/types";

export type RecentItem =
  | { type: "issue"; id: string; visitedAt: number; issue: Issue }
  | { type: "project"; id: string; visitedAt: number; project: Project }
  | { type: "agent"; id: string; visitedAt: number; agent: Agent }
  | { type: "autopilot"; id: string; visitedAt: number; autopilot: Autopilot };

const MAX_RECENT = 8;

/**
 * Recently opened issues, projects, agents and autopilots, newest first.
 * Everything is gated on `open`: the palette is mounted for the whole
 * session, and an ungated lookup fired up to 20 issue detail requests on every
 * cold app load for a surface the user may never open.
 */
export function useRecentItems(open: boolean): RecentItem[] {
  const wsId = useWorkspaceId();
  const recentIssues = useRecentIssuesStore(selectRecentIssues(wsId));
  const recentVisits = useRecentVisitsStore(selectRecentVisits(wsId));

  // Resolve each recent issue via its cached detail entry. Recent items are
  // typically already in the detail cache because the user has opened them;
  // if not, this triggers a lookup per id so Recent never depends on whether
  // the issue falls inside the paginated list cache.
  const issueQueries = useQueries({
    queries: open ? recentIssues.map((item) => issueDetailOptions(wsId, item.id)) : [],
  });
  const wants = (type: string) => open && recentVisits.some((v) => v.type === type);
  const { data: projects } = useQuery({ ...projectListOptions(wsId), enabled: wants("project") });
  const { data: agents } = useQuery({ ...agentListOptions(wsId), enabled: wants("agent") });
  const { data: autopilots } = useQuery({ ...autopilotListOptions(wsId), enabled: wants("autopilot") });

  return useMemo(() => {
    if (!open) return [];
    const items: RecentItem[] = [];
    recentIssues.forEach((entry, index) => {
      const issue = issueQueries[index]?.data;
      if (issue) items.push({ type: "issue", id: issue.id, visitedAt: entry.visitedAt, issue });
    });
    for (const visit of recentVisits) {
      if (visit.type === "project") {
        const project = projects?.find((p) => p.id === visit.id);
        if (project) items.push({ type: "project", id: project.id, visitedAt: visit.visitedAt, project });
      } else if (visit.type === "agent") {
        const agent = agents?.find((a) => a.id === visit.id);
        if (agent && !agent.archived_at) {
          items.push({ type: "agent", id: agent.id, visitedAt: visit.visitedAt, agent });
        }
      } else {
        const autopilot = autopilots?.find((a) => a.id === visit.id);
        if (autopilot && autopilot.status !== "archived") {
          items.push({ type: "autopilot", id: autopilot.id, visitedAt: visit.visitedAt, autopilot });
        }
      }
    }
    return items.sort((a, b) => b.visitedAt - a.visitedAt).slice(0, MAX_RECENT);
  }, [agents, autopilots, issueQueries, open, projects, recentIssues, recentVisits]);
}
