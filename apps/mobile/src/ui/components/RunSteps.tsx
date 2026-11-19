import { ActivityIndicator, StyleSheet, View } from "react-native";
import type { RunStep } from "../model/steps";
import { Icon } from "../icons/Icon";
import { useTheme } from "../theme";
import { Txt } from "./Text";

/** Collapsed tool calls of an agent run: a check per finished step, a spinner on the live one. */
export function RunSteps({ steps, more }: { steps: readonly RunStep[]; more?: string }) {
  const palette = useTheme();
  return (
    <View style={styles.list}>
      {more ? (
        <Txt variant="meta" tone="tertiary" style={styles.more}>
          {more}
        </Txt>
      ) : null}
      {steps.map((step) => (
        <View key={step.id} style={styles.step}>
          <View style={styles.mark}>{step.done ? <Icon name="Check" size={14} color={palette.label3} /> : <ActivityIndicator size="small" color={palette.warning} style={styles.spinner} />}</View>
          <Txt variant="meta" tone={step.done ? "secondary" : "primary"} numberOfLines={1} style={styles.flex}>
            {step.label}
          </Txt>
        </View>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  list: { gap: 6 },
  more: { marginLeft: 24 },
  step: { flexDirection: "row", alignItems: "center", gap: 8 },
  mark: { width: 16, alignItems: "center" },
  spinner: { transform: [{ scale: 0.7 }] },
  flex: { flex: 1 },
});
