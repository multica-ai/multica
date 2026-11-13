import { createBottomTabNavigator } from "@react-navigation/bottom-tabs";
import { createNativeStackNavigator } from "@react-navigation/native-stack";
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
import { ShellTabBar } from "./ShellTabBar";

const Stack = createNativeStackNavigator<RootStackParamList>();

/** Create and edit forms: a page sheet the user can swipe away. */
const EDITOR = { presentation: "modal" } as const;
const Tab = createBottomTabNavigator<TabParamList>();

function Tabs() {
  return (
    <Tab.Navigator screenOptions={{ headerShown: false, animation: "none" }} tabBar={(props) => <ShellTabBar {...props} />}>
      <Tab.Screen name="inbox" component={InboxScreen} />
      <Tab.Screen name="myIssues" component={MyIssuesScreen} />
      <Tab.Screen name="agents" component={AgentsScreen} />
      <Tab.Screen name="projects" component={ProjectsScreen} />
      <Tab.Screen name="squads" component={SquadsScreen} />
      <Tab.Screen name="autopilots" component={AutopilotsScreen} />
    </Tab.Navigator>
  );
}

export function RootNavigator() {
  return (
    <Stack.Navigator screenOptions={{ headerShown: false }}>
      <Stack.Screen name="Tabs" component={Tabs} />
      <Stack.Screen name="Issue" component={IssueScreen} />
      <Stack.Screen name="Chat" component={ChatScreen} />
      <Stack.Screen name="Settings" component={SettingsScreen} />
      <Stack.Screen name="NotificationSettings" component={NotificationSettingsScreen} />
      <Stack.Screen name="Search" component={SearchScreen} options={{ animation: "fade" }} />
      <Stack.Screen name="Project" component={ProjectScreen} />
      <Stack.Screen name="ProjectEditor" component={ProjectEditorScreen} options={EDITOR} />
      <Stack.Screen name="Views" component={ViewsScreen} />
      <Stack.Screen name="SavedView" component={SavedViewScreen} />
      <Stack.Screen name="Squad" component={SquadScreen} />
      <Stack.Screen name="SquadEditor" component={SquadEditorScreen} options={EDITOR} />
      <Stack.Screen name="Autopilot" component={AutopilotScreen} />
      <Stack.Screen name="AutopilotEditor" component={AutopilotEditorScreen} options={EDITOR} />
      <Stack.Screen name="TriggerEditor" component={TriggerEditorScreen} options={EDITOR} />
      <Stack.Screen name="NewChat" component={NewChatScreen} options={{ presentation: "formSheet", sheetAllowedDetents: [0.6, 1], sheetGrabberVisible: true, sheetCornerRadius: 28 }} />
    </Stack.Navigator>
  );
}
