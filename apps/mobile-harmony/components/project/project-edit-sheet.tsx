/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/project/[id]/edit.tsx.
 * Edit project title / description / icon. The iOS route is presented as a
 * modal with header Cancel/Save buttons; here the shell is a `BottomSheet`
 * (the standing-in component for formSheet/modal routes) and the header
 * buttons render as an in-sheet row.
 *
 * Save runs an optimistic `useUpdateProject`; the sheet closes on success.
 * Cancel/dismiss flow: the Cancel button checks dirty state and pops an
 * Alert if there are unsaved edits (the hardware-back path closes the sheet
 * directly — see BottomSheet; drag-down dismissal is not part of the
 * harmony shell yet).
 */
import { useCallback, useEffect, useMemo, useState } from "react";
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
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { projectDetailOptions } from "@/data/queries/projects";
import { useUpdateProject } from "@/data/mutations/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useQuery } from "@tanstack/react-query";
import { useThemeColors } from "@/lib/use-theme-colors";
import { withAlpha } from "@/lib/theme";

interface Props {
  projectId: string;
  visible: boolean;
  onClose: () => void;
}

export function ProjectEditSheet({ projectId, visible, onClose }: Props) {
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const detail = useQuery(projectDetailOptions(wsId, projectId));
  const update = useUpdateProject(projectId);

  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [icon, setIcon] = useState("");
  const [seeded, setSeeded] = useState(false);

  // Seed local state once detail lands. Effect (not setState-in-render)
  // so we don't accidentally retrigger on every parent re-render — the
  // `seeded` guard makes it idempotent.
  useEffect(() => {
    if (!detail.data || seeded) return;
    setTitle(detail.data.title);
    setDescription(detail.data.description ?? "");
    setIcon(detail.data.icon ?? "");
    setSeeded(true);
  }, [detail.data, seeded]);

  const dirty = useMemo(() => {
    if (!detail.data) return false;
    return (
      title.trim() !== detail.data.title ||
      description.trim() !== (detail.data.description ?? "") ||
      icon.trim() !== (detail.data.icon ?? "")
    );
  }, [detail.data, title, description, icon]);

  const canSave =
    seeded && title.trim().length > 0 && dirty && !update.isPending;

  const onCancel = useCallback(() => {
    if (!dirty) {
      onClose();
      return;
    }
    Alert.alert(
      "Discard changes?",
      "Your edits to this project will be lost.",
      [
        { text: "Keep editing", style: "cancel" },
        {
          text: "Discard",
          style: "destructive",
          onPress: () => onClose(),
        },
      ],
    );
  }, [dirty, onClose]);

  const onSave = useCallback(() => {
    if (!canSave) return;
    const patch = {
      title: title.trim(),
      description: description.trim() || null,
      icon: icon.trim() || null,
    };
    update.mutate(patch, {
      onSuccess: () => onClose(),
      onError: (err) => {
        Alert.alert(
          "Failed to save",
          err instanceof Error ? err.message : "Unknown error",
        );
      },
    });
  }, [canSave, title, description, icon, update, onClose]);

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <View style={styles.headerRow}>
        <Pressable onPress={onCancel} hitSlop={8}>
          <Text style={[styles.headerAction, { color: c.brand }]}>Cancel</Text>
        </Pressable>
        <Text style={[styles.headerTitle, { color: c.foreground }]} numberOfLines={1}>
          Edit Project
        </Text>
        <Pressable onPress={onSave} disabled={!canSave} hitSlop={8}>
          <Text
            style={[
              styles.headerAction,
              styles.headerActionStrong,
              { color: c.brand, opacity: canSave ? 1 : 0.4 },
            ]}
          >
            {update.isPending ? "Saving…" : "Save"}
          </Text>
        </Pressable>
      </View>
      <ScrollView
        style={styles.body}
        contentContainerStyle={styles.bodyContent}
        keyboardShouldPersistTaps="handled"
      >
        {!detail.data ? (
          <Text style={{ fontSize: 14, color: c.mutedForeground }}>
            Loading…
          </Text>
        ) : (
          <>
            <Field label="Icon (emoji)">
              <TextInput
                value={icon}
                onChangeText={(v) => {
                  // Cap at two characters — emoji are usually 1-2 UTF-16
                  // code units. Prevents the user typing a full sentence
                  // by accident.
                  setIcon(v.slice(0, 4));
                }}
                placeholder="📦"
                placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
                style={[styles.iconInput, { color: c.foreground, backgroundColor: withAlpha(c.secondary, 0.5) }]}
                maxLength={4}
              />
            </Field>

            <Field label="Title">
              <TextInput
                value={title}
                onChangeText={setTitle}
                placeholder="Project title"
                placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
                style={[styles.textInput, { color: c.foreground, backgroundColor: withAlpha(c.secondary, 0.5) }]}
                autoFocus={!detail.data?.title}
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
                  { backgroundColor: withAlpha(c.secondary, 0.5), color: c.foreground },
                ]}
                minHeight={MIN_BODY_INPUT_HEIGHT_PX}
              />
            </Field>
          </>
        )}
      </ScrollView>
    </BottomSheet>
  );
}

// bg-secondary/50 — translucent theme shade via lib/theme withAlpha.
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
  // Header row standing in for the iOS modal nav bar (Cancel / title / Save).
  headerRow: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
    paddingHorizontal: 16,
    paddingTop: 12,
    paddingBottom: 4,
  },
  // text-base text-brand
  headerAction: { fontSize: 16 },
  headerActionStrong: { fontWeight: "600" },
  headerTitle: { flex: 1, textAlign: "center", fontSize: 16, fontWeight: "600" },
  body: { flexGrow: 0 },
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
});
