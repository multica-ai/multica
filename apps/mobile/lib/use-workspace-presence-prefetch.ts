/**
 * Warm the three queries that power agent presence (agents + runtimes +
 * agent-task-snapshot) the moment the user enters a workspace. Without this,
 * surfaces that don't otherwise touch these queries (Inbox row, Issue list)
 * flash a dotless avatar on first render while the fetch is in flight.
 *
 * Mirror of packages/core/agents/use-workspace-presence-prefetch.ts — same
 * intent, different sources (mobile-owned queries). usePresenceRealtime() and
 * the 30s tick in useAgentPresence keep these caches fresh after warm-up.
 */
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceStore } from "@/data/workspace-store";
import { agentListOptions } from "@/data/queries/agents";
import { runtimeListOptions } from "@/data/queries/runtimes";
import { agentTaskSnapshotOptions } from "@/data/queries/agent-task-snapshot";

export function useWorkspacePresencePrefetch(): void {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  useQuery({ ...agentListOptions(wsId), enabled: !!wsId });
  useQuery({ ...runtimeListOptions(wsId), enabled: !!wsId });
  useQuery({ ...agentTaskSnapshotOptions(wsId), enabled: !!wsId });
}
