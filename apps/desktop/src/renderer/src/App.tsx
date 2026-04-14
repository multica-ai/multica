import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { CoreProvider } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceStore, workspaceListOptions } from "@multica/core/workspace";
import { api } from "@multica/core/api";
import { ThemeProvider } from "@multica/ui/components/common/theme-provider";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { Toaster } from "sonner";
import { DesktopLoginPage } from "./pages/login";
import { DesktopShell } from "./components/desktop-layout";
import { UpdateNotification } from "./components/update-notification";

function AppContent() {
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const [daemonRunning, setDaemonRunning] = useState(false);

  // Tell the main process which backend URL we talk to, so daemon-manager
  // can pick the matching CLI profile (server_url from ~/.multica config).
  useEffect(() => {
    window.daemonAPI.setTargetApiUrl(DAEMON_TARGET_API_URL);
  }, []);

  // Track daemon lifecycle so workspace reconciliation only runs when the
  // daemon is actually listening — avoids a startup race where the reconcile
  // effect fires before autoStart has spawned the child process.
  useEffect(() => {
    const unsub = window.daemonAPI.onStatusChange((s) => {
      setDaemonRunning(s.state === "running");
    });
    window.daemonAPI.getStatus().then((s) => {
      setDaemonRunning(s.state === "running");
    });
    return unsub;
  }, []);

  // Listen for auth token delivered via deep link (multica://auth/callback?token=...)
  useEffect(() => {
    return window.desktopAPI.onAuthToken(async (token) => {
      try {
        const loggedIn = await useAuthStore.getState().loginWithToken(token);
        await window.daemonAPI.syncToken(token, loggedIn.id);
        const wsList = await api.listWorkspaces();
        const lastWsId = localStorage.getItem("multica_workspace_id");
        useWorkspaceStore.getState().hydrateWorkspace(wsList, lastWsId);
      } catch {
        // Token invalid or expired — user stays on login page
      }
    });
  }, []);

  // Sync token and start the daemon whenever the user logs in.
  useEffect(() => {
    if (!user) return;
    const token = localStorage.getItem("multica_token");
    if (!token) return;
    const userId = user.id;
    (async () => {
      try {
        await window.daemonAPI.syncToken(token, userId);
        await window.daemonAPI.autoStart();
      } catch (err) {
        console.error("Failed to sync daemon on login", err);
      }
    })();
  }, [user]);

  // Reconcile the daemon's watched workspaces with what the user is a member
  // of. The query already hydrates on login and invalidates on create/delete
  // mutations, so this one effect covers both initial sync and incremental
  // updates. Opt-outs (unwatched denylist) are respected.
  const { data: workspaces } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });
  const lastSyncedIds = useRef<Set<string>>(new Set());
  useEffect(() => {
    if (!user || !workspaces || !daemonRunning) return;
    (async () => {
      const state = await window.daemonAPI.listWatched().catch(() => null);
      if (!state) return;
      const watchedIds = new Set(state.watched.map((w) => w.id));
      const unwatchedIds = new Set(state.unwatched);
      const currentIds = new Set(workspaces.map((w) => w.id));

      // Add: anything in the API list but not yet watched (and not opted out).
      for (const ws of workspaces) {
        if (watchedIds.has(ws.id) || unwatchedIds.has(ws.id)) continue;
        try {
          await window.daemonAPI.watchWorkspace(ws.id, ws.name);
        } catch (err) {
          console.warn("watch workspace failed", ws.id, err);
        }
      }
      // Remove: anything we previously synced that is no longer in the API
      // list (the user left or deleted it).
      for (const prevId of lastSyncedIds.current) {
        if (!currentIds.has(prevId)) {
          try {
            await window.daemonAPI.unwatchWorkspace(prevId);
          } catch (err) {
            console.warn("unwatch workspace failed", prevId, err);
          }
        }
      }
      lastSyncedIds.current = currentIds;
    })();
  }, [user, workspaces, daemonRunning]);

  if (isLoading) {
    return (
      <div className="flex h-screen items-center justify-center">
        <MulticaIcon className="size-6 animate-pulse" />
      </div>
    );
  }

  if (!user) return <DesktopLoginPage />;
  return <DesktopShell />;
}

const remoteProxy = Boolean(import.meta.env.VITE_REMOTE_API);
// Backend the daemon should connect to. In remote-proxy mode the renderer
// talks through a local Vite proxy, but the daemon needs the real upstream
// URL — which is what VITE_REMOTE_API holds. Fall back to VITE_API_URL
// (direct mode) and finally localhost:8080 (local dev default).
const DAEMON_TARGET_API_URL =
  import.meta.env.VITE_REMOTE_API ||
  import.meta.env.VITE_API_URL ||
  "http://localhost:8080";

// On logout, clear any cached PAT and stop the daemon so that a subsequent
// login as a different user never inherits the previous user's credentials.
async function handleDaemonLogout() {
  try {
    await window.daemonAPI.clearToken();
  } catch {
    // Best-effort — clearing is followed by stop which also hardens state.
  }
  try {
    await window.daemonAPI.stop();
  } catch {
    // Daemon may already be stopped.
  }
}

export default function App() {
  return (
    <ThemeProvider>
      <CoreProvider
        apiBaseUrl={remoteProxy ? "" : (import.meta.env.VITE_API_URL || "http://localhost:8080")}
        wsUrl={remoteProxy ? "ws://localhost:5173/ws" : (import.meta.env.VITE_WS_URL || "ws://localhost:8080/ws")}
        onLogout={handleDaemonLogout}
      >
        <AppContent />
      </CoreProvider>
      <Toaster />
      <UpdateNotification />
    </ThemeProvider>
  );
}
