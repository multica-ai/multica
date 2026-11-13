import { Pressable, StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Icon, type IconName } from "../icons/Icon";
import { floatShadow, useTheme } from "../theme";
import { BottomFade } from "./BottomFade";
import { FloatingButton } from "./Floating";

export interface TabItem {
  key: string;
  icon: IconName;
  label: string;
  dot?: boolean;
}

interface TabBarProps {
  tabs: readonly TabItem[];
  active: string;
  onSelect: (key: string) => void;
  onSearch: () => void;
  searchLabel: string;
}

/** Icon-only floating capsule plus a separate search button. */
export function TabBar({ tabs, active, onSelect, onSearch, searchLabel }: TabBarProps) {
  const palette = useTheme();
  const insets = useSafeAreaInsets();
  return (
    <>
      <BottomFade height={insets.bottom + 110} />
      <View style={[styles.row, { bottom: Math.max(insets.bottom - 8, 12) }]} pointerEvents="box-none">
        <View style={[styles.bar, { backgroundColor: palette.float }, floatShadow(palette)]} accessibilityRole="tablist">
          {tabs.map((tab) => {
            const on = tab.key === active;
            return (
              <Pressable
                key={tab.key}
                accessibilityRole="tab"
                accessibilityLabel={tab.label}
                accessibilityState={{ selected: on }}
                onPress={() => onSelect(tab.key)}
                style={[styles.tab, on && { backgroundColor: palette.fill }]}
              >
                <Icon name={tab.icon} size={23} color={palette.label} />
                {tab.dot ? <View style={[styles.dot, { backgroundColor: palette.accent, borderColor: palette.float }]} /> : null}
              </Pressable>
            );
          })}
        </View>
        <FloatingButton icon="Search" label={searchLabel} size={56} onPress={onSearch} />
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
