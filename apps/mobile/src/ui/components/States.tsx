import { ActivityIndicator, Pressable, StyleSheet, View } from "react-native";
import { useTheme } from "../theme";
import { Txt } from "./Text";

export function Loading() {
  const palette = useTheme();
  return (
    <View style={styles.center}>
      <ActivityIndicator color={palette.label2} />
    </View>
  );
}

export function Empty({ title, detail }: { title: string; detail?: string }) {
  return (
    <View style={styles.center}>
      <Txt variant="calloutStrong" tone="secondary">
        {title}
      </Txt>
      {detail ? (
        <Txt variant="meta" tone="tertiary" style={styles.detail}>
          {detail}
        </Txt>
      ) : null}
    </View>
  );
}

export function Failure({ title, retryLabel, onRetry }: { title: string; retryLabel: string; onRetry: () => void }) {
  return (
    <View style={styles.center}>
      <Txt variant="calloutStrong" tone="secondary">
        {title}
      </Txt>
      <Pressable accessibilityRole="button" onPress={onRetry} hitSlop={8}>
        <Txt variant="callout" tone="accent" style={styles.detail}>
          {retryLabel}
        </Txt>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  center: { flex: 1, alignItems: "center", justifyContent: "center", padding: 32, paddingBottom: 120, minHeight: 240 },
  detail: { marginTop: 6, textAlign: "center" },
});
