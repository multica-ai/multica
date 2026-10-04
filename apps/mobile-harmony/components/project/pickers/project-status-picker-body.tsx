/**
 * HarmonyOS port of apps/mobile/components/project/pickers/project-status-picker-body.tsx.
 * Pure picker body for project status — single-select over the 5
 * ProjectStatus enum values. See issue/pickers/status-picker-body.tsx for
 * the "extract body, route owns shell" rationale; on this side the shell is
 * a BottomSheet (pickers/project-picker-sheets.tsx).
 */
import { ScrollView, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import type { ProjectStatus } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Icon } from "@/components/ui/icon";
import { ProjectStatusIcon } from "@/components/ui/project-status-icon";
import {
  PROJECT_STATUSES,
  PROJECT_STATUS_LABEL,
} from "@/lib/project-status";
import { useThemeColors } from "@/lib/use-theme-colors";

interface Props {
  value: ProjectStatus | string;
  onChange: (next: ProjectStatus) => void;
}

export function ProjectStatusPickerBody({ value, onChange }: Props) {
  const c = useThemeColors();

  return (
    <ScrollView showsVerticalScrollIndicator={false}>
      <View style={styles.titleRow}>
        <Text style={[styles.title, { color: c.foreground }]}>Status</Text>
      </View>
      <View style={styles.list}>
        {PROJECT_STATUSES.map((status) => {
          const selected = status === value;
          return (
            <Pressable
              key={status}
              onPress={() => onChange(status)}
              style={({ pressed }) => [
                styles.row,
                pressed ? { backgroundColor: c.secondary } : null,
              ]}
            >
              <ProjectStatusIcon status={status} size={18} />
              <Text style={[styles.rowLabel, { color: c.foreground }]}>
                {PROJECT_STATUS_LABEL[status]}
              </Text>
              {selected ? (
                <Icon name="checkmark" size={20} color={c.primary} />
              ) : null}
            </Pressable>
          );
        })}
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  // px-4 pt-3 pb-2
  titleRow: { paddingHorizontal: 16, paddingTop: 12, paddingBottom: 8 },
  // text-lg font-semibold
  title: { fontSize: 18, fontWeight: "600" },
  // px-2
  list: { paddingHorizontal: 8 },
  // flex-row items-center gap-3 rounded-lg px-3 py-3
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    borderRadius: 8,
    paddingHorizontal: 12,
    paddingVertical: 12,
  },
  // flex-1 text-base
  rowLabel: { flex: 1, fontSize: 16 },
});
