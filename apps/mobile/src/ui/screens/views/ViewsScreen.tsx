import { useMemo } from "react";
import { useNavigation } from "@react-navigation/native";
import type { NativeStackNavigationProp } from "@react-navigation/native-stack";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { issueViewEverywhereOptions } from "@multica/core/issue-views/queries";
import { useSidebarViews } from "@multica/core/issue-views/sidebar-views";
import { findViewItem, viewItemOpensTab, viewRows } from "../../model/views";
import { useWorkspace } from "../../model/workspace-context";
import type { RootStackParamList } from "../../navigation/types";
import { useStrings } from "../../strings";
import { ViewsView } from "./ViewsView";

export function ViewsScreen() {
  const navigation = useNavigation<NativeStackNavigationProp<RootStackParamList>>();
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const userId = useAuthStore((state) => state.user?.id);
  const views = useQuery(issueViewEverywhereOptions(workspace.id));
  const sidebar = useSidebarViews(workspace.id, userId);
  const rows = useMemo(() => viewRows(sidebar.items, views.data ?? [], userId, strings), [sidebar.items, views.data, userId, strings]);

  return (
    <ViewsView
      state={views.data ? "ready" : views.isError ? "error" : "loading"}
      starred={rows.starred}
      others={rows.others}
      onBack={() => navigation.goBack()}
      onRetry={() => void views.refetch()}
      onOpen={(id) => {
        const item = findViewItem(id, views.data ?? []);
        if (item && viewItemOpensTab(item)) navigation.popTo("Tabs", { screen: "myIssues" });
        else navigation.navigate("SavedView", { itemId: id });
      }}
      onToggleStar={(view) => (view.starred ? sidebar.unstar(view.id) : sidebar.star(view.id))}
    />
  );
}
