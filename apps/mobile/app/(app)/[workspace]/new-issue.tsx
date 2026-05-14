/**
 * New issue creation modal — manual only.
 *
 * title + description + status/priority/assignee/due-date/project chips.
 * Fully wired to `apiClient.issues.create` via useCreateIssue().
 * Description supports inline `@mention` (members + agents).
 *
 * Mention pipeline is shared with `comment-composer.tsx` via the
 * `useMentionInput` hook — both surfaces produce the same canonical
 * `[@name](mention://type/id)` markdown on submit, recognised by the
 * server's util.ParseMentions and mobile's mention-chip renderer.
 *
 * Defaults (status="todo", priority="none") mirror web's ManualCreatePanel —
 * behavioral parity rule (apps/mobile/CLAUDE.md).
 */
import { useCallback, useMemo, useState } from "react";
import {
  Alert,
  KeyboardAvoidingView,
  Platform,
  ScrollView,
  TextInput,
  View,
} from "react-native";
import { Stack, router } from "expo-router";
import type { IssuePriority, IssueStatus } from "@multica/core/types";
import { SubmitIssueButton } from "@/components/issue/submit-issue-button";
import { CreateFormAttributeRow } from "@/components/issue/create-form-attribute-row";
import type { AssigneeValue } from "@/components/issue/pickers/assignee-picker-sheet";
import { MentionSuggestionBar } from "@/components/issue/mention-suggestion-bar";
import { MarkdownToolbar } from "@/components/editor/markdown-toolbar";
import { useFileAttach } from "@/components/editor/use-file-attach";
import {
  MIN_BODY_INPUT_HEIGHT_PX,
  MOBILE_PLACEHOLDER_COLOR,
} from "@/components/ui/input-tokens";
import { cn } from "@/lib/utils";
import { useCreateIssue } from "@/data/mutations/issues";
import {
  useMentionInput,
  type UseMentionInputReturn,
} from "@/lib/use-mention-input";

export default function NewIssueModal() {
  const [title, setTitle] = useState("");
  const description = useMentionInput();
  const [status, setStatus] = useState<IssueStatus>("todo");
  const [priority, setPriority] = useState<IssuePriority>("none");
  const [assignee, setAssignee] = useState<AssigneeValue>(null);
  const [dueDate, setDueDate] = useState<string | null>(null);
  const [projectId, setProjectId] = useState<string | null>(null);

  const createIssue = useCreateIssue();
  const isSubmitting = createIssue.isPending;

  const canSubmit = !isSubmitting && title.trim().length > 0;

  const onSubmit = useCallback(async () => {
    const trimmedTitle = title.trim();
    if (trimmedTitle.length === 0) return;
    const finalDescription = description.serialize().trim();
    try {
      await createIssue.mutateAsync({
        title: trimmedTitle,
        description: finalDescription || undefined,
        status,
        priority,
        ...(assignee
          ? { assignee_type: assignee.type, assignee_id: assignee.id }
          : {}),
        ...(dueDate ? { due_date: dueDate } : {}),
        ...(projectId ? { project_id: projectId } : {}),
      });
      router.back();
    } catch (err) {
      Alert.alert(
        "Failed to create issue",
        err instanceof Error ? err.message : "Unknown error",
      );
    }
  }, [
    title,
    description,
    status,
    priority,
    assignee,
    dueDate,
    projectId,
    createIssue,
  ]);

  const headerRight = useMemo(() => {
    function HeaderRight() {
      return (
        <SubmitIssueButton
          disabled={!canSubmit}
          loading={isSubmitting}
          onPress={onSubmit}
        />
      );
    }
    return HeaderRight;
  }, [canSubmit, isSubmitting, onSubmit]);

  return (
    <>
      <Stack.Screen options={{ headerRight }} />
      <KeyboardAvoidingView
        className="flex-1 bg-background"
        behavior={Platform.OS === "ios" ? "padding" : undefined}
      >
        <ManualPanel
          title={title}
          onTitleChange={setTitle}
          description={description}
          status={status}
          onStatusChange={setStatus}
          priority={priority}
          onPriorityChange={setPriority}
          assignee={assignee}
          onAssigneeChange={setAssignee}
          dueDate={dueDate}
          onDueDateChange={setDueDate}
          projectId={projectId}
          onProjectIdChange={setProjectId}
          submitting={isSubmitting}
        />
      </KeyboardAvoidingView>
    </>
  );
}

function ManualPanel({
  title,
  onTitleChange,
  description,
  status,
  onStatusChange,
  priority,
  onPriorityChange,
  assignee,
  onAssigneeChange,
  dueDate,
  onDueDateChange,
  projectId,
  onProjectIdChange,
  submitting,
}: {
  title: string;
  onTitleChange: (next: string) => void;
  description: UseMentionInputReturn;
  status: IssueStatus;
  onStatusChange: (next: IssueStatus) => void;
  priority: IssuePriority;
  onPriorityChange: (next: IssuePriority) => void;
  assignee: AssigneeValue;
  onAssigneeChange: (next: AssigneeValue) => void;
  dueDate: string | null;
  onDueDateChange: (next: string | null) => void;
  projectId: string | null;
  onProjectIdChange: (next: string | null) => void;
  submitting: boolean;
}) {
  const fileAttach = useFileAttach();

  // Issue not yet created → no issueId / commentId in upload context. The
  // attachment is hooked to the issue at create time via the standard
  // backend flow (same as web's "create issue" path).
  const handleAttachImage = async () => {
    const result = await fileAttach.pickAndUploadImage();
    if (result) description.insertAtCursor(`![](${result.url})`);
  };

  const handleAttachFile = async () => {
    const result = await fileAttach.pickAndUploadFile();
    if (result) {
      description.insertAtCursor(
        `[📎 ${result.filename}](${result.url})`,
      );
    }
  };

  return (
    <>
      <ScrollView
        className="flex-1"
        contentContainerClassName="px-4 pt-4 pb-2 gap-2"
        keyboardShouldPersistTaps="handled"
      >
        <TextInput
          value={title}
          onChangeText={onTitleChange}
          placeholder="Issue title"
          placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
          className="text-2xl font-semibold text-foreground py-2"
          autoFocus
          returnKeyType="next"
          editable={!submitting}
        />
        <DescriptionField description={description} disabled={submitting} />
      </ScrollView>

      <View className="bg-background">
        <MentionSuggestionBar {...description.suggestionBar} />
        <MarkdownToolbar
          onAt={description.handlers.onAtButtonPress}
          onList={() => description.insertAtLineStart("- ")}
          onCheckbox={() => description.insertAtLineStart("- [ ] ")}
          onCode={() => description.insertAtCursor("\n```\n\n```", 4)}
          onQuote={() => description.insertAtLineStart("> ")}
          onImage={handleAttachImage}
          onFile={handleAttachFile}
          disabled={submitting || fileAttach.uploading}
        />
        <CreateFormAttributeRow
          status={status}
          onStatusChange={onStatusChange}
          priority={priority}
          onPriorityChange={onPriorityChange}
          assignee={assignee}
          onAssigneeChange={onAssigneeChange}
          dueDate={dueDate}
          onDueDateChange={onDueDateChange}
          projectId={projectId}
          onProjectIdChange={onProjectIdChange}
        />
      </View>
    </>
  );
}

/** Description field with a focus-tinted rounded-2xl container, visually
 *  matching `CommentComposer`'s input so the two "write markdown body"
 *  surfaces feel like the same product. */
function DescriptionField({
  description,
  disabled,
}: {
  description: UseMentionInputReturn;
  disabled: boolean;
}) {
  const [focused, setFocused] = useState(false);
  return (
    <View
      className={cn(
        "rounded-2xl border px-3",
        focused
          ? "border-primary/30 bg-secondary"
          : "border-transparent bg-secondary/40",
      )}
    >
      <TextInput
        value={description.text}
        onChangeText={description.handlers.onChangeText}
        selection={description.selection}
        onSelectionChange={description.handlers.onSelectionChange}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        placeholder="Description… (type @ to mention)"
        placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
        className="text-base text-foreground py-2"
        style={{ minHeight: MIN_BODY_INPUT_HEIGHT_PX }}
        multiline
        textAlignVertical="top"
        editable={!disabled}
      />
    </View>
  );
}
