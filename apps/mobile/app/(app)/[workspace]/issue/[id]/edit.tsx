/**
 * Edit issue title / description. Modal presentation, configured in
 * `[workspace]/_layout.tsx`. Save runs the optimistic `useUpdateIssue`
 * mutation; modal dismisses on success.
 *
 * Mirrors `project/[id]/edit.tsx` so users get the same gesture on both
 * record types (cancel/save in header, dirty Alert on dismiss-while-dirty).
 *
 * Description uses `useMentionInput` + `<DescriptionField>` so the @-mention
 * pipeline matches `new-issue.tsx`. v1 note: existing mentions in the
 * server-side description render as raw markdown text while editing because
 * there's no markdown-to-marker deserializer yet — `serialize()` still
 * produces a valid round-trip since unparsed `[@name](mention://...)` literals
 * pass through unchanged. New @-mentions added during the edit get serialized
 * normally via the marker pipeline.
 *
 * Properties (status / priority / assignee / labels / project / due_date)
 * are NOT edited here — they have dedicated chip pickers on the detail page.
 * This screen only owns the two free-text fields.
 */
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Alert,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  TextInput,
  View,
} from "react-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { DescriptionField } from "@/components/issue/description-field";
import { MentionSuggestionBar } from "@/components/issue/mention-suggestion-bar";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import { issueDetailOptions } from "@/data/queries/issues";
import { useUpdateIssue } from "@/data/mutations/issues";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useMentionInput } from "@/lib/use-mention-input";

export default function EditIssue() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const detail = useQuery(issueDetailOptions(wsId, id));
  const update = useUpdateIssue(id);

  const [title, setTitle] = useState("");
  const description = useMentionInput();
  const [seeded, setSeeded] = useState(false);
  // `useMentionInput` returns `setText` from `useState`, which is a stable
  // identity across renders. Pulling it out of the hook return lets us list
  // it explicitly in the seeding effect's dep array without the whole
  // `description` object (which changes every render) re-triggering the
  // seed and overwriting in-progress edits.
  const setDescriptionText = description.setText;

  useEffect(() => {
    if (!detail.data || seeded) return;
    setTitle(detail.data.title);
    setDescriptionText(detail.data.description ?? "");
    setSeeded(true);
  }, [detail.data, seeded, setDescriptionText]);

  const initialDescription = detail.data?.description ?? "";
  const currentDescription = description.serialize();

  const dirty = useMemo(() => {
    if (!detail.data || !seeded) return false;
    return (
      title.trim() !== detail.data.title ||
      currentDescription.trim() !== initialDescription
    );
  }, [detail.data, seeded, title, currentDescription, initialDescription]);

  const canSave =
    seeded && title.trim().length > 0 && dirty && !update.isPending;

  const onCancel = useCallback(() => {
    if (!dirty) {
      router.back();
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
          onPress: () => router.back(),
        },
      ],
    );
  }, [dirty]);

  const onSave = useCallback(() => {
    if (!canSave) return;
    // `UpdateIssueRequest.description` is `string | undefined` — server
    // treats empty string as "clear the description", which is what we
    // want when the user wipes the field.
    const patch = {
      title: title.trim(),
      description: currentDescription.trim(),
    };
    update.mutate(patch, {
      onSuccess: () => router.back(),
      onError: (err) => {
        Alert.alert(
          "Failed to save",
          err instanceof Error ? err.message : "Unknown error",
        );
      },
    });
  }, [canSave, title, currentDescription, update]);

  const headerLeft = useCallback(
    () => (
      <Pressable onPress={onCancel} className="px-1 py-1">
        <Text className="text-base text-brand">Cancel</Text>
      </Pressable>
    ),
    [onCancel],
  );

  const headerRight = useCallback(
    () => (
      <Pressable
        onPress={onSave}
        disabled={!canSave}
        className={canSave ? "px-1 py-1" : "px-1 py-1 opacity-40"}
      >
        <Text className="text-base text-brand font-semibold">
          {update.isPending ? "Saving…" : "Save"}
        </Text>
      </Pressable>
    ),
    [canSave, onSave, update.isPending],
  );

  return (
    <>
      <Stack.Screen options={{ headerLeft, headerRight }} />
      <KeyboardAvoidingView
        className="flex-1 bg-background"
        behavior={Platform.OS === "ios" ? "padding" : undefined}
      >
        <ScrollView
          className="flex-1"
          contentContainerClassName="px-4 pt-4 pb-6 gap-4"
          keyboardShouldPersistTaps="handled"
        >
          {!detail.data ? (
            <Text className="text-sm text-muted-foreground">Loading…</Text>
          ) : (
            <>
              <Field label="Title">
                <TextInput
                  value={title}
                  onChangeText={setTitle}
                  placeholder="Issue title"
                  placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
                  className="text-base text-foreground bg-secondary/50 rounded-md px-3 py-2"
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
            form body. */}
        <MentionSuggestionBar {...description.suggestionBar} />
      </KeyboardAvoidingView>
    </>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <View className="gap-1.5">
      <Text className="text-xs uppercase tracking-wider text-muted-foreground">
        {label}
      </Text>
      {children}
    </View>
  );
}
