import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const sprintKeys = {
  all: (wsId: string) => ["sprints", wsId] as const,
  project: (wsId: string, projectId: string) => [...sprintKeys.all(wsId), "project", projectId] as const,
  list: (wsId: string, projectId: string) => [...sprintKeys.project(wsId, projectId), "list"] as const,
  detail: (wsId: string, sprintId: string) => [...sprintKeys.all(wsId), "detail", sprintId] as const,
  issues: (wsId: string, sprintId: string) => [...sprintKeys.detail(wsId, sprintId), "issues"] as const,
  backlog: (wsId: string, projectId: string) => [...sprintKeys.project(wsId, projectId), "backlog"] as const,
  velocity: (wsId: string, sprintId: string) => [...sprintKeys.detail(wsId, sprintId), "velocity"] as const,
  projectVelocity: (wsId: string, projectId: string) => [...sprintKeys.project(wsId, projectId), "velocity"] as const,
  burndown: (wsId: string, sprintId: string) => [...sprintKeys.detail(wsId, sprintId), "burndown"] as const,
};

export function sprintListOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: sprintKeys.list(wsId, projectId),
    queryFn: () => api.listSprints(projectId),
    select: (data) => data.sprints,
  });
}

export function sprintDetailOptions(wsId: string, sprintId: string) {
  return queryOptions({
    queryKey: sprintKeys.detail(wsId, sprintId),
    queryFn: () => api.getSprint(sprintId),
  });
}

export function sprintIssuesOptions(wsId: string, sprintId: string) {
  return queryOptions({
    queryKey: sprintKeys.issues(wsId, sprintId),
    queryFn: () => api.listSprintIssues(sprintId),
    select: (data) => data.issues,
  });
}

export function backlogOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: sprintKeys.backlog(wsId, projectId),
    queryFn: () => api.listBacklog(projectId),
    select: (data) => data.issues,
  });
}

export function sprintVelocityOptions(wsId: string, sprintId: string) {
  return queryOptions({
    queryKey: sprintKeys.velocity(wsId, sprintId),
    queryFn: () => api.getSprintVelocity(sprintId),
    select: (data) => data.velocity,
  });
}

export function projectVelocityOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: sprintKeys.projectVelocity(wsId, projectId),
    queryFn: () => api.getProjectVelocity(projectId),
    select: (data) => data.velocity,
  });
}

export function sprintBurndownOptions(wsId: string, sprintId: string) {
  return queryOptions({
    queryKey: sprintKeys.burndown(wsId, sprintId),
    queryFn: () => api.getSprintBurndown(sprintId),
    select: (data) => data.issues,
  });
}
