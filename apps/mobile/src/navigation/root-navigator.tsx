import { useCallback, useEffect, useRef, useState } from "react";
import { ActivityIndicator, Alert, Linking, StyleSheet, View } from "react-native";
import { NavigationIndependentTree } from "@react-navigation/core";
import {
  NavigationContainer,
  type NavigationContainerRef,
} from "@react-navigation/native";
import { createBottomTabNavigator } from "@react-navigation/bottom-tabs";
import { createNativeStackNavigator } from "@react-navigation/native-stack";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "@multica/core/auth";
import { useCoreQueryClient } from "@multica/core/provider";
import { setCurrentWorkspace } from "@multica/core/platform";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { useWorkspaceList } from "@multica/core/workspace/hooks";
import type { Workspace } from "@multica/core/types";
import { CircleUserRound, ListTodo } from "lucide-react-native";
import { useMobileLogout } from "../auth/use-mobile-logout";
import { Button, EmptyState, LoadingState, Screen } from "../components/ui/primitives";
import { LoginScreen } from "../screens/auth/login-screen";
import { CreateIssueScreen } from "../screens/issues/create-issue-screen";
import {
  IssueDetailScreen,
  IssueTaskRunsScreen,
  IssueTimelineScreen,
} from "../screens/issues/issue-detail-screen";
import { IssuePropertiesScreen } from "../screens/issues/issue-properties-screen";
import { IssueTaskTranscriptScreen } from "../screens/issues/issue-task-transcript-screen";
import { IssuesScreen } from "../screens/issues/issues-screen";
import { SearchScreen } from "../screens/issues/search-screen";
import { InboxDetailScreen } from "../screens/mine/inbox-detail-screen";
import { InboxScreen } from "../screens/mine/inbox-screen";
import { AgentsScreen } from "../screens/mine/agents-screen";
import { MineScreen } from "../screens/mine/mine-screen";
import { SettingScreen } from "../screens/mine/setting-screen";
import { SquadsScreen } from "../screens/mine/squads-screen";
import { RuntimesScreen } from "../screens/runtimes/runtimes-screen";
import { WikiDetailScreen, WikiScreen } from "../screens/wiki/wiki-screen";
import { WorkspaceSetupScreen } from "../screens/workspace/workspace-setup-screen";
import {
  addMobilePushNotificationUrlListener,
  consumeMobilePushPendingNotificationUrl,
} from "../push/mobile-push-notifications";
import { getMobileIssueLinkBaseUrls } from "../runtime/env";
import { colors, spacing } from "../theme/tokens";
import { parseMobileIssueLink } from "./issue-links";
import { linking } from "./linking";
import { useMobileWorkspace, WorkspaceContext } from "./workspace-context";

export type RootStackParamList = {
  Main: undefined;
  IssueDetail: { commentId?: string; issueId: string };
  IssueProperties: { issueId: string };
  IssueTimeline: { issueId: string };
  IssueTaskRuns: { issueId: string };
  IssueTaskTranscript: { issueId: string; taskId: string };
  CreateIssue: { parentIssueId?: string; parentIssueIdentifier?: string } | undefined;
  Search: undefined;
  Runtimes: undefined;
  Agents: undefined;
  Squads: undefined;
  Inbox: undefined;
  InboxDetail: { inboxItemId: string };
  Wiki: undefined;
  WikiDetail: { pageId: string };
  Setting: undefined;
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
  const loginWithToken = useAuthStore((state) => state.loginWithToken);
  const queryClient = useCoreQueryClient();
  const handledAuthUrlRef = useRef<string | null>(null);

  useEffect(() => {
    async function handleAuthUrl(url: string | null) {
      if (!url || handledAuthUrlRef.current === url) return;

      let parsed: URL;
      try {
        parsed = new URL(url);
      } catch {
        return;
      }

      const authPath = `${parsed.hostname}${parsed.pathname}`.replace(/^\/+/, "");
      if (authPath !== "auth/callback") return;

      const token = parsed.searchParams.get("token");
      if (!token) return;

      handledAuthUrlRef.current = url;
      try {
        queryClient.clear();
        await loginWithToken(token);
      } catch {
        handledAuthUrlRef.current = null;
      }
    }

    void Linking.getInitialURL().then(handleAuthUrl);
    const subscription = Linking.addEventListener("url", (event) => {
      void handleAuthUrl(event.url);
    });

    return () => {
      subscription.remove();
    };
  }, [loginWithToken, queryClient]);

  if (isLoading) {
    return (
      <View style={styles.loading}>
        <ActivityIndicator color={colors.foreground} />
      </View>
    );
  }

  if (!user) return <LoginScreen />;

  return <AuthenticatedNavigator />;
}

function AuthenticatedNavigator() {
  const navigationRef = useRef<NavigationContainerRef<RootStackParamList>>(null);
  const pendingNotificationUrlRef = useRef<string | null>(null);
  const [navigationReady, setNavigationReady] = useState(false);
  const { data: workspaces = [], isError, isLoading } = useWorkspaceList();

  useEffect(() => {
    function openNotificationUrl(url: string) {
      if (!navigationRef.current) {
        pendingNotificationUrlRef.current = url;
        return;
      }
      navigateToNotificationUrl(navigationRef.current, url);
    }

    void consumeMobilePushPendingNotificationUrl().then((url) => {
      if (url) openNotificationUrl(url);
    });
    return addMobilePushNotificationUrlListener(openNotificationUrl);
  }, []);

  if (isLoading) return <LoadingState />;
  if (isError) return <SignedInErrorScreen />;
  if (workspaces.length === 0) return <WorkspaceSetupScreen />;

  return (
    <NavigationIndependentTree>
      <NavigationContainer
        linking={linking}
        onReady={() => {
          setNavigationReady(true);
          const url = pendingNotificationUrlRef.current;
          if (!url || !navigationRef.current) return;
          pendingNotificationUrlRef.current = null;
          navigateToNotificationUrl(navigationRef.current, url);
        }}
        ref={navigationRef}
      >
        <WorkspaceGate workspaces={workspaces}>
          <MobileWebDeepLinkHandler
            navigationReady={navigationReady}
            navigationRef={navigationRef}
            workspaces={workspaces}
          />
          <Stack.Navigator
            screenOptions={{
              contentStyle: { backgroundColor: colors.background },
              headerShown: false,
            }}
          >
            <Stack.Screen component={MainTabs} name="Main" />
            <Stack.Screen component={IssueDetailScreen} name="IssueDetail" />
            <Stack.Screen component={IssuePropertiesScreen} name="IssueProperties" />
            <Stack.Screen component={IssueTimelineScreen} name="IssueTimeline" />
            <Stack.Screen component={IssueTaskRunsScreen} name="IssueTaskRuns" />
            <Stack.Screen component={IssueTaskTranscriptScreen} name="IssueTaskTranscript" />
            <Stack.Screen component={CreateIssueScreen} name="CreateIssue" />
            <Stack.Screen component={SearchScreen} name="Search" />
            <Stack.Screen component={RuntimesScreen} name="Runtimes" />
            <Stack.Screen component={AgentsScreen} name="Agents" />
            <Stack.Screen component={SquadsScreen} name="Squads" />
            <Stack.Screen component={InboxScreen} name="Inbox" />
            <Stack.Screen component={InboxDetailScreen} name="InboxDetail" />
            <Stack.Screen component={WikiScreen} name="Wiki" />
            <Stack.Screen component={WikiDetailScreen} name="WikiDetail" />
            <Stack.Screen component={SettingScreen} name="Setting" />
          </Stack.Navigator>
        </WorkspaceGate>
      </NavigationContainer>
    </NavigationIndependentTree>
  );
}

function MobileWebDeepLinkHandler({
  navigationReady,
  navigationRef,
  workspaces,
}: {
  navigationReady: boolean;
  navigationRef: React.RefObject<NavigationContainerRef<RootStackParamList> | null>;
  workspaces: Workspace[];
}) {
  const checkedInitialUrlRef = useRef(false);
  const { t } = useTranslation();
  const { workspace, setWorkspace } = useMobileWorkspace();

  const openIssue = useCallback((issueId: string, commentId?: string) => {
    if (!navigationRef.current) return;
    navigationRef.current.navigate("IssueDetail", {
      issueId,
      ...(commentId ? { commentId } : {}),
    });
  }, [navigationRef]);

  const handleUrl = useCallback((url: string) => {
    const target = parseMobileIssueLink(url, getMobileIssueLinkBaseUrls());
    if (!target) return;

    const targetWorkspace = workspaces.find((item) => item.slug === target.workspaceSlug);
    if (!targetWorkspace) {
      Alert.alert(
        t("issues.link_unavailable_title"),
        t("issues.link_workspace_unavailable"),
      );
      return;
    }

    if (targetWorkspace.id === workspace.id) {
      openIssue(target.issueId, target.commentId);
      return;
    }

    Alert.alert(
      t("issues.switch_workspace_title"),
      t("issues.switch_workspace_description", { name: targetWorkspace.name }),
      [
        { style: "cancel", text: t("common.cancel") },
        {
          text: t("issues.switch_workspace_confirm"),
          onPress: () => {
            setCurrentWorkspace(targetWorkspace.slug, targetWorkspace.id);
            setWorkspace(targetWorkspace);
            openIssue(target.issueId, target.commentId);
          },
        },
      ],
    );
  }, [
    openIssue,
    setWorkspace,
    t,
    workspace.id,
    workspaces,
  ]);

  useEffect(() => {
    if (!navigationReady) return;

    if (!checkedInitialUrlRef.current) {
      checkedInitialUrlRef.current = true;
      void Linking.getInitialURL().then((url) => {
        if (url) handleUrl(url);
      });
    }

    const subscription = Linking.addEventListener("url", (event) => {
      handleUrl(event.url);
    });

    return () => {
      subscription.remove();
    };
  }, [handleUrl, navigationReady]);

  return null;
}

function navigateToNotificationUrl(
  navigation: NavigationContainerRef<RootStackParamList>,
  url: string,
) {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return;
  }
  if (parsed.protocol !== "wujieai-multicam:") return;

  const path = `${parsed.hostname}${parsed.pathname}`.replace(/^\/+/, "");
  const [route, id] = path.split("/");
  if (route === "issues" && id) {
    const commentId = parsed.searchParams.get("commentId") ?? undefined;
    navigation.navigate("IssueDetail", { issueId: id, commentId });
    return;
  }
  if (route === "inbox" && id) {
    navigation.navigate("InboxDetail", { inboxItemId: id });
  }
}

function WorkspaceGate({
  children,
  workspaces,
}: {
  children: React.ReactNode;
  workspaces: Workspace[];
}) {
  const [workspace, setWorkspace] = useState<Workspace | null>(null);

  useEffect(() => {
    if (workspace && workspaces.some((item) => item.id === workspace.id)) return;
    const first = workspaces[0];
    if (!first) return;
    setCurrentWorkspace(first.slug, first.id);
    setWorkspace(first);
  }, [workspace, workspaces]);

  if (!workspace) return <NoWorkspaceState />;

  return (
    <WorkspaceSlugProvider slug={workspace.slug}>
      <WorkspaceContext.Provider value={{ workspace, setWorkspace }}>
        {children}
      </WorkspaceContext.Provider>
    </WorkspaceSlugProvider>
  );
}

function SignedInErrorScreen() {
  const logout = useMobileLogout();
  const { t } = useTranslation();

  return (
    <Screen>
      <View style={styles.errorState}>
        <EmptyState
          detail={t("common.check_connection")}
          title={t("common.unable_to_load_workspaces")}
        />
        <Button onPress={logout} style={styles.errorLogoutButton} variant="secondary">
          {t("common.log_out")}
        </Button>
      </View>
    </Screen>
  );
}

function NoWorkspaceState() {
  const { t } = useTranslation();
  return <EmptyState title={t("common.no_workspaces_available")} />;
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
          tabBarLabel: "Issues",
          tabBarIcon: ({ color, size }) => <ListTodo color={color} size={size} />,
        }}
      />
      <Tabs.Screen
        component={MineScreen}
        name="Mine"
        options={{
          tabBarLabel: "Mine",
          tabBarIcon: ({ color, size }) => <CircleUserRound color={color} size={size} />,
        }}
      />
    </Tabs.Navigator>
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
  errorState: {
    backgroundColor: colors.background,
    flex: 1,
  },
  errorLogoutButton: {
    alignSelf: "center",
    bottom: spacing.xl,
    position: "absolute",
    width: "60%",
  },
});
