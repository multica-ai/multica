import { createBottomTabNavigator } from "@react-navigation/bottom-tabs";
import { createNativeStackNavigator } from "@react-navigation/native-stack";
import { AgentsScreen } from "../screens/agents/AgentsScreen";
import { ChatScreen } from "../screens/chat/ChatScreen";
import { NewChatScreen } from "../screens/chat/NewChatScreen";
import { InboxScreen } from "../screens/inbox/InboxScreen";
import { IssueScreen } from "../screens/issue/IssueScreen";
import { MyIssuesScreen } from "../screens/my-issues/MyIssuesScreen";
import { SearchScreen } from "../screens/search/SearchScreen";
import { SettingsScreen } from "../screens/settings/SettingsScreen";
import type { RootStackParamList, TabParamList } from "../navigation/types";
import { ShellTabBar } from "./ShellTabBar";

const Stack = createNativeStackNavigator<RootStackParamList>();
const Tab = createBottomTabNavigator<TabParamList>();

function Tabs() {
  return (
    <Tab.Navigator screenOptions={{ headerShown: false, animation: "none" }} tabBar={(props) => <ShellTabBar {...props} />}>
      <Tab.Screen name="inbox" component={InboxScreen} />
      <Tab.Screen name="myIssues" component={MyIssuesScreen} />
      <Tab.Screen name="agents" component={AgentsScreen} />
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
      <Stack.Screen name="Search" component={SearchScreen} options={{ animation: "fade" }} />
      <Stack.Screen name="NewChat" component={NewChatScreen} options={{ presentation: "formSheet", sheetAllowedDetents: [0.6, 1], sheetGrabberVisible: true, sheetCornerRadius: 28 }} />
    </Stack.Navigator>
  );
}
