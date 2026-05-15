"use client";

import { useQuery } from "@tanstack/react-query";
import { agentListOptions, squadListOptions } from "../workspace/queries";
import { runtimeListOptions } from "../runtimes/queries";
import { agentTaskSnapshotOptions } from "./queries";

// Subscribe to the queries that power agent presence and the @mention
// suggestion list so they're warm by the time any hover card / inline
// indicator / mention popup first renders. Without this warm-up, surfaces
// that don't otherwise touch the snapshot (inbox, issues, chat) flash a
// skeleton on first hover while the fetch is in flight, and the @mention
// list may show incomplete results (e.g. missing squads).
//
// useRealtimeSync (WS task / agent / daemon / squad invalidations) and the
// 30s presence tick keep these caches fresh after the initial fetch — this
// hook only collapses the cold-start window.
//
// All queries are workspace-scoped; the queryKeys include wsId so workspace
// switch automatically refetches the new workspace's data with no extra
// wiring here. The workspace-scoped layouts on both apps gate rendering on
// "workspace resolved", so callers can safely pass useWorkspaceId() — by the
// time this hook mounts, wsId is guaranteed non-empty.
export function useWorkspacePresencePrefetch(wsId: string | undefined): void {
  useQuery({ ...agentListOptions(wsId ?? ""), enabled: !!wsId });
  useQuery({ ...runtimeListOptions(wsId ?? ""), enabled: !!wsId });
  useQuery({ ...agentTaskSnapshotOptions(wsId ?? ""), enabled: !!wsId });
  useQuery({ ...squadListOptions(wsId ?? ""), enabled: !!wsId });
}
