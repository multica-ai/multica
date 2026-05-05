# @multica/cerebro-realtime

Cerebro-fork WebSocket handlers. Registers query-cache invalidations for cerebro-only events (artifacts, etc.) so that `packages/core/realtime/use-realtime-sync.ts` stays free of cerebro-specific imports.

The single entry point `registerCerebroHandlers(ws, queryClient)` is called once from `useRealtimeSync` inside core; it returns a teardown function that core invokes on unmount.
