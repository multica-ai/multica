import { ActivityIndicator, StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Button } from "../../components/Button";
import { Screen } from "../../components/Screen";
import { Txt } from "../../components/Text";
import { useTheme } from "../../theme";

interface GateAction {
  label: string;
  onPress: () => void;
}

interface GateViewProps {
  title: string;
  detail?: string;
  busy?: boolean;
  primary?: GateAction;
  secondary?: GateAction;
}

/** Full-screen stop between sign-in and the app: restoring, loading, failed or nothing to show. */
export function GateView({ title, detail, busy = false, primary, secondary }: GateViewProps) {
  const palette = useTheme();
  const insets = useSafeAreaInsets();
  return (
    <Screen>
      <View style={[styles.body, { paddingTop: insets.top }]}>
        {busy ? <ActivityIndicator color={palette.label2} style={styles.spinner} /> : null}
        <Txt variant={busy ? "callout" : "heading"} tone={busy ? "secondary" : "primary"} style={styles.center}>
          {title}
        </Txt>
        {detail ? (
          <Txt variant="callout" tone="secondary" style={[styles.center, styles.detail]}>
            {detail}
          </Txt>
        ) : null}
      </View>
      {primary || secondary ? (
        <View style={[styles.actions, { paddingBottom: insets.bottom + 12 }]}>
          {primary ? <Button label={primary.label} onPress={primary.onPress} /> : null}
          {secondary ? <Button kind="plain" label={secondary.label} onPress={secondary.onPress} /> : null}
        </View>
      ) : null}
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { flex: 1, alignItems: "center", justifyContent: "center", paddingHorizontal: 40 },
  spinner: { marginBottom: 14 },
  center: { textAlign: "center" },
  detail: { marginTop: 8 },
  actions: { paddingHorizontal: 24, gap: 8 },
});
