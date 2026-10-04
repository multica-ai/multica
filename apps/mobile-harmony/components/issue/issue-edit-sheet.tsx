/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/issue/[id]/edit.tsx —
 * edit issue title / description. The iOS route is a modal with header
 * Cancel/Save; here the shell is a `BottomSheet` (the standing-in component
 * for formSheet/modal routes) and the header buttons render as an in-sheet
 * row (same treatment as project-edit-sheet.tsx).
 *
 * Description uses `useMentionInput` + `<DescriptionField>` so the @-mention
 * pipeline matches the new-issue screen. v1 note: existing mentions in the
 * server-side description render as raw markdown text while editing because
 * there's no markdown-to-marker deserializer yet — `serialize()` still
 * produces a valid round-trip. New @-mentions added during the edit get
 * serialized normally via the marker pipeline.
 *
 * Save runs the optimistic `useUpdateIssue`; the sheet closes on success.
 * Properties (status / priority / assignee / labels / project / due_date)
 * are NOT edited here — they have dedicated chip pickers on the detail
 * page. This sheet only owns the two free-text fields.
 */
import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  Alert,
  Pressable,
  ScrollView,
  StyleSheet,
  TextInput,
  View,
} from "react-native";
import { useQuery } from "@tanstack/react-query";
import { stripChannelMediaMarkers } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { DescriptionField } from "./description-field";
import { MentionSuggestionBar } from "./mention-suggestion-bar";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { issueDetailOptions } from "@/data/queries/issues";
import { useUpdateIssue } from "@/data/mutations/issues";
import { buildIssueTextUpdate } from "@/data/issue-edit";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useMentionInput } from "@/lib/use-mention-input";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

interface Props {
  issueId: string;
  visible: boolean;
  onClose: () => void;
}

export function IssueEditSheet({ issueId, visible, onClose }: Props) {
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const detail = useQuery(issueDetailOptions(wsId, issueId));
  const update = useUpdateIssue(issueId);

  const [title, setTitle] = useState("");
  const [initialTitle, setInitialTitle] = useState("");
  const [initialDescription, setInitialDescription] = useState("");
  const description = useMentionInput();
  const [seeded, setSeeded] = useState(false);
  // `useMentionInput` returns `setText` from `useState`, which is a stable
  // identity across renders. Pulling it out of the hook return lets us list
  // it explicitly in the seeding effect's dep array without the whole
  // `description` object (which changes every render) re-triggering the
  // seed and overwriting in-progress edits.
  const setDescriptionText = description.setText;

  useEffect(() => {
    if (!visible) return;
    if (!detail.data || seeded) return;
    setTitle(detail.data.title);
    setInitialTitle(detail.data.title);
    const initial = detail.data.description ?? "";
    setDescriptionText(stripChannelMediaMarkers(initial));
    setInitialDescription(initial);
    setSeeded(true);
  }, [visible, detail.data, seeded, setDescriptionText]);

  // Re-arm the seed on every open so a reopened sheet reflects the latest
  // server state instead of the first mount's.
  useEffect(() => {
    if (!visible) setSeeded(false);
  }, [visible]);

  const currentDescription = description.serialize();

  const dirty = useMemo(() => {
    if (!detail.data || !seeded) return false;
    return (
      title.trim() !== initialTitle ||
      currentDescription.trim() !==
        stripChannelMediaMarkers(initialDescription).trim()
    );
  }, [detail.data, seeded, title, initialTitle, currentDescription, initialDescription]);

  const canSave =
    seeded && title.trim().length > 0 && dirty && !update.isPending;

  const onCancel = useCallback(() => {
    if (!dirty) {
      onClose();
      return;
    }
    Alert.alert(
      "Discard changes?",
      "Your edits to this issue will be lost.",
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
    // `UpdateIssueRequest.description` is `string | undefined` — server
    // treats empty string as "clear the description", which is what we
    // want when the user wipes the field. Mobile deliberately omits strict
    // text baselines until this surface has a conflict reconciliation flow.
    const patch = buildIssueTextUpdate(title, currentDescription);
    update.mutate(patch, {
      onSuccess: () => onClose(),
      onError: (err) => {
        Alert.alert(
          "Failed to save",
          err instanceof Error ? err.message : "Unknown error",
        );
      },
    });
  }, [canSave, title, currentDescription, update, onClose]);

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <View style={styles.headerRow}>
        <Pressable onPress={onCancel} hitSlop={8}>
          <Text style={[styles.headerAction, { color: c.brand }]}>Cancel</Text>
        </Pressable>
        <Text
          style={[styles.headerTitle, { color: c.foreground }]}
          numberOfLines={1}
        >
          Edit Issue
        </Text>
        <Pressable onPress={onSave} disabled={!canSave} hitSlop={8}>
          <Text
            style={[
              styles.headerAction,
              {
                color: c.brand,
                fontWeight: "600",
                opacity: canSave ? 1 : 0.4,
              },
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
          <Text style={[styles.loading, { color: c.mutedForeground }]}>
            Loading…
          </Text>
        ) : (
          <>
            <Field label="Title">
              <TextInput
                value={title}
                onChangeText={setTitle}
                placeholder="Issue title"
                placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
                style={[
                  styles.titleInput,
                  {
                    color: c.foreground,
                    backgroundColor: withAlpha(c.secondary, 0.5),
                  },
                ]}
                returnKeyType="next"
                editable={!update.isPending}
              />
            </Field>

            <Field label="Description">
              <DescriptionField
                description={description}
                disabled={update.isPending}
              />
            </Field>
          </>
        )}
      </ScrollView>
      {/* Mention suggestion bar floats above the keyboard while the user
          is mid-@. Outside the ScrollView so it doesn't scroll with the
          form body. The sheet's KeyboardAvoidingView lifts both. */}
      <MentionSuggestionBar {...description.suggestionBar} />
    </BottomSheet>
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
  headerRow: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingHorizontal: 16,
    paddingTop: 12,
    paddingBottom: 8,
  },
  // text-base
  headerAction: { fontSize: 16 },
  // text-base font-semibold
  headerTitle: { fontSize: 16, fontWeight: "600" },
  // flex-1
  body: { maxHeight: 420 },
  // px-4 pt-4 pb-6 gap-4
  bodyContent: {
    paddingHorizontal: 16,
    paddingTop: 8,
    paddingBottom: 24,
    gap: 16,
  },
  // text-sm
  loading: { fontSize: 14 },
  // text-base bg-secondary/50 rounded-md px-3 py-2
  titleInput: {
    fontSize: 16,
    borderRadius: 6,
    paddingHorizontal: 12,
    paddingVertical: 8,
  },
  // gap-1.5
  field: { gap: 6 },
  // text-xs uppercase tracking-wider
  fieldLabel: {
    fontSize: 12,
    textTransform: "uppercase",
    letterSpacing: 0.6,
  },
});
