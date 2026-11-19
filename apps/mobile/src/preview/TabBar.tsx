import { Pressable, StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { BottomFade } from "../ui/components/BottomFade";
import { FloatingButton } from "../ui/components/Floating";
import { Icon, type IconName } from "../ui/icons/Icon";
import { useStrings } from "../ui/strings";
import { floatShadow, useTheme } from "../ui/theme";

export type TabKey = "inbox" | "myIssues" | "agents" | "more";

const TABS: readonly { key: TabKey; icon: IconName }[] = [
  { key: "inbox", icon: "Inbox" },
  { key: "myIssues", icon: "CircleUser" },
  { key: "agents", icon: "Bot" },
  { key: "more", icon: "ChevronsUpDown" },
];

/**
 * Stands in for the system tab bar, which the app uses and the browser cannot draw: the three tabs,
 * the menu tab, and search on its own.
 */
export function TabBar({ active }: { active: TabKey }) {
  const palette = useTheme();
  const strings = useStrings();
  const insets = useSafeAreaInsets();
  return (
    <>
      <BottomFade height={insets.bottom + 110} />
      <View style={[styles.row, { bottom: Math.max(insets.bottom - 8, 12) }]} pointerEvents="box-none">
        <View style={[styles.bar, { backgroundColor: palette.float }, floatShadow(palette)]} accessibilityRole="tablist">
          {TABS.map((tab) => {
            const on = tab.key === active;
            return (
              <Pressable
                key={tab.key}
                accessibilityRole="tab"
                accessibilityLabel={strings.tabs[tab.key]}
                accessibilityState={{ selected: on }}
                style={[styles.tab, on && { backgroundColor: palette.fill }]}
              >
                <Icon name={tab.icon} size={23} color={palette.label} />
                {tab.key === "inbox" ? <View style={[styles.dot, { backgroundColor: palette.accent, borderColor: palette.float }]} /> : null}
              </Pressable>
            );
          })}
        </View>
        <FloatingButton icon="Search" label={strings.search.title} size={56} onPress={() => undefined} />
      </View>
    </>
  );
}

const styles = StyleSheet.create({
  row: { position: "absolute", left: 16, right: 16, flexDirection: "row", alignItems: "center", gap: 10 },
  bar: { flex: 1, height: 56, borderRadius: 28, flexDirection: "row", alignItems: "center", justifyContent: "space-around", paddingHorizontal: 6 },
  tab: { width: 60, height: 44, borderRadius: 22, alignItems: "center", justifyContent: "center" },
  dot: { position: "absolute", top: 7, right: 15, width: 10, height: 10, borderRadius: 5, borderWidth: 2 },
});
