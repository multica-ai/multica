"use client";

import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { hashKey, notifyManager, useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueViewKeys } from "@multica/core/issue-views/queries";
import { issueViewContainerKey, useActiveIssueViewStore } from "@multica/core/issue-views/active-view-store";
import { filteredBuiltinView } from "@multica/core/issue-views/builtin-issue-views";
import type { IssueView } from "@multica/core/api/schemas";
import { createTrajectoryEntryStore, type TrajectoryNavigation } from "@multica/core/trajectory/web-entry";
import type { IssueTableQuerySpec } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useNavigation } from "../../../../navigation";
import { useT } from "../../../../i18n";
import type { IssueSurfaceController } from "../../../surface/use-issue-surface-controller";
import { TrajectoryEntry, type TrajectoryPageLoader } from "./trajectory-entry";
import { parseTrajectoryLink, trajectoryLinkKeys } from "@multica/core/trajectory/link";
import { trajectoryCapabilityOptions } from "@multica/core/trajectory/capability";

export type TrajectoryHeaderRenderer = (controller: IssueSurfaceController) => ReactNode;
const emptyQuery: IssueTableQuerySpec = { scope: { kind: "workspace" }, filters: {}, sort: { field: "created_at", direction: "asc" } };

function HeaderEntry({ controller, open, navigation, requestKey, store, urlViewId }: {
  controller: IssueSurfaceController;
  open: (controller: IssueSurfaceController) => void;
  navigation: TrajectoryNavigation;
  requestKey: string;
  urlViewId: string | null;
  store: ReturnType<typeof createTrajectoryEntryStore>;
}) {
  const { t } = useT("issues");
  const processed = useRef<string | null>(null);
  const workspaceId = useWorkspaceId();
  const scope = controller.projectId ? { scope_type: "project" as const, scope_id: controller.projectId } : { scope_type: "workspace" as const };
  const containerKey = issueViewContainerKey(workspaceId, scope);
  const activeViewId = useActiveIssueViewStore(state => state.active[containerKey] ?? null);
  const client = useQueryClient();
  const queryKey = useMemo(() => issueViewKeys.list(workspaceId, controller.projectId
    ? { scope_type: "project", scope_id: controller.projectId }
    : { scope_type: "workspace" }), [workspaceId, controller.projectId]);
  const queryHash = hashKey(queryKey);
  // Use the native query's notification queue so its controller settles first.
  const subscribe = useCallback((listener: () => void) => {
    const notify = notifyManager.batchCalls(listener);
    return client.getQueryCache().subscribe(event => {
      if (hashKey(event.query.queryKey) === queryHash) notify();
    });
  }, [client, queryHash]);
  const getSnapshot = useCallback(() => client.getQueryData<IssueView[]>(queryKey), [client, queryKey]);
  const views = useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
  const expectedViewId = urlViewId ?? activeViewId;
  const resolved = !expectedViewId || activeViewId === expectedViewId && (
    scope.scope_type === "workspace" && !!filteredBuiltinView(expectedViewId) || !!views?.some(view => view.id === expectedViewId)
  );
  const ready = resolved && !controller.isLoading && !controller.filterDependencyError;
  useEffect(() => {
    if (!navigation.requested) { processed.current = null; return; }
    if (navigation.error || !ready || processed.current === requestKey) return;
    // Capture after the native query subscribers have committed their controller.
    let cancelled = false;
    notifyManager.schedule(() => {
      if (cancelled) return;
      processed.current = requestKey;
      if (!store.getState().active) open(controller);
    });
    return () => { cancelled = true; };
  }, [controller, navigation.requested, navigation.error, open, ready, requestKey, store]);
  return <Button variant="ghost" size="sm" disabled={!ready} onClick={() => open(controller)}>{t($ => $.trajectory.enter)}</Button>;
}

/** Route-local host: native children remain mounted; only activation copies their query. */
export function TrajectoryRouteHost({ children, loadPage }: {
  children: (renderHeaderTrailing: TrajectoryHeaderRenderer) => ReactNode;
  loadPage?: TrajectoryPageLoader;
}) {
  const adapter = useNavigation();
  const [store] = useState(() => createTrajectoryEntryStore());
  const workspaceId = useWorkspaceId();
  const capability = useQuery(trajectoryCapabilityOptions(workspaceId));
  const enabled = capability.data?.enabled === true;
  const disabled = capability.isSuccess && !enabled;
  const [snapshot, setSnapshot] = useState<IssueTableQuerySpec | null>(null);
  const inProject = snapshot?.scope.kind === "project";
  const text = adapter.searchParams.toString();
  const navigation = useMemo<TrajectoryNavigation>(() => {
    const parsed = parseTrajectoryLink(new URLSearchParams(text));
    const write: NonNullable<TrajectoryNavigation["select"]> = selection => {
      const params = new URLSearchParams(text);
      trajectoryLinkKeys.forEach(key => params.delete(key));
      if (inProject) params.set("tab", "issues");
      params.set("trajectory", "1");
      if (selection) {
        params.set("target_kind", selection.kind); params.set("target_id", selection.id);
        if (selection.issueId) params.set("target_issue_id", selection.issueId);
        if (selection.seq !== undefined) params.set("seq", String(selection.seq));
      }
      // The open page already shows the selection: only the URL follows it.
      (adapter.replaceInPlace ?? adapter.replace)(`${adapter.pathname}?${params}${adapter.hash}`);
    };
    const exit = () => {
      const params = new URLSearchParams(text);
      trajectoryLinkKeys.forEach(key => params.delete(key));
      adapter.replace(`${adapter.pathname}${params.size ? `?${params}` : ""}${adapter.hash}`);
    };
    return { ...parsed, select: write, exit, dismissError: exit };
  }, [adapter, inProject, text]);
  const open = useCallback((controller: IssueSurfaceController) => {
    const query = controller.viewMode === "table"
      ? { ...controller.tableQuerySpec, search: controller.tableSearch.trim() || undefined }
      : controller.tableQuerySpec;
    setSnapshot(JSON.parse(JSON.stringify(query)));
  }, []);
  // Commit the snapshot before the existing Entry captures it on activation.
  useEffect(() => { if (snapshot) store.getState().setActive(true); }, [snapshot, store]);
  // A confirmed disabled capability leaves the view (or drops a stale link);
  // the native list stays mounted underneath with its search and filters.
  useEffect(() => {
    if (!disabled || !store.getState().active && !navigation.requested && !navigation.error) return;
    store.getState().setActive(false);
    setSnapshot(null);
    navigation.exit?.();
  }, [disabled, navigation, store]);
  const renderHeaderTrailing = useCallback<TrajectoryHeaderRenderer>(controller => enabled
    ? <HeaderEntry controller={controller} open={open} navigation={navigation} requestKey={`${adapter.pathname}?${text}`} urlViewId={adapter.searchParams.get("view")} store={store} />
    : null,
  [adapter.pathname, adapter.searchParams, enabled, navigation, open, store, text]);
  return <TrajectoryEntry store={store} query={snapshot ?? emptyQuery} navigation={enabled ? navigation : undefined} ready={enabled} loadPage={loadPage}>
    {children(renderHeaderTrailing)}
  </TrajectoryEntry>;
}
