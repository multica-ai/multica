"use client";

import { Fragment, useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { hashKey, isCancelledError, notifyManager, useInfiniteQuery, useQueries, useQuery, useQueryClient, type UseQueryOptions } from "@tanstack/react-query";
import type { IssueTableQuerySpec, IssueTableFilters } from "@multica/core/types";
import type { TrajectoryNavigation } from "@multica/core/trajectory/web-entry";
import { buildGraphProjects, emptyTrajectory, mergeTrajectoryGraphs, type TrajectoryGraph } from "@multica/core/trajectory/graph";
import { deriveCurrentExecution, type CurrentExecutionCategory } from "@multica/core/trajectory/current-execution";
import { attachGraphLifecycle, closeGraphScope, graphIssueOptions, graphListOptions, membershipOptions, openGraphScope, isGraphScopeClosed, resetGraphWindow, type GraphScope, type GraphSubscription } from "@multica/core/trajectory/graph-web";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { ArrowDown, ArrowLeft, ChevronRight, CircleAlert, RefreshCw, Search, Target } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { issueRouteRef, useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../../../navigation";
import { useLocale, useT } from "../../../../i18n";
import { GraphTimeline } from "./graph-timeline";
import { GraphInspector } from "./graph-inspector";
import { GraphLog } from "./graph-log";
import { CURRENT_CATEGORIES, GraphCurrentBar, GraphCurrentSummary } from "./graph-current-summary";
import type { TrajectoryOrder } from "@multica/core/trajectory/visual";
import { latestLoadedRecord, loadedIssueContextIds } from "@multica/core/trajectory/record-display";
import { recordKindKey } from "./graph-record-text";
import { GraphFilters } from "./graph-filters";
import { GraphRunLocator } from "./graph-run-locator";
import { graphFilterUnreadOptions } from "@multica/core/trajectory/filter-catalog";
import "./graph-ui.css";

interface WindowReadProof { recovering: boolean; receipts: Set<string>; seenErrors: Map<string, number> }
interface GraphWindowContext { readProofs: Map<string, WindowReadProof> }

interface GraphViewProps { scope: GraphScope; query: IssueTableQuerySpec; navigation?: TrajectoryNavigation; realtime?: GraphSubscription; onNativeFallback?: () => void; ready?: boolean; children?: ReactNode }
export function GraphTrajectoryView(props: GraphViewProps) {
  return <GraphScopeView key={JSON.stringify([props.scope.accountId, props.scope.workspaceId])} {...props}/>;
}
function GraphScopeView({ scope: scopeProp, realtime, ready = true, children, ...props }: GraphViewProps) {
  const client = useQueryClient();
  const scope = useMemo(() => ({ accountId: scopeProp.accountId, workspaceId: scopeProp.workspaceId }), [scopeProp.accountId, scopeProp.workspaceId]);
  useState(() => { openGraphScope(client, scope); return null; });
  useEffect(() => { openGraphScope(client, scope); return () => closeGraphScope(client, scope); }, [client, scope]);
  useEffect(() => attachGraphLifecycle(client, scope, realtime, { manageScope: false }), [client, scope, realtime]);
  const readProofs = useRef(new Map<string, WindowReadProof>());
  const firstWindow = useRef<{ query: string; valid: boolean } | null>(null);
  const querySignature = hashKey([props.query]);
  if (ready && firstWindow.current === null) firstWindow.current = { query: querySignature, valid: true };
  if (firstWindow.current && (!ready || firstWindow.current.query !== querySignature)) firstWindow.current.valid = false;
  const selectionCleared = useRef(false);
  const allowLinkSelection = firstWindow.current?.valid ?? false;
  useEffect(() => {
    if (!allowLinkSelection && firstWindow.current && !selectionCleared.current) {
      selectionCleared.current = true; props.navigation?.select?.(null);
    }
  }, [allowLinkSelection, props.navigation]);
  return ready ? <GraphSearchWindow {...props} scope={scope} readProofs={readProofs.current} allowLinkSelection={allowLinkSelection}/> : children;
}
function GraphSearchWindow({ allowLinkSelection, ...props }: GraphViewProps & GraphWindowContext & { allowLinkSelection: boolean }) {
  const client = useQueryClient();
  const [allowSelection, setAllowSelection] = useState(allowLinkSelection);
  // A link that arrives after the window changed (an in-app link to another
  // record) targets that record again; clearing the selection never does.
  const linkKey = props.navigation?.selection ? JSON.stringify(props.navigation.selection) : null;
  const [seenLink, setSeenLink] = useState(linkKey);
  const [followLinks, setFollowLinks] = useState(false);
  if (seenLink !== linkKey) { setSeenLink(linkKey); if (linkKey) { setAllowSelection(true); setFollowLinks(true); } }
  const normalize = (value: IssueTableQuerySpec): IssueTableQuerySpec => ({ ...JSON.parse(JSON.stringify(value)), search: value.search ?? "" });
  const [query, setQuery] = useState<IssueTableQuerySpec>(() => normalize(props.query));
  const incoming = hashKey([props.query]);
  const [seedKey, setSeedKey] = useState(incoming);
  if (seedKey !== incoming) { setSeedKey(incoming); setQuery(normalize(props.query)); }
  const change = (next: IssueTableQuerySpec) => {
    resetGraphWindow(client, props.scope);
    setAllowSelection(false); props.navigation?.select?.(null); setQuery(next);
  };
  return <GraphViewWindow {...props} navigation={allowSelection && (allowLinkSelection || followLinks) ? props.navigation : props.navigation ? { ...props.navigation, selection: null } : undefined} query={query}
    onSearchChange={value => change({ ...query, search: value })} initialFilters={props.query.filters} onFiltersChange={filters => change({ ...query, filters })}/>;
}
function GraphViewWindow({ scope, query, navigation, onNativeFallback, onSearchChange, onFiltersChange, initialFilters, readProofs }: GraphViewProps & GraphWindowContext & { onSearchChange: (search: string) => void; onFiltersChange: (filters: IssueTableFilters) => void; initialFilters: IssueTableFilters }) {
  const { t } = useT("issues");
  const locale = useLocale();
  const paths = useWorkspacePaths();
  const client = useQueryClient();
  const closed = useSyncExternalStore(useCallback(listener => client.getQueryCache().subscribe(notifyManager.batchCalls(listener)), [client]), () => isGraphScopeClosed(client, scope), () => false);
  const unreadOptions = graphFilterUnreadOptions(scope);
  const cancelUnread = () => { void client.cancelQueries({ queryKey: unreadOptions.queryKey, exact: true }); client.removeQueries({ queryKey: unreadOptions.queryKey, exact: true }); };
  const host = useRef<HTMLDivElement>(null);
  const opener = useRef<HTMLElement | null>(null);
  const detailScroll = useRef<HTMLDivElement>(null);
  const scrollPositions = useRef(new Map<string, number>());
  const queryHash = hashKey([query]);
  let readProof = readProofs.get(queryHash);
  if (!readProof) { readProof = { recovering: false, receipts: new Set(), seenErrors: new Map() }; readProofs.set(queryHash, readProof); }
  const membership = useInfiniteQuery({ ...membershipOptions(scope, query), enabled: !closed });
  const issueIds = useMemo(() => [...new Set(!closed ? membership.data?.pages.flatMap(page => page.rows.map(row => row.issue.id)) ?? [] : [])].slice(0, 200), [membership.data, closed]);
  const baseListOptions = graphListOptions(scope, issueIds);
  const listOptions = baseListOptions as UseQueryOptions<TrajectoryGraph>;
  const listKey = [...baseListOptions.queryKey, query];
  const list = useQuery<TrajectoryGraph>({ ...listOptions, queryKey: listKey, staleTime: readProof.recovering ? 0 : listOptions.staleTime, enabled: !closed && issueIds.length > 0 });
  const memberHash = hashKey(membershipOptions(scope, query).queryKey);
  const listHash = issueIds.length ? hashKey(listKey) : null;
  const recoveryBlocked = useSyncExternalStore(useCallback(listener => {
    const cache = client.getQueryCache();
    const required = new Set([memberHash, ...(listHash ? [listHash] : [])]);
    const proof = readProof;
    for (const hash of required) {
      const state = cache.get(hash)?.state;
      if (state?.status === "error" && !isCancelledError(state.error) && proof.seenErrors.get(hash) !== state.errorUpdateCount) {
        proof.seenErrors.set(hash, state.errorUpdateCount); proof.recovering = true; proof.receipts.clear();
      }
    }
    if (proof.recovering && [...required].every(hash => proof.receipts.has(hash))) proof.recovering = false;
    const unsubscribe = cache.subscribe(event => {
      if (event.type !== "updated" || !required.has(event.query.queryHash)) return;
      const { action, query: updated } = event;
      if (action.type === "error" && !isCancelledError(action.error)) {
        proof.seenErrors.set(updated.queryHash, updated.state.errorUpdateCount); proof.recovering = true; proof.receipts.clear();
      } else if (proof.recovering && action.type === "fetch") {
        proof.receipts.delete(updated.queryHash);
      } else if (proof.recovering && action.type === "success" && !action.manual) {
        proof.receipts.add(updated.queryHash);
        if ([...required].every(hash => proof.receipts.has(hash))) proof.recovering = false;
      }
      listener();
    });
    listener();
    return unsubscribe;
  }, [client, memberHash, listHash, readProof]), () => readProof.recovering, () => false);
  const failed = membership.isError || list.isError || recoveryBlocked;
  const summaryAvailable = !closed && !failed && membership.isSuccess && (issueIds.length === 0 || list.isSuccess);
  const [openProjects, setOpenProjects] = useState<Set<string> | null>(null);
  const [expandedIssues, setExpandedIssues] = useState(new Set<string>());
  const [expandedRuns, setExpandedRuns] = useState(new Set<string>());
  const [order, setOrder] = useState<TrajectoryOrder>("task");
  const [focusCurrent, setFocusCurrent] = useState(true);
  const [statusFilter, setStatusFilter] = useState<CurrentExecutionCategory | null>(null);
  const [filterRequest, setFilterRequest] = useState(0);
  const [pendingLocate, setPendingLocate] = useState<string | null>(null);
  const [location, setLocation] = useState<{ projectKey: string; category: CurrentExecutionCategory } | null>(null);
  const handledLocation = useRef<typeof location>(null);
  const linkSelection = navigation?.selection ?? null;
  const linkNode = linkSelection ? `${linkSelection.kind === "activity" ? "fact" : linkSelection.kind}:${linkSelection.id}` : null;
  const linkOwner = linkSelection ? linkSelection.issueId ?? (linkSelection.kind === "issue" ? linkSelection.id : null) : null;
  const linkTab = linkSelection?.kind === "run" && linkSelection.seq !== undefined ? "log" as const : "detail" as const;
  const [selectedId, setSelectedId] = useState<string | null>(linkNode);
  const [inspectorOpen, setInspectorOpen] = useState(!!linkSelection);
  const [tab, setTab] = useState<"detail" | "log">(linkTab);
  // A cold link also opens the target's project and issue and scrolls to its row once.
  const [pendingReveal, setPendingReveal] = useState<{ id: string; scroll: boolean } | null>(linkNode ? { id: linkNode, scroll: true } : null);
  const [appliedLink, setAppliedLink] = useState(linkSelection ? JSON.stringify(linkSelection) : null);
  const [recordHistory, setRecordHistory] = useState<{ id: string; issueId: string }[]>([]);
  const [selectedOwner, setSelectedOwner] = useState<string | null>(linkOwner);
  const currentLink = linkSelection ? JSON.stringify(linkSelection) : null;
  if (appliedLink !== currentLink) {
    setAppliedLink(currentLink);
    // The URL echo of an in-page selection names the open record: nothing to do.
    if (linkNode && (linkNode !== selectedId || !inspectorOpen)) {
      setSelectedId(linkNode); setInspectorOpen(true); setSelectedOwner(linkOwner); setTab(linkTab);
      // Same page: keep the reader's scroll position, only make the row reachable.
      setPendingReveal({ id: linkNode, scroll: false });
    } else if (linkNode && linkTab === "log") setTab("log");
  }
  const [windowHash, setWindowHash] = useState(queryHash);
  const sameWindow = windowHash === queryHash;
  // Reset before React commits the new window, keeping the search DOM and
  // scope denial lifecycle stable while detaching previous detail observers.
  if (!sameWindow) {
    setWindowHash(queryHash); setSelectedId(null); setInspectorOpen(false); setSelectedOwner(null); setTab("detail");
    setExpandedIssues(new Set()); setExpandedRuns(new Set()); setOpenProjects(null); setLocation(null); setFocusCurrent(true);
    setStatusFilter(null); setPendingLocate(null); setPendingReveal(null);
    setRecordHistory([]); scrollPositions.current.clear();
  }
  const selectedIssueId = sameWindow && inspectorOpen ? selectedOwner ?? list.data?.nodes.find(item => item.id === selectedId)?.issue_id ?? null : null;
  const currentWindow = closed ? emptyTrajectory("") : list.data ?? emptyTrajectory("");
  const currentIssues = new Map(currentWindow.nodes.filter(item => item.kind === "issue").map(item => [item.issue_id, item]));
  const availableHistory = sameWindow && !closed ? recordHistory.filter(entry => currentIssues.has(entry.issueId)) : [];
  const requestedIssues = [...new Set([
    ...(sameWindow ? [...expandedIssues].filter(id => currentIssues.has(id)) : []),
    ...(selectedIssueId && currentIssues.has(selectedIssueId) ? [selectedIssueId] : []),
    ...availableHistory.map(entry => entry.issueId),
  ])];
  const details = useQueries({ queries: requestedIssues.map(id => ({ ...graphIssueOptions(scope, id), enabled: !closed })) });
  const merged = closed ? emptyTrajectory("") : mergeTrajectoryGraphs(currentWindow, details.flatMap((result, index) =>
    result.data && currentIssues.has(requestedIssues[index]!) ? [result.data] : []));
  const nodes = merged.nodes.filter(item => currentIssues.has(item.issue_id)).map(item => {
    if (item.kind !== "issue") return item;
    const current = currentIssues.get(item.issue_id)!;
    // The query graph owns current membership/status/context. The loaded
    // own-Issue projection supplements its native count without old fields.
    return { ...current, details: { ...current.details, ...(item.details.run_count !== undefined ? { run_count: item.details.run_count } : {}) } };
  });
  const present = new Set(nodes.map(item => item.id));
  const graph = { ...merged, nodes, edges: merged.edges.filter(edge => present.has(edge.from) && present.has(edge.to)),
    findings: merged.findings.filter(finding => present.has(finding.node_id)), evidence: merged.evidence.filter(item => currentIssues.has(item.issue_id)) };
  const execution = deriveCurrentExecution(summaryAvailable ? currentWindow : emptyTrajectory(""), new Set(summaryAvailable ? issueIds : []));
  const projects = buildGraphProjects(currentWindow, new Set(issueIds));
  const locatedIssues = useMemo(() => {
    if (!location || !summaryAvailable) return new Set<string>();
    const projectIds = buildGraphProjects(currentWindow, new Set(issueIds)).find(project => project.key === location.projectKey)?.issueIds ?? [];
    return new Set(deriveCurrentExecution(currentWindow, new Set(projectIds)).categories[location.category]);
  }, [location, summaryAvailable, currentWindow, issueIds]);
  const opened = openProjects ?? new Set(projects[0] ? [projects[0].key] : []);
  const node = graph.nodes.find(item => item.id === selectedId && (!selectedIssueId || item.issue_id === selectedIssueId));
  const logCandidate = node?.kind === "run" ? node : node?.run_id ? graph.nodes.find(item => item.kind === "run" && item.run_id === node.run_id && item.issue_id === node.issue_id) : undefined;
  const logRun = logCandidate?.run_id?.trim() ? logCandidate : undefined;
  useEffect(() => { if (closed) { setSelectedId(null); setInspectorOpen(false); setSelectedOwner(null); setTab("detail"); setExpandedIssues(new Set()); setExpandedRuns(new Set()); setOpenProjects(null); setLocation(null); setFocusCurrent(true); setStatusFilter(null); setPendingLocate(null); setRecordHistory([]); scrollPositions.current.clear(); } }, [closed]);
  useEffect(() => { if (detailScroll.current) detailScroll.current.scrollTop = selectedId ? scrollPositions.current.get(selectedId) ?? 0 : 0; }, [selectedId, tab]);
  useEffect(() => { if (!summaryAvailable) { setLocation(null); handledLocation.current = null; } }, [summaryAvailable]);
  useEffect(() => {
    if (!location || !locatedIssues.size || closed || handledLocation.current === location) return;
    const section = [...(host.current?.querySelectorAll<HTMLElement>("section[data-project-key]") ?? [])].find(project => project.dataset.projectKey === location.projectKey);
    const target = [...(section?.querySelectorAll<HTMLElement>("[data-node-id]") ?? [])]
      .find(row => row.dataset.nodeId?.startsWith("issue:") && locatedIssues.has(row.dataset.issueOwner ?? ""));
    const button = target?.querySelector<HTMLButtonElement>("button.trajectory-content");
    if (button) { button.scrollIntoView({ block: "nearest", behavior: "auto" }); button.focus({ preventScroll: true }); handledLocation.current = location; }
  }, [location, locatedIssues, closed]);
  useEffect(() => {
    if (!pendingLocate) return;
    const row = [...(host.current?.querySelectorAll<HTMLElement>("[data-node-id]") ?? [])].find(item => item.dataset.nodeId === pendingLocate);
    const button = row?.querySelector<HTMLButtonElement>("button.trajectory-content");
    if (!button) return;
    button.scrollIntoView({ block: "nearest", behavior: "auto" }); button.focus({ preventScroll: true }); setPendingLocate(null);
  }, [pendingLocate]);
  const [pendingScroll, setPendingScroll] = useState<string | null>(null);
  const revealNode = pendingReveal && !closed ? graph.nodes.find(item => item.id === pendingReveal.id) : undefined;
  const firstProject = projects[0]?.key;
  const revealProject = revealNode ? projects.find(project => loadedIssueContextIds(graph, project.issueIds).has(revealNode.issue_id))?.key : undefined;
  useEffect(() => {
    if (!pendingReveal || !revealNode) return;
    if (revealProject) setOpenProjects(current => {
      const base = current ?? new Set(firstProject ? [firstProject] : []);
      return base.has(revealProject) ? current : new Set([...base, revealProject]);
    });
    if (revealNode.kind !== "issue") setExpandedIssues(current => current.has(revealNode.issue_id) ? current : new Set([...current, revealNode.issue_id]));
    if (revealNode.kind !== "issue" && revealNode.kind !== "run" && revealNode.run_id) {
      const runId = revealNode.run_id;
      setExpandedRuns(current => current.has(runId) ? current : new Set([...current, runId]));
    }
    if (pendingReveal.scroll) setPendingScroll(revealNode.id);
    setPendingReveal(null);
  }, [pendingReveal, revealNode, revealProject, firstProject]);
  useEffect(() => {
    if (!pendingScroll) return;
    const row = [...(host.current?.querySelectorAll<HTMLElement>("[data-node-id]") ?? [])].find(item => item.dataset.nodeId === pendingScroll);
    if (!row) return;
    // The inspector holds focus; the row only comes into view behind it.
    row.scrollIntoView({ block: "center", behavior: "auto" }); setPendingScroll(null);
  }, [pendingScroll]);
  const toggle = (set: Set<string>, id: string) => { const next = new Set(set); if (next.has(id)) next.delete(id); else next.add(id); return next; };
  const select = (id: string | null, remember = true) => {
    if (!id) {
      const original = opener.current?.closest<HTMLElement>("[data-node-id]")?.dataset.nodeId ?? selectedId;
      setInspectorOpen(false); setSelectedId(graph.nodes.some(item => item.id === original) ? original : null); setSelectedOwner(null); setTab("detail"); setRecordHistory([]); scrollPositions.current.clear(); navigation?.select?.(null);
      return;
    }
    const target = graph.nodes.find(item => item.id === id);
    if (!target || closed) return;
    if (inspectorOpen && node && node.id !== id) {
      scrollPositions.current.set(node.id, detailScroll.current?.scrollTop ?? 0);
      if (remember) setRecordHistory([...availableHistory, { id: node.id, issueId: node.issue_id }]);
    }
    if (!inspectorOpen) {
      const active = document.activeElement;
      const activeRow = active instanceof HTMLElement ? active.closest<HTMLElement>("[data-node-id]") : null;
      const activeProject = active instanceof HTMLElement ? active.closest<HTMLElement>("[data-project-key]") : null;
      const region = activeProject && host.current?.contains(activeProject) ? activeProject : host.current;
      const row = activeRow?.dataset.nodeId === id && host.current?.contains(activeRow) ? activeRow
        : [...(region?.querySelectorAll<HTMLElement>("[data-node-id]") ?? [])].find(element => element.dataset.nodeId === id);
      const main = row?.querySelector<HTMLButtonElement>("button.trajectory-content");
      opener.current = main ?? (active instanceof HTMLElement && !active.classList.contains("trajectory-lane-hit") ? active : null);
    }
    setSelectedId(id); setInspectorOpen(true); setTab("detail");
    setSelectedOwner(target?.issue_id ?? null);
    if (!target) navigation?.select?.(null);
    else if (["issue", "run", "comment", "artifact", "platform_event"].includes(target.kind)) {
      const kind = target.kind === "artifact" ? "attachment" : target.kind === "platform_event" ? "activity" : target.kind;
      navigation?.select?.({ kind, id: target.id.slice(target.id.indexOf(":") + 1), issueId: target.issue_id });
    }
  };
  const selectedLoading = !!selectedIssueId && details[requestedIssues.indexOf(selectedIssueId)]?.isPending;
  const selectedResult = selectedIssueId ? details[requestedIssues.indexOf(selectedIssueId)] : undefined;
  const edgeMap = new Map(graph.edges.map(edge => [edge.id, edge]));
  const ownEdges = selectedResult?.data?.edges ?? [];
  const ownEdgeIds = new Set(ownEdges.map(edge => edge.id));
  const inspectorGraph = { ...graph, edges: [...ownEdges.flatMap(edge => edgeMap.has(edge.id) ? [edgeMap.get(edge.id)!] : []), ...graph.edges.filter(edge => !ownEdgeIds.has(edge.id))] };
  const backHistory = availableHistory.filter(entry => graph.nodes.some(item => item.id === entry.id && item.issue_id === entry.issueId));
  const goBack = () => {
    const previous = backHistory.at(-1);
    if (previous && !closed) { setRecordHistory(backHistory.slice(0, -1)); select(previous.id, false); }
  };
  const locateRun = (id: string, projectKey: string) => {
    const run = graph.nodes.find(item => item.id === id && item.kind === "run" && expandedIssues.has(item.issue_id));
    if (!run || closed) return;
    const section = [...(host.current?.querySelectorAll<HTMLElement>("section[data-project-key]") ?? [])].find(item => item.dataset.projectKey === projectKey);
    const button = [...(section?.querySelectorAll<HTMLElement>("[data-node-id]") ?? [])]
      .find(row => row.dataset.nodeId === id)?.querySelector<HTMLButtonElement>("button.trajectory-content");
    button?.scrollIntoView({ block: "nearest", behavior: "auto" }); button?.focus({ preventScroll: true });
  };
  const dialogTitle = node?.kind === "issue" ? node.title : node
    ? t($ => $.trajectory_detail[recordKindKey(node.kind)]) + " · " + ((node.kind === "run" && node.run_id?.trim() ? node.run_id : undefined) ?? node.id.split(":").slice(1).join(":")).slice(-8)
    : t($ => $.trajectory.loading);
  const refresh = async () => {
    openGraphScope(client, scope);
    await membership.refetch();
    if (!isGraphScopeClosed(client, scope)) await Promise.all([...(issueIds.length > 0 ? [list.refetch()] : []), ...details.map(detail => detail.refetch())]);
  };
  const error = membership.error ?? list.error;
  const inaccessible = error && "status" in error && [401, 403, 404].includes(Number(error.status));
  const locate = (key: string, category: CurrentExecutionCategory) => {
    if (!summaryAvailable) return;
    setOpenProjects(new Set([...opened, key])); setLocation({ projectKey: key, category });
  };
  const statusIssues = statusFilter && summaryAvailable ? new Set(execution.categories[statusFilter]) : null;
  const visibleProjects = projects.map(project => ({ ...project, issueIds: statusIssues ? project.issueIds.filter(id => statusIssues.has(id)) : project.issueIds }))
    .filter(project => !statusIssues || project.issueIds.length > 0);
  const projectTitle = (project: { key: string; title: string }) => project.key === "none" ? t($ => $.trajectory_graph.no_project) : project.key === "unknown" ? t($ => $.trajectory_graph.unknown_project) : project.title;
  const shownIssues = new Set(visibleProjects.filter(project => opened.has(project.key)).flatMap(project => [...loadedIssueContextIds(graph, project.issueIds)]));
  const latest = latestLoadedRecord(graph.nodes.filter(item => shownIssues.has(item.issue_id) && (item.kind === "issue" || item.kind === "run" && expandedIssues.has(item.issue_id))));
  const locateLatest = () => {
    if (!latest || closed) return;
    setPendingLocate(latest.id);
  };
  // The window is cut when the list read itself was; one expanded task's own
  // cut is reported on that task, where narrowing the filters would not help.
  const limitReached = !closed && (currentWindow.coverage.truncated || membership.hasNextPage && issueIds.length >= 200);
  const truncatedIssues = new Set(closed ? [] : requestedIssues.filter((_id, index) => details[index]?.data?.coverage.truncated));
  const loadedRuns = graph.nodes.filter(item => item.kind === "run").length;
  const scopeLabel = query.scope.kind === "project" && projects.length === 1 ? projectTitle(projects[0]!) : t($ => $.trajectory_graph.scope_all);
  const updated = list.dataUpdatedAt || membership.dataUpdatedAt;
  const updatedLabel = !updated ? null : Date.now() - updated < 60_000 ? t($ => $.trajectory_graph.updated_just_now)
    : t($ => $.trajectory_graph.updated_at, { time: new Date(updated).toLocaleTimeString(locale, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" }) });
  const renderProject = (project: typeof visibleProjects[number]) => {
    const title = projectTitle(project);
    const projectExecution = deriveCurrentExecution(currentWindow, new Set(project.issueIds));
    const isOpen = opened.has(project.key);
    const header = <Button variant="ghost" size="sm" className="trajectory-project-header" aria-label={title} aria-expanded={isOpen} onClick={() => setOpenProjects(toggle(opened, project.key))}>
      <ChevronRight aria-hidden="true" className={cn("transition-transform duration-200 ease-[cubic-bezier(0.23,1,0.32,1)] motion-reduce:transition-none", isOpen && "rotate-90")}/>
      <span className="trajectory-project-title">{title}</span>
      <span className="trajectory-project-count">{summaryAvailable ? t($ => $.trajectory_graph.project_count, { count: project.issueIds.length }) : t($ => $.trajectory_graph.current_unavailable)}</span>
    </Button>;
    if (!isOpen) return <div key={project.key} className="trajectory-collapsed-row" data-project-key={project.key}>
      {header}
      {summaryAvailable ? <GraphCurrentBar execution={projectExecution} total={project.issueIds.length}/> : <span/>}
      <GraphCurrentSummary layout="columns" title={title} execution={projectExecution} available={summaryAvailable} locate={category => locate(project.key, category)}/>
    </div>;
    return <section key={project.key} className="trajectory-project" data-project-key={project.key} aria-label={title}>
      <div className="trajectory-project-head">
        {header}
        <GraphCurrentSummary title={title} execution={projectExecution} available={summaryAvailable} locate={category => locate(project.key, category)}/>
      </div>
      <div className="trajectory-project-body">
        {[...loadedIssueContextIds(graph, project.issueIds)].filter(id => expandedIssues.has(id) && details[requestedIssues.indexOf(id)]?.isSuccess)
          .map(id => <GraphRunLocator key={id} graph={graph} issueId={id} locate={nodeId => locateRun(nodeId, project.key)}/>)}
        <GraphTimeline graph={graph} projectKey={project.key} issueIds={project.issueIds} expandedIssues={expandedIssues} expandedRuns={expandedRuns} selectedId={selectedId} execution={execution} focusCurrent={focusCurrent && summaryAvailable} locatedIssues={location?.projectKey === project.key ? locatedIssues : undefined} order={order}
          toggleIssue={id => setExpandedIssues(toggle(expandedIssues, id))} toggleRun={id => setExpandedRuns(toggle(expandedRuns, id))} select={select}/>
        {requestedIssues.filter(id => project.issueIds.includes(id)).map(id => {
          const result = details[requestedIssues.indexOf(id)];
          if (truncatedIssues.has(id)) {
            const issue = currentIssues.get(id);
            return <p key={id} role="status" className="trajectory-note flex items-center gap-2">{t($ => $.trajectory_graph.issue_truncated)}
              <AppLink href={paths.issueDetail(issueRouteRef({ id, identifier: typeof issue?.details.issue_key === "string" ? issue.details.issue_key : null }))} className="text-primary underline">{t($ => $.trajectory_graph.open_task)}</AppLink></p>;
          }
          return result?.isPending ? <p key={id} role="status" className="trajectory-note">{t($ => $.trajectory.loading)}</p> : result?.isError ? <p key={id} role="alert" className="trajectory-note">{t($ => $.trajectory_graph.read_failed)} <Button variant="outline" size="xs" onClick={() => void result.refetch()}>{t($ => $.trajectory.retry)}</Button></p> : null;
        })}
      </div>
    </section>;
  };
  // Consecutive collapsed projects share one list; an opened project is its own card.
  const blocks: Array<{ open: boolean; projects: typeof visibleProjects }> = [];
  for (const project of visibleProjects) {
    const isOpen = opened.has(project.key);
    const last = blocks.at(-1);
    if (last && !last.open && !isOpen) last.projects.push(project);
    else blocks.push({ open: isOpen, projects: [project] });
  }
  return <div ref={host} className="trajectory-theme flex min-h-0 min-w-0 max-w-full flex-1 flex-col" data-testid="graph-trajectory-view">
    <header className="trajectory-header">
      <div className="trajectory-header-top">
        {onNativeFallback && <Button variant="outline" size="icon-sm" aria-label={t($ => $.trajectory.exit)} onClick={onNativeFallback}><ArrowLeft/></Button>}
        <div className="trajectory-heading">
          <h2>{t($ => $.trajectory.title)}</h2>
          <span role="status" aria-label={summaryAvailable ? t($ => $.trajectory_graph.loaded_count, { count: issueIds.length }) : t($ => $.trajectory_graph.current_unavailable)}>
            {scopeLabel} · <span className="trajectory-loaded">{summaryAvailable ? t($ => $.trajectory_graph.project_count, { count: issueIds.length }) : closed || failed ? t($ => $.trajectory_graph.current_unavailable) : t($ => $.trajectory.loading)}</span>
            {summaryAvailable && updatedLabel ? ` · ${updatedLabel}` : null}
          </span>
        </div>
        <div className="trajectory-header-actions">
          <Tabs value={order} onValueChange={value => { if (value === "task" || value === "time") setOrder(value); }}>
            <TabsList aria-label={t($ => $.trajectory_graph.order.label)}>
              <TabsTrigger value="task">{t($ => $.trajectory_graph.order.task)}</TabsTrigger>
              <TabsTrigger value="time">{t($ => $.trajectory_graph.order.time)}</TabsTrigger>
            </TabsList>
          </Tabs>
          <Button variant="outline" size="icon-sm" aria-label={t($ => $.trajectory_graph.refresh)} disabled={membership.isFetching || list.isFetching} onClick={() => void refresh()}><RefreshCw/></Button>
          <Button variant="outline" size="sm" disabled={!latest} onClick={locateLatest}><ArrowDown/>{t($ => $.trajectory_graph.latest_record)}</Button>
          <Button variant={focusCurrent ? "default" : "outline"} size="sm" aria-pressed={focusCurrent} onClick={() => setFocusCurrent(value => !value)}><Target/>{t($ => $.trajectory_graph.focus_current)}</Button>
        </div>
      </div>
      <div className="trajectory-header-search">
        <div className="trajectory-search">
          <Search aria-hidden="true"/>
          <Input data-readonly-preview="search" aria-label={t($ => $.trajectory_graph.search)} placeholder={t($ => $.trajectory_graph.search)} value={query.search ?? ""} onChange={event => onSearchChange(event.target.value)}/>
        </div>
        <GraphFilters filters={query.filters} initial={initialFilters} openRequest={filterRequest}
          loadUnread={() => client.fetchQuery(unreadOptions)} cancelUnread={cancelUnread} onChange={onFiltersChange} />
      </div>
      {summaryAvailable && <div role="radiogroup" aria-label={t($ => $.trajectory_graph.status_filter)} className="trajectory-chips" onKeyDown={event => {
        const step = event.key === "ArrowRight" || event.key === "ArrowDown" ? 1 : event.key === "ArrowLeft" || event.key === "ArrowUp" ? -1 : 0;
        if (!step) return;
        event.preventDefault();
        const options = [null, ...CURRENT_CATEGORIES] as const;
        const next = options[(options.indexOf(statusFilter) + step + options.length) % options.length]!;
        setStatusFilter(next);
        event.currentTarget.querySelector<HTMLElement>(`[data-chip="${next ?? "all"}"]`)?.focus();
      }}>
        {([null, ...CURRENT_CATEGORIES] as const).map(category => <Button key={category ?? "all"} role="radio" aria-checked={statusFilter === category} tabIndex={statusFilter === category ? 0 : -1} variant={statusFilter === category ? "brand" : "outline"} size="xs"
          className="trajectory-chip" data-chip={category ?? "all"} data-current-state={category ?? undefined} onClick={() => setStatusFilter(category)}>
          {category && <span className="trajectory-dot" aria-hidden="true"/>}
          {category ? t($ => $.trajectory_graph.symbols[category]) : t($ => $.trajectory_graph.status_all)}
          <span className="trajectory-chip-count">{category ? execution.categories[category].length : issueIds.length}</span>
        </Button>)}
      </div>}
    </header>
    <div className="trajectory-body">
      {!closed && (membership.isPending || issueIds.length > 0 && list.isPending) ? <p role="status" className="trajectory-note">{t($ => $.trajectory.loading)}</p> : null}
      {(closed || failed) && <p role="alert" className="trajectory-note flex items-center gap-2">{closed || inaccessible ? t($ => $.trajectory.unavailable) : t($ => $.trajectory_graph.read_failed)}<Button size="xs" variant="outline" onClick={() => void refresh()}>{t($ => $.trajectory.retry)}</Button></p>}
      {!closed && !failed && !membership.isPending && issueIds.length === 0 && <p className="trajectory-note">{t($ => $.trajectory.empty)}</p>}
      {limitReached && <div role="status" className="trajectory-banner">
        <CircleAlert aria-hidden="true"/>
        <p>{t($ => $.trajectory_graph.limit_reached)} {t($ => $.trajectory_graph.loaded_scope, { issues: issueIds.length, runs: loadedRuns })}</p>
        <Button variant="link" size="xs" onClick={() => setFilterRequest(value => value + 1)}>{t($ => $.trajectory_graph.adjust_filters)}</Button>
      </div>}
      {!closed && list.data && !graph.coverage.complete && !graph.coverage.truncated && <p role="status" className="trajectory-note">{t($ => $.trajectory.partial)}</p>}
      {!closed && list.data && execution.unknown > 0 && <p role="status" className="trajectory-note">{t($ => $.trajectory_graph.unknown_current, { count: execution.unknown })}</p>}
      {focusCurrent && !closed && !failed && !membership.isPending && (issueIds.length === 0 || list.data) && execution.focusIssueIds.size === 0 && <div role="status" className="trajectory-note trajectory-no-focus">
        <span>{t($ => $.trajectory_graph.no_focus)}</span><Button variant="link" size="xs" onClick={() => setFocusCurrent(false)}>{t($ => $.trajectory_graph.view_full)}</Button>
      </div>}
      {!closed && inspectorOpen && selectedId && !node && !selectedLoading && !membership.isPending && !list.isPending && <div role="alert" className="trajectory-note space-y-2"><p>{t($ => $.trajectory.unavailable)}</p>{onNativeFallback ? <Button variant="outline" size="sm" onClick={onNativeFallback}>{t($ => $.trajectory.exit)}</Button> : <AppLink href={paths.issues()} className="inline-block text-primary underline">{t($ => $.trajectory.exit)}</AppLink>}</div>}
      {!closed && blocks.map(block => block.open ? <Fragment key={block.projects[0]!.key}>{renderProject(block.projects[0]!)}</Fragment>
        : <section key={`collapsed:${block.projects[0]!.key}`} className="trajectory-collapsed" aria-label={t($ => $.trajectory_graph.col_project)}>
          {block.projects.map(renderProject)}
          <div className="trajectory-collapsed-columns" aria-hidden="true">
            <span>{t($ => $.trajectory_graph.col_project)}</span><span>{t($ => $.trajectory_graph.col_breakdown)}</span>
            {CURRENT_CATEGORIES.map(category => <span key={category}>{t($ => $.trajectory_graph.symbols[category])}</span>)}
          </div>
        </section>)}
      {membership.hasNextPage && issueIds.length < 200 && <Button variant="outline" size="sm" className="self-start" disabled={membership.isFetchingNextPage} onClick={() => void membership.fetchNextPage()}>{t($ => $.trajectory.more)}</Button>}
    </div>
    <Dialog open={inspectorOpen && (!!node || selectedLoading)} onOpenChange={open => { if (!open) select(null); }}>
      <DialogContent className={node?.kind === "run"
          ? "trajectory-run-dialog flex max-h-[85%] w-[calc(100%-2rem)] min-w-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-[760px]"
          : "trajectory-theme trajectory-dialog flex max-h-[85%] w-[calc(100%-2rem)] flex-col overflow-hidden sm:max-w-[760px]"} aria-describedby={undefined} finalFocus={() => opener.current?.isConnected ? opener.current : host.current?.querySelector<HTMLButtonElement>("button")}>
        <DialogHeader className={node?.kind === "run" ? "shrink-0 border-b px-4 py-3 pr-12" : "shrink-0 pr-8"}>
          {backHistory.length > 0 && <Button size="xs" variant="ghost" className="trajectory-detail-back" onClick={goBack}>{t($ => $.trajectory_detail.back)}</Button>}
          <DialogTitle className={node?.kind === "run" ? "wrap-anywhere text-title-sm font-semibold" : "break-words leading-snug"}>{dialogTitle}</DialogTitle>
        </DialogHeader>
        {node && <>
          <Tabs value={tab} onValueChange={value => { if (value === "detail" || value === "log") setTab(value); }} className={node.kind === "run" ? "min-h-0 gap-0" : "trajectory-tabs min-h-0 flex-1"}>
            <TabsList aria-label={t($ => $.trajectory.detail)} variant={node.kind === "run" ? "line" : "default"} className={node.kind === "run" ? "mx-4 shrink-0" : "shrink-0"}>
              <TabsTrigger value="detail" aria-label={t($ => $.trajectory_graph.evidence_details)}>{t($ => $.trajectory_graph.evidence_label)}</TabsTrigger>
              <TabsTrigger value="log" aria-label={t($ => $.trajectory.logs)} disabled={!logRun}>{t($ => $.trajectory_graph.log_label)}</TabsTrigger>
            </TabsList>
            <TabsContent value="detail" ref={detailScroll} className={node.kind === "run" ? "min-h-0 overflow-auto border-t px-4 py-4" : "min-h-0 overflow-auto"}>
              {selectedLoading ? <p role="status">{t($ => $.trajectory.loading)}</p>
                : selectedResult?.isError ? <p role="alert">{t($ => $.trajectory_graph.read_failed)} <Button size="sm" variant="outline" onClick={() => void selectedResult.refetch()}>{t($ => $.trajectory.retry)}</Button></p>
                  : <GraphInspector key={node.id} graph={inspectorGraph} node={node} select={select} canOpenLog={!!logRun} openLog={() => { if (logRun) setTab("log"); }}/>}
            </TabsContent>
            <TabsContent value="log" className={node.kind === "run" ? "min-h-0 overflow-auto border-t" : "min-h-0 overflow-auto"}>{tab === "log" && logRun && <GraphLog key={logRun.run_id} scope={scope} run={logRun} messageSeq={linkSelection?.kind === "run" && linkSelection.id === logRun.run_id ? linkSelection.seq : undefined}/>}</TabsContent>
          </Tabs>
        </>}
      </DialogContent>
    </Dialog>
  </div>;
}
