/**
 * Boilerplate-killer for the Layer-3 realtime hooks.
 *
 * Every `use-X-realtime` hook used to repeat this same shape:
 *
 *   const ws = useWSClient();
 *   const wsId = useWorkspaceStore(s => s.currentWorkspaceId);
 *   useEffect(() => {
 *     if (!ws || !wsId) return;
 *     const unsubs = [ ws.on(...), ws.on(...), ws.onReconnect(...) ];
 *     return () => { for (const u of unsubs) u(); };
 *   }, [ws, wsId, ...]);
 *
 * `useWSSubscriptions(setup, deps)` collapses the lifecycle scaffolding so
 * each hook focuses on its actual subscription list. The `setup` callback
 * runs only when both ws + wsId are non-null, receives them as args, and
 * returns the array of unsubs to wire into cleanup.
 *
 * Per-record hooks pass their record id via the deps array (so it
 * re-subscribes when navigating between records). They can also access
 * other narrow-scoped values via closure from the calling component.
 */
import { useEffect } from "react";
import type { WSClient } from "@/data/realtime/ws-client";
import { useWSClient } from "@/data/realtime/realtime-provider";
import { useWorkspaceStore } from "@/data/workspace-store";

export type WSSubscriptionSetup = (
  ws: WSClient,
  wsId: string,
) => (() => void)[] | undefined;

export function useWSSubscriptions(
  setup: WSSubscriptionSetup,
  deps: readonly unknown[],
) {
  const ws = useWSClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  useEffect(() => {
    if (!ws || !wsId) return;
    const unsubs = setup(ws, wsId) ?? [];
    return () => {
      for (const u of unsubs) u();
    };
    // setup is intentionally NOT in deps — callers control re-subscription
    // via the explicit `deps` array. Putting setup in deps would re-fire
    // on every render (closures), defeating the whole point.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ws, wsId, ...deps]);
}
