/**
 * Bottom tab bar — the native iOS UITabBar via expo-router NativeTabs (over
 * react-native-screens). This is the real platform tab bar: iOS 26 liquid
 * glass, native blur, the system selection spring + haptic, and SF Symbol
 * icons that fill on selection. The previous JS `<Tabs>` (react-navigation)
 * couldn't render any of that.
 *
 * "More" is now a real tab → a pushed More screen (more.tsx), replacing the
 * old JS dropdown-popover hack. That hack only existed because NativeTabs
 * can't `preventDefault` a tab press; with the workspace switcher moved into
 * the header, the dropdown's contents are a natural fit for a More list.
 */
import { NativeTabs } from "expo-router/unstable-native-tabs";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  useInboxUnreadCount,
  useChatUnreadSessionCount,
} from "@/lib/unread-counts";

export default function TabsLayout() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const inboxUnread = useInboxUnreadCount(wsId);
  const chatUnread = useChatUnreadSessionCount(wsId);

  return (
    <NativeTabs>
      <NativeTabs.Trigger name="inbox">
        <NativeTabs.Trigger.Icon
          sf={{ default: "tray", selected: "tray.fill" }}
        />
        <NativeTabs.Trigger.Label>Inbox</NativeTabs.Trigger.Label>
        {inboxUnread > 0 ? (
          <NativeTabs.Trigger.Badge>
            {inboxUnread > 99 ? "99+" : String(inboxUnread)}
          </NativeTabs.Trigger.Badge>
        ) : null}
      </NativeTabs.Trigger>

      <NativeTabs.Trigger name="my-issues">
        <NativeTabs.Trigger.Icon
          sf={{ default: "checklist.unchecked", selected: "checklist" }}
        />
        <NativeTabs.Trigger.Label>My Issues</NativeTabs.Trigger.Label>
      </NativeTabs.Trigger>

      <NativeTabs.Trigger name="chat">
        <NativeTabs.Trigger.Icon
          sf={{ default: "bubble.left", selected: "bubble.left.fill" }}
        />
        <NativeTabs.Trigger.Label>Chat</NativeTabs.Trigger.Label>
        {chatUnread > 0 ? (
          <NativeTabs.Trigger.Badge>
            {chatUnread > 9 ? "9+" : String(chatUnread)}
          </NativeTabs.Trigger.Badge>
        ) : null}
      </NativeTabs.Trigger>

      <NativeTabs.Trigger name="more">
        <NativeTabs.Trigger.Icon sf="ellipsis" />
        <NativeTabs.Trigger.Label>More</NativeTabs.Trigger.Label>
      </NativeTabs.Trigger>
    </NativeTabs>
  );
}
