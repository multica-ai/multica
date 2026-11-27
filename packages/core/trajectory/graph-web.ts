import { CancelledError, QueryClient, infiniteQueryOptions, queryOptions, type QueryKey } from '@tanstack/react-query';
import { api } from '../api';
import type { IssueTableQuerySpec, IssueTableRowsResponse } from '../types';
import type { WSEventType } from '../types/events';
import { sanitizeTrajectoryGraph, type TrajectoryGraph } from './graph';
import { trajectoryCapabilityKeys } from './capability';

export interface GraphScope { accountId: string; workspaceId: string }
/** Realtime events drive freshness; polling only covers a missed event or a dropped socket. */
export const GRAPH_POLL_MS = 60_000;
/** A burst of workspace events refreshes the open graph once. Content denials still apply at once. */
export const GRAPH_EVENT_COALESCE_MS = 3_000;
export interface GraphSubscription {
  subscribe: (event: WSEventType, handler: (payload: unknown) => void) => () => void;
  onReconnect: (callback: () => void) => () => void;
}
export const graphKeys = {
  all: (scope: GraphScope) => ['trajectory-graph', scope.accountId, scope.workspaceId] as const,
  membership: (scope: GraphScope, query: IssueTableQuerySpec) => [...graphKeys.all(scope), 'membership', query] as const,
  list: (scope: GraphScope, ids: string[]) => [...graphKeys.all(scope), 'list', ids] as const,
  issue: (scope: GraphScope, id: string) => [...graphKeys.all(scope), 'issue', id] as const,
  messages: (scope: GraphScope, id: string) => [...graphKeys.all(scope), 'messages', id] as const,
};
interface Session { closed: boolean; denied: Set<string> }
const sessions = new WeakMap<QueryClient, Map<string, Session>>();
function sessionFor(client: QueryClient, scope: GraphScope): Session {
  let map = sessions.get(client);
  if (!map) { map = new Map(); sessions.set(client, map); }
  const key = JSON.stringify([scope.accountId, scope.workspaceId]);
  let state = map.get(key);
  if (!state) { state = { closed: false, denied: new Set() }; map.set(key, state); }
  return state;
}
function assertCurrent(session: Session, signal: AbortSignal) {
  if (session.closed || signal.aborted) throw new CancelledError({ silent: true, revert: false });
}
export class GraphReadError extends Error {
  constructor(public readonly status?: number) { super(status === 401 || status === 403 || status === 404 ? 'trajectory_unavailable' : 'trajectory_read_failed'); }
}
async function read<T>(client: QueryClient, scope: GraphScope, signal: AbortSignal, request: () => Promise<T>, owner?: string, currentKey?: QueryKey): Promise<T> {
  const session = sessionFor(client, scope);
  assertCurrent(session, signal);
  try {
    const result = await request();
    assertCurrent(session, signal);
    return result;
  } catch (error) {
    assertCurrent(session, signal);
    const status = error && typeof error === 'object' && 'status' in error && typeof error.status === 'number' ? error.status : undefined;
    // A 404 may mean the workspace lost access to Trajectory; the entry
    // host leaves the view once the capability confirms it.
    if (status === 404) void client.invalidateQueries({ queryKey: trajectoryCapabilityKeys.detail(scope.workspaceId) });
    if (status === 401 || ((status === 403 || status === 404) && !owner)) closeGraphScope(client, scope, currentKey);
    else if ((status === 403 || status === 404) && owner) denyGraphContent(client, scope, [owner], currentKey);
    throw new GraphReadError(status);
  }
}
function sanitizeRows(rows: IssueTableRowsResponse, denied: Set<string>): IssueTableRowsResponse {
  return { ...rows, rows: rows.rows.filter(row => !denied.has(`issue:${row.issue.id}`)) };
}
export function membershipOptions(scope: GraphScope, query: IssueTableQuerySpec) {
  return infiniteQueryOptions({
    queryKey: graphKeys.membership(scope, query), initialPageParam: null as string | null,
    queryFn: async ({ client, signal, pageParam, queryKey }) => {
      const result = await read(client, scope, signal, () => api.listIssueTableRows({ query, group: { kind: 'none' }, group_key: null, parent_id: null, hierarchy: { enabled: false }, page: { limit: 100, ...(pageParam ? { cursor: pageParam } : {}) } }, { workspaceId: scope.workspaceId, signal, safeRead: true }), undefined, queryKey);
      return sanitizeRows(result, sessionFor(client, scope).denied);
    },
    getNextPageParam: page => page.next_cursor || undefined,
    refetchInterval: GRAPH_POLL_MS,
  });
}
export function graphListOptions(scope: GraphScope, issueIds: string[]) {
  return queryOptions({
    queryKey: graphKeys.list(scope, issueIds), staleTime: 10_000, refetchInterval: GRAPH_POLL_MS,
    queryFn: async ({ client, signal, queryKey }) => {
      const result = await read(client, scope, signal, () => api.listTaskTrajectory(issueIds.filter(id => !sessionFor(client, scope).denied.has(`issue:${id}`)), { workspaceId: scope.workspaceId, signal }), undefined, queryKey);
      return sanitizeTrajectoryGraph(result, sessionFor(client, scope).denied);
    },
  });
}
export function graphIssueOptions(scope: GraphScope, issueId: string) {
  return queryOptions({
    queryKey: graphKeys.issue(scope, issueId), staleTime: 10_000, refetchInterval: GRAPH_POLL_MS,
    queryFn: async ({ client, signal, queryKey }) => {
      const result = await read(client, scope, signal, () => api.getIssueTrajectory(issueId, { workspaceId: scope.workspaceId, signal }), `issue:${issueId}`, queryKey);
      return sanitizeTrajectoryGraph(result, sessionFor(client, scope).denied);
    },
  });
}
export function graphMessagesOptions(scope: GraphScope, runId: string) {
  return queryOptions({
    queryKey: graphKeys.messages(scope, runId),
    queryFn: ({ client, signal, queryKey }) => read(client, scope, signal, () => api.listTrajectoryTaskMessages(runId, { workspaceId: scope.workspaceId, signal }), `run:${runId}`, queryKey),
  });
}
function matchesKey(a: QueryKey, b?: QueryKey): boolean { return !!b && JSON.stringify(a) === JSON.stringify(b); }
export function denyGraphContent(client: QueryClient, scope: GraphScope, keys: string[], currentKey?: QueryKey) {
  const session = sessionFor(client, scope);
  for (const key of keys) session.denied.add(key);
  // Query cancellation reverts its state synchronously; sanitize after it so
  // a previously cached body cannot return through that revert.
  void client.cancelQueries({ queryKey: graphKeys.all(scope), predicate: query => !matchesKey(query.queryKey, currentKey) });
  for (const [, graph] of client.getQueriesData<TrajectoryGraph>({ queryKey: graphKeys.all(scope), predicate: query => ['issue', 'list'].includes(String(query.queryKey[3])) })) {
    for (const node of graph?.nodes ?? []) {
      if (node.kind !== 'run' || !node.run_id) continue;
      const commentId = node.details.trigger_comment_id;
      const evidenceId = node.details.trigger_evidence_kind === 'comment' ? node.details.trigger_evidence_ref_id : null;
      if (keys.includes(`issue:${node.issue_id}`) || keys.includes(node.id)
        || typeof commentId === 'string' && keys.includes(`comment:${commentId}`)
        || typeof evidenceId === 'string' && keys.includes(`comment:${evidenceId}`)) {
        client.removeQueries({ queryKey: graphKeys.messages(scope, node.run_id) });
      }
    }
  }
  client.setQueriesData<TrajectoryGraph>({ predicate: query => query.queryKey[0] === 'trajectory-graph' && query.queryKey[1] === scope.accountId && query.queryKey[2] === scope.workspaceId && ['issue', 'list'].includes(String(query.queryKey[3])) }, graph => graph ? sanitizeTrajectoryGraph(graph, session.denied) : graph);
  client.setQueriesData<{ pages: IssueTableRowsResponse[]; pageParams: unknown[] }>({ queryKey: [...graphKeys.all(scope), 'membership'] }, data => data ? { ...data, pages: data.pages.map(page => sanitizeRows(page, session.denied)) } : data);
  for (const key of keys) {
    if (key.startsWith('run:')) client.removeQueries({ queryKey: graphKeys.messages(scope, key.slice(4)) });
    if (key.startsWith('issue:')) {
      client.removeQueries({ queryKey: graphKeys.issue(scope, key.slice(6)), predicate: query => !matchesKey(query.queryKey, currentKey) });
      client.removeQueries({ queryKey: [...graphKeys.all(scope), 'messages'] });
    }
  }
}
export function closeGraphScope(client: QueryClient, scope: GraphScope, currentKey?: QueryKey) {
  sessionFor(client, scope).closed = true;
  void client.cancelQueries({ queryKey: graphKeys.all(scope), predicate: query => !matchesKey(query.queryKey, currentKey) });
  client.removeQueries({ queryKey: graphKeys.all(scope), predicate: query => !matchesKey(query.queryKey, currentKey) });
  if (currentKey) client.getQueryCache().find({ queryKey: currentKey, exact: true })?.setState({ data: undefined });
}
export function openGraphScope(client: QueryClient, scope: GraphScope) { sessionFor(client, scope).closed = false; }
export function isGraphScopeClosed(client: QueryClient, scope: GraphScope): boolean { return sessionFor(client, scope).closed; }
export function resetGraphWindow(client: QueryClient, scope: GraphScope) {
  void client.cancelQueries({ queryKey: graphKeys.all(scope) });
  client.removeQueries({ queryKey: graphKeys.all(scope) });
}
const graphEvents: WSEventType[] = ['issue:created', 'issue:updated', 'issue:deleted', 'comment:created', 'comment:updated', 'comment:deleted', 'issue_attachments:changed', 'activity:created', 'task:queued', 'task:running', 'task:completed', 'task:failed', 'task:cancelled', 'member:removed'];
function object(value: unknown): Record<string, unknown> { return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}; }
export function attachGraphLifecycle(client: QueryClient, scope: GraphScope, ws?: GraphSubscription, options: { manageScope?: boolean } = {}) {
  const session = sessionFor(client, scope);
  if (options.manageScope !== false) session.closed = false;
  const refresh = () => { if (!session.closed) void client.invalidateQueries({ queryKey: graphKeys.all(scope) }); };
  let pending: ReturnType<typeof setTimeout> | undefined;
  const refreshSoon = () => { if (pending === undefined) pending = setTimeout(() => { pending = undefined; refresh(); }, GRAPH_EVENT_COALESCE_MS); };
  const cleanups: (() => void)[] = [() => { if (pending !== undefined) clearTimeout(pending); }];
  if (ws) {
    cleanups.push(ws.onReconnect(refresh));
    for (const event of graphEvents) cleanups.push(ws.subscribe(event, payload => {
      if (session.closed) return;
      const value = object(payload), comment = object(value.comment), issue = object(value.issue);
      const workspace = value.workspace_id ?? comment.workspace_id ?? issue.workspace_id;
      if (typeof workspace === 'string' && workspace !== scope.workspaceId) return;
      if (event === 'member:removed' && value.user_id === scope.accountId) { closeGraphScope(client, scope); return; }
      if (event === 'comment:deleted' && typeof value.comment_id === 'string') denyGraphContent(client, scope, [`comment:${value.comment_id}`]);
      if (event === 'comment:updated' && typeof comment.id === 'string' && typeof comment.deleted_at === 'string' && comment.deleted_at) denyGraphContent(client, scope, [`comment:${comment.id}`]);
      if (event === 'issue:deleted') {
        const id = value.id ?? value.issue_id ?? issue.id;
        if (typeof id === 'string') denyGraphContent(client, scope, [`issue:${id}`]);
      }
      refreshSoon();
    }));
  }
  return () => { for (const cleanup of cleanups) cleanup(); if (options.manageScope !== false) closeGraphScope(client, scope); };
}
