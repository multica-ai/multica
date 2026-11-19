import type { ReactNode } from "react";
import { Pressable, StyleSheet, View } from "react-native";
import { Txt } from "./Text";

interface SectionTitleProps {
  children: string;
  /** Count or summary on the right. */
  right?: string;
  /** A text action on the right, e.g. "Add". */
  action?: { label: string; onPress: () => void };
  accessory?: ReactNode;
}

/** Small caption over a block of a detail page. */
export function SectionTitle({ children, right, action, accessory }: SectionTitleProps) {
  return (
    <View style={styles.row}>
      <Txt variant="metaStrong" tone="secondary" accessibilityRole="header" style={styles.flex}>
        {children}
      </Txt>
      {right ? (
        <Txt variant="meta" tone="tertiary">
          {right}
        </Txt>
      ) : null}
      {accessory}
      {action ? (
        <Pressable accessibilityRole="button" hitSlop={10} onPress={action.onPress}>
          <Txt variant="metaStrong" tone="accent">
            {action.label}
          </Txt>
        </Pressable>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  row: { flexDirection: "row", alignItems: "center", gap: 12, marginBottom: 10 },
  flex: { flex: 1 },
});
