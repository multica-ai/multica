import { createNativeBottomTabNavigator, type NativeBottomTabNavigationOptions } from "@react-navigation/bottom-tabs/unstable";
import { createNativeStackNavigator, type NativeStackNavigationOptions } from "@react-navigation/native-stack";
import { useInboxUnreadCount } from "@multica/core/inbox/queries";
import { sfSymbol } from "../icons/sf-symbols";
import { useWorkspace } from "../model/workspace-context";
import { AgentsScreen } from "../screens/agents/AgentsScreen";
import { AutopilotEditorScreen } from "../screens/autopilots/AutopilotEditorScreen";
import { AutopilotScreen } from "../screens/autopilots/AutopilotScreen";
import { AutopilotsScreen } from "../screens/autopilots/AutopilotsScreen";
import { TriggerEditorScreen } from "../screens/autopilots/TriggerEditorScreen";
import { ChatScreen } from "../screens/chat/ChatScreen";
import { NewChatScreen } from "../screens/chat/NewChatScreen";
import { InboxScreen } from "../screens/inbox/InboxScreen";
import { IssueScreen } from "../screens/issue/IssueScreen";
import { MyIssuesScreen } from "../screens/my-issues/MyIssuesScreen";
import { ProjectEditorScreen } from "../screens/projects/ProjectEditorScreen";
import { ProjectScreen } from "../screens/projects/ProjectScreen";
import { ProjectsScreen } from "../screens/projects/ProjectsScreen";
import { SearchScreen } from "../screens/search/SearchScreen";
import { NotificationSettingsScreen } from "../screens/settings/NotificationSettingsScreen";
import { SettingsScreen } from "../screens/settings/SettingsScreen";
import { SquadEditorScreen } from "../screens/squads/SquadEditorScreen";
import { SquadScreen } from "../screens/squads/SquadScreen";
import { SquadsScreen } from "../screens/squads/SquadsScreen";
import { SavedViewScreen } from "../screens/views/SavedViewScreen";
import { ViewsScreen } from "../screens/views/ViewsScreen";
import type { RootStackParamList, TabParamList } from "../navigation/types";
import { useStrings } from "../strings";
import { useTheme } from "../theme";
import { withListDetail } from "./ListDetail";
import { WorkspaceMenuScreen } from "./WorkspaceMenuScreen";

const Stack = createNativeStackNavigator<RootStackParamList>();
const Tab = createNativeBottomTabNavigator<TabParamList>();

const Inbox = withListDetail("inbox", InboxScreen);
const MyIssues = withListDetail("myIssues", MyIssuesScreen);
const Agents = withListDetail("agents", AgentsScreen);

/** Screens draw their header through `TitleHeader` / `PushHeader`; these hold until they do. */
const NATIVE_HEADER: NativeStackNavigationOptions = { headerShown: true, headerTransparent: true, headerShadowVisible: false, headerBackButtonDisplayMode: "minimal", headerTitle: "" };
/** Screens with their own chrome: editors, search, sheets. */
const NO_HEADER: NativeStackNavigationOptions = { headerShown: false };
/** Create and edit forms: a page sheet the user can swipe away. */
const EDITOR: NativeStackNavigationOptions = { ...NO_HEADER, presentation: "modal" };

/** Renders nothing: the menu and search tabs open screens of the root stack instead of showing one. */
function Placeholder() {
  return null;
}

function Tabs() {
  const strings = useStrings();
  const palette = useTheme();
  const { workspace } = useWorkspace();
  const unread = useInboxUnreadCount(workspace.id, { whileHidden: true });
  const tab = (title: string, symbol: string): NativeBottomTabNavigationOptions => ({
    title,
    tabBarIcon: { type: "sfSymbol", name: symbol as never },
  });
  return (
    <Tab.Navigator
      screenOptions={{
        headerShown: true,
        headerTransparent: true,
        headerLargeTitleEnabled: true,
        headerTintColor: palette.label,
        tabBarActiveTintColor: palette.label,
        tabBarMinimizeBehavior: "none",
      }}
    >
      <Tab.Screen name="inbox" component={Inbox} options={{ ...tab(strings.tabs.inbox, sfSymbol("Inbox")), tabBarBadge: unread > 0 ? unread : undefined }} />
      <Tab.Screen name="myIssues" component={MyIssues} options={tab(strings.tabs.myIssues, sfSymbol("CircleUser"))} />
      <Tab.Screen name="agents" component={Agents} options={tab(strings.tabs.agents, sfSymbol("Bot"))} />
      <Tab.Screen
        name="more"
        component={Placeholder}
        options={{ ...tab(strings.tabs.more, sfSymbol("ChevronsUpDown")), tabBarSelectionEnabled: false }}
        listeners={({ navigation }) => ({ tabPress: () => navigation.getParent()?.navigate("Menu") })}
      />
      <Tab.Screen
        name="search"
        component={Placeholder}
        options={{ title: strings.search.title, tabBarRole: "search", tabBarSelectionEnabled: false }}
        listeners={({ navigation }) => ({ tabPress: () => navigation.getParent()?.navigate("Search") })}
      />
    </Tab.Navigator>
  );
}

export function RootNavigator() {
  const palette = useTheme();
  return (
    <Stack.Navigator screenOptions={{ ...NATIVE_HEADER, headerTintColor: palette.label }}>
      <Stack.Screen name="Tabs" component={Tabs} options={NO_HEADER} />
      <Stack.Screen name="Issue" component={IssueScreen} />
      <Stack.Screen name="Chat" component={ChatScreen} />
      <Stack.Screen name="Settings" component={SettingsScreen} />
      <Stack.Screen name="NotificationSettings" component={NotificationSettingsScreen} />
      <Stack.Screen name="Search" component={SearchScreen} options={{ ...NO_HEADER, animation: "fade" }} />
      <Stack.Screen
        name="Menu"
        component={WorkspaceMenuScreen}
        options={{ ...NO_HEADER, presentation: "formSheet", sheetAllowedDetents: "fitToContents", sheetGrabberVisible: true, sheetCornerRadius: 28 }}
      />
      <Stack.Screen name="Projects" component={ProjectsScreen} />
      <Stack.Screen name="Project" component={ProjectScreen} />
      <Stack.Screen name="ProjectEditor" component={ProjectEditorScreen} options={EDITOR} />
      <Stack.Screen name="Views" component={ViewsScreen} />
      <Stack.Screen name="SavedView" component={SavedViewScreen} />
      <Stack.Screen name="Squads" component={SquadsScreen} />
      <Stack.Screen name="Squad" component={SquadScreen} />
      <Stack.Screen name="SquadEditor" component={SquadEditorScreen} options={EDITOR} />
      <Stack.Screen name="Autopilots" component={AutopilotsScreen} />
      <Stack.Screen name="Autopilot" component={AutopilotScreen} />
      <Stack.Screen name="AutopilotEditor" component={AutopilotEditorScreen} options={EDITOR} />
      <Stack.Screen name="TriggerEditor" component={TriggerEditorScreen} options={EDITOR} />
      <Stack.Screen name="NewChat" component={NewChatScreen} options={{ ...NO_HEADER, presentation: "formSheet", sheetAllowedDetents: [0.6, 1], sheetGrabberVisible: true, sheetCornerRadius: 28 }} />
    </Stack.Navigator>
  );
}
