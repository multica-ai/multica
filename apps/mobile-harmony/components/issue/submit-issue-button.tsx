/**
 * HarmonyOS port of apps/mobile/components/issue/submit-issue-button.tsx —
 * ↑ submit icon button rendered in the new-issue screen's header (right
 * slot). Shows a spinner instead of the arrow while the mutation is
 * in-flight, so the user can't double-tap.
 */
import React from "react";
import { ActivityIndicator, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { Icon } from "@/components/ui/icon";
import { useThemeColors } from "@/lib/use-theme-colors";

interface Props {
  disabled: boolean;
  onPress: () => void;
  loading?: boolean;
}

export function SubmitIssueButton({ disabled, onPress, loading }: Props) {
  const c = useThemeColors();
  const interactive = !disabled && !loading;
  return (
    <Pressable
      onPress={interactive ? onPress : undefined}
      hitSlop={8}
      accessibilityLabel="Create issue"
      accessibilityState={{ disabled: !interactive, busy: !!loading }}
      style={({ pressed }) => [
        styles.wrap,
        interactive && pressed ? { opacity: 0.6 } : null,
      ]}
    >
      <View
        style={[
          // size-7 items-center justify-center rounded-full
          styles.circle,
          { backgroundColor: interactive ? c.brand : c.secondary },
        ]}
      >
        {loading ? (
          <ActivityIndicator size="small" color="#ffffff" />
        ) : (
          <Icon
            name="arrow-up"
            size={18}
            color={interactive ? "#ffffff" : DIM}
          />
        )}
      </View>
    </Pressable>
  );
}

// The dimmed arrow on iOS was text-zinc-400 (#a1a1aa) — same value as the
// shared MOBILE_PLACEHOLDER_COLOR dim used across inputs.
const DIM = "#a1a1aa";

const styles = StyleSheet.create({
  wrap: { padding: 2 },
  circle: {
    width: 28,
    height: 28,
    alignItems: "center",
    justifyContent: "center",
    borderRadius: 999,
  },
});
