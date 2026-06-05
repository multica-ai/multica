/**
 * Cerebro feature-flag query + resolution for mobile.
 *
 * Mobile can't import @multica/cerebro-feature-flags (it's outside the
 * @multica/core types/pure-functions sharing whitelist — see apps/mobile/
 * CLAUDE.md), so the default value and the precedence rule are mirrored here.
 * Keep both in lockstep with their web sources:
 *   - default-on:  packages/cerebro-feature-flags/registry.ts (cerebro_comment_reminders: true)
 *   - precedence:  packages/cerebro-feature-flags/store.ts → resolveFlag()
 *
 * Flags change rarely (an owner kill-switch), so the query is cached with a
 * long staleTime rather than wired to the feature_flag:changed WS event.
 */
import { queryOptions, useQuery } from "@tanstack/react-query";
import { api } from "@/data/api";
import { useWorkspaceStore } from "@/data/workspace-store";

export const featureFlagKeys = {
  all: (wsId: string | null) => ["feature-flags", wsId] as const,
  list: (wsId: string | null) =>
    [...featureFlagKeys.all(wsId), "list"] as const,
};

export const featureFlagsOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: featureFlagKeys.list(wsId),
    queryFn: ({ signal }) => api.listFeatureFlags(wsId as string, { signal }),
    enabled: !!wsId,
    // Owner-flips are rare; avoid refetching on every comment-menu mount.
    staleTime: 5 * 60 * 1000,
  });

/**
 * Frontend defaults — the server never sends them. Only the keys mobile
 * actually reads need to live here. Source of truth is web's registry.ts.
 */
const FLAG_DEFAULTS: Record<string, boolean> = {
  cerebro_comment_reminders: true,
  // FIR-31: per-reply cost badge + accumulated session-cost chip in chat.
  cerebro_chat_message_cost: true,
};

/**
 * Pure precedence resolver — mirrors packages/cerebro-feature-flags/store.ts:
 *   1. a LOCKED workspace override wins outright,
 *   2. otherwise a personal override,
 *   3. otherwise an unlocked workspace override,
 *   4. otherwise the registry default.
 */
function resolveFlag(
  key: string,
  overrides: Record<string, boolean>,
  workspaceOverrides: Record<string, boolean>,
  locked: Record<string, boolean>,
): boolean {
  const ws = workspaceOverrides[key];
  if (locked[key] && ws !== undefined) return ws;
  return overrides[key] ?? ws ?? FLAG_DEFAULTS[key] ?? false;
}

/**
 * Whether the "Remind me" comment action is available. Default-on: while the
 * flags query is loading (or drifts to the empty fallback) the action stays
 * visible, matching the server's opt-out semantics. The server still rejects a
 * comment reminder with 403 if an owner disabled the flag after this resolved,
 * so a stale `true` degrades safely.
 */
export function useCommentRemindersEnabled(): boolean {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data } = useQuery(featureFlagsOptions(wsId));
  if (!data) return FLAG_DEFAULTS.cerebro_comment_reminders;
  return resolveFlag(
    "cerebro_comment_reminders",
    data.overrides,
    data.workspace_overrides,
    data.locked,
  );
}

/**
 * Whether chat cost (per-reply badge + accumulated session total) is shown.
 * Default-on, matching web's `cerebro_chat_message_cost`. While the flags
 * query is loading it stays visible (opt-out semantics); the cost numbers are
 * read-only so a stale `true` only ever shows a price, never mis-acts.
 */
export function useChatMessageCostEnabled(): boolean {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data } = useQuery(featureFlagsOptions(wsId));
  if (!data) return FLAG_DEFAULTS.cerebro_chat_message_cost;
  return resolveFlag(
    "cerebro_chat_message_cost",
    data.overrides,
    data.workspace_overrides,
    data.locked,
  );
}
