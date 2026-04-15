import { View, Text, StyleSheet } from "react-native";
import { colors } from "@/lib/theme";

export function MetricTile({
  label,
  value,
  tone = "default",
}: {
  label: string;
  value: string | number;
  tone?: "default" | "green" | "red" | "blue";
}) {
  return (
    <View style={styles.tile}>
      <Text style={[styles.value, toneColor(tone)]}>{value}</Text>
      <Text style={styles.label}>{label}</Text>
    </View>
  );
}

function toneColor(tone: "default" | "green" | "red" | "blue") {
  switch (tone) {
    case "green":
      return { color: colors.success };
    case "red":
      return { color: colors.destructive };
    case "blue":
      return { color: colors.info };
    default:
      return { color: colors.foreground };
  }
}

const styles = StyleSheet.create({
  tile: {
    flex: 1,
    minHeight: 76,
    justifyContent: "center",
    borderRadius: 8,
    backgroundColor: colors.secondary,
    paddingHorizontal: 12,
  },
  value: {
    fontSize: 24,
    fontWeight: "800",
  },
  label: {
    marginTop: 4,
    fontSize: 12,
    color: colors.mutedForeground,
  },
});
