import type { Agent, MemberRole, Project } from "../types";

// Mirrors the server rule; this only decides whether delete is offered.
export function canDeleteProject(
  project: Pick<Project, "creator_type" | "creator_id">,
  userId: string | null,
  role: MemberRole | null,
  agents: readonly Pick<Agent, "id" | "owner_id">[],
): boolean {
  if (!userId || !role) return false;
  if (role === "owner" || role === "admin") return true;
  if (!project.creator_type || !project.creator_id) return false;
  if (project.creator_type === "member") return project.creator_id === userId;
  return (
    project.creator_type === "agent" &&
    agents.some(
      (agent) => agent.id === project.creator_id && agent.owner_id === userId,
    )
  );
}
