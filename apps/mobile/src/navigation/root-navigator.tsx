import { useEffect, useRef, useState } from "react";
import { ActivityIndicator, StyleSheet, Text, View } from "react-native";
import { NavigationIndependentTree } from "@react-navigation/core";
import {
  NavigationContainer,
  type NavigationContainerRef,
} from "@react-navigation/native";
import { createBottomTabNavigator } from "@react-navigation/bottom-tabs";
import { createNativeStackNavigator } from "@react-navigation/native-stack";
import { useAuthStore } from "@multica/core/auth";
import { setCurrentWorkspace } from "@multica/core/platform";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { useWorkspaceList } from "@multica/core/workspace/hooks";
import type { Workspace } from "@multica/core/types";
import { CircleUserRound, ListTodo } from "lucide-react-native";
import { EmptyState, LoadingState, Screen } from "../components/ui/primitives";
import { LoginScreen } from "../screens/auth/login-screen";
import { CreateIssueScreen } from "../screens/issues/create-issue-screen";
import { IssueDetailScreen } from "../screens/issues/issue-detail-screen";
import { IssueTaskTranscriptScreen } from "../screens/issues/issue-task-transcript-screen";
import { IssuesScreen } from "../screens/issues/issues-screen";
import { InboxDetailScreen } from "../screens/mine/inbox-detail-screen";
import { InboxScreen } from "../screens/mine/inbox-screen";
import { MineScreen } from "../screens/mine/mine-screen";
import { RuntimesScreen } from "../screens/runtimes/runtimes-screen";
import { colors, spacing } from "../theme/tokens";
import { linking } from "./linking";
import { WorkspaceContext } from "./workspace-context";

export type RootStackParamList = {
  Main: undefined;
  IssueDetail: { issueId: string };
  IssueTaskTranscript: { issueId: string; taskId: string };
  CreateIssue: undefined;
  Search: undefined;
  Runtimes: undefined;
  Agents: undefined;
  Inbox: undefined;
  InboxDetail: { inboxItemId: string };
};

type TabParamList = {
  Issues: undefined;
  Mine: undefined;
};

const Stack = createNativeStackNavigator<RootStackParamList>();
const Tabs = createBottomTabNavigator<TabParamList>();

export function RootNavigator() {
  const user = useAuthStore((state) => state.user);
  const isLoading = useAuthStore((state) => state.isLoading);
  const navigationRef = useRef<NavigationContainerRef<RootStackParamList>>(null);

  if (isLoading) {
    return (
      <View style={styles.loading}>
        <ActivityIndicator color={colors.foreground} />
      </View>
    );
  }

  if (!user) return <LoginScreen />;

  return (
    <NavigationIndependentTree>
      <NavigationContainer linking={linking} ref={navigationRef}>
        <WorkspaceGate>
          <Stack.Navigator
            screenOptions={{
              contentStyle: { backgroundColor: colors.background },
              headerShown: false,
            }}
          >
            <Stack.Screen component={MainTabs} name="Main" />
            <Stack.Screen component={IssueDetailScreen} name="IssueDetail" />
            <Stack.Screen component={IssueTaskTranscriptScreen} name="IssueTaskTranscript" />
            <Stack.Screen component={CreateIssueScreen} name="CreateIssue" />
            <Stack.Screen component={SearchScreen} name="Search" />
            <Stack.Screen component={RuntimesScreen} name="Runtimes" />
            <Stack.Screen component={AgentsScreen} name="Agents" />
            <Stack.Screen component={InboxScreen} name="Inbox" />
            <Stack.Screen component={InboxDetailScreen} name="InboxDetail" />
          </Stack.Navigator>
        </WorkspaceGate>
      </NavigationContainer>
    </NavigationIndependentTree>
  );
}

function WorkspaceGate({ children }: { children: React.ReactNode }) {
  const { data: workspaces = [], isError, isLoading } = useWorkspaceList();
  const [workspace, setWorkspace] = useState<Workspace | null>(null);

  useEffect(() => {
    if (workspace || workspaces.length === 0) return;
    const first = workspaces[0];
    if (!first) return;
    setCurrentWorkspace(first.slug, first.id);
    setWorkspace(first);
  }, [workspace, workspaces]);

  if (isLoading) return <LoadingState />;
  if (isError) return <EmptyState title="Unable to load workspaces" />;
  if (!workspace) return <EmptyState title="No workspaces available" />;

  return (
    <WorkspaceSlugProvider slug={workspace.slug}>
      <WorkspaceContext.Provider value={{ workspace, setWorkspace }}>
        {children}
      </WorkspaceContext.Provider>
    </WorkspaceSlugProvider>
  );
}

function MainTabs() {
  return (
    <Tabs.Navigator
      screenOptions={{
        headerShown: false,
        tabBarActiveTintColor: colors.foreground,
        tabBarInactiveTintColor: colors.mutedForeground,
        tabBarLabelPosition: "below-icon",
        tabBarLabelStyle: styles.tabLabel,
        tabBarStyle: styles.tabBar,
      }}
    >
      <Tabs.Screen
        component={IssuesScreen}
        name="Issues"
        options={{
          tabBarIcon: ({ color, size }) => <ListTodo color={color} size={size} />,
        }}
      />
      <Tabs.Screen
        component={MineScreen}
        name="Mine"
        options={{
          tabBarIcon: ({ color, size }) => <CircleUserRound color={color} size={size} />,
        }}
      />
    </Tabs.Navigator>
  );
}

function SearchScreen() {
  return (
    <Screen>
      <Text style={styles.title}>Search</Text>
      <Text style={styles.muted}>Issue and project search entry point.</Text>
    </Screen>
  );
}

function AgentsScreen() {
  return <ReadOnlyPlaceholderScreen title="Agents" />;
}

function ReadOnlyPlaceholderScreen({ title }: { title: string }) {
  return (
    <Screen>
      <Text style={styles.title}>{title}</Text>
      <Text style={styles.muted}>Read-only mobile view entry point.</Text>
    </Screen>
  );
}

const styles = StyleSheet.create({
  loading: {
    alignItems: "center",
    backgroundColor: colors.background,
    flex: 1,
    justifyContent: "center",
  },
  tabBar: {
    backgroundColor: colors.card,
    borderTopColor: colors.border,
  },
  tabLabel: {
    fontSize: 12,
    fontWeight: "500",
  },
  title: {
    color: colors.foreground,
    fontSize: 20,
    fontWeight: "500",
    marginBottom: spacing.sm,
  },
  muted: {
    color: colors.mutedForeground,
    fontSize: 14,
    lineHeight: 20,
  },
});
