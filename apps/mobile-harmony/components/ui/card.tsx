/**
 * HarmonyOS port of apps/mobile/components/ui/card.tsx. The iOS version
 * styles via NativeWind classes ("rounded-xl border border-border bg-card
 * p-4", CardPressable adds active:bg-secondary); NativeWind className
 * styling regressed on RNOH 0.82, so the same values live in a StyleSheet
 * table (rounded-xl=12, p-4=16). Continuous corner curves and the
 * pressed-card background are preserved.
 */
import React from "react";
import {
  Pressable,
  StyleSheet,
  View,
  type PressableProps,
  type PressableStateCallbackType,
  type StyleProp,
  type ViewProps,
  type ViewStyle,
} from "react-native";
import { continuousCorners } from "@/lib/radius";
import { useThemeColors } from "@/lib/use-theme-colors";

const buildStyles = (c: ReturnType<typeof useThemeColors>) =>
  StyleSheet.create({
    card: {
      borderRadius: 12,
      borderWidth: 1,
      borderColor: c.border,
      backgroundColor: c.card,
      padding: 16,
      ...continuousCorners,
    },
    pressed: { backgroundColor: c.secondary },
  });

const Card = React.forwardRef<View, ViewProps & { style?: StyleProp<ViewStyle> }>(
  ({ style, ...props }, ref) => {
    const c = useThemeColors();
    return <View ref={ref} style={[buildStyles(c).card, style]} {...props} />;
  },
);
Card.displayName = "Card";

const CardPressable = React.forwardRef<
  View,
  PressableProps & { children?: React.ReactNode }
>(({ children, style, ...props }, ref) => {
  const c = useThemeColors();
  const table = buildStyles(c);
  const resolveStyle = (
    state: PressableStateCallbackType,
  ): StyleProp<ViewStyle> => [
    table.card,
    typeof style === "function" ? style(state) : style,
    state.pressed ? table.pressed : null,
  ];
  return (
    <Pressable ref={ref as React.Ref<View>} style={resolveStyle} {...props}>
      {children as React.ReactNode}
    </Pressable>
  );
});
CardPressable.displayName = "CardPressable";

export { Card, CardPressable };
