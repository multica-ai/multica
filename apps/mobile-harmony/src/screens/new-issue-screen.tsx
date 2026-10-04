/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/new-issue.tsx — new
 * issue creation screen, manual only.
 *
 * Layout follows Apple Reminders / Linear iOS / Things 3: one vertical
 * scrolling form (title → description → property chips), no sticky bottom
 * toolbar. Property chips are part of the form, not pinned above keyboard.
 * MentionSuggestionBar floats above keyboard only when the user is mid-@.
 *
 * No markdown toolbar / upload buttons in v1 (mirrors the iOS screen's
 * scope cut): mobile users creating an issue rarely format markdown, and
 * attachment upload is deferred.
 *
 * Attribute chips (status / priority / assignee / due date / project) live
 * in `useNewIssueDraftStore`; the draft-picker BottomSheets
 * (pickers/new-issue-picker-sheets.tsx) read and write the same values.
 * The store is reset on mount + on unmount so re-opening starts clean.
 *
 * Mention pipeline shares `useMentionInput` with the issue edit sheet —
 * both surfaces produce canonical `[@name](mention://type/id)` markdown
 * recognised by util.ParseMentions on the server.
 *
 * On success: the caller owns both moves via `onCreated(issueId)` (the
 * route registry pops this screen and pushes the new issue's detail) —
 * same contract as project-new-screen.tsx.
 */
import React, { useCallback, useEffect, useState } from "react";
import { Alert, Pressable, ScrollView, StyleSheet, TextInput, View } from "react-native";
import { useKeyboardHeight } from "@/lib/use-keyboard-height";
import { Text } from "@/components/ui/text";
import { SubmitIssueButton } from "@/components/issue/submit-issue-button";
import { CreateFormAttributeRow } from "@/components/issue/create-form-attribute-row";
import { type NewIssuePickerField } from "@/components/issue/create-form-attribute-row";
import { MentionSuggestionBar } from "@/components/issue/mention-suggestion-bar";
import { DescriptionField } from "@/components/issue/description-field";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import {
  NewIssueStatusPickerSheet,
  NewIssuePriorityPickerSheet,
  NewIssueAssigneePickerSheet,
  NewIssueProjectPickerSheet,
  NewIssueDueDatePickerSheet,
} from "@/components/issue/pickers/new-issue-picker-sheets";
import { ScreenHeader } from "./screen-header";
import { useNav } from "@/src/navigation/navigator";
import { useCreateIssue } from "@/data/mutations/issues";
import { useNewIssueDraftStore } from "@/data/stores/new-issue-draft-store";
import { useMentionInput } from "@/lib/use-mention-input";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

/** Local stack-nav view — the cancel path pops this screen; the create
 *  path's push is owned by the caller via onCreated (import-cycle rule). */
type NewIssueNav = { pop: () => void };

export function NewIssueScreen({
  onCreated,
}: {
  /** Fired after a successful create; the caller navigates. */
  onCreated: (issueId: string) => void;
}) {
  const nav = useNav<NewIssueNav>();
  const c = useThemeColors();
  const [title, setTitle] = useState("");
  const description = useMentionInput();
  const [pickerField, setPickerField] = useState<NewIssuePickerField | null>(
    null,
  );

  const resetDraft = useNewIssueDraftStore((s) => s.reset);

  useEffect(() => {
    resetDraft();
    return () => {
      resetDraft();
    };
  }, [resetDraft]);

  const createIssue = useCreateIssue();
  const isSubmitting = createIssue.isPending;

  const canSubmit = !isSubmitting && title.trim().length > 0;

  const onSubmit = useCallback(async () => {
    const trimmedTitle = title.trim();
    if (trimmedTitle.length === 0) return;
    const finalDescription = description.serialize().trim();
    const status = useNewIssueDraftStore.getState().status;
    const priority = useNewIssueDraftStore.getState().priority;
    const assignee = useNewIssueDraftStore.getState().assignee;
    const dueDate = useNewIssueDraftStore.getState().dueDate;
    const project = useNewIssueDraftStore.getState().project;
    try {
      const issue = await createIssue.mutateAsync({
        title: trimmedTitle,
        description: finalDescription || undefined,
        status,
        priority,
        ...(assignee
          ? { assignee_type: assignee.type, assignee_id: assignee.id }
          : {}),
        ...(dueDate ? { due_date: dueDate } : {}),
        ...(project ? { project_id: project.id } : {}),
      });
      onCreated(issue.id);
    } catch (err) {
      Alert.alert(
        "Failed to create issue",
        err instanceof Error ? err.message : "Unknown error",
      );
    }
  }, [title, description, createIssue, onCreated]);

  const keyboardHeight = useKeyboardHeight();
  return (
    <View style={[styles.screen, { backgroundColor: c.background }]}>
      <ScreenHeader
        title="New Issue"
        backLabel="Cancel"
        onBack={() => {
          resetDraft();
          nav.pop();
        }}
        right={
          <SubmitIssueButton
            disabled={!canSubmit}
            loading={isSubmitting}
            onPress={onSubmit}
          />
        }
      />
      <View style={[styles.fill, { paddingBottom: keyboardHeight }]}>
        <ScrollView
          style={styles.fill}
          contentContainerStyle={styles.content}
          keyboardShouldPersistTaps="handled"
        >
          <TextInput
            value={title}
            onChangeText={setTitle}
            placeholder="Issue title"
            placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
            style={[styles.titleInput, { color: c.foreground }]}
            autoFocus
            returnKeyType="next"
            editable={!isSubmitting}
          />
          <DescriptionField
            description={description}
            disabled={isSubmitting}
          />
          <CreateFormAttributeRow
            onOpenPicker={(field) => setPickerField(field)}
          />
        </ScrollView>

        {/* Mention suggestions float above the keyboard only when the user
            types `@`. Self-hides via `if (!visible) return null` so it
            doesn't take space at rest. */}
        <MentionSuggestionBar {...description.suggestionBar} />
      </View>

      {/* The five iOS new-issue-picker routes, as draft-picker sheets. */}
      <NewIssueStatusPickerSheet
        visible={pickerField === "status"}
        onClose={() => setPickerField(null)}
      />
      <NewIssuePriorityPickerSheet
        visible={pickerField === "priority"}
        onClose={() => setPickerField(null)}
      />
      <NewIssueAssigneePickerSheet
        visible={pickerField === "assignee"}
        onClose={() => setPickerField(null)}
      />
      <NewIssueProjectPickerSheet
        visible={pickerField === "project"}
        onClose={() => setPickerField(null)}
      />
      <NewIssueDueDatePickerSheet
        visible={pickerField === "due-date"}
        onClose={() => setPickerField(null)}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  fill: { flex: 1 },
  // px-4 pt-4 pb-6 gap-4
  content: {
    paddingHorizontal: 16,
    paddingTop: 16,
    paddingBottom: 24,
    gap: 16,
  },
  // text-2xl font-semibold py-2
  titleInput: {
    fontSize: 24,
    fontWeight: "600",
    paddingVertical: 8,
  },
});
