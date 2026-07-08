/**
 * Bottom tab bar — JS `<Tabs>` from expo-router (react-navigation under the
 * hood). All four tabs, including More, are plain navigation targets;
 * More pushes to `(tabs)/more.tsx`, a real page (previously this tab
 * intercepted tabPress to open a dropdown popover instead — see git
 * history if you need that shape again).
 *
 * Active / inactive tint colors are derived from the current colour
 * scheme via THEME so dark mode picks contrasting values automatically.
 */
import { Tabs } from "expo-router";
import { Image } from "expo-image";
import { Platform, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useTranslation } from "react-i18next";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import {
  useInboxUnreadCount,
  useChatUnreadSessionCount,
} from "@/lib/unread-counts";

// Only override backgroundColor — @react-navigation/elements Badge internally
// sets borderRadius = size/2, height = size, minWidth = size, so a single
// character renders as a perfect circle. Overriding minWidth/fontSize here
// breaks that geometry. Text color is auto-derived from backgroundColor
// luminance by Badge itself (white on brand blue).
const BADGE_STYLE = {
  backgroundColor: THEME.light.brand,
};

// SF Symbols (the `sf:` source prefix expo-image resolves) only render on
// iOS — Android silently shows nothing for that source string. Route to
// Ionicons (already used elsewhere in this app, e.g. settings.tsx's
// chevron) on Android instead of losing tab icons there entirely.
function TabIcon({
  sfSymbol,
  ionicon,
  color,
  size,
}: {
  sfSymbol: string;
  ionicon: keyof typeof Ionicons.glyphMap;
  color: string;
  size: number;
}) {
  if (Platform.OS === "ios") {
    return (
      <Image
        source={sfSymbol}
        tintColor={color}
        style={{ width: size, height: size }}
      />
    );
  }
  return <Ionicons name={ionicon} size={size} color={color} />;
}

export default function TabsLayout() {
  const { colorScheme } = useColorScheme();
  const t = THEME[colorScheme];
  const { t: tInbox } = useTranslation("inbox");
  const { t: tCommon } = useTranslation("common");

  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const inboxUnread = useInboxUnreadCount(wsId);
  const chatUnread = useChatUnreadSessionCount(wsId);

  // Truncation aligned with web: inbox 99+, chat 9+ (matches sidebar +
  // ChatFab respectively). `undefined` makes React Navigation hide the
  // badge, so zero-count is a free no-op.
  const inboxBadge =
    inboxUnread > 0 ? (inboxUnread > 99 ? "99+" : String(inboxUnread)) : undefined;
  const chatBadge =
    chatUnread > 0 ? (chatUnread > 9 ? "9+" : String(chatUnread)) : undefined;

  return (
    <View style={{ flex: 1 }}>
      <Tabs
        screenOptions={{
          headerShown: false,
          tabBarActiveTintColor: t.foreground,
          tabBarInactiveTintColor: t.mutedForeground,
          tabBarStyle: { backgroundColor: t.background },
          tabBarLabelStyle: { fontSize: 11 },
        }}
      >
        <Tabs.Screen
          name="inbox"
          options={{
            title: tInbox("tab_title"),
            tabBarBadge: inboxBadge,
            tabBarBadgeStyle: BADGE_STYLE,
            tabBarIcon: ({ color, size, focused }) => (
              <TabIcon
                sfSymbol={focused ? "sf:tray.fill" : "sf:tray"}
                ionicon={focused ? "file-tray" : "file-tray-outline"}
                color={color}
                size={size}
              />
            ),
          }}
        />
        <Tabs.Screen
          name="my-issues"
          options={{
            title: tCommon("tabs.my_issues"),
            tabBarIcon: ({ color, size, focused }) => (
              <TabIcon
                sfSymbol={focused ? "sf:checklist" : "sf:checklist.unchecked"}
                ionicon={focused ? "checkbox" : "checkbox-outline"}
                color={color}
                size={size}
              />
            ),
          }}
        />
        <Tabs.Screen
          name="chat"
          options={{
            title: tCommon("tabs.chat"),
            tabBarBadge: chatBadge,
            tabBarBadgeStyle: BADGE_STYLE,
            tabBarIcon: ({ color, size, focused }) => (
              <TabIcon
                sfSymbol={focused ? "sf:bubble.left.fill" : "sf:bubble.left"}
                ionicon={focused ? "chatbubble" : "chatbubble-outline"}
                color={color}
                size={size}
              />
            ),
          }}
        />
        <Tabs.Screen
          name="more"
          options={{
            title: tCommon("tabs.more"),
            tabBarIcon: ({ color, size }) => (
              <TabIcon
                sfSymbol="sf:ellipsis"
                ionicon="ellipsis-horizontal"
                color={color}
                size={size}
              />
            ),
          }}
        />
      </Tabs>
    </View>
  );
}
