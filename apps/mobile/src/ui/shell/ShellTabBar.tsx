import { useState } from "react";
import type { BottomTabBarProps } from "@react-navigation/bottom-tabs";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { useInboxUnreadCount } from "@multica/core/inbox/queries";
import { useSidebarViews } from "@multica/core/issue-views/sidebar-views";
import { projectListOptions } from "@multica/core/projects/queries";
import { selectWorkspace } from "../../platform/workspace";
import { TabBar } from "../components/TabBar";
import { useActiveTasks } from "../model/tasks";
import { viewItemLabel, viewItemOpensTab } from "../model/views";
import { useWorkspace } from "../model/workspace-context";
import { tabItems } from "../navigation/tabs";
import type { TabParamList } from "../navigation/types";
import { useStrings } from "../strings";
import { WorkspaceMenu, type MenuDestination } from "./WorkspaceMenu";

const BAR_TABS = new Set(["inbox", "myIssues", "agents"]);
const MENU_TABS = new Set<string>(["inbox", "myIssues", "agents", "projects", "squads", "autopilots"]);
/** Starred views the menu lists before "All" takes over. */
const MENU_VIEWS = 5;

export function ShellTabBar({ state, navigation }: BottomTabBarProps) {
  const strings = useStrings();
  const { workspace, workspaces } = useWorkspace();
  const userId = useAuthStore((s) => s.user?.id);
  const unread = useInboxUnreadCount(workspace.id, { whileHidden: true });
  const running = useActiveTasks(workspace.id).tasks.length;
  const projects = useQuery(projectListOptions(workspace.id));
  const autopilots = useQuery(autopilotListOptions(workspace.id));
  const views = useSidebarViews(workspace.id, userId);
  const [menuOpen, setMenuOpen] = useState(false);
  const active = state.routes[state.index]?.name ?? "inbox";

  const openProjects = projects.data?.filter((project) => project.status === "planned" || project.status === "in_progress").length ?? 0;
  const paused = autopilots.data?.filter((autopilot) => autopilot.status === "paused").length ?? 0;

  const go = (key: string) => {
    setMenuOpen(false);
    if (MENU_TABS.has(key)) {
      navigation.navigate(key as keyof TabParamList);
      return;
    }
    const item = views.items.find((view) => view.id === key);
    if (item && viewItemOpensTab(item)) navigation.navigate("myIssues");
    else navigation.getParent()?.navigate("SavedView", { itemId: key });
  };

  const bar = (
    <TabBar
      tabs={tabItems(strings, unread > 0)}
      active={menuOpen || !BAR_TABS.has(active) ? "more" : active}
      onSelect={(key) => (key === "more" ? setMenuOpen(!menuOpen) : go(key))}
      onSearch={() => {
        setMenuOpen(false);
        navigation.getParent()?.navigate("Search");
      }}
      searchLabel={strings.search.title}
    />
  );

  const primary: MenuDestination[] = [
    { key: "inbox", icon: "Inbox", label: strings.tabs.inbox, meta: unread > 0 ? String(unread) : undefined },
    { key: "myIssues", icon: "CircleUser", label: strings.tabs.myIssues },
    { key: "agents", icon: "Bot", label: strings.tabs.agents, meta: running > 0 ? strings.more.running(running) : undefined },
  ];
  const workspaceSection: MenuDestination[] = [
    { key: "projects", icon: "FolderKanban", label: strings.nav.projects, meta: openProjects > 0 ? String(openProjects) : undefined },
    { key: "squads", icon: "Users", label: strings.nav.squads },
    { key: "autopilots", icon: "Zap", label: strings.nav.autopilots, meta: paused > 0 ? strings.autopilot.pausedCount(paused) : undefined },
  ];

  return (
    <>
      {bar}
      <WorkspaceMenu
        visible={menuOpen}
        onClose={() => setMenuOpen(false)}
        workspace={workspace}
        workspaces={workspaces}
        active={active}
        sections={[primary, workspaceSection]}
        views={{
          items: views.items.slice(0, MENU_VIEWS).map((item) => ({ key: item.id, icon: "Layers", label: viewItemLabel(item, strings) })),
          onAll: () => {
            setMenuOpen(false);
            navigation.getParent()?.navigate("Views");
          },
        }}
        onNavigate={go}
        onSwitch={(slug) => {
          setMenuOpen(false);
          if (slug !== workspace.slug) selectWorkspace(slug);
        }}
        onSettings={() => {
          setMenuOpen(false);
          navigation.getParent()?.navigate("Settings");
        }}
      >
        {bar}
      </WorkspaceMenu>
    </>
  );
}
