/**
 * Bottom tab bar — JS `<Tabs>` from expo-router (react-navigation under the
 * hood). We tried NativeTabs first but its `canPreventDefault: false`
 * constraint makes "tap More → open something" impossible. JS Tabs
 * supports `listeners.tabPress + e.preventDefault()`, the canonical RN
 * pattern for tab-as-action.
 *
 * The "More" tab is **not a navigation target** — its press opens a
 * DropdownMenu popover anchored above the tab. The popover is rendered
 * by `<MoreTabDropdownAnchor />` as a sibling of `<Tabs>`, NOT as a
 * `tabBarButton` replacement: keeping the real tab button intact means
 * the icon + "More" label render identically to the other three tabs.
 * We just open the dropdown imperatively from `listeners.tabPress` via
 * the exposed `TriggerRef.open()`.
 *
 * The stub (tabs)/more.tsx file still exists only because expo-router
 * requires every Tabs.Screen to have a backing route file — the press
 * is preventDefault'd so we never actually navigate to it.
 *
 * Active / inactive tint colors are derived from the current colour
 * scheme via THEME so dark mode picks contrasting values automatically.
 */
import { useRef } from "react";
import { Tabs } from "expo-router";
import { Image } from "expo-image";
import { Platform, View } from "react-native";
import type { TriggerRef } from "@rn-primitives/dropdown-menu";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import {
  useInboxUnreadCount,
  useChatUnreadMessageCount,
} from "@/lib/unread-counts";
import { MoreTabDropdownAnchor } from "@/components/nav/more-tab-dropdown";
import { TabletTabBar } from "@/components/nav/tablet-tab-bar";

// Only override backgroundColor — @react-navigation/elements Badge internally
// sets borderRadius = size/2, height = size, minWidth = size, so a single
// character renders as a perfect circle. Overriding minWidth/fontSize here
// breaks that geometry. Text color is auto-derived from backgroundColor
// luminance by Badge itself (white on brand blue).
const BADGE_STYLE = {
  backgroundColor: THEME.light.brand,
};

export default function TabsLayout() {
  const { colorScheme } = useColorScheme();
  const t = THEME[colorScheme];

  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const inboxUnread = useInboxUnreadCount(wsId);
  const chatUnread = useChatUnreadMessageCount(wsId);

  // Truncation aligned with web's sidebar badges: 99+ for both. `undefined`
  // makes React Navigation hide the badge, so zero-count is a free no-op.
  const inboxBadge =
    inboxUnread > 0 ? (inboxUnread > 99 ? "99+" : String(inboxUnread)) : undefined;
  const chatBadge =
    chatUnread > 0 ? (chatUnread > 99 ? "99+" : String(chatUnread)) : undefined;
  const isIPad = Platform.OS === "ios" && Platform.isPad;

  // Imperative handle into the More tab's dropdown — listeners.tabPress
  // calls .open(); the @rn-primitives Trigger measures itself inside
  // open() so the popover anchors to MoreTabDropdownAnchor's rect.
  const moreTriggerRef = useRef<TriggerRef>(null);

  return (
    <View style={{ flex: 1 }}>
      <Tabs
        tabBar={
          isIPad
            ? (props) => (
                <TabletTabBar
                  state={props.state}
                  navigation={props.navigation}
                  inboxBadge={inboxBadge}
                />
              )
            : undefined
        }
        screenOptions={{
          headerShown: false,
          tabBarPosition: isIPad ? "left" : "bottom",
          tabBarActiveTintColor: t.foreground,
          tabBarInactiveTintColor: t.mutedForeground,
          tabBarStyle: { backgroundColor: t.background },
          tabBarLabelStyle: { fontSize: 11 },
        }}
      >
        <Tabs.Screen
          name="inbox"
          options={{
            title: "Inbox",
            tabBarBadge: inboxBadge,
            tabBarBadgeStyle: BADGE_STYLE,
            tabBarIcon: ({ color, size, focused }) => (
              <Image
                source={focused ? "sf:tray.fill" : "sf:tray"}
                tintColor={color}
                style={{ width: size, height: size }}
              />
            ),
          }}
        />
        <Tabs.Screen
          name="my-issues"
          options={{
            title: "My Issues",
            tabBarButton: isIPad ? () => null : undefined,
            tabBarIcon: ({ color, size, focused }) => (
              <Image
                source={focused ? "sf:checklist" : "sf:checklist.unchecked"}
                tintColor={color}
                style={{ width: size, height: size }}
              />
            ),
          }}
        />
        <Tabs.Screen
          name="chat"
          options={{
            title: "Chat",
            tabBarButton: isIPad ? () => null : undefined,
            tabBarBadge: chatBadge,
            tabBarBadgeStyle: BADGE_STYLE,
            tabBarIcon: ({ color, size, focused }) => (
              <Image
                source={focused ? "sf:bubble.left.fill" : "sf:bubble.left"}
                tintColor={color}
                style={{ width: size, height: size }}
              />
            ),
          }}
        />
        <Tabs.Screen
          name="more"
          options={{
            title: "More",
            tabBarButton: isIPad ? () => null : undefined,
            tabBarIcon: ({ color, size }) => (
              <Image
                source="sf:ellipsis"
                tintColor={color}
                style={{ width: size, height: size }}
              />
            ),
          }}
          listeners={() => ({
            tabPress: (e) => {
              // Don't navigate to the (stub) /more screen — open the
              // dropdown popover instead. The trigger is invisible and
              // mounted in MoreTabDropdownAnchor below; ref.open() also
              // measures its rect so the popover anchors correctly.
              e.preventDefault();
              moreTriggerRef.current?.open();
            },
          })}
        />
        <Tabs.Screen
          name="issues"
          options={{
            title: "Issues",
            tabBarButton: isIPad ? undefined : () => null,
          }}
        />
        <Tabs.Screen
          name="projects"
          options={{
            title: "Projects",
            tabBarButton: isIPad ? undefined : () => null,
          }}
        />
        <Tabs.Screen
          name="agents"
          options={{
            title: "Agents",
            tabBarButton: isIPad ? undefined : () => null,
          }}
        />
        <Tabs.Screen
          name="settings"
          options={{
            title: "Settings",
            tabBarButton: isIPad ? undefined : () => null,
          }}
        />
      </Tabs>

      {!isIPad ? <MoreTabDropdownAnchor triggerRef={moreTriggerRef} /> : null}
    </View>
  );
}
