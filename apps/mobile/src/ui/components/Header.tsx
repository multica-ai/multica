import type { ReactNode } from "react";
import { StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { FloatingButton } from "./Floating";
import { Txt } from "./Text";

/** Top-level screen header: compact title on the left, actions on the right. */
export function TitleHeader({ title, right }: { title: string; right?: ReactNode }) {
  const insets = useSafeAreaInsets();
  return (
    <View style={[styles.bar, { paddingTop: insets.top }]}>
      <View style={[styles.row, styles.title]}>
        <Txt variant="title" accessibilityRole="header" numberOfLines={1} style={styles.flex}>
          {title}
        </Txt>
        {right}
      </View>
    </View>
  );
}

/** Pushed screen header: floating back button, optional centered title, actions. */
export function PushHeader({ title, onBack, backLabel, right }: { title?: string; onBack: () => void; backLabel: string; right?: ReactNode }) {
  const insets = useSafeAreaInsets();
  return (
    <View style={[styles.bar, { paddingTop: insets.top }]}>
      <View style={[styles.row, styles.push]}>
        <FloatingButton icon="ChevronLeft" label={backLabel} onPress={onBack} />
        <View style={styles.center}>
          {title ? (
            <Txt variant="navTitle" numberOfLines={1}>
              {title}
            </Txt>
          ) : null}
        </View>
        {right ?? <View style={styles.spacer} />}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  bar: { zIndex: 10 },
  row: { height: 56, flexDirection: "row", alignItems: "center", gap: 12 },
  title: { paddingLeft: 20, paddingRight: 16 },
  push: { paddingHorizontal: 16 },
  flex: { flex: 1 },
  center: { flex: 1, alignItems: "center" },
  spacer: { width: 44 },
});
