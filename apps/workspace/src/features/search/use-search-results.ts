import { useState, useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import type { Issue, Project, MemberWithUser } from "@/shared/types";
import { api } from "@/shared/api";
import { useWorkspaceStore } from "@/features/workspace";
import { useProjectsQuery } from "@/features/projects/queries";

/** Static quick-action items shown regardless of query */
export interface SearchAction {
  type: "action";
  id: string;
  label: string;
  description?: string;
  action: () => void;
}

export interface SearchResults {
  issues: Issue[];
  projects: Project[];
  members: MemberWithUser[];
  isLoading: boolean;
}

const SEARCH_DEBOUNCE_MS = 200;
const ISSUES_SEARCH_LIMIT = 8;
const PROJECTS_SEARCH_LIMIT = 5;
const MEMBERS_SEARCH_LIMIT = 5;

/**
 * Aggregates search results from issues (API), projects (cached), and members
 * (cached). Debounces the query to reduce API calls.
 */
export function useSearchResults(query: string): SearchResults {
  const [debouncedQuery, setDebouncedQuery] = useState(query);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Debounce the raw query input
  useEffect(() => {
    if (timerRef.current) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => {
      setDebouncedQuery(query);
    }, SEARCH_DEBOUNCE_MS);
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [query]);

  const workspaceId = useWorkspaceStore((s) => s.workspace?.id);

  // Issue search — hits the API with debounced query
  const issuesQuery = useQuery({
    queryKey: ["search", "issues", workspaceId, debouncedQuery],
    queryFn: () =>
      api.listIssues({ search: debouncedQuery, limit: ISSUES_SEARCH_LIMIT }),
    enabled: Boolean(workspaceId) && debouncedQuery.length > 0,
    staleTime: 10_000,
  });

  // Projects — filter from cached list
  const projectsQuery = useProjectsQuery();
  const allProjects: Project[] = projectsQuery.data ?? [];
  const filteredProjects =
    debouncedQuery.length > 0
      ? allProjects
          .filter((p) =>
            p.title.toLowerCase().includes(debouncedQuery.toLowerCase()),
          )
          .slice(0, PROJECTS_SEARCH_LIMIT)
      : [];

  // Members — filter from zustand/query cache
  const allMembers: MemberWithUser[] = useWorkspaceStore((s) => s.members);
  const filteredMembers =
    debouncedQuery.length > 0
      ? allMembers
          .filter(
            (m) =>
              m.name?.toLowerCase().includes(debouncedQuery.toLowerCase()) ||
              m.email?.toLowerCase().includes(debouncedQuery.toLowerCase()),
          )
          .slice(0, MEMBERS_SEARCH_LIMIT)
      : [];

  return {
    issues: issuesQuery.data?.issues ?? [],
    projects: filteredProjects,
    members: filteredMembers,
    isLoading: issuesQuery.isFetching,
  };
}
