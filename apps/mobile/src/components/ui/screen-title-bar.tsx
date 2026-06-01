import { useRef, type ReactNode } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useTranslation } from "react-i18next";
import { ChevronLeft } from "lucide-react-native";
import { colors, radii, spacing } from "../../theme/tokens";

const TITLE_DOUBLE_PRESS_DELAY_MS = 300;

export function ScreenTitleBar({
  onBack,
  onTitleDoublePress,
  right,
  title,
}: {
  onBack: () => void;
  onTitleDoublePress?: () => void;
  right?: ReactNode;
  title: string;
}) {
  const insets = useSafeAreaInsets();
  const { t } = useTranslation();
  const lastTitlePressAtRef = useRef(0);

  const handleTitlePress = () => {
    if (!onTitleDoublePress) return;
    const now = Date.now();
    if (now - lastTitlePressAtRef.current <= TITLE_DOUBLE_PRESS_DELAY_MS) {
      lastTitlePressAtRef.current = 0;
      onTitleDoublePress();
      return;
    }
    lastTitlePressAtRef.current = now;
  };

  const titleContent = (
    <Text
      adjustsFontSizeToFit
      minimumFontScale={0.82}
      numberOfLines={1}
      style={styles.titleBarTitle}
    >
      {title}
    </Text>
  );

  return (
    <View style={[styles.titleBar, { paddingTop: Math.max(insets.top, spacing.sm) }]}>
      <View style={styles.titleBarContent}>
        <Pressable
          accessibilityLabel={t("common.go_back")}
          accessibilityRole="button"
          onPress={onBack}
          style={({ pressed }) => [
            styles.titleBarBackButton,
            pressed && styles.buttonPressed,
          ]}
        >
          <ChevronLeft color={colors.foreground} size={22} />
        </Pressable>
        {onTitleDoublePress ? (
          <Pressable
            accessibilityLabel={title}
            accessibilityRole="button"
            onPress={handleTitlePress}
            style={({ pressed }) => [
              styles.titleBarTitleWrap,
              pressed && styles.buttonPressed,
            ]}
          >
            {titleContent}
          </Pressable>
        ) : (
          <View pointerEvents="none" style={styles.titleBarTitleWrap}>
            {titleContent}
          </View>
        )}
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
    lineHeight: 21,
    maxWidth: "100%",
    textAlign: "center",
    width: "100%",
  },
  titleBarSideSpacer: {
    alignItems: "flex-end",
    minWidth: 40,
  },
  buttonPressed: {
    opacity: 0.7,
  },
});
