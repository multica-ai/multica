import { useState } from "react";
import type { BottomTabBarProps } from "@react-navigation/bottom-tabs";
import { useInboxUnreadCount } from "@multica/core/inbox/queries";
import { selectWorkspace } from "../../platform/workspace";
import { TabBar } from "../components/TabBar";
import { useActiveTasks } from "../model/tasks";
import { useWorkspace } from "../model/workspace-context";
import { tabItems } from "../navigation/tabs";
import { useStrings } from "../strings";
import { WorkspaceMenu } from "./WorkspaceMenu";

export function ShellTabBar({ state, navigation }: BottomTabBarProps) {
  const strings = useStrings();
  const { workspace, workspaces } = useWorkspace();
  const unread = useInboxUnreadCount(workspace.id, { whileHidden: true });
  const running = useActiveTasks(workspace.id).tasks.length;
  const [menuOpen, setMenuOpen] = useState(false);
  const active = state.routes[state.index]?.name ?? "inbox";

  const go = (key: string) => {
    setMenuOpen(false);
    navigation.navigate(key);
  };

  const bar = (
    <TabBar
      tabs={tabItems(strings, unread > 0)}
      active={menuOpen ? "more" : active}
      onSelect={(key) => (key === "more" ? setMenuOpen(!menuOpen) : go(key))}
      onSearch={() => {
        setMenuOpen(false);
        navigation.getParent()?.navigate("Search");
      }}
      searchLabel={strings.search.title}
    />
  );

  return (
    <>
      {bar}
      <WorkspaceMenu
        visible={menuOpen}
        onClose={() => setMenuOpen(false)}
        workspace={workspace}
        workspaces={workspaces}
        active={active}
        destinations={[
          { key: "inbox", icon: "Inbox", label: strings.tabs.inbox, meta: unread > 0 ? String(unread) : undefined },
          { key: "myIssues", icon: "CircleUser", label: strings.tabs.myIssues },
          { key: "agents", icon: "Bot", label: strings.tabs.agents, meta: running > 0 ? strings.more.running(running) : undefined },
        ]}
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
