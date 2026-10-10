/**
 * Bottom tab bar + lazy tab view in pure React Native — mirrors the iOS
 * app's JS `<Tabs>` layout (apps/mobile/app/(app)/[workspace]/(tabs)/
 * _layout.tsx): 49pt bar above the safe-area inset, 24pt glyphs, 11pt
 * labels, brand-colored unread badge (99+ cap applied by the caller), and
 * inactive tabs staying mounted with `display: none` so query caches and
 * scroll positions survive tab switches.
 */
import React, { useMemo, useState } from "react";
import {
  Pressable,
  StyleSheet,
  Text,
  View,
  type StyleProp,
  type ViewStyle,
} from "react-native";
import { useSafeAreaInsets } from "@/lib/safe-area";
import { Icon } from "@/components/ui/icon";
import { useThemeColors } from "@/lib/use-theme-colors";
import { impactAsync, ImpactFeedbackStyle } from "@/lib/haptics";

export const TAB_BAR_HEIGHT = 49;

export type TabDef = {
  key: string;
  label: string;
  icon: string;
  iconFocused: string;
  /** Preformatted badge ("5", "99+") or null/undefined to hide. */
  badge?: string | null;
};

export function TabBar({
  tabs,
  active,
  onChange,
  style,
}: {
  tabs: TabDef[];
  active: string;
  onChange: (key: string) => void;
  style?: StyleProp<ViewStyle>;
}) {
  const c = useThemeColors();
  const insets = useSafeAreaInsets();

  return (
    <View
      style={[
        styles.bar,
        {
          backgroundColor: c.background,
          borderTopColor: c.border,
          // Height must grow with the inset: Yoga heights are border-box, so
          // a fixed 49 with paddingBottom would squeeze the icon row upward
          // through the top hairline instead of reserving space below it.
          height: TAB_BAR_HEIGHT + insets.bottom,
          paddingBottom: insets.bottom,
        },
        style,
      ]}
    >
      {tabs.map((tab) => {
        const focused = tab.key === active;
        const tint = focused ? c.foreground : c.mutedForeground;
        return (
          <Pressable
            key={tab.key}
            accessibilityRole="tab"
            accessibilityState={{ selected: focused }}
            accessibilityLabel={tab.label}
            style={styles.item}
            onPress={() => {
              if (!focused) void impactAsync(ImpactFeedbackStyle.Light);
              onChange(tab.key);
            }}
          >
            <View style={styles.iconWrap}>
              <Icon name={focused ? tab.iconFocused : tab.icon} size={24} color={tint} />
              {tab.badge ? (
                <View style={[styles.badge, { backgroundColor: c.brand }]}>
                  <Text style={styles.badgeText} numberOfLines={1}>
                    {tab.badge}
                  </Text>
                </View>
              ) : null}
            </View>
            <Text style={[styles.label, { color: tint }]} numberOfLines={1}>
              {tab.label}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}

export function TabView({
  tabs,
  active,
  renderTab,
}: {
  tabs: { key: string }[];
  active: string;
  renderTab: (key: string) => React.ReactNode;
}) {
  // Lazy-mount on first visit; visited tabs stay mounted (display:none).
  const [visited, setVisited] = useState<string[]>(() => [active]);
  const keys = useMemo(() => tabs.map((t) => t.key), [tabs]);
  if (!visited.includes(active)) {
    setVisited((current) => [...current, active]);
  }
  return (
    <View style={styles.fill}>
      {keys.map((key) =>
        visited.includes(key) ? (
          <View
            key={key}
            style={[styles.fillAbsolute, key !== active && styles.hidden]}
          >
            {renderTab(key)}
          </View>
        ) : null,
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  bar: {
    flexDirection: "row",
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  item: { flex: 1, alignItems: "center", justifyContent: "center", gap: 2 },
  iconWrap: {},
  badge: {
    position: "absolute",
    top: -4,
    left: 12,
    minWidth: 18,
    height: 18,
    borderRadius: 9,
    paddingHorizontal: 4,
    alignItems: "center",
    justifyContent: "center",
  },
  badgeText: { color: "#fff", fontSize: 10, fontWeight: "600" },
  label: { fontSize: 11 },
  fill: { flex: 1 },
  fillAbsolute: { ...StyleSheet.absoluteFillObject },
  hidden: { display: "none" },
});
