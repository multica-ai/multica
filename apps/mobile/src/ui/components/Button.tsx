import { ActivityIndicator, Pressable, StyleSheet } from "react-native";
import { useTheme } from "../theme";
import { Txt } from "./Text";

interface ButtonProps {
  label: string;
  onPress: () => void;
  disabled?: boolean;
  busy?: boolean;
  kind?: "primary" | "plain";
}

/** Full-width action: solid ink for the primary step, bare text for the alternative. */
export function Button({ label, onPress, disabled = false, busy = false, kind = "primary" }: ButtonProps) {
  const palette = useTheme();
  const primary = kind === "primary";
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityState={{ disabled: disabled || busy, busy }}
      onPress={onPress}
      disabled={disabled || busy}
      style={({ pressed }) => [
        styles.button,
        primary && { backgroundColor: palette.label },
        { opacity: disabled ? 0.35 : pressed ? 0.7 : 1 },
      ]}
    >
      {busy ? (
        <ActivityIndicator color={primary ? palette.background : palette.label2} />
      ) : (
        <Txt variant="bodyStrong" style={{ color: primary ? palette.background : palette.label2 }}>
          {label}
        </Txt>
      )}
    </Pressable>
  );
}

const styles = StyleSheet.create({
  button: { height: 52, borderRadius: 26, alignItems: "center", justifyContent: "center", alignSelf: "stretch" },
});
