import { Pressable, StyleSheet, View } from "react-native";
import { useTheme } from "../theme";
import { Avatar, type AvatarActor } from "./Avatar";
import { StatusIcon } from "./StatusIcon";
import { Txt } from "./Text";

export interface IssueRowModel {
  id: string;
  identifier: string;
  title: string;
  status: { key: string; color: string | null; icon: string | null };
  assignee: AvatarActor | null;
  /** Second line, e.g. a search match. */
  detail?: string;
}

/** One issue in a list: status glyph, identifier, title, assignee. */
export function IssueRow({ issue, onPress }: { issue: IssueRowModel; onPress: (issue: IssueRowModel) => void }) {
  const palette = useTheme();
  return (
    <Pressable
      accessibilityRole="button"
      onPress={() => onPress(issue)}
      style={({ pressed }) => [styles.row, pressed && { backgroundColor: palette.fill }]}
    >
      <StatusIcon status={issue.status.key} color={issue.status.color} icon={issue.status.icon} size={16} />
      <View style={styles.body}>
        <Txt variant="rowRegular" numberOfLines={1}>
          <Txt variant="rowRegular" tone="secondary">
            {issue.identifier}
            {"  "}
          </Txt>
          {issue.title}
        </Txt>
        {issue.detail ? (
          <Txt variant="meta" tone="tertiary" numberOfLines={1} style={styles.detail}>
            {issue.detail}
          </Txt>
        ) : null}
      </View>
      {issue.assignee ? <Avatar actor={issue.assignee} size={22} /> : null}
    </Pressable>
  );
}

const styles = StyleSheet.create({
  row: { minHeight: 46, flexDirection: "row", alignItems: "center", gap: 12, paddingHorizontal: 20, paddingVertical: 10 },
  body: { flex: 1, minWidth: 0 },
  detail: { marginTop: 2 },
});
