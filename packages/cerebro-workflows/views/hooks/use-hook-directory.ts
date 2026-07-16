"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { issueListOptions } from "@multica/core/issues/queries";
import { api } from "@multica/core/api";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { agentListOptions, memberListOptions, skillListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { cerebroWorkflowsListOptions } from "../../core";
import type { HookDirectory, HookTargetOption } from "./hook-target-picker";

export function useHookDirectory(workspaceId: string): HookDirectory {
  const agents = useQuery(agentListOptions(workspaceId));
  const issues = useQuery(issueListOptions(workspaceId));
  const members = useQuery(memberListOptions(workspaceId));
  const sessions = useQuery(chatSessionsOptions(workspaceId));
  const artifacts = useQuery({ queryKey: ["workflow-hooks", workspaceId, "artifacts"], queryFn: () => api.searchArtifacts({ limit: 50 }) });
  const projects = useQuery(projectListOptions(workspaceId));
  const skills = useQuery(skillListOptions(workspaceId));
  const squads = useQuery(squadListOptions(workspaceId));
  const workflows = useQuery(cerebroWorkflowsListOptions(workspaceId));

  return useMemo(() => {
    const activeAgents = (agents.data ?? []).filter((agent) => !agent.archived_at);
    const models = new Map<string, HookTargetOption>();
    for (const agent of activeAgents) {
      if (agent.model) models.set(agent.model, { value: agent.model, label: agent.model, description: `Used by ${agent.name}` });
    }
    return {
      agent: activeAgents.map((agent) => ({ value: agent.id, label: agent.name, description: agent.model || undefined })),
      member: (members.data ?? []).map((member) => ({ value: member.id, label: member.name || member.email || "Workspace member", description: member.email || undefined })),
      model: [...models.values()],
      issue: (issues.data ?? []).map((issue) => ({ value: issue.id, label: issue.title, description: issue.identifier })),
      project: (projects.data ?? []).map((project) => ({ value: project.id, label: project.title })),
      skill: (skills.data ?? []).map((skill) => ({ value: skill.name, label: skill.name, description: skill.description || undefined })),
      squad: (squads.data ?? []).map((squad) => ({ value: squad.id, label: squad.name })),
      workflow: (workflows.data?.workflows ?? []).map((workflow) => ({ value: workflow.id, label: workflow.name, description: workflow.workflow_type === "issue_loop" ? "Issue workflow" : "Standard workflow" })),
      session: (sessions.data ?? []).map((session) => ({ value: session.id, label: session.title || "Untitled chat", description: session.status === "archived" ? "Archived chat" : "Active chat" })),
      artifact: (artifacts.data ?? []).map((artifact) => ({ value: artifact.id, label: artifact.title, description: artifact.kind })),
      searchIssues: async (query: string) => (await api.searchIssues({ q: query, limit: 20, include_closed: true })).issues.map((issue) => ({ value: issue.id, label: issue.title, description: issue.identifier })),
    };
  }, [agents.data, artifacts.data, issues.data, members.data, projects.data, sessions.data, skills.data, squads.data, workflows.data]);
}
