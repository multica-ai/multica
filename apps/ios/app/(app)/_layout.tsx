import { Tabs } from "expo-router";
import { View, Text, StyleSheet } from "react-native";
import { WorkspaceProvider } from "@/lib/providers";
import { colors } from "@/lib/theme";

function TabIcon({ name, focused }: { name: string; focused: boolean }) {
  const icons: Record<string, string> = {
    Home: "⌂",
    Issues: "☰",
    Inbox: "✉",
    More: "⋯",
    Settings: "⚙",
  };
  return (
    <View style={styles.tabIcon}>
      <Text
        style={[
          styles.tabIconText,
          { color: focused ? colors.foreground : colors.mutedForeground },
        ]}
      >
        {icons[name] ?? "•"}
      </Text>
    </View>
  );
}

export default function AppLayout() {
  return (
    <WorkspaceProvider>
      <Tabs
        screenOptions={{
          headerShown: false,
          tabBarStyle: styles.tabBar,
          tabBarActiveTintColor: colors.foreground,
          tabBarInactiveTintColor: colors.mutedForeground,
          tabBarLabelStyle: styles.tabLabel,
        }}
      >
        <Tabs.Screen
          name="(home)"
          options={{
            title: "Home",
            tabBarIcon: ({ focused }) => (
              <TabIcon name="Home" focused={focused} />
            ),
          }}
        />
        <Tabs.Screen
          name="(issues)"
          options={{
            title: "Issues",
            tabBarIcon: ({ focused }) => (
              <TabIcon name="Issues" focused={focused} />
            ),
          }}
        />
        <Tabs.Screen
          name="(inbox)"
          options={{
            title: "Inbox",
            tabBarIcon: ({ focused }) => (
              <TabIcon name="Inbox" focused={focused} />
            ),
          }}
        />
        <Tabs.Screen
          name="(more)"
          options={{
            title: "More",
            tabBarIcon: ({ focused }) => (
              <TabIcon name="More" focused={focused} />
            ),
          }}
        />
        <Tabs.Screen
          name="(projects)"
          options={{
            href: null,
          }}
        />
        <Tabs.Screen name="(agents)" options={{ href: null }} />
        <Tabs.Screen
          name="(settings)"
          options={{
            href: null,
          }}
        />
      </Tabs>
    </WorkspaceProvider>
  );
}

const styles = StyleSheet.create({
  tabBar: {
    backgroundColor: colors.background,
    borderTopColor: colors.border,
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingTop: 4,
  },
  tabLabel: {
    fontSize: 11,
    fontWeight: "500",
  },
  tabIcon: {
    alignItems: "center",
    justifyContent: "center",
  },
  tabIconText: {
    fontSize: 20,
  },
});
