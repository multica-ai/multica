/**
 * HarmonyOS ports of the project picker routes
 * (`apps/mobile/app/(app)/[workspace]/project/[id]/picker/{status,priority,lead}.tsx`).
 * On iOS each is a self-contained formSheet that reads the project from
 * cache, fires `useUpdateProject` on selection, then dismisses; here the
 * shell is a `BottomSheet` and the three sheets mount from the project
 * detail screen via local state.
 *
 * The lead sheet's iOS-native search bar (useNativeSearchBar) becomes a
 * plain TextField above the list; the body keeps its scroll-reset-on-query
 * behavior via useScrollToTopOnChange.
 */
import { useState } from "react";
import { StyleSheet, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { ProjectStatusPickerBody } from "./project-status-picker-body";
import { ProjectPriorityPickerBody } from "./project-priority-picker-body";
import { ProjectLeadPickerBody, type LeadValue } from "./project-lead-picker-body";
import { projectDetailOptions } from "@/data/queries/projects";
import { useUpdateProject } from "@/data/mutations/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useThemeColors } from "@/lib/use-theme-colors";

interface SheetProps {
  projectId: string;
  visible: boolean;
  onClose: () => void;
}

export function ProjectStatusPickerSheet({ projectId, visible, onClose }: SheetProps) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: project } = useQuery(projectDetailOptions(wsId, projectId));
  const updateProject = useUpdateProject(projectId);

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <ProjectStatusPickerBody
        value={project?.status ?? "planned"}
        onChange={(next) => {
          updateProject.mutate({ status: next });
          onClose();
        }}
      />
    </BottomSheet>
  );
}

export function ProjectPriorityPickerSheet({ projectId, visible, onClose }: SheetProps) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: project } = useQuery(projectDetailOptions(wsId, projectId));
  const updateProject = useUpdateProject(projectId);

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <ProjectPriorityPickerBody
        value={project?.priority ?? "none"}
        onChange={(next) => {
          updateProject.mutate({ priority: next });
          onClose();
        }}
      />
    </BottomSheet>
  );
}

const LEAD_SHEET_MAX_RATIO = 0.7;

export function ProjectLeadPickerSheet({ projectId, visible, onClose }: SheetProps) {
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: project } = useQuery(projectDetailOptions(wsId, projectId));
  const updateProject = useUpdateProject(projectId);
  const [query, setQuery] = useState("");

  // iOS resets the native search text when the sheet dismisses; replicate
  // by clearing on close (state lives here, so reopening starts fresh).
  const handleClose = () => {
    setQuery("");
    onClose();
  };

  const value: LeadValue | null =
    project?.lead_type && project?.lead_id
      ? { type: project.lead_type, id: project.lead_id }
      : null;

  return (
    <BottomSheet
      visible={visible}
      onClose={handleClose}
      maxHeightRatio={LEAD_SHEET_MAX_RATIO}
    >
      <View style={styles.leadHeader}>
        <Text style={[styles.leadTitle, { color: c.foreground }]}>Lead</Text>
        <TextField
          value={query}
          onChangeText={setQuery}
          placeholder="Search members or agents"
          autoCapitalize="none"
          autoFocus
          style={styles.search}
        />
      </View>
      <View style={styles.leadList}>
        <ProjectLeadPickerBody
          value={value}
          query={query}
          onChange={(next) => {
            if (next === null) {
              updateProject.mutate({ lead_type: null, lead_id: null });
            } else {
              updateProject.mutate({ lead_type: next.type, lead_id: next.id });
            }
            handleClose();
          }}
        />
      </View>
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  leadHeader: {
    paddingHorizontal: 16,
    paddingTop: 12,
    paddingBottom: 8,
    gap: 8,
  },
  leadTitle: { fontSize: 18, fontWeight: "600" },
  search: { fontSize: 15 },
  // The body FlatList grows with content inside the content-sized sheet;
  // a max height keeps long directories from pushing past the cap.
  leadList: { maxHeight: 420 },
});
