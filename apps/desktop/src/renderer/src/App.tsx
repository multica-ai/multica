import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CoreProvider } from "@multica/core/platform";
import { pickLocale, type SupportedLocale } from "@multica/core/i18n";
import { useAuthStore } from "@multica/core/auth";
import { useWelcomeStore } from "@multica/core/onboarding";
import { workspaceKeys, workspaceListOptions } from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import { useHasOnboarded } from "@multica/core/paths";
import { setCurrentWorkspace } from "@multica/core/platform";
import { ThemeProvider } from "@multica/ui/components/common/theme-provider";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { Toaster } from "@multica/ui/components/ui/sonner";
import { DesktopLoginPage } from "./pages/login";
import { DesktopShell } from "./components/desktop-layout";
import { PageviewTracker } from "./components/pageview-tracker";
import { UpdateNotification } from "./components/update-notification";
import { useTabStore } from "./stores/tab-store";
import { useWindowOverlayStore } from "./stores/window-overlay-store";
import { useDaemonIPCBridge } from "./platform/daemon-ipc-bridge";
import { createDesktopLocaleAdapter } from "./platform/i18n-adapter";
import { captureEvent } from "@multica/core/analytics";
import { RESOURCES } from "@multica/views/locales";

// BCP-47 region tags for the <html lang> attribute, mirroring
// apps/web/app/layout.tsx HTML_LANG. index.html ships a static lang="en";
// we sync it to the resolved locale at boot so screen readers announce the
// right language AND the Japanese-scoped CJK font override in globals.css
// (`html[lang|="ja"]`) can take effect.
const HTML_LANG: Record<SupportedLocale, string> = {
  en: "en",
  "zh-Hans": "zh-CN",
  ko: "ko-KR",
  ja: "ja-JP",
};


/**
 * Cmd/Ctrl+W: close the active tab. When the last real tab is closed
 * (or no tabs/workspace exist — e.g. login page), close the window.
 *
 * Mounted at the App root so every renderer state — including login,
 * loading, onboarding, and runtime-config errors — has a working Cmd+W
 * handler. Without this, states outside the tab shell would swallow the
 * shortcut and do nothing.
 */
function useCmdWCloseTab() {
  useEffect(() => {
    return window.desktopAPI.onCloseActiveTab(() => {
      const store = useTabStore.getState();
      const { activeWorkspaceSlug, byWorkspace } = store;
      if (!activeWorkspaceSlug) {
        // No workspace — nothing to close, dismiss the window.
        window.desktopAPI.closeWindow();
        return;
      }
      const group = byWorkspace[activeWorkspaceSlug];
      if (!group || group.tabs.length <= 1) {
        // Last tab (or no tabs) — close the window.
        window.desktopAPI.closeWindow();
        return;
      }
      // Multiple tabs — close the active one.
      store.closeActiveTab();
    });
  }, []);
}

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

  const runtimeConfig = window.desktopAPI.runtimeConfig.ok
    ? window.desktopAPI.runtimeConfig.config
    : null;

  // Tell the main process which backend URL we talk to, so daemon-manager
  // can pick the matching CLI profile (server_url from ~/.multica config).
  useEffect(() => {
    if (!runtimeConfig) return;
    window.daemonAPI.setTargetApiUrl(runtimeConfig.apiUrl);
  }, [runtimeConfig]);

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

  // Bridge local daemon IPC status into the runtimes cache so this user's
  // own daemon flips to offline/online sub-second instead of waiting on the
  // server's 75s sweeper. Resolves wsId from the active tab so workspace
  // switches automatically rebind the subscription.
  const activeWorkspaceSlug = useTabStore((s) => s.activeWorkspaceSlug);
  const activeWsId = activeWorkspaceSlug
    ? workspaces.find((w) => w.slug === activeWorkspaceSlug)?.id
    : undefined;
  useDaemonIPCBridge(activeWsId);

  // Pre-workspace overlay routing for desktop. Mirrors the web layout
  // hard gate via overlays (desktop has no URL bar, so we open the
  // onboarding overlay instead of router.replace):
  //   onboarded + has workspace      → no overlay, dashboard
  //   un-onboarded (any wsCount):
  //     pending invites on email     → /invitations overlay
  //     no invites                   → /onboarding overlay
  //   onboarded + no workspace       → /workspaces/new overlay
  //
  // V3 invariant: `onboarded_at != null` is the only path into the
  // dashboard. CreateWorkspace does not mark onboarded; only Step 3's
  // CompleteOnboarding (and AcceptInvitation) flip the flag. A user who
  // somehow has a workspace but no onboarded mark must be sent back to
  // /onboarding — we also clear the active workspace so the dashboard
  // doesn't render under the overlay with stale workspace context.
  useEffect(() => {
    if (!user || !workspaceListFetched) return undefined;
    const { overlay, open } = useWindowOverlayStore.getState();
    if (overlay) return undefined;
    if (hasOnboarded && wsCount > 0) return undefined;
    if (!hasOnboarded) {
      // Stale workspace context (if any) would leak X-Workspace-Slug
      // headers into onboarding-time API calls. Clear it before opening
      // the overlay.
      setCurrentWorkspace(null, null);
      // Look up pending invitations by email. Network blip is non-fatal —
      // fall through to onboarding so the user isn't stuck on a blank
      // window. The sidebar's pending-invitations dropdown will surface
      // missed invites later once they're onboarded.
      let cancelled = false;
      void api
        .listMyInvitations()
        .then((invites) => {
          if (cancelled) return;
          const { overlay: latestOverlay, open: latestOpen } =
            useWindowOverlayStore.getState();
          if (latestOverlay) return;
          if (invites.length > 0) {
            qc.setQueryData(workspaceKeys.myInvitations(), invites);
            latestOpen({ type: "invitations" });
          } else {
            latestOpen({ type: "onboarding" });
          }
        })
        .catch(() => {
          if (cancelled) return;
          const { overlay: latestOverlay, open: latestOpen } =
            useWindowOverlayStore.getState();
          if (latestOverlay) return;
          latestOpen({ type: "onboarding" });
        });
      return () => {
        cancelled = true;
      };
    }
    open({ type: "new-workspace" });
    return undefined;
  }, [user, workspaceListFetched, wsCount, workspaces, hasOnboarded, qc]);


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

function BlockingRuntimeConfigError({ message }: { message: string }) {
  return (
    <div className="flex h-screen items-center justify-center bg-background p-8 text-foreground">
      <div className="max-w-xl rounded-lg border bg-card p-6 shadow-sm">
        <h1 className="text-lg font-semibold">Desktop configuration error</h1>
        <p className="mt-3 text-sm text-muted-foreground">
          Multica Desktop could not load <code>~/.multica/desktop.json</code>. Fix or remove the file and restart the app.
        </p>
        <pre className="mt-4 whitespace-pre-wrap rounded-md bg-muted p-3 text-xs text-muted-foreground">
          {message}
        </pre>
      </div>
    </div>
  );
}

// On logout, wipe desktop-only in-memory state and stop the daemon so that
// a subsequent login as a different user never inherits the previous user's
// tabs, overlay, or credentials. Zustand persist only writes to localStorage;
// useLogout clears the storage key, but the live stores stay populated until
// we explicitly reset them here.
async function handleDaemonLogout() {
  useTabStore.getState().reset();
  useWindowOverlayStore.getState().close();
  // Drop any post-onboarding welcome signal so user B logging in next
  // doesn't inherit user A's pending modal state.
  useWelcomeStore.getState().reset();
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
  const systemLocale = window.desktopAPI.systemLocale;
  const runtimeConfigResult = window.desktopAPI.runtimeConfig;
  useCmdWCloseTab();

  // Flush a freeze/crash breadcrumb the main process parked from a previous
  // session. A true hang or process death can't report itself when it happens
  // (the renderer is blocked or gone), so the main process persists it and we
  // emit it here on the next boot. The in-thread, recoverable freeze tier is
  // handled separately by the shared watchdog in CoreProvider.
  useEffect(() => {
    const last = window.desktopAPI.getLastFreeze();
    if (!last) return;
    const crashed = last.kind === "render-process-gone";
    captureEvent(crashed ? "client_crash" : "client_unresponsive", {
      // Spread context FIRST so our explicit fields below always win — a
      // future context key (e.g. its own `source`) must not silently override.
      ...last.context,
      source: crashed ? "render-process-gone" : "main-unresponsive",
      recovered: false,
      breadcrumb_ts: last.ts,
      crashed_version: last.version,
    });
  }, []);

  // Stable identity reference so downstream effects (WS reconnect) don't
  // tear down on every parent render.
  const identity = useMemo(
    () => ({ platform: "desktop", version, os }),
    [version, os],
  );
  // Locale resolution happens once at app boot. Switching language goes
  // through window.location.reload() to avoid hydration mismatch.
  const localeAdapter = useMemo(
    () => createDesktopLocaleAdapter(systemLocale),
    [systemLocale],
  );
  const locale = useMemo(() => pickLocale(localeAdapter), [localeAdapter]);
  const resources = useMemo(
    () => ({ [locale]: RESOURCES[locale] }),
    [locale],
  );

  // Keep <html lang> in sync with the resolved locale (index.html hardcodes
  // "en"). Drives the lang-scoped Japanese CJK font override and a11y.
  // useLayoutEffect (not useEffect) so lang is committed before the first
  // paint — otherwise Japanese users would see one frame of Kanji rendered
  // with the Chinese-first fallback stack before the override kicks in.
  useLayoutEffect(() => {
    document.documentElement.lang = HTML_LANG[locale];
  }, [locale]);

  // React to OS-level language changes detected by main on focus regain.
  // Only act when the user is following the system signal (no explicit
  // Settings choice) — otherwise their preference wins. Cross-device sync
  // for the explicit-choice case is handled inside CoreProvider.
  useEffect(() => {
    return window.desktopAPI.onSystemLocaleChanged((nextSystemLocale) => {
      if (localeAdapter.getUserChoice()) return;
      const next = pickLocale({
        ...localeAdapter,
        getSystemPreferences: () =>
          nextSystemLocale ? [nextSystemLocale] : [],
      });
      if (next === locale) return;
      localeAdapter.persist(next);
      window.location.reload();
    });
  }, [localeAdapter, locale]);

  return (
    <ThemeProvider>
      {runtimeConfigResult.ok ? (
        <CoreProvider
          apiBaseUrl={runtimeConfigResult.config.apiUrl}
          wsUrl={runtimeConfigResult.config.wsUrl}
          onLogout={handleDaemonLogout}
          identity={identity}
          locale={locale}
          resources={resources}
          localeAdapter={localeAdapter}
        >
          <AppContent />
        </CoreProvider>
      ) : (
        <BlockingRuntimeConfigError message={runtimeConfigResult.error.message} />
      )}
      <Toaster />
      <UpdateNotification />
    </ThemeProvider>
  );
}
