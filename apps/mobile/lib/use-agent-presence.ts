/**
 * Mobile-owned mirror of packages/core/agents/use-agent-presence.ts.
 *
 * Why a mirror, not an import: that file is React/Query-runtime code (not on
 * the mobile sharing whitelist) and reads workspace-state from `useWorkspaceId`
 * (web context). Mobile reads `useWorkspaceStore` directly and owns its own
 * QueryClient + query keys.
 *
 * Pure derivation is shared though — `deriveAgentPresenceDetail` /
 * `buildPresenceMap` are imported from `@multica/core/agents` (pure functions,
 * on the whitelist). Don't re-implement the three-state logic locally; the
 * web/desktop/mobile semantics MUST agree (apps/mobile/CLAUDE.md
 * "State enums and transitions must agree").
 *
 * Two differences from the web hook:
 *   1. AppState gate on the 30s tick — iOS freezes JS timers in the
 *      background, and a stale `Date.now()` baseline would leave the
 *      `unstable → offline` transition stuck until the next unrelated
 *      refetch. We clearInterval on background and force a recompute
 *      (`setTick(t => t + 1)`) the instant the app comes back active.
 *   2. No `useWorkspaceId` Context — accept `wsId` as a param so the hook
 *      works outside `WorkspaceIdProvider` (e.g. avatars rendered before
 *      workspace is resolved on cold start).
 */
import { useEffect, useMemo, useState } from "react";
import { AppState, type AppStateStatus } from "react-native";
import { useQuery } from "@tanstack/react-query";
import {
  buildPresenceMap,
  deriveAgentPresenceDetail,
  type AgentPresenceDetail,
} from "@multica/core/agents";
import { agentListOptions } from "@/data/queries/agents";
import { runtimeListOptions } from "@/data/queries/runtimes";
import { agentTaskSnapshotOptions } from "@/data/queries/agent-task-snapshot";

const PRESENCE_TICK_MS = 30_000;

// AppState-gated wall-clock tick. Foreground: 30s interval. Background/
// inactive: timer torn down. Return to foreground: force one immediate
// recompute (not waiting on the next tick) so the user never sees a stale
// dot the moment they reopen the app.
function usePresenceTick(): number {
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let interval: ReturnType<typeof setInterval> | null = null;

    const start = () => {
      if (interval) return;
      interval = setInterval(() => setTick((t) => t + 1), PRESENCE_TICK_MS);
    };
    const stop = () => {
      if (!interval) return;
      clearInterval(interval);
      interval = null;
    };

    // Initial state: AppState.currentState is "active" on app launch; on
    // a launched-into-background cold start it's "background" and we
    // correctly defer the interval until the user opens the app.
    if (AppState.currentState === "active") start();

    const sub = AppState.addEventListener("change", (next: AppStateStatus) => {
      if (next === "active") {
        // Force one recompute before scheduling the next interval — the
        // wall clock has moved while we were backgrounded, so e.g. a
        // runtime that was "unstable" 4 min ago is now "offline" and we
        // want that visible on the very first frame after resume.
        setTick((t) => t + 1);
        start();
      } else {
        stop();
      }
    });

    return () => {
      stop();
      sub.remove();
    };
  }, []);

  return tick;
}

/**
 * Workspace-wide presence map keyed by agent.id. Use this for any list / card
 * surface that needs presence for more than one agent in the same render
 * pass — Map.get(id) reads are O(1) and avoid the forest of redundant
 * subscriptions you'd get from N copies of useAgentPresence.
 */
export function useWorkspacePresenceMap(wsId: string | null | undefined): {
  byAgent: Map<string, AgentPresenceDetail>;
  loading: boolean;
} {
  const { data: agents, isPending: agentsPending, isError: agentsErr } =
    useQuery({ ...agentListOptions(wsId ?? null), enabled: !!wsId });
  const { data: runtimes, isPending: runtimesPending, isError: runtimesErr } =
    useQuery({ ...runtimeListOptions(wsId ?? null), enabled: !!wsId });
  const {
    data: snapshot,
    isPending: snapshotPending,
    isError: snapshotErr,
  } = useQuery({
    ...agentTaskSnapshotOptions(wsId ?? null),
    enabled: !!wsId,
  });
  const tick = usePresenceTick();

  const byAgent = useMemo(() => {
    // Treat errored queries as empty — a 404 / 5xx on the snapshot endpoint
    // (e.g. backend hasn't deployed the new route yet on the user's server)
    // shouldn't blank out every row's dot. The runtime-driven availability
    // dimension can still render.
    const safeAgents = agents ?? (agentsErr ? [] : null);
    const safeRuntimes = runtimes ?? (runtimesErr ? [] : null);
    const safeSnapshot = snapshot ?? (snapshotErr ? [] : null);
    if (!safeAgents || !safeRuntimes || !safeSnapshot) {
      return new Map<string, AgentPresenceDetail>();
    }
    return buildPresenceMap({
      agents: safeAgents,
      runtimes: safeRuntimes,
      snapshot: safeSnapshot,
      now: Date.now(),
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- tick is intentional
  }, [agents, runtimes, snapshot, agentsErr, runtimesErr, snapshotErr, tick]);

  return {
    byAgent,
    loading:
      (agentsPending && !agentsErr) ||
      (runtimesPending && !runtimesErr) ||
      (snapshotPending && !snapshotErr),
  };
}

// Fallback for agents we know about by id but can't resolve in the active
// list (deleted / archived / referenced by stale data). Rendering "offline +
// idle" beats spinning a skeleton forever — and matches what web does for
// the same condition.
const MISSING_AGENT_DETAIL: AgentPresenceDetail = {
  availability: "offline",
  workload: "idle",
  runningCount: 0,
  queuedCount: 0,
  capacity: 0,
};

/**
 * Single-agent presence detail. Returns `"loading"` only while the underlying
 * queries are genuinely pending — a missing runtime is a legitimate state
 * (offline) and resolves into a non-loading detail.
 */
export function useAgentPresence(
  wsId: string | null | undefined,
  agentId: string | null | undefined,
): AgentPresenceDetail | "loading" {
  const { data: agents, isError: agentsErr } = useQuery({
    ...agentListOptions(wsId ?? null),
    enabled: !!wsId,
  });
  const { data: runtimes, isError: runtimesErr } = useQuery({
    ...runtimeListOptions(wsId ?? null),
    enabled: !!wsId,
  });
  const { data: snapshot, isError: snapshotErr } = useQuery({
    ...agentTaskSnapshotOptions(wsId ?? null),
    enabled: !!wsId,
  });
  const tick = usePresenceTick();

  return useMemo<AgentPresenceDetail | "loading">(() => {
    if (!wsId || !agentId) return "loading";

    const safeAgents = agents ?? (agentsErr ? [] : null);
    const safeRuntimes = runtimes ?? (runtimesErr ? [] : null);
    const safeSnapshot = snapshot ?? (snapshotErr ? [] : null);
    if (!safeAgents || !safeRuntimes || !safeSnapshot) return "loading";

    const agent = safeAgents.find((a) => a.id === agentId);
    if (!agent) return MISSING_AGENT_DETAIL;
    const runtime =
      safeRuntimes.find((r) => r.id === agent.runtime_id) ?? null;

    const tasks = safeSnapshot.filter((t) => t.agent_id === agentId);
    return deriveAgentPresenceDetail({
      agent,
      runtime,
      tasks,
      now: Date.now(),
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- tick is intentional
  }, [wsId, agentId, agents, runtimes, snapshot, agentsErr, runtimesErr, snapshotErr, tick]);
}
