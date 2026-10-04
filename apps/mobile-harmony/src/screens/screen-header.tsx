/**
 * Navigation chrome for screens pushed onto the root StackNavigator. The
 * hand-rolled navigator has no native bar (unlike the iOS app's Stack
 * headers), so pushed screens render this row themselves — back chevron +
 * "Back" label on the left, single-line title in the middle, action slot on
 * the right. Mirrors the iOS options used by the ported screens
 * (title + headerBackTitle: "Back" + headerRight).
 *
 * Self-handles the top safe area via @/lib/safe-area (AGENTS.md — the tpl
 * safe-area JS component layer is not wired on this matrix).
 */
import { Pressable, StyleSheet, View } from "react-native";
import type { ReactNode } from "react";
import { SafeAreaView } from "@/lib/safe-area";
import { Icon } from "@/components/ui/icon";
import { Text } from "@/components/ui/text";
import { useThemeColors } from "@/lib/use-theme-colors";

interface Props {
  title?: string;
  onBack?: () => void;
  /** Back label; the iOS nav bar shows "Back" (headerBackTitle) by default. */
  backLabel?: string;
  right?: ReactNode;
}

export function ScreenHeader({ title, onBack, backLabel = "Back", right }: Props) {
  const c = useThemeColors();
  return (
    <SafeAreaView edges={["top"]} style={styles.safe}>
      <View style={[styles.row, { borderBottomColor: c.border }]}>
        <View style={styles.left}>
          {onBack ? (
            <Pressable onPress={onBack} hitSlop={8} style={styles.back}>
              <Icon name="chevron-back" size={22} color={c.brand} />
              <Text style={[styles.backLabel, { color: c.brand }]}>
                {backLabel}
              </Text>
            </Pressable>
          ) : null}
        </View>
        <View style={styles.center}>
          {title ? (
            <Text
              style={[styles.title, { color: c.foreground }]}
              numberOfLines={1}
            >
              {title}
            </Text>
          ) : null}
        </View>
        <View style={styles.right}>{right}</View>
      </View>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  safe: { backgroundColor: "transparent" },
  row: {
    flexDirection: "row",
    alignItems: "center",
    height: 48,
    borderBottomWidth: 1,
    paddingHorizontal: 8,
  },
  left: { flexDirection: "row", alignItems: "center", minWidth: 64 },
  back: { flexDirection: "row", alignItems: "center" },
  // text-base
  backLabel: { fontSize: 16 },
  center: { flex: 1, alignItems: "center", justifyContent: "center" },
  // iOS nav title: 17pt semibold
  title: { fontSize: 17, fontWeight: "600" },
  right: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "flex-end",
    minWidth: 64,
    gap: 4,
  },
});
