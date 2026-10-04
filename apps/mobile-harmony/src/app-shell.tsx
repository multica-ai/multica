/**
 * App shell — the HarmonyOS counterpart of apps/mobile's expo-router tree:
 * auth bootstrap, the stack route registry (StackNavigator), and the
 * bottom-tab workspace shell with the More popover. Route names map 1:1 to
 * the iOS route files listed in the README roadmap; screens land here as
 * they are ported, with PlaceholderScreen holding their slot until then.
 */
import React, { useEffect, useMemo, useRef, useState } from "react";
import { AppState, StatusBar, Text, View, type AppStateStatus } from "react-native";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { InboxItem, Workspace } from "@multica/core/types";
import { api } from "@/data/api";
import { useAuthStore } from "@/data/auth-store";
import { workspaceListOptions } from "@/data/queries/workspaces";
import { inboxUnreadSummaryOptions } from "@/data/queries/inbox";
import { useWorkspaceStore } from "@/data/workspace-store";
import { maybeRenewSession } from "@/data/session-renewal";
import { RealtimeProvider } from "@/data/realtime/realtime-provider";
import { useThemeColors } from "@/lib/use-theme-colors";
import { setMarkdownNavigator } from "@/lib/markdown/markdown";
import { MoreMenu, type MoreMenuPath } from "@/components/nav/more-menu";
import { SessionActivityBoundary } from "@/components/auth/session-activity-boundary";
import { useProjectsRealtime } from "@/data/realtime/use-projects-realtime";
import { usePinsRealtime } from "@/data/realtime/use-pins-realtime";
import { useIssuesRealtime } from "@/data/realtime/use-issues-realtime";
import { useChatSessionsRealtime } from "@/data/realtime/use-chat-sessions-realtime";
import { LoginScreen } from "@/src/screens/login-screen";
import { VerifyScreen } from "@/src/screens/verify-screen";
import { WorkspacePickerScreen } from "@/src/screens/workspace-picker-screen";
import { InboxScreen } from "@/src/screens/inbox-screen";
import { InboxDetailScreen } from "@/src/screens/inbox-detail-screen";
import { MyIssuesScreen } from "@/src/screens/my-issues-screen";
import { MoreProjectsScreen } from "@/src/screens/more-projects-screen";
import { MorePinsScreen } from "@/src/screens/more-pins-screen";
import { MoreIssuesScreen } from "@/src/screens/more-issues-screen";
import { MoreAgentsScreen } from "@/src/screens/more-agents-screen";
import { ProjectDetailScreen } from "@/src/screens/project-detail-screen";
import { ProjectNewScreen } from "@/src/screens/project-new-screen";
import { SettingsScreen } from "@/src/screens/settings-screen";
import { SettingsNotificationsScreen } from "@/src/screens/settings-notifications-screen";
import { SettingsProfileScreen } from "@/src/screens/settings-profile-screen";
import { SwitchWorkspaceScreen } from "@/src/screens/switch-workspace-screen";
import { SearchScreen } from "@/src/screens/search-screen";
import { IssueDetailScreen } from "@/src/screens/issue-detail-screen";
import { IssueRunsScreen } from "@/src/screens/issue-runs-screen";
import { NewIssueScreen } from "@/src/screens/new-issue-screen";
import { ChatScreen } from "@/src/screens/chat-screen";
import { PlaceholderScreen } from "@/src/screens/placeholder-screen";
import { StackNavigator, type StackNav } from "@/src/navigation/navigator";
import { TabBar, TabView, type TabDef } from "@/src/navigation/tabs";

export type AppRoute =
  | { name: "login" }
  | { name: "verify"; email: string }
  | { name: "workspace-picker" }
  | { name: "tabs" }
  | { name: "inbox-detail"; item: InboxItem }
  | { name: "issue"; issueId: string }
  | { name: "issue-runs"; issueId: string }
  | { name: "issue-new" }
  | { name: "project"; projectId: string }
  | { name: "search" }
  | { name: "switch-workspace" }
  | { name: "settings" }
  | { name: "settings-notifications" }
  | { name: "settings-profile" }
  | { name: "more-pins" }
  | { name: "more-issues" }
  | { name: "more-projects" }
  | { name: "more-agents" }
  | { name: "project-new" };

const TAB_DEFS: TabDef[] = [
  { key: "inbox", label: "Inbox", icon: "file-tray-outline", iconFocused: "file-tray" },
  { key: "my-issues", label: "My Issues", icon: "checkbox-outline", iconFocused: "checkbox" },
  { key: "chat", label: "Chat", icon: "chatbubble-outline", iconFocused: "chatbubble" },
  { key: "more", label: "More", icon: "ellipsis-horizontal", iconFocused: "ellipsis-horizontal" },
];

/** Cross-workspace unread count for the Inbox tab badge (lib/unread-counts
 * parity: server-computed summary, newest-per-issue rule). */
function useInboxBadge(): string | null {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data } = useQuery({
    ...inboxUnreadSummaryOptions(wsId ?? null),
    select: (summary) =>
      wsId ? (summary.find((s) => s.workspace_id === wsId)?.count ?? 0) : 0,
  });
  const count = data ?? 0;
  return count > 0 ? (count > 99 ? "99+" : String(count)) : null;
}

function TabsRoot({ nav }: { nav: StackNav<AppRoute> }) {
  const c = useThemeColors();
  const [active, setActive] = useState("inbox");
  const [menuOpen, setMenuOpen] = useState(false);
  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const user = useAuthStore((s) => s.user);
  const inboxBadge = useInboxBadge();

  // Listing-level realtime caches for the workspace session — the
  // counterpart of the hooks iOS mounts in the workspace layout. The
  // inbox/my-issues/chat screens own their record-level hooks themselves.
  useProjectsRealtime();
  usePinsRealtime();
  useIssuesRealtime();
  useChatSessionsRealtime();

  const { data: workspaces } = useQuery(workspaceListOptions());
  const currentWorkspace = useMemo(
    () => (slug ? workspaces?.find((w) => w.slug === slug) : undefined),
    [workspaces, slug],
  );
  const canSwitch = (workspaces?.length ?? 0) > 1;

  // Cold start restores the persisted slug but not the in-memory id (see
  // workspace-store.ts), and every workspace-scoped query is gated on the id.
  // Resolve it from the list once the query lands — the same write the
  // workspace picker performs on selection. A slug with no matching workspace
  // (deleted workspace, removed member) clears the store so the shell's
  // session effect reroutes to the picker instead of leaving every query
  // silently disabled behind a dead slug.
  const setCurrentWorkspace = useWorkspaceStore((s) => s.setCurrentWorkspace);
  const clearWorkspace = useWorkspaceStore((s) => s.clear);
  useEffect(() => {
    if (!slug || wsId || workspaces === undefined) return;
    const match = workspaces.find((w) => w.slug === slug);
    if (match) void setCurrentWorkspace(match.id, match.slug);
    else void clearWorkspace();
  }, [slug, wsId, workspaces, setCurrentWorkspace, clearWorkspace]);

  const renderTab = (key: string) => {
    switch (key) {
      case "inbox":
        return (
          <InboxScreen
            workspaceName={currentWorkspace?.name ?? slug ?? ""}
            onOpenItem={(item) => nav.push({ name: "inbox-detail", item })}
            onOpenSearch={() => nav.push({ name: "search" })}
            onCreateIssue={() => nav.push({ name: "issue-new" })}
          />
        );
      case "my-issues":
        return (
          <MyIssuesScreen
            onOpenIssue={(issueId) => nav.push({ name: "issue", issueId })}
            onOpenSearch={() => nav.push({ name: "search" })}
            onCreateIssue={() => nav.push({ name: "issue-new" })}
          />
        );
      case "chat":
        return (
          <ChatScreen
            active={active === "chat"}
            onOpenAgents={() => nav.push({ name: "more-agents" })}
          />
        );
      default:
        return <PlaceholderScreen title="More" icon="ellipsis-horizontal" />;
    }
  };

  const openPath = (path: MoreMenuPath) => {
    setMenuOpen(false);
    if (path === "pins") nav.push({ name: "more-pins" });
    if (path === "issues") nav.push({ name: "more-issues" });
    if (path === "projects") nav.push({ name: "more-projects" });
  };

  const activePaths = useMemo(() => new Set<string>(), []);

  return (
    <RealtimeProvider>
      <View style={{ flex: 1, backgroundColor: c.background }}>
        <TabView tabs={TAB_DEFS} active={active} renderTab={renderTab} />
        {menuOpen ? (
          <MoreMenu
            user={user}
            currentWorkspace={currentWorkspace}
            canSwitch={canSwitch}
            activePaths={activePaths}
            onPressUser={() => {
              setMenuOpen(false);
              nav.push({ name: "settings" });
            }}
            onPressWorkspace={() => {
              setMenuOpen(false);
              nav.push({ name: "switch-workspace" });
            }}
            onPressPath={openPath}
            onClose={() => setMenuOpen(false)}
          />
        ) : null}
        <TabBar
          tabs={TAB_DEFS.map((t) =>
            t.key === "inbox" ? { ...t, badge: inboxBadge } : t,
          )}
          active={active}
          onChange={(key) => {
            if (key === "more") {
              setMenuOpen((open) => !open);
              return;
            }
            setMenuOpen(false);
            setActive(key);
          }}
        />
      </View>
    </RealtimeProvider>
  );
}

function renderAppRoute(
  route: AppRoute,
  nav: StackNav<AppRoute>,
): React.ReactNode {
  switch (route.name) {
    case "login":
      return <LoginScreen onCodeSent={(email) => nav.push({ name: "verify", email })} />;
    case "verify":
      return (
        <VerifyScreen
          email={route.email}
          onVerified={() => {
            // The auth-effect in AppShell reacts to the new session and
            // resets the stack; nothing to do here.
          }}
          onBack={() => nav.pop()}
        />
      );
    case "workspace-picker":
      return (
        <WorkspacePickerScreen
          onSelected={(_workspace: Workspace) => nav.reset({ name: "tabs" })}
        />
      );
    case "tabs":
      return <TabsRoot nav={nav} />;
    case "inbox-detail":
      return <InboxDetailScreen item={route.item} onBack={() => nav.pop()} />;
    case "issue":
      return (
        <IssueDetailScreen
          issueId={route.issueId}
          onOpenRuns={(runIssueId) => nav.push({ name: "issue-runs", issueId: runIssueId })}
        />
      );
    case "issue-runs":
      return <IssueRunsScreen issueId={route.issueId} />;
    case "issue-new":
      return (
        <NewIssueScreen
          onCreated={(issueId) => {
            nav.pop();
            nav.push({ name: "issue", issueId });
          }}
        />
      );
    case "project":
      return (
        <ProjectDetailScreen
          projectId={route.projectId}
          onOpenIssue={(issueId) => nav.push({ name: "issue", issueId })}
        />
      );
    case "search":
      return (
        <SearchScreen
          onClose={() => nav.pop()}
          onOpenIssue={(issueId) => nav.push({ name: "issue", issueId })}
          onOpenProject={(projectId) => nav.push({ name: "project", projectId })}
        />
      );
    case "switch-workspace":
      return (
        <SwitchWorkspaceScreen
          onBack={() => nav.pop()}
          onSwitched={() => {
            nav.popTo("tabs");
          }}
        />
      );
    case "settings":
      return (
        <SettingsScreen
          onBack={() => nav.pop()}
          onOpenProfile={() => nav.push({ name: "settings-profile" })}
          onOpenNotifications={() => nav.push({ name: "settings-notifications" })}
          onSignedOut={() => {
            // The auth effect reacts to the cleared session and resets the
            // stack to login.
          }}
        />
      );
    case "settings-notifications":
      return <SettingsNotificationsScreen onBack={() => nav.pop()} />;
    case "settings-profile":
      return <SettingsProfileScreen onBack={() => nav.pop()} />;
    case "more-pins":
      return (
        <MorePinsScreen
          onOpenIssue={(issueId) => nav.push({ name: "issue", issueId })}
          onOpenProject={(projectId) => nav.push({ name: "project", projectId })}
        />
      );
    case "more-issues":
      return (
        <MoreIssuesScreen
          onOpenIssue={(issueId) => nav.push({ name: "issue", issueId })}
        />
      );
    case "more-projects":
      return (
        <MoreProjectsScreen
          onOpenProject={(projectId) => nav.push({ name: "project", projectId })}
          onCreateProject={() => nav.push({ name: "project-new" })}
        />
      );
    case "project-new":
      return (
        <ProjectNewScreen
          onCreated={(projectId) => {
            nav.pop();
            nav.push({ name: "project", projectId });
          }}
        />
      );
    case "more-agents":
      return <MoreAgentsScreen />;
  }
}

export function AppShell() {
  const c = useThemeColors();
  const queryClient = useQueryClient();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const navRef = useRef<StackNav<AppRoute> | null>(null);
  // Idempotent guard: 401 on multiple in-flight requests would otherwise
  // logout/reset repeatedly during the same session-expire moment.
  const signingOutRef = useRef(false);

  // Auth bootstrap mirrors the iOS entry chain: restore token + slug, wire
  // the idempotent 401 teardown, then route by session state.
  useEffect(() => {
    api.setOptions({
      onUnauthorized: () => {
        if (signingOutRef.current) return;
        signingOutRef.current = true;
        void (async () => {
          await useAuthStore.getState().logout();
          await useWorkspaceStore.getState().clear();
          queryClient.clear();
          // Reset on next tick so a fresh session can hit 401 again later.
          setTimeout(() => {
            signingOutRef.current = false;
          }, 0);
        })();
      },
    });
    void useAuthStore.getState().initialize().then(() => maybeRenewSession());
  }, [queryClient]);

  // Renew an aging session whenever the app returns to the foreground.
  useEffect(() => {
    const sub = AppState.addEventListener("change", (state: AppStateStatus) => {
      if (state === "active") maybeRenewSession();
    });
    return () => sub.remove();
  }, []);

  // Markdown mention:// links navigate in-app (issue/project); the renderer
  // stays navigator-agnostic through this channel.
  useEffect(() => {
    setMarkdownNavigator(({ type, id }) => {
      const nav = navRef.current;
      if (!nav) return;
      if (type === "issue") nav.push({ name: "issue", issueId: id });
      if (type === "project") nav.push({ name: "project", projectId: id });
    });
    return () => setMarkdownNavigator(null);
  }, []);

  // Route by session state: this replaces the per-screen redirect logic the
  // iOS app expresses with expo-router <Redirect> components.
  useEffect(() => {
    if (isLoading) return;
    const nav = navRef.current;
    if (!nav) return;
    if (!user) {
      nav.reset({ name: "login" });
    } else if (!slug) {
      nav.reset({ name: "workspace-picker" });
    } else {
      nav.reset({ name: "tabs" });
    }
  }, [user, isLoading, slug]);

  const dark = c.background === "hsl(0 0% 3.9%)";

  const content = isLoading ? (
    <View
      style={{
        flex: 1,
        alignItems: "center",
        justifyContent: "center",
        backgroundColor: c.background,
      }}
    >
      <Text style={{ color: c.mutedForeground, fontSize: 16, fontWeight: "600" }}>
        Multica
      </Text>
    </View>
  ) : (
    <StackNavigator
      initialRoute={{ name: "login" }}
      navRef={navRef}
      renderRoute={(route) => {
        const nav = navRef.current;
        if (!nav) return null;
        return renderAppRoute(route, nav);
      }}
    />
  );

  return (
    <SessionActivityBoundary>
      <View style={{ flex: 1, backgroundColor: c.background }}>
        <StatusBar barStyle={dark ? "light-content" : "dark-content"} />
        {content}
      </View>
    </SessionActivityBoundary>
  );
}
