/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/project/new.tsx.
 * New project screen mirroring the `new-issue` shape — vertical form,
 * header Cancel / Create buttons. Title is required; everything else has a
 * default (status=planned, priority=none, no lead, no description, no icon).
 *
 * Lead is intentionally NOT exposed in the create form. Web does the same:
 * lead assignment is a follow-up action because most users create the
 * project from a "I need to track this stream of work" intent and figure
 * out who's leading it later. The picker lives on the detail screen.
 *
 * Status / priority pick through the two draft-picker BottomSheets
 * (pickers/new-project-picker-sheets.tsx), which read/write
 * `useNewProjectDraftStore` — the same cross-screen channel the iOS
 * formSheet picker routes used.
 *
 * On success: iOS dismisses the modal, waits for the dismiss animation, then
 * pushes the new project's detail page. Here the caller owns both moves —
 * `onCreated(projectId)` fires after the create mutation settles so the
 * route registry can pop this screen and push `{ name: "project" }`.
 */
import { useCallback, useState } from "react";
import {
  Alert,
  Pressable,
  ScrollView,
  StyleSheet,
  TextInput,
  View,
} from "react-native";
import { Text } from "@/components/ui/text";
import { AutosizeTextArea } from "@/components/ui/autosize-textarea";
import {
  MIN_BODY_INPUT_HEIGHT_PX,
  MOBILE_PLACEHOLDER_COLOR,
} from "@/components/ui/input-tokens";
import { ProjectStatusIcon } from "@/components/ui/project-status-icon";
import { ProjectPriorityIcon } from "@/components/ui/project-priority-icon";
import { ScreenHeader } from "@/src/screens/screen-header";
import { useNav } from "@/src/navigation/navigator";
import {
  NewProjectStatusPickerSheet,
  NewProjectPriorityPickerSheet,
} from "@/components/project/pickers/new-project-picker-sheets";
import {
  projectPriorityLabel,
  projectStatusLabel,
} from "@/lib/project-status";
import { useCreateProject } from "@/data/mutations/projects";
import { useNewProjectDraftStore } from "@/data/stores/new-project-draft-store";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

export function ProjectNewScreen({
  onCreated,
}: {
  /** Fired after a successful create; the caller navigates to the detail. */
  onCreated: (projectId: string) => void;
}) {
  // Local stack-nav view — the cancel path pops this screen; the create
  // path's push is owned by the caller via onCreated (import-cycle rule).
  const nav = useNav<{ pop: () => void }>();
  const c = useThemeColors();
  const create = useCreateProject();

  const [title, setTitle] = useState("");
  const [icon, setIcon] = useState("");
  const [description, setDescription] = useState("");
  const [statusPickerOpen, setStatusPickerOpen] = useState(false);
  const [priorityPickerOpen, setPriorityPickerOpen] = useState(false);
  const status = useNewProjectDraftStore((s) => s.status);
  const priority = useNewProjectDraftStore((s) => s.priority);
  const resetDraft = useNewProjectDraftStore((s) => s.reset);

  const dirty =
    title.length > 0 ||
    icon.length > 0 ||
    description.length > 0 ||
    status !== "planned" ||
    priority !== "none";

  const canCreate = title.trim().length > 0 && !create.isPending;

  const onCancel = useCallback(() => {
    if (!dirty) {
      resetDraft();
      nav.pop();
      return;
    }
    Alert.alert(
      "Discard project?",
      "Your draft will be lost.",
      [
        { text: "Keep editing", style: "cancel" },
        {
          text: "Discard",
          style: "destructive",
          onPress: () => {
            resetDraft();
            nav.pop();
          },
        },
      ],
    );
  }, [dirty, resetDraft, nav]);

  const onCreate = useCallback(() => {
    if (!canCreate) return;
    create.mutate(
      {
        title: title.trim(),
        description: description.trim() || undefined,
        icon: icon.trim() || undefined,
        status,
        priority,
      },
      {
        onSuccess: (project) => {
          resetDraft();
          onCreated(project.id);
        },
        onError: (err) => {
          Alert.alert(
            "Failed to create project",
            err instanceof Error ? err.message : "Unknown error",
          );
        },
      },
    );
  }, [
    canCreate,
    create,
    title,
    description,
    icon,
    status,
    priority,
    resetDraft,
    onCreated,
  ]);

  return (
    <View style={[styles.screen, { backgroundColor: c.background }]}>
      <ScreenHeader
        title="New Project"
        backLabel="Cancel"
        onBack={onCancel}
        right={
          <Pressable
            onPress={onCreate}
            disabled={!canCreate}
            hitSlop={8}
            style={styles.headerAction}
          >
            <Text
              style={[
                styles.headerActionLabel,
                { color: c.brand, opacity: canCreate ? 1 : 0.4 },
              ]}
            >
              {create.isPending ? "Creating…" : "Create"}
            </Text>
          </Pressable>
        }
      />
      <ScrollView
        style={styles.body}
        contentContainerStyle={styles.bodyContent}
        keyboardShouldPersistTaps="handled"
      >
        <Field label="Icon (emoji)">
          <TextInput
            value={icon}
            onChangeText={(v) => setIcon(v.slice(0, 4))}
            placeholder="📦"
            placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
            style={[
              styles.iconInput,
              {
                color: c.foreground,
                backgroundColor: withAlpha(c.secondary, 0.5),
              },
            ]}
            maxLength={4}
          />
        </Field>

        <Field label="Title">
          <TextInput
            value={title}
            onChangeText={setTitle}
            placeholder="Project title"
            placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
            style={[
              styles.textInput,
              {
                color: c.foreground,
                backgroundColor: withAlpha(c.secondary, 0.5),
              },
            ]}
            autoFocus
            returnKeyType="next"
          />
        </Field>

        <Field label="Description">
          <AutosizeTextArea
            value={description}
            onChangeText={setDescription}
            placeholder="What is this project about?"
            placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
            style={[
              styles.textInput,
              {
                backgroundColor: withAlpha(c.secondary, 0.5),
                color: c.foreground,
              },
            ]}
            minHeight={MIN_BODY_INPUT_HEIGHT_PX}
          />
        </Field>

        <View style={styles.pickerRow}>
          <View style={styles.pickerCol}>
            <Field label="Status">
              <Pressable
                onPress={() => setStatusPickerOpen(true)}
                style={[
                  styles.pickerButton,
                  { backgroundColor: withAlpha(c.secondary, 0.5) },
                ]}
              >
                <ProjectStatusIcon status={status} size={16} />
                <Text
                  style={[styles.pickerLabel, { color: c.foreground }]}
                  numberOfLines={1}
                >
                  {projectStatusLabel(status)}
                </Text>
              </Pressable>
            </Field>
          </View>
          <View style={styles.pickerCol}>
            <Field label="Priority">
              <Pressable
                onPress={() => setPriorityPickerOpen(true)}
                style={[
                  styles.pickerButton,
                  { backgroundColor: withAlpha(c.secondary, 0.5) },
                ]}
              >
                <ProjectPriorityIcon priority={priority} size={16} />
                <Text
                  style={[styles.pickerLabel, { color: c.foreground }]}
                  numberOfLines={1}
                >
                  {projectPriorityLabel(priority)}
                </Text>
              </Pressable>
            </Field>
          </View>
        </View>
      </ScrollView>

      <NewProjectStatusPickerSheet
        visible={statusPickerOpen}
        onClose={() => setStatusPickerOpen(false)}
      />
      <NewProjectPriorityPickerSheet
        visible={priorityPickerOpen}
        onClose={() => setPriorityPickerOpen(false)}
      />
    </View>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  const c = useThemeColors();
  return (
    <View style={styles.field}>
      <Text style={[styles.fieldLabel, { color: c.mutedForeground }]}>
        {label}
      </Text>
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  headerAction: { paddingHorizontal: 4, paddingVertical: 4 },
  headerActionLabel: { fontSize: 16, fontWeight: "600" },
  // flex-1 (screen-level scroll body; the sheets size to content themselves)
  body: { flex: 1 },
  // px-4 pt-4 pb-6 gap-4
  bodyContent: {
    paddingHorizontal: 16,
    paddingTop: 16,
    paddingBottom: 24,
    gap: 16,
  },
  field: { gap: 6 },
  // text-xs uppercase tracking-wider
  fieldLabel: {
    fontSize: 12,
    textTransform: "uppercase",
    letterSpacing: 0.6,
  },
  // text-2xl bg-secondary/50 rounded-md px-3 py-2 self-start min-w-[60px] text-center
  iconInput: {
    fontSize: 24,
    borderRadius: 6,
    paddingHorizontal: 12,
    paddingVertical: 8,
    alignSelf: "flex-start",
    minWidth: 60,
    textAlign: "center",
  },
  // text-base bg-secondary/50 rounded-md px-3 py-2
  textInput: {
    fontSize: 16,
    borderRadius: 6,
    paddingHorizontal: 12,
    paddingVertical: 8,
  },
  // flex-row gap-2
  pickerRow: { flexDirection: "row", gap: 8 },
  pickerCol: { flex: 1 },
  // flex-row items-center gap-2 bg-secondary/50 rounded-md px-3 py-2.5
  pickerButton: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    borderRadius: 6,
    paddingHorizontal: 12,
    paddingVertical: 10,
  },
  pickerLabel: { fontSize: 14, flex: 1 },
});
