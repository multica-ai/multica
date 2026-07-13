/**
 * Presence realtime — Layer 3 of the realtime stack. Listing-level (always
 * on while the user is inside a workspace).
 *
 * Invalidates the queries that back the presence dot:
 *   - runtimeListOptions      ← daemon:register, runtime sweeper transitions
 *   - agentListOptions        ← agent:status / created / archived / restored
 *   - agentTaskSnapshotOptions← task:queued / dispatch / completed / failed /
 *                               cancelled
 *
 * Deliberately NOT subscribed (cellular-data rule, apps/mobile/CLAUDE.md):
 *   - daemon:heartbeat — every 15s × in-online runtime; web also skips it
 *     (packages/core/realtime/use-realtime-sync.ts:147). An invalidate per
 *     heartbeat would refetch agents+runtimes+snapshot 4× a minute per
 *     online runtime — guaranteed to wedge the user on cellular.
 *   - task:progress / task:message — fire many times per active task. The
 *     presence cache only needs lifecycle transitions, not per-step updates.
 *
 * Reconnect: re-invalidate runtimes + snapshot (NOT agents — agent identity
 * doesn't drift while we're offline; runtime status and task counts do).
 */
import { useQueryClient } from "@tanstack/react-query";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";

export function usePresenceRealtime() {
  const queryClient = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const runtimesKey = ["runtimes", wsId];
      const agentsKey = ["agents", wsId];
      const snapshotKey = ["agent-task-snapshot", wsId];

      const invalidateRuntimes = () =>
        queryClient.invalidateQueries({ queryKey: runtimesKey });
      const invalidateAgents = () =>
        queryClient.invalidateQueries({ queryKey: agentsKey });
      const invalidateSnapshot = () =>
        queryClient.invalidateQueries({ queryKey: snapshotKey });

      return [
        // Daemon lifecycle — register events mean a runtime came online or
        // re-registered; the sweeper's offline transitions are NOT pushed as
        // a WS event, but the next agent:status / task:* event will pull a
        // fresh runtime list anyway, and the 30s wall-clock tick masks the
        // gap. Heartbeats deliberately omitted.
        ws.on("daemon:register", invalidateRuntimes),

        // Agent identity churn — visible in pickers / chat header straight
        // away, so invalidate the cached list.
        ws.on("agent:status", invalidateAgents),
        ws.on("agent:created", invalidateAgents),
        ws.on("agent:archived", invalidateAgents),
        ws.on("agent:restored", invalidateAgents),

        // Task lifecycle — drives the workload dimension of presence and the
        // reserved-for-P1 peek sheet. progress / message intentionally absent.
        ws.on("task:queued", invalidateSnapshot),
        ws.on("task:dispatch", invalidateSnapshot),
        ws.on("task:completed", invalidateSnapshot),
        ws.on("task:failed", invalidateSnapshot),
        ws.on("task:cancelled", invalidateSnapshot),

        // We may have missed sweeper-driven runtime offline transitions
        // while disconnected — refetch runtimes + snapshot. Agents not
        // re-invalidated because agent:created / archived are rare enough
        // that the user can pull-to-refresh if needed.
        ws.onReconnect(() => {
          invalidateRuntimes();
          invalidateSnapshot();
        }),
      ];
    },
    [queryClient],
  );
}
