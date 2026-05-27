/**
 * Mobile-owned QueryClient.
 *
 * Bridges TanStack Query's two cross-cutting managers to native signals:
 *   - focusManager ← AppState ('active' = focused)
 *   - onlineManager ← NetInfo (isConnected)
 *
 * After this wiring is in place, queries with `refetchOnWindowFocus: true`
 * (the default) refetch on foreground, and queries are automatically paused
 * while offline + replayed when the network returns. The realtime
 * WebSocket layer (data/realtime/) reads the same signals to drive socket
 * connect/disconnect, so the two stay in sync.
 *
 * Web/desktop use a different QueryClient (packages/core/query-client.ts).
 * Mobile maintains its own to keep React Native deps out of shared code.
 */
import { focusManager, onlineManager, QueryClient } from "@tanstack/react-query";
import { AppState, type AppStateStatus } from "react-native";
import NetInfo from "@react-native-community/netinfo";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 60 * 1000, // 1 minute
      gcTime: 10 * 60 * 1000, // 10 minutes
      retry: 1,
      refetchOnWindowFocus: true, // honored via focusManager bridge below
    },
    mutations: {
      retry: false,
    },
  },
});

// ── focusManager ← AppState ──────────────────────────────────────────
// Foregrounding the app counts as "focus" → triggers refetchOnWindowFocus
// for any stale queries the user is currently looking at.
focusManager.setEventListener((handleFocus) => {
  const sub = AppState.addEventListener("change", (status: AppStateStatus) => {
    handleFocus(status === "active");
  });
  return () => sub.remove();
});

// ── onlineManager ← NetInfo ──────────────────────────────────────────
// While offline, TanStack Query pauses queries; when isConnected flips
// back to true it replays paused fetches. RealtimeProvider also listens
// to NetInfo to force-reconnect the WS — both flows are driven by the
// same signal so client state and server state catch up together.
onlineManager.setEventListener((setOnline) => {
  return NetInfo.addEventListener((state) => {
    setOnline(state.isConnected === true);
  });
});
