import type { Issue, ListIssuesParams } from "@multica/core/types";
import { api } from "@/data/api";
import { assertCurrentSession, currentSessionEpoch } from "@/data/session-epoch";
import { i18n } from "@/lib/i18n/singleton";

/** Keep the flat Issue[] cache used by mutations and realtime, but drain the
 * server's 100-row pages before publishing a complete result. */
export async function fetchAllIssues(
  wsId: string | null,
  filter: Omit<ListIssuesParams, "limit" | "offset" | "sort_by" | "sort_direction">,
  signal?: AbortSignal,
): Promise<Issue[]> {
  const epoch = currentSessionEpoch();
  // Key-only query consumers must not load native persistence.
  const { useWorkspaceStore } = await import("@/data/workspace-store");
  const assertScope = () => {
    assertCurrentSession(epoch);
    if (!wsId || useWorkspaceStore.getState().currentWorkspaceId !== wsId) {
      throw new Error("Workspace changed");
    }
    if (signal?.aborted) throw new Error("Request aborted");
  };
  const incomplete = () => new Error(i18n.t("issues:errors.incomplete_list"));
  const issues = new Map<string, Issue>();
  let offset = 0;
  let total = 0;
  do {
    assertScope();
    // An immutable ordering avoids edits moving rows across page boundaries.
    const page = await api.listIssues(
      { ...filter, limit: 100, offset, sort_by: "created_at", sort_direction: "asc" },
      { signal },
    );
    assertScope();
    if (page.issues.length === 0 && offset < total) throw incomplete();
    if (page.total < offset + page.issues.length) throw incomplete();
    const previousSize = issues.size;
    for (const issue of page.issues) issues.set(issue.id, issue);
    if (page.issues.length > 0 && issues.size === previousSize) throw incomplete();
    offset += page.issues.length;
    total = page.total;
  } while (offset < total);
  if (issues.size < total) throw incomplete();
  // Preserve the list's existing manual order after traversing stable pages.
  return [...issues.values()].sort((a, b) => a.position - b.position || a.id.localeCompare(b.id));
}
