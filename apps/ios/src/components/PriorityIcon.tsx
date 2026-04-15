import { View, StyleSheet } from "react-native";
import type { IssuePriority } from "@multica/core/types";
import { priorityColors, colors } from "@/lib/theme";

const BAR_WIDTH = 3;
const BAR_GAP = 2;

/**
 * Priority indicator showing 0-4 ascending bars.
 */
export function PriorityIcon({
  priority,
  size = 16,
}: {
  priority: IssuePriority;
  size?: number;
}) {
  const color = priorityColors[priority];
  const barsCount =
    priority === "urgent"
      ? 4
      : priority === "high"
        ? 3
        : priority === "medium"
          ? 2
          : priority === "low"
            ? 1
            : 0;

  const barHeights = [0.25, 0.5, 0.75, 1.0];

  return (
    <View style={[styles.container, { width: size, height: size }]}>
      {barHeights.map((heightRatio, i) => (
        <View
          key={i}
          style={[
            styles.bar,
            {
              width: BAR_WIDTH,
              height: size * heightRatio,
              backgroundColor: i < barsCount ? color : colors.border,
              marginLeft: i > 0 ? BAR_GAP : 0,
            },
          ]}
        />
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flexDirection: "row",
    alignItems: "flex-end",
    justifyContent: "center",
  },
  bar: {
    borderRadius: 1,
  },
});
