/**
 * HarmonyOS port of apps/mobile/components/project/project-properties-section.tsx.
 * Tappable rows for Status / Priority / Lead. Each row opens a picker sheet
 * via the corresponding `onPress*` callback.
 *
 * Layout mirrors iOS Settings rows: label on left, current value on right
 * with a disclosure chevron, full-width separator below each row. Tapping
 * anywhere on the row triggers the picker.
 *
 * Lead supports both member and agent (Project.lead_type), resolved via
 * useActorLookup so it shares the same lookup with my-issues + issue detail.
 * The harmony ActorAvatar takes `name`/`avatarUrl` explicitly (no built-in
 * directory lookup — see components/ui/actor-avatar.tsx), so the resolved
 * values are passed straight through; `showPresence` is dropped until the
 * presence data layer lands on this side.
 */
import { StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import type { Project } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { Icon } from "@/components/ui/icon";
import { ProjectStatusIcon } from "@/components/ui/project-status-icon";
import { ProjectPriorityIcon } from "@/components/ui/project-priority-icon";
import {
  projectPriorityLabel,
  projectStatusLabel,
} from "@/lib/project-status";
import { useActorLookup } from "@/data/use-actor-name";
import { useThemeColors } from "@/lib/use-theme-colors";
import { withAlpha } from "@/lib/theme";

interface Props {
  project: Project;
  onPressStatus: () => void;
  onPressPriority: () => void;
  onPressLead: () => void;
}

export function ProjectPropertiesSection({
  project,
  onPressStatus,
  onPressPriority,
  onPressLead,
}: Props) {
  const c = useThemeColors();
  const { getName, getAvatarUrl } = useActorLookup();
  const hasLead = !!(project.lead_type && project.lead_id);
  const leadName = hasLead
    ? getName(project.lead_type, project.lead_id)
    : null;

  return (
    <View
      style={[
        styles.section,
        { borderColor: c.border, backgroundColor: c.background },
      ]}
    >
      <Row
        label="Status"
        onPress={onPressStatus}
        left={<ProjectStatusIcon status={project.status} size={16} />}
        right={
          <Text style={[styles.rowValue, { color: c.foreground }]}>
            {projectStatusLabel(project.status)}
          </Text>
        }
      />
      <Separator />
      <Row
        label="Priority"
        onPress={onPressPriority}
        left={<ProjectPriorityIcon priority={project.priority} size={16} />}
        right={
          <Text style={[styles.rowValue, { color: c.foreground }]}>
            {projectPriorityLabel(project.priority)}
          </Text>
        }
      />
      <Separator />
      <Row
        label="Lead"
        onPress={onPressLead}
        left={
          hasLead ? (
            <ActorAvatar
              type={project.lead_type}
              id={project.lead_id}
              name={leadName ?? undefined}
              avatarUrl={getAvatarUrl(project.lead_type, project.lead_id)}
              size={20}
            />
          ) : (
            <PlaceholderAvatar />
          )
        }
        right={
          <Text
            style={[
              styles.rowValue,
              { color: leadName ? c.foreground : c.mutedForeground },
            ]}
          >
            {leadName ?? "Unassigned"}
          </Text>
        }
      />
    </View>
  );
}

function Row({
  label,
  onPress,
  left,
  right,
}: {
  label: string;
  onPress: () => void;
  left: React.ReactNode;
  right: React.ReactNode;
}) {
  const c = useThemeColors();
  return (
    <Pressable
      onPress={onPress}
      style={({ pressed }) => [
        // flex-row items-center gap-3 px-4 py-3
        styles.row,
        pressed ? { backgroundColor: c.secondary } : null,
      ]}
    >
      <Text style={[styles.rowLabel, { color: c.mutedForeground }]}>
        {label}
      </Text>
      <View style={styles.rowValueGroup}>
        {left}
        {right}
      </View>
      <Icon name="chevron-forward" size={14} color={c.mutedForeground} />
    </Pressable>
  );
}

function Separator() {
  const c = useThemeColors();
  // h-px bg-border ml-4
  return <View style={[styles.separator, { backgroundColor: c.border }]} />;
}

function PlaceholderAvatar() {
  const c = useThemeColors();
  return (
    <View
      style={{
        width: 20,
        height: 20,
        borderRadius: 10,
        borderWidth: 1,
        borderStyle: "dashed",
        borderColor: withAlpha(c.mutedForeground, 0.4),
      }}
    />
  );
}

const styles = StyleSheet.create({
  // border-y border-border
  section: {
    borderTopWidth: 1,
    borderBottomWidth: 1,
  },
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 12,
  },
  // text-sm w-20
  rowLabel: { fontSize: 14, width: 80 },
  // flex-row items-center gap-2 flex-1
  rowValueGroup: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    flex: 1,
  },
  // text-sm
  rowValue: { fontSize: 14 },
  separator: { height: 1, marginLeft: 16 },
});
