import type { TabItem } from "../components/TabBar";
import type { Strings } from "../strings";

export type TabKey = "inbox" | "myIssues" | "agents" | "more";

export function tabItems(strings: Strings, inboxUnread: boolean): TabItem[] {
  return [
    { key: "inbox", icon: "Inbox", label: strings.tabs.inbox, dot: inboxUnread },
    { key: "myIssues", icon: "CircleUser", label: strings.tabs.myIssues },
    { key: "agents", icon: "Bot", label: strings.tabs.agents },
    { key: "more", icon: "ChevronsUpDown", label: strings.tabs.more },
  ];
}
