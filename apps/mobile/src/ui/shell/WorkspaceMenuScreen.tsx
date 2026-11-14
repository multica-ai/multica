import { StyleSheet, View } from "react-native";
import { useNavigation, useNavigationState } from "@react-navigation/native";
import type { NativeStackNavigationProp } from "@react-navigation/native-stack";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { useInboxUnreadCount } from "@multica/core/inbox/queries";
import { useSidebarViews } from "@multica/core/issue-views/sidebar-views";
import { projectListOptions } from "@multica/core/projects/queries";
import { selectWorkspace } from "../../platform/workspace";
import { useActiveTasks } from "../model/tasks";
import { viewItemLabel, viewItemOpensTab } from "../model/views";
import { useWorkspace } from "../model/workspace-context";
import type { RootStackParamList, TabParamList } from "../navigation/types";
import { useStrings } from "../strings";
import { WorkspaceMenuBody, type MenuDestination } from "./WorkspaceMenu";

/** Starred views the menu lists before "All" takes over. */
const MENU_VIEWS = 5;

const TABS = new Set<string>(["inbox", "myIssues", "agents"]);
const PAGES: Record<string, "Projects" | "Squads" | "Autopilots"> = { projects: "Projects", squads: "Squads", autopilots: "Autopilots" };

/** The destination the menu was opened over, so it can mark it. */
function useOpenedOver(): string {
  return useNavigationState((state) => {
    const below = state.routes[state.index - 1];
    if (!below) return "inbox";
    if (below.name === "Tabs") {
      const tabs = below.state;
      return (tabs?.routes[tabs.index ?? 0]?.name as string | undefined) ?? "inbox";
    }
    return Object.keys(PAGES).find((key) => PAGES[key] === below.name) ?? "";
  });
}

export function WorkspaceMenuScreen() {
  const navigation = useNavigation<NativeStackNavigationProp<RootStackParamList>>();
  const strings = useStrings();
  const { workspace, workspaces } = useWorkspace();
  const userId = useAuthStore((s) => s.user?.id);
  const unread = useInboxUnreadCount(workspace.id, { whileHidden: true });
  const running = useActiveTasks(workspace.id).tasks.length;
  const projects = useQuery(projectListOptions(workspace.id));
  const autopilots = useQuery(autopilotListOptions(workspace.id));
  const views = useSidebarViews(workspace.id, userId);
  const active = useOpenedOver();

  const openProjects = projects.data?.filter((project) => project.status === "planned" || project.status === "in_progress").length ?? 0;
  const paused = autopilots.data?.filter((autopilot) => autopilot.status === "paused").length ?? 0;

  const toTab = (screen: keyof TabParamList) => navigation.popTo("Tabs", { screen });

  const go = (key: string) => {
    if (TABS.has(key)) return toTab(key as keyof TabParamList);
    const page = PAGES[key];
    if (page) {
      navigation.popToTop();
      navigation.navigate(page);
      return;
    }
    const item = views.items.find((view) => view.id === key);
    if (item && viewItemOpensTab(item)) return toTab("myIssues");
    navigation.popToTop();
    navigation.navigate("SavedView", { itemId: key });
  };

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
    <View style={styles.sheet}>
      <WorkspaceMenuBody
        workspace={workspace}
        workspaces={workspaces}
        active={active}
        sections={[primary, workspaceSection]}
        views={{
          items: views.items.slice(0, MENU_VIEWS).map((item) => ({ key: item.id, icon: "Layers", label: viewItemLabel(item, strings) })),
          onAll: () => {
            navigation.goBack();
            navigation.navigate("Views");
          },
        }}
        onNavigate={go}
        onSwitch={(slug) => {
          navigation.goBack();
          if (slug !== workspace.slug) selectWorkspace(slug);
        }}
        onSettings={() => {
          navigation.goBack();
          navigation.navigate("Settings");
        }}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  sheet: { paddingHorizontal: 16, paddingTop: 24, paddingBottom: 16 },
});
