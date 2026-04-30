import type { ReactNode } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { ChevronLeft } from "lucide-react-native";
import { colors, radii, spacing } from "../../theme/tokens";

export function ScreenTitleBar({
  onBack,
  right,
  title,
}: {
  onBack: () => void;
  right?: ReactNode;
  title: string;
}) {
  const insets = useSafeAreaInsets();

  return (
    <View style={[styles.titleBar, { paddingTop: Math.max(insets.top, spacing.sm) }]}>
      <View style={styles.titleBarContent}>
        <Pressable
          accessibilityLabel="Go back"
          accessibilityRole="button"
          onPress={onBack}
          style={({ pressed }) => [
            styles.titleBarBackButton,
            pressed && styles.buttonPressed,
          ]}
        >
          <ChevronLeft color={colors.foreground} size={22} />
        </Pressable>
        <View pointerEvents="none" style={styles.titleBarTitleWrap}>
          <Text numberOfLines={1} style={styles.titleBarTitle}>
            {title}
          </Text>
        </View>
        <View style={styles.titleBarSideSpacer}>{right}</View>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  titleBar: {
    backgroundColor: colors.background,
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    paddingBottom: spacing.sm,
    paddingHorizontal: spacing.md,
  },
  titleBarContent: {
    alignItems: "center",
    flexDirection: "row",
    height: 40,
    justifyContent: "space-between",
    position: "relative",
  },
  titleBarBackButton: {
    alignItems: "center",
    borderRadius: radii.md,
    height: 40,
    justifyContent: "center",
    width: 40,
  },
  titleBarTitleWrap: {
    alignItems: "center",
    bottom: 0,
    justifyContent: "center",
    left: 56,
    position: "absolute",
    right: 56,
    top: 0,
  },
  titleBarTitle: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "600",
    maxWidth: "100%",
    textAlign: "center",
  },
  titleBarSideSpacer: {
    alignItems: "flex-end",
    minWidth: 40,
  },
  buttonPressed: {
    opacity: 0.7,
  },
});
