import type { QueryClient } from "@tanstack/react-query";
import { findHomeLink, linkListOptions, workspaceLandingPath } from "@multica/core/links";
import { paths } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { getActiveTab, useTabStore } from "@/stores/tab-store";

/** Select sessions, never the router. Unavailable links must not block resume. */
export async function selectWorkspaceEntry(client: QueryClient, slug: string): Promise<void> {
  const store = useTabStore.getState();
  const existing = store.byWorkspace[slug]?.tabs.length;
  store.switchWorkspace(slug, existing ? undefined : paths.workspace(slug).start());
  // First entry is resolved by DesktopWorkspaceEntry, replacing its seed tab.
  if (!existing) return;

  const workspace = client.getQueryData<Workspace[]>(workspaceKeys.list())
    ?.find((workspace) => workspace.slug === slug);
  if (!workspace) return;
  const resumedTab = getActiveTab(useTabStore.getState());
  let timer: number | undefined;
  try {
    const links = await Promise.race([
      client.fetchQuery({ ...linkListOptions(workspace.id), staleTime: 1_000, retry: false }),
      new Promise<undefined>((resolve) => { timer = window.setTimeout(resolve, 1_500); }),
    ]);
    const current = useTabStore.getState();
    const activeTab = getActiveTab(current);
    // Do not steal focus after a subsequent switch or in-workspace navigation.
    if (!links || current.activeWorkspaceSlug !== slug || activeTab?.id !== resumedTab?.id || activeTab?.url !== resumedTab?.url) return;
    if (findHomeLink(links.filter((link) => !link.projectId))) {
      current.switchWorkspace(slug, workspaceLandingPath(slug, links));
    }
  } catch {
    // The existing tab (including query/hash) is already resumed.
  } finally {
    clearTimeout(timer);
  }
}
