import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CoreProvider } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import { workspaceKeys, workspaceListOptions } from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import { useHasOnboarded } from "@multica/core/paths";
import { ThemeProvider } from "@multica/ui/components/common/theme-provider";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { Toaster } from "sonner";
import { DesktopLoginPage } from "./pages/login";
import { DesktopShell } from "./components/desktop-layout";
import { PageviewTracker } from "./components/pageview-tracker";
import { UpdateNotification } from "./components/update-notification";
import { useTabStore } from "./stores/tab-store";
import { useWindowOverlayStore } from "./stores/window-overlay-store";


function AppContent() {
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const qc = useQueryClient();
  // Deep-link login runs loginWithToken → syncToken → listWorkspaces →
  // setQueryData sequentially. loginWithToken sets user+isLoading=false
  // as soon as getMe resolves, which would cause DesktopShell to mount
  // before the workspace list is hydrated and briefly see `!workspace`.
  // This local flag keeps the loading screen up until the whole chain
  // finishes, so IndexRedirect gets a definitive workspace state on
  // first render.
  const [bootstrapping, setBootstrapping] = useState(false);

  // Tell the main process which backend URL we talk to, so daemon-manager
  // can pick the matching CLI profile (server_url from ~/.multica config).
  useEffect(() => {
    window.daemonAPI.setTargetApiUrl(DAEMON_TARGET_API_URL);
  }, []);

  // Listen for invite IDs delivered via deep link (multica://invite/<id>).
  // We open the overlay regardless of login state — if the user isn't logged
  // in, InvitePage's queries will fail and render the "not found" state,
  // which is acceptable; the expected pre-flight happens in the web app
  // (login + next=/invite/... dance) before the deep link is ever dispatched.
  useEffect(() => {
    return window.desktopAPI.onInviteOpen((invitationId) => {
      useWindowOverlayStore.getState().open({ type: "invite", invitationId });
    });
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

  // When a user who started the session with zero workspaces creates their
  // first one, restart the daemon so it picks up the new workspace
  // immediately (otherwise workspaceSyncLoop's next 30s tick would be the
  // earliest pickup point). Specifically scoped to "started empty" because
  // account switches (user A logout → user B login) should not trigger a
  // daemon restart here — daemon-manager already restarts on user change
  // via syncToken.
  const { data: workspaces = [], isFetched: workspaceListFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });
  const wsCount = workspaces.length;
  const hasOnboarded = useHasOnboarded();

  // Onboarding and zero-workspace both resolve to an overlay, but
  // onboarding wins: a user who hasn't completed it gets the onboarding
  // overlay regardless of how many workspaces already exist.
  useEffect(() => {
    if (!user || !workspaceListFetched) return;
    const { overlay, open } = useWindowOverlayStore.getState();
    if (overlay) return;
    if (!hasOnboarded) {
      open({ type: "onboarding" });
      return;
    }
    if (wsCount === 0) {
      open({ type: "new-workspace" });
    }
  }, [user, workspaceListFetched, wsCount, workspaces, hasOnboarded]);

  // Validate persisted tab state against the current user's workspace list,
  // and pick an active workspace if none is set. Runs in useLayoutEffect
  // (synchronously after render, before paint) rather than the render
  // phase — the original render-phase pattern triggered React's
  // "Cannot update a component while rendering a different component"
  // warning because `switchWorkspace` is a Zustand setState that the
  // TabBar is subscribed to. useLayoutEffect flushes both renders before
  // the user sees anything, so there's no visible flicker.
  //
  // Gate on `workspaceListFetched`: useQuery defaults `data` to `[]` before
  // the first fetch, so without this guard we'd run validation against an
  // empty slug set, wipe the persisted `activeWorkspaceSlug`, then fall
  // back to `workspaces[0]` once the real list arrives — losing the user's
  // last-opened workspace on every app start.
  useLayoutEffect(() => {
    if (!workspaceListFetched) return;
    const validSlugs = new Set(workspaces.map((w) => w.slug));
    useTabStore.getState().validateWorkspaceSlugs(validSlugs);
    const { activeWorkspaceSlug, switchWorkspace } = useTabStore.getState();
    if (!activeWorkspaceSlug && workspaces.length > 0) {
      switchWorkspace(workspaces[0].slug);
    }
  }, [workspaces, workspaceListFetched]);

  // null = undecided (pre-login or list hasn't settled yet)
  // true  = session started with zero workspaces; next transition to >=1 triggers restart
  // false = session started with >=1 workspace, OR we've already restarted; skip
  const sessionStartedEmptyRef = useRef<boolean | null>(null);
  useEffect(() => {
    if (!user) {
      sessionStartedEmptyRef.current = null;
      return;
    }
    if (!workspaceListFetched) return;
    if (sessionStartedEmptyRef.current === null) {
      sessionStartedEmptyRef.current = wsCount === 0;
      return;
    }
    if (sessionStartedEmptyRef.current && wsCount >= 1) {
      void window.daemonAPI.restart();
      sessionStartedEmptyRef.current = false;
    }
  }, [user, workspaceListFetched, wsCount]);

  if (isLoading || bootstrapping) {
    return (
      <div className="flex h-screen items-center justify-center">
        <MulticaIcon className="size-6 animate-pulse" />
      </div>
    );
  }

  // Pageview tracker sits at the app root so it covers every visible
  // surface (login, overlays, tab paths) — mounting it inside DesktopShell
  // would miss the logged-out and overlay states.
  return (
    <>
      <PageviewTracker />
      {user ? <DesktopShell /> : <DesktopLoginPage />}
    </>
  );
}

// Backend the daemon should connect to — same URL the renderer talks to.
const DAEMON_TARGET_API_URL =
  import.meta.env.VITE_API_URL || "http://localhost:8080";

// On logout, wipe desktop-only in-memory state and stop the daemon so that
// a subsequent login as a different user never inherits the previous user's
// tabs, overlay, or credentials. Zustand persist only writes to localStorage;
// useLogout clears the storage key, but the live stores stay populated until
// we explicitly reset them here.
async function handleDaemonLogout() {
  useTabStore.getState().reset();
  useWindowOverlayStore.getState().close();
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
  const { version, os } = window.desktopAPI.appInfo;
  // Stable identity reference so downstream effects (WS reconnect) don't
  // tear down on every parent render.
  const identity = useMemo(
    () => ({ platform: "desktop", version, os }),
    [version, os],
  );
  return (
    <ThemeProvider>
      <CoreProvider
        apiBaseUrl={import.meta.env.VITE_API_URL || "http://localhost:8080"}
        wsUrl={import.meta.env.VITE_WS_URL || "ws://localhost:8080/ws"}
        onLogout={handleDaemonLogout}
        identity={identity}
      >
        <AppContent />
      </CoreProvider>
      <Toaster />
      <UpdateNotification />
    </ThemeProvider>
  );
}
