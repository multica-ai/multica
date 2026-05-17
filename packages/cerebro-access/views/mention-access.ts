import type { QueryClient } from "@tanstack/react-query";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { projectKeys } from "@multica/core/projects/queries";
import { issueKeys, flattenIssueBuckets } from "@multica/core/issues/queries";
import type {
  Issue,
  ListIssuesCache,
  ListProjectsResponse,
  MemberWithUser,
  Project,
  ProjectMember,
} from "@multica/core/types";

/**
 * Result of evaluating "who in the @mention list lacks access to the current
 * issue's project". A non-null `restrictedUserIds` set means the picker
 * should mute those users and show a lock badge. A `null` set means render
 * the picker as usual — either the issue isn't in a restricted project,
 * or the membership data hasn't loaded yet so we can't draw a conclusion.
 */
export interface MentionAccessContext {
  restrictedUserIds: Set<string> | null;
}

const NO_ACCESS_CONTEXT: MentionAccessContext = { restrictedUserIds: null };

/**
 * Look up the synchronously-known access context for an editor's current
 * issue. Reads exclusively from the TanStack Query cache — the editor
 * factory runs outside React render, so this cannot fetch on demand.
 *
 * The caller is responsible for keeping the cache warm (see
 * `useEnsureRestrictedProjectMembers`). If members aren't cached yet, we
 * return `restrictedUserIds: null` so the picker falls back to its
 * upstream behaviour rather than incorrectly marking everyone as restricted.
 */
export function getMentionAccessContext(
  qc: QueryClient,
  wsId: string,
  currentIssueId: string | null | undefined,
): MentionAccessContext {
  if (!currentIssueId) return NO_ACCESS_CONTEXT;

  const issue = lookupIssue(qc, wsId, currentIssueId);
  const projectId = issue?.project_id ?? null;
  if (!projectId) return NO_ACCESS_CONTEXT;

  const project = lookupProject(qc, wsId, projectId);
  if (!project || project.access !== "restricted") return NO_ACCESS_CONTEXT;

  const cachedMembers = qc.getQueryData<{ members: ProjectMember[] }>([
    "projects",
    projectId,
    "members",
  ]);
  if (!cachedMembers) return NO_ACCESS_CONTEXT;

  const workspaceMembers =
    qc.getQueryData<MemberWithUser[]>(workspaceKeys.members(wsId)) ?? [];
  const adminIds = new Set(
    workspaceMembers
      .filter((m) => m.role === "owner" || m.role === "admin")
      .map((m) => m.user_id),
  );
  const allowedIds = new Set(cachedMembers.members.map((m) => m.user_id));

  const restricted = new Set<string>();
  for (const m of workspaceMembers) {
    if (adminIds.has(m.user_id)) continue;
    if (allowedIds.has(m.user_id)) continue;
    restricted.add(m.user_id);
  }
  return { restrictedUserIds: restricted };
}

/**
 * Ensure the @mention picker has the data it needs to mark restricted
 * members. Triggers a fetch of the project's member list when (and only
 * when) the project is restricted. Returns nothing — its job is to keep
 * the TanStack Query cache warm so the synchronous `getMentionAccessContext`
 * call from the editor can read it back without itself doing I/O.
 */
export function useEnsureMentionAccessData(
  projectId: string | null | undefined,
  projectAccess: Project["access"] | null | undefined,
): void {
  useQuery({
    queryKey: ["projects", projectId ?? "", "members"],
    queryFn: () => api.listProjectMembers(projectId!),
    enabled: !!projectId && projectAccess === "restricted",
    staleTime: 30_000,
  });
}

function lookupIssue(
  qc: QueryClient,
  wsId: string,
  issueId: string,
): Issue | null {
  const detail = qc.getQueryData<Issue>(issueKeys.detail(wsId, issueId));
  if (detail) return detail;
  const list = qc.getQueryData<ListIssuesCache>(issueKeys.list(wsId));
  if (!list) return null;
  return flattenIssueBuckets(list).find((i) => i.id === issueId) ?? null;
}

function lookupProject(
  qc: QueryClient,
  wsId: string,
  projectId: string,
): Project | null {
  const detail = qc.getQueryData<Project>(projectKeys.detail(wsId, projectId));
  if (detail) return detail;
  const list = qc.getQueryData<ListProjectsResponse>(projectKeys.list(wsId));
  if (!list) return null;
  return list.projects.find((p) => p.id === projectId) ?? null;
}
