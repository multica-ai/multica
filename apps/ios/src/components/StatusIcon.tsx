import { View, StyleSheet } from "react-native";
import type { IssueStatus } from "@multica/core/types";
import { statusColors } from "@/lib/theme";

const SIZE = 16;
const STROKE = 2;

/**
 * Renders a status indicator circle.
 * - backlog/todo: hollow circle
 * - in_progress: half-filled circle
 * - in_review: 3/4 filled circle
 * - done: filled circle with checkmark (solid fill)
 * - blocked: filled red circle
 * - cancelled: grey circle with line-through
 */
export function StatusIcon({
  status,
  size = SIZE,
}: {
  status: IssueStatus;
  size?: number;
}) {
  const color = statusColors[status];
  const half = size / 2;

  if (status === "done" || status === "blocked") {
    // Filled circle
    return (
      <View
        style={[
          styles.circle,
          {
            width: size,
            height: size,
            borderRadius: half,
            backgroundColor: color,
          },
        ]}
      />
    );
  }

  if (status === "in_progress" || status === "in_review") {
    // Half/3-quarter filled - show as bordered circle with colored half
    return (
      <View
        style={[
          styles.circle,
          {
            width: size,
            height: size,
            borderRadius: half,
            borderWidth: STROKE,
            borderColor: color,
            overflow: "hidden",
          },
        ]}
      >
        <View
          style={{
            position: "absolute",
            bottom: 0,
            left: 0,
            right: 0,
            height: status === "in_progress" ? half : half + half * 0.5,
            backgroundColor: color,
          }}
        />
      </View>
    );
  }

  if (status === "cancelled") {
    // Hollow circle with strikethrough
    return (
      <View
        style={[
          styles.circle,
          {
            width: size,
            height: size,
            borderRadius: half,
            borderWidth: STROKE,
            borderColor: color,
            justifyContent: "center",
            alignItems: "center",
          },
        ]}
      >
        <View
          style={{
            width: size * 0.6,
            height: STROKE,
            backgroundColor: color,
            transform: [{ rotate: "-45deg" }],
          }}
        />
      </View>
    );
  }

  // backlog, todo: hollow circle
  return (
    <View
      style={[
        styles.circle,
        {
          width: size,
          height: size,
          borderRadius: half,
          borderWidth: STROKE,
          borderColor: color,
          borderStyle: status === "backlog" ? "dashed" : "solid",
        },
      ]}
    />
  );
}

const styles = StyleSheet.create({
  circle: {
    // base styles
  },
});
