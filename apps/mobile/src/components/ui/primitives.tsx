import type { PropsWithChildren } from "react";
import {
  ActivityIndicator,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  View,
  type TextInputProps,
  type ViewStyle,
} from "react-native";
import { colors, radii, spacing } from "../../theme/tokens";

export function Screen({
  children,
  padded = true,
}: PropsWithChildren<{ padded?: boolean }>) {
  return <View style={[styles.screen, padded && styles.padded]}>{children}</View>;
}

export function Heading({ children }: PropsWithChildren) {
  return <Text style={styles.heading}>{children}</Text>;
}

export function MutedText({ children }: PropsWithChildren) {
  return <Text style={styles.muted}>{children}</Text>;
}

export function Field(props: TextInputProps) {
  return (
    <TextInput
      placeholderTextColor={colors.mutedForeground}
      {...props}
      style={[styles.field, props.style]}
    />
  );
}

export function Button({
  children,
  onPress,
  disabled,
  variant = "primary",
  style,
}: PropsWithChildren<{
  onPress?: () => void;
  disabled?: boolean;
  variant?: "primary" | "secondary" | "ghost";
  style?: ViewStyle;
}>) {
  return (
    <Pressable
      accessibilityRole="button"
      disabled={disabled}
      onPress={onPress}
      style={({ pressed }) => [
        styles.button,
        variant === "primary" && styles.buttonPrimary,
        variant === "secondary" && styles.buttonSecondary,
        variant === "ghost" && styles.buttonGhost,
        disabled && styles.buttonDisabled,
        pressed && !disabled && styles.buttonPressed,
        style,
      ]}
    >
      <Text
        style={[
          styles.buttonText,
          variant === "primary" && styles.buttonPrimaryText,
        ]}
      >
        {children}
      </Text>
    </Pressable>
  );
}

export function EmptyState({ title, detail }: { title: string; detail?: string }) {
  return (
    <View style={styles.empty}>
      <Text style={styles.emptyTitle}>{title}</Text>
      {detail ? <Text style={styles.muted}>{detail}</Text> : null}
    </View>
  );
}

export function LoadingState() {
  return (
    <View style={styles.empty}>
      <ActivityIndicator color={colors.foreground} />
    </View>
  );
}

export const styles = StyleSheet.create({
  screen: {
    backgroundColor: colors.background,
    flex: 1,
  },
  padded: {
    padding: spacing.lg,
  },
  heading: {
    color: colors.foreground,
    fontSize: 20,
    fontWeight: "500",
  },
  muted: {
    color: colors.mutedForeground,
    fontSize: 14,
    lineHeight: 20,
  },
  field: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    color: colors.foreground,
    fontSize: 16,
    minHeight: 48,
    paddingHorizontal: spacing.md,
  },
  button: {
    alignItems: "center",
    borderRadius: radii.md,
    justifyContent: "center",
    minHeight: 44,
    paddingHorizontal: spacing.lg,
  },
  buttonPrimary: {
    backgroundColor: colors.primary,
  },
  buttonSecondary: {
    backgroundColor: colors.muted,
  },
  buttonGhost: {
    backgroundColor: "transparent",
  },
  buttonDisabled: {
    opacity: 0.45,
  },
  buttonPressed: {
    opacity: 0.8,
  },
  buttonText: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
  },
  buttonPrimaryText: {
    color: colors.primaryForeground,
  },
  empty: {
    alignItems: "center",
    flex: 1,
    gap: spacing.sm,
    justifyContent: "center",
    padding: spacing.xl,
  },
  emptyTitle: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "500",
  },
});
