/**
 * Realtime provider — Layer 2 of the realtime stack.
 *
 * Owns the WSClient instance, decides when it should be connected, and
 * exposes it via context to Layer 3 hooks (use-inbox-realtime, etc).
 *
 * Mounted INSIDE the workspace layout (app/(app)/[workspace]/_layout.tsx)
 * so by the time it runs we already have:
 *   - an authenticated user (auth-store.user is non-null)
 *   - a workspace slug + id (workspace-store, validated against
 *     workspace list)
 *
 * Workspace switch → unmount/remount → fresh client with the new slug.
 *
 * Lifecycle signals:
 *   AppState 'background'  → pause socket (iOS will kill it anyway; clean
 *                            close avoids a kernel-level reset on resume)
 *   AppState 'active'      → resume socket
 *   AppState 'inactive'    → ignore (transient: app switcher / Control
 *                            Center / incoming call — tearing down here
 *                            causes spurious reconnect storms)
 *   NetInfo offline → online edge → force reconnect (don't wait for TCP
 *                                    keepalive timeout to notice the dead
 *                                    socket after wifi↔cellular handoff)
 *
 * Provider does NOT register business event handlers — those live in
 * per-feature hooks (use-inbox-realtime, etc.) so the realtime layer
 * scales without one giant 700-line file like web's use-realtime-sync.
 */
import {
  createContext,
  use,
  useEffect,
  useRef,
  useState,
} from "react";
import { AppState, type AppStateStatus } from "react-native";
import NetInfo from "@react-native-community/netinfo";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { getToken } from "@/data/secure-storage";
import { WSClient } from "./ws-client";

const API_URL = process.env.EXPO_PUBLIC_API_URL;

if (!API_URL) {
  // ApiClient already throws on this; keeping a defensive check here
  // avoids a confusing "URL constructor failed" deep in WSClient.
  throw new Error("EXPO_PUBLIC_API_URL is not set");
}

// http(s)://host → ws(s)://host/ws
const WS_URL = `${API_URL.replace(/^http/, "ws")}/ws`;

const RealtimeContext = createContext<WSClient | null>(null);

/** Subscribe to the realtime WebSocket. Returns null while disconnected
 *  (cold start, between workspace switches, signed out). Consumers must
 *  guard with `if (!ws) return` in their effect. */
export function useWSClient(): WSClient | null {
  return use(RealtimeContext);
}

export function RealtimeProvider({ children }: { children: React.ReactNode }) {
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const [client, setClient] = useState<WSClient | null>(null);

  // Track NetInfo's last known state so we only force-reconnect on the
  // offline → online EDGE, not on every change event (NetInfo fires for
  // wifi strength changes, type changes, etc).
  const lastConnectedRef = useRef<boolean | null>(null);

  useEffect(() => {
    if (!userId || !wsSlug) {
      setClient(null);
      return;
    }

    let cancelled = false;
    let ws: WSClient | null = null;
    let appStateSub: { remove: () => void } | null = null;
    let netInfoUnsub: (() => void) | null = null;

    void (async () => {
      const token = await getToken();
      if (cancelled || !token) return;

      ws = new WSClient({
        url: WS_URL,
        token,
        workspaceSlug: wsSlug,
        clientVersion: "0.1.0",
        logger: console,
      });
      ws.connect();
      setClient(ws);

      // ── AppState ────────────────────────────────────────────────
      appStateSub = AppState.addEventListener(
        "change",
        (status: AppStateStatus) => {
          if (status === "active") {
            // Foreground. The socket may have been paused (we put it
            // there on background) or it may be a zombie (iOS killed
            // it silently). Either way: resume / force-reconnect.
            ws?.resume();
            ws?.forceReconnect();
          } else if (status === "background") {
            ws?.pause();
          }
          // 'inactive' (iOS-only, transient) → ignore.
        },
      );

      // ── NetInfo ─────────────────────────────────────────────────
      lastConnectedRef.current = null;
      netInfoUnsub = NetInfo.addEventListener((state) => {
        const isConnected = state.isConnected === true;
        const previous = lastConnectedRef.current;
        lastConnectedRef.current = isConnected;
        // Edge: false → true. First event (previous === null) is
        // skipped — connect()/resume() above already handle the
        // initial state.
        if (previous === false && isConnected) {
          console.info("[realtime] netinfo: back online → forceReconnect");
          ws?.forceReconnect();
        }
      });
    })();

    return () => {
      cancelled = true;
      appStateSub?.remove();
      netInfoUnsub?.();
      ws?.disconnect();
      setClient(null);
    };
  }, [userId, wsSlug]);

  return (
    <RealtimeContext.Provider value={client}>
      {children}
    </RealtimeContext.Provider>
  );
}
