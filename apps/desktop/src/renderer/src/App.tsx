import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CoreProvider } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import { workspaceKeys, workspaceListOptions } from "@multica/core/workspace/queries";
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
  const qc = useQueryClient();
  // Deep-link login runs loginWithToken → syncToken → listWorkspaces →
  // setQueryData sequentially. loginWithToken sets user+isLoading=false
  // as soon as getMe resolves, which would cause DesktopShell to mount
  // before the workspace list is hydrated and briefly see `!workspace`.
  // This local flag keeps the loading screen up until the whole chain
  // finishes, so the shell's "needs onboarding?" check gets a definitive
  // workspace state on first render.
  const [bootstrapping, setBootstrapping] = useState(false);

  // Tell the main process which backend URL we talk to, so daemon-manager
  // can pick the matching CLI profile (server_url from ~/.multica config).
  useEffect(() => {
    window.daemonAPI.setTargetApiUrl(DAEMON_TARGET_API_URL);
  }, []);

  // Listen for auth token delivered via deep link (multica://auth/callback?token=...).
  // daemonAPI.syncToken is handled separately by the [user] effect below, which
  // fires whenever a user logs in (deep link, session restore, account switch).
  useEffect(() => {
    return window.desktopAPI.onAuthToken(async (token) => {
      setBootstrapping(true);
      try {
        await useAuthStore.getState().loginWithToken(token);
        // Seed React Query cache with the workspace list so the index-route
        // redirect (routes.tsx `IndexRedirect`) can resolve the initial
        // destination without a second fetch. Workspace side-effects
        // (setCurrentWorkspace, persist namespace) are synced later by
        // WorkspaceRouteLayout when the URL resolves.
        const wsList = await api.listWorkspaces();
        qc.setQueryData(workspaceKeys.list(), wsList);
      } catch {
        // Token invalid or expired — user stays on login page
      } finally {
        setBootstrapping(false);
      }
    });
  }, [qc]);

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

  // When the user creates their first workspace, the daemon may have been
  // running in an idle state (post daemon.go bootstrap fix: zero workspaces
  // no longer aborts Run) or may have been started before the workspace
  // existed. Either way, workspaceSyncLoop's next tick is up to 30s away —
  // restart the daemon now so the new workspace is picked up immediately.
  const { data: workspaces } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });
  const wsCount = workspaces?.length ?? 0;
  const prevCountRef = useRef(0);
  useEffect(() => {
    if (!user) {
      prevCountRef.current = 0;
      return;
    }
    if (prevCountRef.current === 0 && wsCount >= 1) {
      void window.daemonAPI.restart();
    }
    prevCountRef.current = wsCount;
  }, [user, wsCount]);

  if (isLoading || bootstrapping) {
    return (
      <div className="flex h-screen items-center justify-center">
        <MulticaIcon className="size-6 animate-pulse" />
      </div>
    );
  }

  if (!user) return <DesktopLoginPage />;
  return <DesktopShell />;
}

// Backend the daemon should connect to — same URL the renderer talks to.
const DAEMON_TARGET_API_URL =
  import.meta.env.VITE_API_URL || "http://localhost:8080";

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
        apiBaseUrl={import.meta.env.VITE_API_URL || "http://localhost:8080"}
        wsUrl={import.meta.env.VITE_WS_URL || "ws://localhost:8080/ws"}
        onLogout={handleDaemonLogout}
      >
        <AppContent />
      </CoreProvider>
      <Toaster />
      <UpdateNotification />
    </ThemeProvider>
  );
}
