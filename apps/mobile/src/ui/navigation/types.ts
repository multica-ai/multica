import type { NavigatorScreenParams } from "@react-navigation/native";

export type RootStackParamList = {
  Tabs: NavigatorScreenParams<TabParamList> | undefined;
  Issue: { issueId: string };
  Chat: { sessionId: string };
  NewChat: undefined;
  Search: undefined;
  Settings: undefined;
};

export type TabParamList = {
  inbox: undefined;
  myIssues: undefined;
  agents: undefined;
};

declare global {
  namespace ReactNavigation {
    // eslint-disable-next-line @typescript-eslint/no-empty-object-type
    interface RootParamList extends RootStackParamList {}
  }
}
