import { useState } from "react";
import { Pressable, StyleSheet, View } from "react-native";
import { Icon } from "../icons/Icon";
import { useTheme } from "../theme";
import { IssueRow, type IssueRowModel } from "./IssueRow";
import { StatusIcon } from "./StatusIcon";
import { Txt } from "./Text";

export interface IssueGroupModel {
  key: string;
  label: string;
  color: string | null;
  icon: string | null;
  /** Done or closed: folded away until asked for. */
  finished: boolean;
  issues: readonly IssueRowModel[];
}

function GroupHead({ group }: { group: IssueGroupModel }) {
  return (
    <View style={styles.head}>
      <StatusIcon status={group.key} color={group.color} icon={group.icon} size={14} />
      <Txt variant="metaStrong" tone="secondary">
        {group.label} {group.issues.length}
      </Txt>
    </View>
  );
}

interface IssueGroupsProps {
  groups: readonly IssueGroupModel[];
  finishedLabel: (count: number) => string;
  onOpen: (issue: IssueRowModel) => void;
}

/** Issues under a header per status; finished statuses sit behind one disclosure row. */
export function IssueGroups({ groups, finishedLabel, onOpen }: IssueGroupsProps) {
  const palette = useTheme();
  const [showFinished, setShowFinished] = useState(false);
  const open = groups.filter((group) => !group.finished);
  const finished = groups.filter((group) => group.finished);
  const finishedCount = finished.reduce((sum, group) => sum + group.issues.length, 0);
  const render = (group: IssueGroupModel) => (
    <View key={group.key}>
      <GroupHead group={group} />
      {group.issues.map((issue) => (
        <IssueRow key={issue.id} issue={issue} onPress={onOpen} />
      ))}
    </View>
  );
  return (
    <View>
      {open.map(render)}
      {finishedCount > 0 ? (
        <Pressable
          accessibilityRole="button"
          accessibilityState={{ expanded: showFinished }}
          onPress={() => setShowFinished(!showFinished)}
          style={({ pressed }) => [styles.head, styles.toggle, pressed && { opacity: 0.6 }]}
        >
          <Icon name={showFinished ? "ChevronDown" : "ChevronRight"} size={14} color={palette.label2} />
          <Txt variant="metaStrong" tone="secondary">
            {finishedLabel(finishedCount)}
          </Txt>
        </Pressable>
      ) : null}
      {showFinished ? finished.map(render) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  head: { flexDirection: "row", alignItems: "center", gap: 8, paddingHorizontal: 20, paddingTop: 10, paddingBottom: 2 },
  toggle: { paddingBottom: 8 },
});
