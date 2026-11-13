import type { NavigatorScreenParams } from "@react-navigation/native";

export type RootStackParamList = {
  Tabs: NavigatorScreenParams<TabParamList> | undefined;
  Issue: { issueId: string };
  Chat: { sessionId: string };
  NewChat: undefined;
  Search: undefined;
  Settings: undefined;
  NotificationSettings: undefined;
  Project: { projectId: string };
  ProjectEditor: { projectId?: string };
  Views: undefined;
  SavedView: { itemId: string };
  Squad: { squadId: string };
  /** `addMembers` opens straight on picking new members for an existing squad. */
  SquadEditor: { squadId?: string; addMembers?: boolean };
  Autopilot: { autopilotId: string };
  AutopilotEditor: { autopilotId?: string };
  TriggerEditor: { autopilotId: string; triggerId?: string };
};

/** The first three show in the tab bar; the rest are reached from the workspace menu. */
export type TabParamList = {
  inbox: undefined;
  myIssues: undefined;
  agents: undefined;
  projects: undefined;
  squads: undefined;
  autopilots: undefined;
};

declare global {
  namespace ReactNavigation {
    // eslint-disable-next-line @typescript-eslint/no-empty-object-type
    interface RootParamList extends RootStackParamList {}
  }
}
