import type { ReactNode } from "react";
import { Pressable, StyleSheet, View, type StyleProp, type ViewStyle } from "react-native";
import { Icon, type IconName } from "../icons/Icon";
import { floatShadow, useTheme } from "../theme";

interface FloatingButtonProps {
  icon: IconName;
  label: string;
  onPress?: () => void;
  size?: 44 | 56;
}

/** Round floating control: back, more, search. */
export function FloatingButton({ icon, label, onPress, size = 44 }: FloatingButtonProps) {
  const palette = useTheme();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      onPress={onPress}
      hitSlop={6}
      style={({ pressed }) => [
        styles.circle,
        { width: size, height: size, borderRadius: size / 2, backgroundColor: palette.float, opacity: pressed ? 0.7 : 1 },
        floatShadow(palette),
      ]}
    >
      <Icon name={icon} size={size === 56 ? 22 : 20} color={palette.label} />
    </Pressable>
  );
}

interface CapsuleAction {
  icon: IconName;
  label: string;
  onPress?: () => void;
}

/** A group of icon actions in one floating capsule, top right of a screen. */
export function Capsule({ actions }: { actions: readonly CapsuleAction[] }) {
  const palette = useTheme();
  return (
    <View style={[styles.capsule, { backgroundColor: palette.float }, floatShadow(palette)]}>
      {actions.map((action) => (
        <Pressable
          key={action.label}
          accessibilityRole="button"
          accessibilityLabel={action.label}
          onPress={action.onPress}
          style={({ pressed }) => [styles.capsuleItem, { opacity: pressed ? 0.5 : 1 }]}
        >
          <Icon name={action.icon} size={20} color={palette.label} />
        </Pressable>
      ))}
    </View>
  );
}

/** Floating surface for custom content (composer, menus). */
export function FloatingSurface({ children, style }: { children: ReactNode; style?: StyleProp<ViewStyle> }) {
  const palette = useTheme();
  return <View style={[{ backgroundColor: palette.float }, floatShadow(palette), style]}>{children}</View>;
}

const styles = StyleSheet.create({
  circle: { alignItems: "center", justifyContent: "center" },
  capsule: { height: 44, borderRadius: 22, flexDirection: "row", alignItems: "center", paddingHorizontal: 4 },
  capsuleItem: { width: 40, height: 36, alignItems: "center", justifyContent: "center" },
});
