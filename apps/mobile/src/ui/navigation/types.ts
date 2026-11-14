import type { NavigatorScreenParams } from "@react-navigation/native";

export type RootStackParamList = {
  Tabs: NavigatorScreenParams<TabParamList> | undefined;
  Issue: { issueId: string };
  /** One property of an issue, edited in a sheet. */
  IssueField: { issueId: string; field: IssueFieldKey };
  Chat: { sessionId: string };
  NewChat: undefined;
  Search: undefined;
  /** The workspace menu: every destination and the workspace switcher. */
  Menu: undefined;
  Settings: undefined;
  NotificationSettings: undefined;
  Projects: undefined;
  Project: { projectId: string };
  ProjectEditor: { projectId?: string };
  Views: undefined;
  SavedView: { itemId: string };
  Squads: undefined;
  Squad: { squadId: string };
  /** `addMembers` opens straight on picking new members for an existing squad. */
  SquadEditor: { squadId?: string; addMembers?: boolean };
  Autopilots: undefined;
  Autopilot: { autopilotId: string };
  AutopilotEditor: { autopilotId?: string };
  TriggerEditor: { autopilotId: string; triggerId?: string };
};

export type IssueFieldKey = "assignee" | "project" | "labels" | "parent" | "start" | "due";

/** The system tab bar. `more` and `search` never become selected: they open the menu and search. */
export type TabParamList = {
  inbox: undefined;
  myIssues: undefined;
  agents: undefined;
  more: undefined;
  search: undefined;
};

declare global {
  namespace ReactNavigation {
    // eslint-disable-next-line @typescript-eslint/no-empty-object-type
    interface RootParamList extends RootStackParamList {}
  }
}
