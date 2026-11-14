import type { ReactNode } from "react";
import { Pressable, StyleSheet, View, type StyleProp, type ViewStyle } from "react-native";
import { Icon, type IconName } from "../icons/Icon";
import { radius, useTheme } from "../theme";
import { Txt } from "./Text";

/** Inset grouped card, the way iOS Settings groups rows. */
export function Group({ children, style }: { children: ReactNode; style?: StyleProp<ViewStyle> }) {
  const palette = useTheme();
  return <View style={[styles.group, { backgroundColor: palette.grouped }, style]}>{children}</View>;
}

/** Small grey caption above or below a Group. */
export function GroupNote({ children, above = false }: { children: string; above?: boolean }) {
  return (
    <Txt variant={above ? "metaStrong" : "meta"} tone={above ? "secondary" : "tertiary"} style={above ? styles.above : styles.below}>
      {children}
    </Txt>
  );
}

interface GroupRowProps {
  icon?: IconName;
  leading?: ReactNode;
  label: string;
  detail?: string;
  value?: string;
  right?: ReactNode;
  chevron?: boolean;
  destructive?: boolean;
  last?: boolean;
  onPress?: () => void;
  accessibilityLabel?: string;
}

export function GroupRow({ icon, leading, label, detail, value, right, chevron = false, destructive = false, last = false, onPress, accessibilityLabel }: GroupRowProps) {
  const palette = useTheme();
  const body = (
    <>
      {leading ?? (icon ? <Icon name={icon} size={19} color={destructive ? palette.destructive : palette.label} /> : null)}
      <View style={styles.flex}>
        <Txt variant="rowRegular" tone={destructive ? "destructive" : "primary"} numberOfLines={1}>
          {label}
        </Txt>
        {detail ? (
          <Txt variant="meta" tone="secondary" numberOfLines={2} style={styles.detail}>
            {detail}
          </Txt>
        ) : null}
      </View>
      {value ? (
        <Txt variant="rowRegular" tone="secondary" numberOfLines={1} style={styles.value}>
          {value}
        </Txt>
      ) : null}
      {right}
      {chevron ? <Icon name="ChevronRight" size={17} color={palette.label3} /> : null}
    </>
  );
  const rowStyle = [styles.row, !last && { borderBottomColor: palette.separator, borderBottomWidth: StyleSheet.hairlineWidth }];
  if (!onPress) return <View style={rowStyle}>{body}</View>;
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={accessibilityLabel} onPress={onPress} style={({ pressed }) => [rowStyle, pressed && { opacity: 0.6 }]}>
      {body}
    </Pressable>
  );
}

const styles = StyleSheet.create({
  group: { borderRadius: radius.group, paddingHorizontal: 16 },
  above: { paddingHorizontal: 4, paddingTop: 22, paddingBottom: 8 },
  below: { paddingHorizontal: 4, paddingTop: 8 },
  row: { minHeight: 50, paddingVertical: 12, flexDirection: "row", alignItems: "center", gap: 12 },
  flex: { flex: 1, minWidth: 0 },
  detail: { marginTop: 2 },
  value: { maxWidth: "55%" },
});
