import { memo, useCallback, useMemo, useState } from "react";
import {
  ActivityIndicator,
  Image,
  KeyboardAvoidingView,
  Linking,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  SectionList,
  StyleSheet,
  Text,
  TextInput,
  useWindowDimensions,
  View,
  type GestureResponderEvent,
  type NativeSyntheticEvent,
  type TextInputProps,
  type TextInputSelectionChangeEventData,
} from "react-native";
import * as ImagePicker from "expo-image-picker";
import type { NativeStackScreenProps } from "@react-navigation/native-stack";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import {
  useCreateComment,
  useDeleteComment,
  useToggleCommentReaction,
  useToggleIssueReaction,
  useUpdateComment,
  useUpdateIssue,
} from "@multica/core/issues/mutations";
import {
  useChildIssueProgress,
  useChildIssues,
  useIssueAttachments,
  useIssueDetail,
  useOptionalIssueDetail,
  useIssueReactions,
  useIssueTaskRuns,
  useIssueTimelineEntries,
  useTaskMessagesQueries,
} from "@multica/core/issues/hooks";
import { ALL_STATUSES, PRIORITY_CONFIG, PRIORITY_ORDER, STATUS_CONFIG } from "@multica/core/issues/config";
import {
  useActorName,
  useWorkspaceMentionTargets,
  type WorkspaceMentionTarget,
} from "@multica/core/workspace/hooks";
import type {
  AgentTask,
  Attachment,
  IssuePriority,
  IssueReaction,
  IssueStatus,
  Reaction,
  TaskMessagePayload,
  TimelineEntry,
} from "@multica/core/types";
import { Button, EmptyState, LoadingState, Screen } from "../../components/ui/primitives";
import { MarkdownText } from "../../components/ui/markdown";
import { ScreenTitleBar } from "../../components/ui/screen-title-bar";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { uploadMobileAsset, type MobileUploadAsset } from "../../platform/upload";
import { colors, radii, spacing } from "../../theme/tokens";
import {
  createDraftCommentAttachment,
  type DraftCommentAttachment,
} from "./comment-attachment-drafts";

type Props = NativeStackScreenProps<RootStackParamList, "IssueDetail">;
type ReactionLike = Pick<Reaction | IssueReaction, "actor_id" | "actor_type" | "emoji">;
type DocumentPickerModule = typeof import("expo-document-picker");
declare const require: (moduleName: string) => unknown;
type DetailListItem = {
  key: string;
  node: React.ReactElement;
};
type DetailSection = {
  key: string;
  title?: string;
  count?: number;
  collapsed?: boolean;
  onToggle?: () => void;
  data: DetailListItem[];
};
type AttachmentPreviewState = {
  attachment: Attachment;
  textContent?: string;
  error?: string;
  loading?: boolean;
};

const DEFAULT_REACTIONS = ["👍", "👀", "🎉", "❤️"];
const MAX_MENTION_SUGGESTIONS = 8;
const TEXT_PREVIEW_MAX_BYTES = 1_000_000;

export function IssueDetailScreen({ navigation, route }: Props) {
  const { issueId } = route.params;
  const insets = useSafeAreaInsets();
  const userId = useAuthStore((state) => state.user?.id);
  const { workspace } = useMobileWorkspace();
  const { getActorName } = useActorName();
  const mentionTargets = useWorkspaceMentionTargets(workspace.id);
  const { data: issue, isError, isLoading } = useIssueDetail(workspace.id, issueId);
  const { data: parentIssue, isLoading: parentIssueLoading } = useOptionalIssueDetail(
    workspace.id,
    issue?.parent_issue_id,
  );
  const { data: children = [] } = useChildIssues(workspace.id, issueId);
  const { data: childProgress } = useChildIssueProgress(workspace.id);
  const { data: attachments = [], refetch: refetchAttachments } = useIssueAttachments(issueId);
  const { data: issueReactions = [] } = useIssueReactions(issueId);
  const { data: taskRuns = [] } = useIssueTaskRuns(issueId);
  const taskMessageQueries = useTaskMessagesQueries(taskRuns.map((task) => task.id));
  const { data: timeline = [] } = useIssueTimelineEntries(issueId);
  const updateIssue = useUpdateIssue();
  const createComment = useCreateComment(issueId);
  const updateComment = useUpdateComment(issueId);
  const deleteComment = useDeleteComment(issueId);
  const toggleIssueReaction = useToggleIssueReaction(issueId);
  const toggleCommentReaction = useToggleCommentReaction(issueId);
  const [comment, setComment] = useState("");
  const [commentAttachments, setCommentAttachments] = useState<DraftCommentAttachment[]>([]);
  const [replyTargetId, setReplyTargetId] = useState<string | null>(null);
  const [replyContent, setReplyContent] = useState("");
  const [editingCommentId, setEditingCommentId] = useState<string | null>(null);
  const [editingContent, setEditingContent] = useState("");
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [metadataOpen, setMetadataOpen] = useState(false);
  const [commentSheetOpen, setCommentSheetOpen] = useState(false);
  const [commentsCollapsed, setCommentsCollapsed] = useState(false);
  const [timelineCollapsed, setTimelineCollapsed] = useState(false);
  const [attachmentPreview, setAttachmentPreview] = useState<AttachmentPreviewState | null>(null);

  const comments = useMemo(
    () => timeline
      .filter((entry) => entry.type === "comment")
      .sort((a, b) => a.created_at.localeCompare(b.created_at)),
    [timeline],
  );
  const activities = useMemo(
    () => timeline
      .filter((entry) => entry.type === "activity")
      .sort((a, b) => a.created_at.localeCompare(b.created_at)),
    [timeline],
  );
  const taskMessagesByTaskId = useMemo(() => {
    const messagesByTaskId = new Map<string, TaskMessagePayload[]>();
    taskRuns.forEach((task, index) => {
      messagesByTaskId.set(task.id, taskMessageQueries[index]?.data ?? []);
    });
    return messagesByTaskId;
  }, [taskMessageQueries, taskRuns]);
  const renderListItem = useCallback(({ item }: { item: DetailListItem }) => item.node, []);
  const renderSectionHeader = useCallback(({ section }: { section: DetailSection }) => (
    section.title ? <StickySectionHeader section={section} /> : null
  ), []);

  if (isLoading) return <LoadingState />;
  if (isError || !issue) return <EmptyState title="Unable to load issue" />;

  async function changeStatus(status: IssueStatus) {
    if (!issue || status === issue.status) return;
    await updateIssue.mutateAsync({ id: issue.id, status });
  }

  async function changePriority(priority: IssuePriority) {
    if (!issue || priority === issue.priority) return;
    await updateIssue.mutateAsync({ id: issue.id, priority });
  }

  async function submitComment() {
    const content = comment.trim();
    if (!content || createComment.isPending || uploading) return;
    setUploading(true);
    setUploadError(null);
    const uploadedAttachments: Attachment[] = [];
    try {
      for (const draft of commentAttachments) {
        const attachment = await uploadMobileAsset(api, draft, { issueId });
        uploadedAttachments.push(attachment);
      }
      await createComment.mutateAsync({
        content,
        attachmentIds: uploadedAttachments.map((attachment) => attachment.id),
      });
      setComment("");
      setCommentAttachments([]);
      await refetchAttachments();
      setCommentSheetOpen(false);
    } catch (err) {
      await Promise.allSettled(uploadedAttachments.map((attachment) => api.deleteAttachment(attachment.id)));
      setUploadError(err instanceof Error ? err.message : "Unable to send comment");
      await refetchAttachments();
    } finally {
      setUploading(false);
    }
  }

  async function submitReply(parentId: string) {
    const content = replyContent.trim();
    if (!content || createComment.isPending) return;
    await createComment.mutateAsync({ content, parentId });
    setReplyTargetId(null);
    setReplyContent("");
  }

  async function saveCommentEdit(commentId: string) {
    const content = editingContent.trim();
    if (!content || updateComment.isPending) return;
    await updateComment.mutateAsync({ commentId, content });
    setEditingCommentId(null);
    setEditingContent("");
  }

  async function removeComment(commentId: string) {
    if (deleteComment.isPending) return;
    await deleteComment.mutateAsync(commentId);
  }

  function closeCommentSheet() {
    setCommentSheetOpen(false);
    setCommentAttachments([]);
    setUploadError(null);
  }

  function addCommentAttachment(asset: MobileUploadAsset) {
    setCommentAttachments((items) => [
      ...items,
      createDraftCommentAttachment(asset, items.length),
    ]);
  }

  function removeCommentAttachment(attachmentId: string) {
    setCommentAttachments((items) => items.filter((attachment) => attachment.id !== attachmentId));
  }

  async function uploadAttachment(asset: MobileUploadAsset, target: "issue" | "comment") {
    if (target === "comment") {
      addCommentAttachment(asset);
      return;
    }

    setUploading(true);
    setUploadError(null);
    try {
      await uploadMobileAsset(api, asset, { issueId });
      await refetchAttachments();
    } catch (err) {
      setUploadError(err instanceof Error ? err.message : "Upload failed");
    } finally {
      setUploading(false);
    }
  }

  async function pickDocument(target: "issue" | "comment") {
    setUploadError(null);

    let DocumentPicker: DocumentPickerModule;
    try {
      DocumentPicker = require("expo-document-picker") as DocumentPickerModule;
    } catch (err) {
      setUploadError(formatDocumentPickerError(err));
      return;
    }

    let result: Awaited<ReturnType<DocumentPickerModule["getDocumentAsync"]>>;
    try {
      result = await DocumentPicker.getDocumentAsync({
        copyToCacheDirectory: true,
        multiple: false,
        base64: false,
      });
    } catch (err) {
      setUploadError(formatDocumentPickerError(err));
      return;
    }

    if (result.canceled) return;
    const asset = result.assets[0];
    if (!asset) return;
    await uploadAttachment(
      {
        uri: asset.uri,
        name: asset.name,
        mimeType: asset.mimeType,
        size: asset.size,
      },
      target,
    );
  }

  async function pickImage(target: "issue" | "comment") {
    const result = await ImagePicker.launchImageLibraryAsync({
      mediaTypes: ["images"],
      quality: 1,
    });
    if (result.canceled) return;
    const asset = result.assets[0];
    if (!asset) return;
    await uploadAttachment(
      {
        uri: asset.uri,
        name: asset.fileName ?? "image",
        mimeType: asset.mimeType,
        size: asset.fileSize,
      },
      target,
    );
  }

  function handleIssueReaction(emoji: string) {
    if (!userId) return;
    const existing = issueReactions.find((reaction) => isOwnReaction(reaction, emoji, userId));
    toggleIssueReaction.mutate({ emoji, existing });
  }

  function handleCommentReaction(entry: TimelineEntry, emoji: string) {
    if (!userId) return;
    const existing = (entry.reactions ?? []).find((reaction) =>
      isOwnReaction(reaction, emoji, userId),
    );
    toggleCommentReaction.mutate({ commentId: entry.id, emoji, existing });
  }

  function startCommentEdit(commentId: string, content: string) {
    setEditingCommentId(commentId);
    setEditingContent(content);
  }

  async function openAttachmentPreview(attachment: Attachment) {
    if (isImageAttachment(attachment)) {
      setAttachmentPreview({ attachment });
      return;
    }

    if (!isTextPreviewAttachment(attachment)) {
      setAttachmentPreview({ attachment });
      return;
    }

    if (attachment.size_bytes > TEXT_PREVIEW_MAX_BYTES) {
      setAttachmentPreview({
        attachment,
        error: "This file is too large to preview in the app.",
      });
      return;
    }

    setAttachmentPreview({ attachment, loading: true });
    try {
      const response = await fetch(attachment.download_url || attachment.url);
      if (!response.ok) {
        throw new Error(`Unable to load preview (${response.status})`);
      }
      const textContent = await response.text();
      setAttachmentPreview({ attachment, textContent });
    } catch (err) {
      setAttachmentPreview({
        attachment,
        error: err instanceof Error ? err.message : "Unable to load preview",
      });
    }
  }

  const overviewItems: DetailListItem[] = [
    {
      key: "issue-summary",
      node: (
        <View style={styles.section}>
          <Text style={styles.issueBodyTitle}>{issue.title}</Text>
          {issue.description ? (
            <Text style={styles.description}>{issue.description}</Text>
          ) : (
            <Text style={styles.emptyText}>No description</Text>
          )}
          <ReactionRow
            onToggle={handleIssueReaction}
            reactions={issueReactions}
            userId={userId}
          />
        </View>
      ),
    },
    {
      key: "properties",
      node: (
        <View style={styles.card}>
          <Pressable
            onPress={() => setMetadataOpen((open) => !open)}
            style={styles.metadataHeader}
          >
            <View style={styles.metadataTitleGroup}>
              <Text style={styles.sectionTitle}>Properties</Text>
              <Text style={styles.metadataSummary}>
                {STATUS_CONFIG[issue.status].label} / {PRIORITY_CONFIG[issue.priority].label}
              </Text>
            </View>
            <Text style={styles.metadataToggle}>{metadataOpen ? "Hide" : "Show"}</Text>
          </Pressable>
          {metadataOpen ? (
            <View style={styles.metadataBody}>
              <Property label="Status">
                <OptionRow>
                  {ALL_STATUSES.map((status) => (
                    <Chip
                      active={issue.status === status}
                      key={status}
                      label={STATUS_CONFIG[status].label}
                      onPress={() => void changeStatus(status)}
                    />
                  ))}
                </OptionRow>
              </Property>
              <Property label="Priority">
                <OptionRow>
                  {PRIORITY_ORDER.map((priority) => (
                    <Chip
                      active={issue.priority === priority}
                      key={priority}
                      label={PRIORITY_CONFIG[priority].label}
                      onPress={() => void changePriority(priority)}
                    />
                  ))}
                </OptionRow>
              </Property>
              <Property label="Assignee">
                <Text style={styles.value}>
                  {issue.assignee_type && issue.assignee_id
                    ? getActorName(issue.assignee_type, issue.assignee_id)
                    : "Unassigned"}
                </Text>
              </Property>
              <Property label="Creator">
                <Text style={styles.value}>
                  {getActorName(issue.creator_type, issue.creator_id)}
                </Text>
              </Property>
              <Property label="Due date">
                <Text style={styles.value}>{formatDate(issue.due_date)}</Text>
              </Property>
            </View>
          ) : null}
        </View>
      ),
    },
    ...(issue.parent_issue_id
      ? [{
          key: "parent",
          node: (
            <View style={styles.section}>
              <Text style={styles.sectionTitle}>Parent issue</Text>
              <Pressable
                disabled={!parentIssue}
                onPress={() => navigation.push("IssueDetail", { issueId: issue.parent_issue_id! })}
                style={styles.childRow}
              >
                {parentIssue ? (
                  <>
                    <Text style={styles.childIdentifier}>{parentIssue.identifier}</Text>
                    <Text style={styles.childTitle}>{parentIssue.title}</Text>
                  </>
                ) : (
                  <Text style={styles.attachmentMeta}>
                    {parentIssueLoading ? "Loading parent issue..." : "Unable to load parent issue"}
                  </Text>
                )}
              </Pressable>
            </View>
          ),
        }]
      : []),
    ...(children.length > 0
      ? [{
          key: "children",
          node: (
            <View style={styles.section}>
              <Text style={styles.sectionTitle}>Child issues</Text>
              {children.map((child) => (
                <Pressable
                  key={child.id}
                  onPress={() => navigation.push("IssueDetail", { issueId: child.id })}
                  style={styles.childRow}
                >
                  <Text style={styles.childIdentifier}>{child.identifier}</Text>
                  <Text style={styles.childTitle}>{child.title}</Text>
                  {childProgress?.get(child.id) ? (
                    <Text style={styles.attachmentMeta}>
                      {childProgress.get(child.id)?.done}/{childProgress.get(child.id)?.total} child issues done
                    </Text>
                  ) : null}
                </Pressable>
              ))}
            </View>
          ),
        }]
      : []),
    {
      key: "attachments",
      node: (
        <View style={styles.section}>
          <View style={styles.sectionHeader}>
            <Text style={styles.sectionTitle}>Attachments</Text>
            <View style={styles.inlineActions}>
              <Button
                disabled={uploading}
                onPress={() => void pickImage("issue")}
                variant="secondary"
              >
                Image
              </Button>
              <Button
                disabled={uploading}
                onPress={() => void pickDocument("issue")}
                variant="secondary"
              >
                File
              </Button>
            </View>
          </View>
          {uploadError ? <Text style={styles.errorText}>{uploadError}</Text> : null}
          <AttachmentList attachments={attachments} onOpen={openAttachmentPreview} />
        </View>
      ),
    },
  ];

  const commentItems: DetailListItem[] = commentsCollapsed
    ? []
    : comments.length === 0
      ? [{ key: "comments-empty", node: <Text style={styles.emptyText}>No comments yet</Text> }]
      : comments.map((entry) => ({
          key: entry.id,
          node: (
            <TimelineItem
              entry={entry}
              editingCommentId={editingCommentId}
              editingContent={editingContent}
              onToggleReaction={handleCommentReaction}
              onOpenAttachment={openAttachmentPreview}
              onCancelEdit={() => {
                setEditingCommentId(null);
                setEditingContent("");
              }}
              onChangeEdit={setEditingContent}
              onDelete={(commentId) => void removeComment(commentId)}
              onReply={(commentId) => setReplyTargetId(commentId)}
              onSaveEdit={(commentId) => void saveCommentEdit(commentId)}
              onStartEdit={startCommentEdit}
              resolveActorName={getActorName}
              replyContent={replyContent}
              replyTargetId={replyTargetId}
              onCancelReply={() => {
                setReplyTargetId(null);
                setReplyContent("");
              }}
              onChangeReply={setReplyContent}
              onSubmitReply={(commentId) => void submitReply(commentId)}
              userId={userId}
              mentionTargets={mentionTargets}
            />
          ),
        }));

  const timelineItems: DetailListItem[] = timelineCollapsed
    ? []
    : activities.length === 0
      ? [{ key: "timeline-empty", node: <Text style={styles.emptyText}>No activity yet</Text> }]
      : activities.map((entry) => ({
          key: entry.id,
          node: (
            <TimelineItem
              entry={entry}
              resolveActorName={getActorName}
            />
          ),
        }));

  const transcriptItems: DetailListItem[] = taskRuns.length === 0
    ? [
        {
          key: "agent-transcript-empty",
          node: (
            <View style={styles.section}>
              <Text style={styles.sectionTitle}>Agent transcript</Text>
              <Text style={styles.emptyText}>No agent runs yet</Text>
            </View>
          ),
        },
      ]
    : taskRuns.flatMap((task, taskIndex) => {
        const messages = taskMessagesByTaskId.get(task.id) ?? [];
        const taskItems: DetailListItem[] = [
          {
            key: `task-${task.id}`,
            node: (
              <TaskRunHeader
                showTitle={taskIndex === 0}
                task={task}
              />
            ),
          },
        ];

        if (messages.length === 0) {
          taskItems.push({
            key: `task-${task.id}-empty`,
            node: <Text style={styles.emptyText}>No transcript messages</Text>,
          });
          return taskItems;
        }

        taskItems.push(
          ...messages.map((message) => ({
            key: `task-${task.id}-message-${message.seq}`,
            node: <TaskMessageRow message={message} />,
          })),
        );
        return taskItems;
      });

  const sections: DetailSection[] = [
    { key: "overview", data: overviewItems },
    {
      key: "comments",
      title: "Comments",
      count: comments.length,
      collapsed: commentsCollapsed,
      onToggle: () => setCommentsCollapsed((collapsed) => !collapsed),
      data: commentItems,
    },
    {
      key: "timeline",
      title: "Timeline",
      count: activities.length,
      collapsed: timelineCollapsed,
      onToggle: () => setTimelineCollapsed((collapsed) => !collapsed),
      data: timelineItems,
    },
    { key: "transcript", data: transcriptItems },
  ];

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar onBack={() => navigation.goBack()} title={issue.identifier} />
      <KeyboardAvoidingView
        behavior={Platform.OS === "ios" ? "padding" : "height"}
        keyboardVerticalOffset={0}
        style={styles.keyboardAvoidingContent}
      >
        <SectionList
          automaticallyAdjustKeyboardInsets={Platform.OS === "ios"}
          contentContainerStyle={[
            styles.content,
            editingCommentId && styles.contentEditingComment,
          ]}
          keyboardShouldPersistTaps="handled"
          keyExtractor={(item) => item.key}
          maxToRenderPerBatch={8}
          removeClippedSubviews={Platform.OS === "android"}
          renderItem={renderListItem}
          renderSectionHeader={renderSectionHeader}
          sections={sections}
          updateCellsBatchingPeriod={50}
          windowSize={7}
          stickySectionHeadersEnabled
        />

        {!editingCommentId ? (
          <Pressable
            accessibilityLabel="Add a comment"
            accessibilityRole="button"
            onPress={() => setCommentSheetOpen(true)}
            style={({ pressed }) => [
              styles.floatingButton,
              {
                bottom: Math.max(insets.bottom, spacing.lg) + spacing.lg,
                right: Math.max(insets.right, spacing.lg),
              },
              pressed && styles.buttonPressed,
            ]}
          >
            <Text style={styles.floatingButtonText}>+</Text>
          </Pressable>
        ) : null}
      </KeyboardAvoidingView>

      <CommentSheet
        bottomInset={insets.bottom}
        comment={comment}
        createPending={createComment.isPending}
        onChangeComment={setComment}
        onClose={closeCommentSheet}
        onPickDocument={() => void pickDocument("comment")}
        onPickImage={() => void pickImage("comment")}
        onRemoveAttachment={removeCommentAttachment}
        onSubmit={() => void submitComment()}
        open={commentSheetOpen}
        uploadError={uploadError}
        uploading={uploading}
        attachments={commentAttachments}
        mentionTargets={mentionTargets}
      />
      <AttachmentPreviewModal
        onClose={() => setAttachmentPreview(null)}
        open={Boolean(attachmentPreview)}
        preview={attachmentPreview}
      />
    </Screen>
  );
}

function CommentSheet({
  attachments,
  bottomInset,
  comment,
  createPending,
  mentionTargets,
  onChangeComment,
  onClose,
  onPickDocument,
  onPickImage,
  onRemoveAttachment,
  onSubmit,
  open,
  uploadError,
  uploading,
}: {
  attachments: DraftCommentAttachment[];
  bottomInset: number;
  comment: string;
  createPending: boolean;
  mentionTargets: WorkspaceMentionTarget[];
  onChangeComment: (content: string) => void;
  onClose: () => void;
  onPickDocument: () => void;
  onPickImage: () => void;
  onRemoveAttachment: (attachmentId: string) => void;
  onSubmit: () => void;
  open: boolean;
  uploadError: string | null;
  uploading: boolean;
}) {
  const canSubmit = comment.trim().length > 0 && !createPending && !uploading;

  return (
    <Modal
      animationType="fade"
      onRequestClose={onClose}
      transparent
      visible={open}
    >
      <KeyboardAvoidingView
        behavior={Platform.OS === "ios" ? "padding" : "height"}
        style={styles.sheetKeyboardView}
      >
        <Pressable style={styles.sheetBackdrop} onPress={onClose} />
        <View style={[styles.sheet, { paddingBottom: Math.max(bottomInset, spacing.md) }]}>
          <View style={styles.sheetHandle} />
          <View style={styles.sheetHeader}>
            <Text style={styles.sheetTitle}>Add a comment</Text>
            <Button onPress={onClose} variant="ghost">
              Close
            </Button>
          </View>
          <MentionTextInput
            autoFocus
            mentionTargets={mentionTargets}
            multiline
            onChangeText={onChangeComment}
            placeholder="Add a comment"
            placeholderTextColor={colors.mutedForeground}
            scrollEnabled
            style={styles.sheetCommentInput}
            value={comment}
          />
          {attachments.length > 0 ? (
            <DraftAttachmentList attachments={attachments} onRemove={onRemoveAttachment} />
          ) : null}
          {uploadError ? <Text style={styles.errorText}>{uploadError}</Text> : null}
          <View style={styles.sheetActions}>
            <View style={styles.inlineActions}>
              <Button disabled={uploading} onPress={onPickImage} variant="secondary">
                Image
              </Button>
              <Button disabled={uploading} onPress={onPickDocument} variant="secondary">
                File
              </Button>
            </View>
            <Button disabled={!canSubmit} onPress={onSubmit}>
              Send
            </Button>
          </View>
        </View>
      </KeyboardAvoidingView>
    </Modal>
  );
}

function StickySectionHeader({ section }: { section: DetailSection }) {
  if (!section.title || !section.onToggle) return null;

  return (
    <Pressable
      accessibilityRole="button"
      onPress={section.onToggle}
      style={({ pressed }) => [
        styles.stickySectionHeader,
        pressed && styles.buttonPressed,
      ]}
    >
      <View style={styles.stickySectionTitleGroup}>
        <Text style={styles.sectionTitle}>{section.title}</Text>
        {typeof section.count === "number" ? (
          <Text style={styles.stickySectionCount}>{section.count}</Text>
        ) : null}
      </View>
      <Text style={styles.metadataToggle}>{section.collapsed ? "Show" : "Hide"}</Text>
    </Pressable>
  );
}

function MentionTextInput({
  mentionTargets,
  onChangeText,
  onSelectionChange,
  value,
  ...props
}: TextInputProps & {
  mentionTargets: WorkspaceMentionTarget[];
  onChangeText: (text: string) => void;
  value: string;
}) {
  const [selection, setSelection] = useState({ start: value.length, end: value.length });
  const mentionQuery = getActiveMentionQuery(value, selection.start);
  const suggestions = useMemo(
    () => filterMentionTargets(mentionTargets, mentionQuery?.query ?? ""),
    [mentionQuery?.query, mentionTargets],
  );
  const showSuggestions = Boolean(mentionQuery && suggestions.length > 0);

  function handleSelectionChange(
    event: NativeSyntheticEvent<TextInputSelectionChangeEventData>,
  ) {
    setSelection(event.nativeEvent.selection);
    onSelectionChange?.(event);
  }

  function insertMention(target: WorkspaceMentionTarget) {
    if (!mentionQuery) return;
    const mentionText = formatMentionMarkdown(target);
    const nextText = `${value.slice(0, mentionQuery.start)}${mentionText} ${value.slice(selection.end)}`;
    const nextCursor = mentionQuery.start + mentionText.length + 1;
    onChangeText(nextText);
    setSelection({ start: nextCursor, end: nextCursor });
  }

  return (
    <View style={styles.mentionInputWrap}>
      <TextInput
        {...props}
        onChangeText={onChangeText}
        onSelectionChange={handleSelectionChange}
        selection={selection}
        value={value}
      />
      {showSuggestions ? (
        <View style={styles.mentionSuggestions}>
          {suggestions.map((target) => (
            <Pressable
              key={`${target.type}-${target.id}`}
              onPress={() => insertMention(target)}
              style={styles.mentionSuggestionRow}
            >
              <View style={styles.mentionAvatar}>
                <Text style={styles.mentionAvatarText}>{target.type === "agent" ? "A" : "@"}</Text>
              </View>
              <View style={styles.mentionSuggestionTextGroup}>
                <Text numberOfLines={1} style={styles.mentionSuggestionName}>
                  {target.label}
                </Text>
                <Text style={styles.mentionSuggestionType}>
                  {target.type === "agent" ? "Agent" : target.type === "all" ? "All members" : "Member"}
                </Text>
              </View>
            </Pressable>
          ))}
        </View>
      ) : null}
    </View>
  );
}

function getActiveMentionQuery(text: string, cursor: number) {
  const beforeCursor = text.slice(0, cursor);
  const match = beforeCursor.match(/(?:^|\s)@([^\s@()[\]]*)$/);
  if (!match) return null;
  const query = match[1] ?? "";
  return {
    query,
    start: cursor - query.length - 1,
  };
}

function filterMentionTargets(targets: WorkspaceMentionTarget[], query: string) {
  const q = query.trim().toLowerCase();
  return targets
    .filter((target) => {
      if (!q) return true;
      return target.label.toLowerCase().includes(q);
    })
    .slice(0, MAX_MENTION_SUGGESTIONS);
}

function formatMentionMarkdown(target: WorkspaceMentionTarget) {
  const label = target.type === "all" ? "@All members" : `@${target.label}`;
  return `[${label}](mention://${target.type}/${target.id})`;
}

function Property({
  children,
  label,
}: {
  children: React.ReactNode;
  label: string;
}) {
  return (
    <View style={styles.property}>
      <Text style={styles.propertyLabel}>{label}</Text>
      <View style={styles.propertyValue}>{children}</View>
    </View>
  );
}

function OptionRow({ children }: { children: React.ReactNode }) {
  return <View style={styles.optionRow}>{children}</View>;
}

function Chip({
  active,
  label,
  onPress,
}: {
  active: boolean;
  label: string;
  onPress: () => void;
}) {
  return (
    <Pressable
      onPress={onPress}
      style={[styles.chip, active && styles.chipActive]}
    >
      <Text style={[styles.chipText, active && styles.chipTextActive]}>{label}</Text>
    </Pressable>
  );
}

function TimelineItem({
  editingCommentId,
  editingContent,
  entry,
  onCancelEdit,
  onCancelReply,
  onChangeEdit,
  onChangeReply,
  onDelete,
  onOpenAttachment,
  onReply,
  onSaveEdit,
  onStartEdit,
  onSubmitReply,
  onToggleReaction,
  replyContent,
  replyTargetId,
  resolveActorName,
  userId,
  mentionTargets,
}: {
  editingCommentId?: string | null;
  editingContent?: string;
  entry: TimelineEntry;
  onCancelEdit?: () => void;
  onCancelReply?: () => void;
  onChangeEdit?: (content: string) => void;
  onChangeReply?: (content: string) => void;
  onDelete?: (commentId: string) => void;
  onOpenAttachment?: (attachment: Attachment) => void;
  onReply?: (commentId: string) => void;
  onSaveEdit?: (commentId: string) => void;
  onStartEdit?: (commentId: string, content: string) => void;
  onSubmitReply?: (commentId: string) => void;
  onToggleReaction?: (entry: TimelineEntry, emoji: string) => void;
  replyContent?: string;
  replyTargetId?: string | null;
  resolveActorName: (type: string, id: string) => string;
  userId?: string;
  mentionTargets?: WorkspaceMentionTarget[];
}) {
  const actor = resolveActorName(entry.actor_type, entry.actor_id);
  const isOwnComment = entry.type === "comment" && entry.actor_type === "member" && entry.actor_id === userId;
  const isEditing = editingCommentId === entry.id;
  const isReplying = replyTargetId === entry.id;
  const [openMenu, setOpenMenu] = useState<"reactions" | "actions" | null>(null);
  const [actionsMenuAnchor, setActionsMenuAnchor] = useState<{ x: number; y: number } | null>(null);
  const { height: windowHeight, width: windowWidth } = useWindowDimensions();
  const body = entry.type === "comment"
    ? entry.content
    : formatActivity(entry, resolveActorName);
  const isComment = entry.type === "comment";
  const reactionOptions = Array.from(new Set([...DEFAULT_REACTIONS, ...(entry.reactions ?? []).map((r) => r.emoji)]));

  function openActionsMenuAtPress(event: GestureResponderEvent) {
    if (!isComment || isEditing) return;
    setActionsMenuAnchor({
      x: event.nativeEvent.pageX,
      y: event.nativeEvent.pageY,
    });
    setOpenMenu("actions");
  }

  function closeActionsMenu() {
    setOpenMenu(null);
    setActionsMenuAnchor(null);
  }

  function renderActionsDropdownContent() {
    return (
      <>
        <DropdownItem
          label="Reply"
          onPress={() => {
            onReply?.(entry.id);
            closeActionsMenu();
          }}
        />
        {isOwnComment ? (
          <>
            <DropdownItem
              label="Edit"
              onPress={() => {
                onStartEdit?.(entry.id, entry.content ?? "");
                closeActionsMenu();
              }}
            />
            <DropdownItem
              destructive
              label="Delete"
              onPress={() => {
                onDelete?.(entry.id);
                closeActionsMenu();
              }}
            />
          </>
        ) : null}
      </>
    );
  }

  function renderActionsMenuModal() {
    if (openMenu !== "actions" || !actionsMenuAnchor) return null;
    const menuWidth = 132;
    const menuHeight = isOwnComment ? 116 : 44;
    const left = Math.max(spacing.md, Math.min(actionsMenuAnchor.x - menuWidth / 2, windowWidth - menuWidth - spacing.md));
    const top = Math.max(spacing.md, Math.min(actionsMenuAnchor.y + spacing.xs, windowHeight - menuHeight - spacing.md));

    return (
      <Modal animationType="fade" onRequestClose={closeActionsMenu} transparent visible>
        <View style={styles.menuModalOverlay}>
          <Pressable style={StyleSheet.absoluteFill} onPress={closeActionsMenu} />
          <View style={[
            styles.commentDropdown,
            styles.commentDropdownFloating,
            { left, top, width: menuWidth },
          ]}>
            {renderActionsDropdownContent()}
          </View>
        </View>
      </Modal>
    );
  }

  return (
    <Pressable
      delayLongPress={320}
      onLongPress={isComment && !isEditing ? openActionsMenuAtPress : undefined}
      style={styles.timelineItem}
    >
      {renderActionsMenuModal()}
      <View style={isComment ? styles.commentHeader : styles.timelineHeader}>
        <View style={styles.timelineActorGroup}>
          <Text style={styles.timelineActor}>{actor}</Text>
          <Text style={styles.timelineDate}>{formatDate(entry.created_at)}</Text>
        </View>
        {isComment ? (
          <View style={styles.commentHeaderActions}>
            <View style={styles.commentHeaderButtonRow}>
              <HeaderIconButton
                disabled={!userId}
                label="React"
                onPress={() => {
                  setActionsMenuAnchor(null);
                  setOpenMenu((menu) => menu === "reactions" ? null : "reactions");
                }}
              >
                ☺
              </HeaderIconButton>
              <HeaderIconButton
                label="Comment actions"
                onPress={(event) => {
                  if (openMenu === "actions") {
                    closeActionsMenu();
                    return;
                  }
                  setActionsMenuAnchor({
                    x: event.nativeEvent.pageX,
                    y: event.nativeEvent.pageY,
                  });
                  setOpenMenu("actions");
                }}
              >
                ⋯
              </HeaderIconButton>
            </View>
            {openMenu === "reactions" ? (
              <View style={styles.commentDropdown}>
                {reactionOptions.map((emoji) => {
                  const count = (entry.reactions ?? []).filter((reaction) => reaction.emoji === emoji).length;
                  const active = Boolean(userId && (entry.reactions ?? []).some((reaction) => isOwnReaction(reaction, emoji, userId)));
                  return (
                    <DropdownItem
                      active={active}
                      key={emoji}
                      label={`${emoji}${count > 0 ? ` ${count}` : ""}`}
                      onPress={() => {
                        onToggleReaction?.(entry, emoji);
                        setOpenMenu(null);
                        setActionsMenuAnchor(null);
                      }}
                    />
                  );
                })}
              </View>
            ) : null}
          </View>
        ) : null}
      </View>
      {isEditing ? (
        <View style={styles.editBox}>
          <MentionTextInput
            autoFocus
            mentionTargets={mentionTargets ?? []}
            multiline
            onChangeText={onChangeEdit ?? (() => {})}
            style={styles.commentInput}
            value={editingContent ?? ""}
          />
          <View style={styles.inlineActions}>
            <Button onPress={() => onSaveEdit?.(entry.id)}>Save</Button>
            <Button onPress={() => {
              setOpenMenu(null);
              setActionsMenuAnchor(null);
              onCancelEdit?.();
            }} variant="secondary">
              Cancel
            </Button>
          </View>
        </View>
      ) : entry.type === "comment" ? (
        <MarkdownText content={entry.content ?? ""} />
      ) : (
        <Text style={styles.timelineBody}>{body}</Text>
      )}
      {entry.type === "comment" ? (
        <AttachmentList
          attachments={entry.attachments ?? []}
          compact
          onOpen={onOpenAttachment ?? ((attachment) => void Linking.openURL(attachment.download_url || attachment.url))}
        />
      ) : null}
      {isReplying ? (
        <View style={styles.replyBox}>
          <MentionTextInput
            mentionTargets={mentionTargets ?? []}
            multiline
            onChangeText={onChangeReply ?? (() => {})}
            placeholder="Reply"
            placeholderTextColor={colors.mutedForeground}
            style={styles.commentInput}
            value={replyContent ?? ""}
          />
          <View style={styles.inlineActions}>
            <Button onPress={() => onSubmitReply?.(entry.id)}>Send reply</Button>
            <Button onPress={onCancelReply} variant="secondary">
              Cancel
            </Button>
          </View>
        </View>
      ) : null}
    </Pressable>
  );
}

function HeaderIconButton({
  children,
  disabled,
  label,
  onPress,
}: React.PropsWithChildren<{
  disabled?: boolean;
  label: string;
  onPress: (event: GestureResponderEvent) => void;
}>) {
  return (
    <Pressable
      accessibilityLabel={label}
      accessibilityRole="button"
      disabled={disabled}
      onPress={onPress}
      style={({ pressed }) => [
        styles.headerIconButton,
        disabled && styles.headerIconButtonDisabled,
        pressed && !disabled && styles.buttonPressed,
      ]}
    >
      <Text style={styles.headerIconButtonText}>{children}</Text>
    </Pressable>
  );
}

function DropdownItem({
  active,
  destructive,
  label,
  onPress,
}: {
  active?: boolean;
  destructive?: boolean;
  label: string;
  onPress: () => void;
}) {
  return (
    <Pressable onPress={onPress} style={[styles.dropdownItem, active && styles.dropdownItemActive]}>
      <Text style={[styles.dropdownItemText, destructive && styles.dropdownItemTextDestructive]}>
        {label}
      </Text>
    </Pressable>
  );
}

function AttachmentList({
  attachments,
  compact,
  onOpen,
  onRemove,
  removingAttachmentId,
}: {
  attachments: Attachment[];
  compact?: boolean;
  onOpen: (attachment: Attachment) => void;
  onRemove?: (attachmentId: string) => void;
  removingAttachmentId?: string | null;
}) {
  if (attachments.length === 0) {
    if (compact) return null;
    return <Text style={styles.emptyText}>No attachments yet</Text>;
  }

  return (
    <View style={styles.attachmentList}>
      {attachments.map((attachment) => (
        <Pressable
          key={attachment.id}
          onPress={() => onOpen(attachment)}
          style={({ pressed }) => [
            styles.attachmentRow,
            pressed && styles.buttonPressed,
          ]}
        >
          <View style={styles.attachmentContent}>
            <Text style={styles.attachmentName}>{attachment.filename}</Text>
            <Text style={styles.attachmentMeta}>
              {formatBytes(attachment.size_bytes)} / {attachment.content_type || "file"} / {attachmentPreviewLabel(attachment)}
            </Text>
          </View>
          {onRemove ? (
            <Pressable
              accessibilityLabel={`Remove ${attachment.filename}`}
              accessibilityRole="button"
              disabled={removingAttachmentId === attachment.id}
              hitSlop={8}
              onPress={(event) => {
                event.stopPropagation();
                onRemove(attachment.id);
              }}
              style={({ pressed }) => [
                styles.attachmentRemoveButton,
                removingAttachmentId === attachment.id && styles.headerIconButtonDisabled,
                pressed && styles.buttonPressed,
              ]}
            >
              <Text style={styles.attachmentRemoveText}>
                {removingAttachmentId === attachment.id ? "..." : "Remove"}
              </Text>
            </Pressable>
          ) : null}
        </Pressable>
      ))}
    </View>
  );
}

function DraftAttachmentList({
  attachments,
  onRemove,
}: {
  attachments: DraftCommentAttachment[];
  onRemove: (attachmentId: string) => void;
}) {
  return (
    <View style={styles.attachmentList}>
      {attachments.map((attachment) => (
        <View key={attachment.id} style={styles.attachmentRow}>
          <View style={styles.attachmentContent}>
            <Text style={styles.attachmentName}>{attachment.name}</Text>
            <Text style={styles.attachmentMeta}>
              {formatBytes(attachment.size ?? 0)} / {attachment.mimeType || "file"}
            </Text>
          </View>
          <Pressable
            accessibilityLabel={`Remove ${attachment.name}`}
            accessibilityRole="button"
            hitSlop={8}
            onPress={() => onRemove(attachment.id)}
            style={({ pressed }) => [
              styles.attachmentRemoveButton,
              pressed && styles.buttonPressed,
            ]}
          >
            <Text style={styles.attachmentRemoveText}>Remove</Text>
          </Pressable>
        </View>
      ))}
    </View>
  );
}

function AttachmentPreviewModal({
  onClose,
  open,
  preview,
}: {
  onClose: () => void;
  open: boolean;
  preview: AttachmentPreviewState | null;
}) {
  const attachment = preview?.attachment;
  const url = attachment ? attachment.download_url || attachment.url : "";
  const canPreviewImage = Boolean(attachment && isImageAttachment(attachment));
  const canPreviewText = Boolean(attachment && isTextPreviewAttachment(attachment));

  return (
    <Modal animationType="slide" onRequestClose={onClose} transparent visible={open}>
      <View style={styles.previewModal}>
        <View style={styles.previewHeader}>
          <View style={styles.previewTitleGroup}>
            <Text numberOfLines={1} style={styles.previewTitle}>
              {attachment?.filename ?? "Attachment"}
            </Text>
            {attachment ? (
              <Text style={styles.previewMeta}>
                {formatBytes(attachment.size_bytes)} / {attachment.content_type || "file"}
              </Text>
            ) : null}
          </View>
          {attachment ? (
            <View style={styles.previewActions}>
              <Button onPress={() => void Linking.openURL(url)} variant="secondary">
                Open
              </Button>
              <Button onPress={onClose} variant="ghost">
                Close
              </Button>
            </View>
          ) : null}
        </View>

        <View style={styles.previewBody}>
          {attachment && canPreviewImage ? (
            <Image resizeMode="contain" source={{ uri: url }} style={styles.previewImage} />
          ) : null}

          {attachment && canPreviewText ? (
            preview?.loading ? (
              <View style={styles.previewCentered}>
                <ActivityIndicator />
                <Text style={styles.attachmentMeta}>Loading preview...</Text>
              </View>
            ) : (
              <ScrollView contentContainerStyle={styles.previewTextContent}>
                <Text selectable style={styles.previewText}>
                  {preview?.error ?? preview?.textContent ?? "No preview available."}
                </Text>
              </ScrollView>
            )
          ) : null}

          {attachment && !canPreviewImage && !canPreviewText ? (
            <View style={styles.previewCentered}>
              <Text style={styles.previewUnsupportedTitle}>Preview unavailable</Text>
              <Text style={styles.previewUnsupportedBody}>
                This file type cannot be displayed in the app yet.
              </Text>
              <Button onPress={() => void Linking.openURL(url)} variant="secondary">
                Open externally
              </Button>
            </View>
          ) : null}

          {attachment && preview?.error && !canPreviewText ? (
            <View style={styles.previewCentered}>
              <Text style={styles.errorText}>{preview.error}</Text>
              <Button onPress={() => void Linking.openURL(url)} variant="secondary">
                Open externally
              </Button>
            </View>
          ) : null}
        </View>
      </View>
    </Modal>
  );
}

const TaskRunHeader = memo(function TaskRunHeader({
  showTitle,
  task,
}: {
  showTitle: boolean;
  task: AgentTask;
}) {
  return (
    <View style={styles.taskCard}>
      {showTitle ? <Text style={styles.sectionTitle}>Agent transcript</Text> : null}
      <View style={styles.timelineHeader}>
        <Text style={styles.timelineActor}>Run {task.id.slice(0, 8)}</Text>
        <Text style={styles.timelineDate}>{task.status}</Text>
      </View>
      {task.error ? <Text style={styles.errorText}>{task.error}</Text> : null}
    </View>
  );
});

const TaskMessageRow = memo(function TaskMessageRow({ message }: { message: TaskMessagePayload }) {
  const content =
    message.content ??
    message.output ??
    (message.input ? JSON.stringify(message.input) : "");

  return (
    <View style={styles.taskMessage}>
      <Text style={styles.taskMessageType}>
        #{message.seq} {message.type}{message.tool ? ` / ${message.tool}` : ""}
      </Text>
      {content ? <Text style={styles.timelineBody}>{content}</Text> : null}
    </View>
  );
});

function ReactionRow({
  compact,
  onToggle,
  reactions,
  userId,
}: {
  compact?: boolean;
  onToggle: (emoji: string) => void;
  reactions: ReactionLike[];
  userId?: string;
}) {
  const emojis = Array.from(new Set([...DEFAULT_REACTIONS, ...reactions.map((r) => r.emoji)]));

  return (
    <View style={styles.reactionRow}>
      {emojis.map((emoji) => {
        const count = reactions.filter((reaction) => reaction.emoji === emoji).length;
        const active = Boolean(userId && reactions.some((reaction) => isOwnReaction(reaction, emoji, userId)));
        return (
          <Pressable
            disabled={!userId}
            key={emoji}
            onPress={() => onToggle(emoji)}
            style={[
              styles.reactionChip,
              compact && styles.reactionChipCompact,
              active && styles.reactionChipActive,
            ]}
          >
            <Text style={[
              styles.reactionText,
              compact && styles.reactionTextCompact,
              active && styles.reactionTextActive,
            ]}>
              {emoji}{count > 0 ? ` ${count}` : ""}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}

function isOwnReaction(reaction: ReactionLike, emoji: string, userId: string) {
  return reaction.emoji === emoji && reaction.actor_type === "member" && reaction.actor_id === userId;
}

function formatDate(date: string | null | undefined) {
  if (!date) return "-";
  return new Date(date).toLocaleDateString();
}

function formatBytes(bytes: number) {
  if (bytes >= 1_000_000) return `${(bytes / 1_000_000).toFixed(1)} MB`;
  if (bytes >= 1_000) return `${Math.round(bytes / 1_000)} KB`;
  return `${bytes} B`;
}

function isImageAttachment(attachment: Attachment) {
  const contentType = attachment.content_type.toLowerCase();
  return contentType.startsWith("image/") || /\.(avif|gif|heic|heif|jpe?g|png|webp)$/i.test(attachment.filename);
}

function isTextPreviewAttachment(attachment: Attachment) {
  const contentType = attachment.content_type.toLowerCase();
  const filename = attachment.filename.toLowerCase();
  if (contentType.startsWith("text/")) return true;
  if (
    [
      "application/json",
      "application/javascript",
      "application/typescript",
      "application/xml",
      "application/x-javascript",
      "application/x-ndjson",
      "application/yaml",
    ].includes(contentType)
  ) {
    return true;
  }
  return /\.(c|conf|cpp|css|csv|go|h|html|java|js|json|jsonl|log|md|py|rb|rs|sh|sql|ts|tsx|txt|xml|ya?ml)$/i.test(filename);
}

function attachmentPreviewLabel(attachment: Attachment) {
  if (isImageAttachment(attachment)) return "tap to view";
  if (isTextPreviewAttachment(attachment)) return "tap to preview";
  return "tap to open";
}

function formatDocumentPickerError(err: unknown) {
  const message = err instanceof Error ? err.message : String(err);
  if (message.includes("ExpoDocumentPicker")) {
    return "File picker is unavailable in this app build. Rebuild and reinstall the mobile app so expo-document-picker is included.";
  }
  return message || "File picker unavailable";
}

function statusLabel(status: string) {
  return STATUS_CONFIG[status as IssueStatus]?.label ?? status;
}

function priorityLabel(priority: string) {
  return PRIORITY_CONFIG[priority as IssuePriority]?.label ?? priority;
}

function formatActivity(
  entry: TimelineEntry,
  resolveActorName: (type: string, id: string) => string,
) {
  const details = (entry.details ?? {}) as Record<string, string>;
  switch (entry.action) {
    case "created":
      return "created this issue";
    case "status_changed":
      return `changed status from ${statusLabel(details.from ?? "?")} to ${statusLabel(details.to ?? "?")}`;
    case "priority_changed":
      return `changed priority from ${priorityLabel(details.from ?? "?")} to ${priorityLabel(details.to ?? "?")}`;
    case "assignee_changed": {
      const toName = details.to_id && details.to_type
        ? resolveActorName(details.to_type, details.to_id)
        : null;
      if (toName) return `assigned to ${toName}`;
      if (details.from_id && !details.to_id) return "removed assignee";
      return "changed assignee";
    }
    case "due_date_changed":
      return details.to ? `set due date to ${formatDate(details.to)}` : "removed due date";
    case "description_updated":
      return "updated the description";
    case "title_changed":
      return "renamed this issue";
    case "task_completed":
      return "completed the task";
    case "task_failed":
      return "task failed";
    default:
      return entry.action ?? "updated this issue";
  }
}

const styles = StyleSheet.create({
  content: {
    gap: spacing.lg,
    padding: spacing.lg,
    paddingBottom: 96,
  },
  contentEditingComment: {
    paddingBottom: 240,
  },
  keyboardAvoidingContent: {
    flex: 1,
  },
  section: {
    gap: spacing.sm,
  },
  sectionHeader: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.md,
    justifyContent: "space-between",
  },
  stickySectionHeader: {
    alignItems: "center",
    backgroundColor: colors.background,
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.md,
    justifyContent: "space-between",
    marginHorizontal: -spacing.lg,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
  },
  stickySectionTitleGroup: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
  },
  stickySectionCount: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  inlineActions: {
    alignItems: "center",
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.sm,
  },
  buttonPressed: {
    opacity: 0.8,
  },
  issueBodyTitle: {
    color: colors.foreground,
    fontSize: 17,
    fontWeight: "700",
    lineHeight: 24,
  },
  description: {
    color: colors.foreground,
    fontSize: 14,
    lineHeight: 20,
  },
  emptyText: {
    color: colors.mutedForeground,
    fontSize: 14,
  },
  errorText: {
    color: colors.destructive,
    fontSize: 14,
    lineHeight: 20,
  },
  card: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.md,
    padding: spacing.md,
  },
  property: {
    gap: spacing.xs,
  },
  metadataHeader: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.md,
    justifyContent: "space-between",
  },
  metadataTitleGroup: {
    flex: 1,
    gap: spacing.xs,
  },
  metadataSummary: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
  metadataToggle: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
  },
  metadataBody: {
    gap: spacing.md,
  },
  propertyLabel: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  propertyValue: {
    minHeight: 28,
    justifyContent: "center",
  },
  optionRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.sm,
  },
  chip: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  chipActive: {
    backgroundColor: colors.primary,
  },
  chipText: {
    color: colors.foreground,
    fontSize: 12,
    fontWeight: "500",
  },
  chipTextActive: {
    color: colors.primaryForeground,
  },
  value: {
    color: colors.foreground,
    fontSize: 14,
  },
  sectionTitle: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "500",
  },
  childRow: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.xs,
    padding: spacing.md,
  },
  childIdentifier: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  childTitle: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
  },
  timelineItem: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.sm,
    padding: spacing.md,
    position: "relative",
  },
  timelineHeader: {
    alignItems: "flex-start",
    flexDirection: "row",
    gap: spacing.md,
    justifyContent: "space-between",
  },
  commentHeader: {
    alignItems: "flex-start",
    flexDirection: "row",
    gap: spacing.md,
    justifyContent: "space-between",
  },
  timelineActorGroup: {
    flex: 1,
    gap: spacing.xs,
    minWidth: 0,
  },
  timelineActor: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
  },
  timelineDate: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
  commentHeaderActions: {
    alignItems: "flex-end",
    position: "relative",
    zIndex: 10,
  },
  commentHeaderButtonRow: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.xs,
  },
  headerIconButton: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.sm,
    height: 30,
    justifyContent: "center",
    width: 30,
  },
  headerIconButtonDisabled: {
    opacity: 0.45,
  },
  headerIconButtonText: {
    color: colors.foreground,
    fontSize: 18,
    fontWeight: "500",
    lineHeight: 22,
  },
  commentDropdown: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    minWidth: 118,
    paddingVertical: spacing.xs,
    position: "absolute",
    right: 0,
    top: 34,
    shadowColor: "#000000",
    shadowOffset: { height: 4, width: 0 },
    shadowOpacity: 0.14,
    shadowRadius: 10,
    elevation: 10,
  },
  commentDropdownFloating: {
    right: "auto",
    top: "auto",
    zIndex: 20,
  },
  menuModalOverlay: {
    flex: 1,
  },
  dropdownItem: {
    minHeight: 36,
    justifyContent: "center",
    paddingHorizontal: spacing.md,
  },
  dropdownItemActive: {
    backgroundColor: colors.muted,
  },
  dropdownItemText: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
  },
  dropdownItemTextDestructive: {
    color: colors.destructive,
  },
  timelineBody: {
    color: colors.foreground,
    fontSize: 14,
    lineHeight: 20,
  },
  editBox: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    gap: spacing.sm,
    padding: spacing.md,
  },
  replyBox: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    gap: spacing.sm,
    padding: spacing.md,
  },
  mentionInputWrap: {
    gap: spacing.sm,
  },
  mentionSuggestions: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  mentionSuggestionRow: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
    minHeight: 48,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  mentionAvatar: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: 14,
    height: 28,
    justifyContent: "center",
    width: 28,
  },
  mentionAvatarText: {
    color: colors.foreground,
    fontSize: 12,
    fontWeight: "600",
  },
  mentionSuggestionTextGroup: {
    flex: 1,
    minWidth: 0,
  },
  mentionSuggestionName: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
  },
  mentionSuggestionType: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
  attachmentList: {
    gap: spacing.sm,
  },
  attachmentRow: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.md,
    justifyContent: "space-between",
    padding: spacing.md,
  },
  attachmentContent: {
    flex: 1,
    gap: spacing.xs,
    minWidth: 0,
  },
  attachmentName: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
  },
  attachmentMeta: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
  attachmentRemoveButton: {
    alignItems: "center",
    justifyContent: "center",
    minHeight: 32,
    paddingHorizontal: spacing.sm,
  },
  attachmentRemoveText: {
    color: colors.destructive,
    fontSize: 12,
    fontWeight: "500",
  },
  previewModal: {
    backgroundColor: colors.background,
    flex: 1,
    paddingTop: Platform.OS === "ios" ? 56 : spacing.lg,
  },
  previewHeader: {
    alignItems: "center",
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.md,
    justifyContent: "space-between",
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
  },
  previewTitleGroup: {
    flex: 1,
    gap: spacing.xs,
  },
  previewTitle: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "600",
  },
  previewMeta: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
  previewActions: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
  },
  previewBody: {
    flex: 1,
  },
  previewImage: {
    flex: 1,
    height: "100%",
    width: "100%",
  },
  previewCentered: {
    alignItems: "center",
    flex: 1,
    gap: spacing.md,
    justifyContent: "center",
    padding: spacing.lg,
  },
  previewTextContent: {
    padding: spacing.lg,
  },
  previewText: {
    color: colors.foreground,
    fontFamily: Platform.select({ ios: "Menlo", android: "monospace", default: undefined }),
    fontSize: 13,
    lineHeight: 19,
  },
  previewUnsupportedTitle: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "600",
    textAlign: "center",
  },
  previewUnsupportedBody: {
    color: colors.mutedForeground,
    fontSize: 14,
    lineHeight: 20,
    textAlign: "center",
  },
  taskCard: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.sm,
    padding: spacing.md,
  },
  taskMessage: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    gap: spacing.xs,
    padding: spacing.sm,
  },
  taskMessageType: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  reactionRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.sm,
  },
  reactionChip: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  reactionChipCompact: {
    borderRadius: radii.sm,
    paddingHorizontal: spacing.xs,
    paddingVertical: spacing.xs,
  },
  reactionChipActive: {
    backgroundColor: colors.primary,
  },
  reactionText: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
  },
  reactionTextCompact: {
    fontSize: 12,
  },
  reactionTextActive: {
    color: colors.primaryForeground,
  },
  floatingButton: {
    alignItems: "center",
    backgroundColor: colors.primary,
    borderRadius: 28,
    height: 56,
    justifyContent: "center",
    position: "absolute",
    shadowColor: "#000000",
    shadowOffset: { height: 4, width: 0 },
    shadowOpacity: 0.18,
    shadowRadius: 10,
    width: 56,
    elevation: 6,
  },
  floatingButtonText: {
    color: colors.primaryForeground,
    fontSize: 30,
    fontWeight: "400",
    lineHeight: 34,
  },
  sheetKeyboardView: {
    flex: 1,
    justifyContent: "flex-end",
  },
  sheetBackdrop: {
    backgroundColor: "rgba(0, 0, 0, 0.28)",
    bottom: 0,
    left: 0,
    position: "absolute",
    right: 0,
    top: 0,
  },
  sheet: {
    backgroundColor: colors.card,
    borderTopLeftRadius: radii.md,
    borderTopRightRadius: radii.md,
    gap: spacing.md,
    maxHeight: "82%",
    paddingHorizontal: spacing.lg,
    paddingTop: spacing.sm,
    shadowColor: "#000000",
    shadowOffset: { height: -3, width: 0 },
    shadowOpacity: 0.12,
    shadowRadius: 12,
    elevation: 12,
  },
  sheetHandle: {
    alignSelf: "center",
    backgroundColor: colors.border,
    borderRadius: 2,
    height: 4,
    width: 40,
  },
  sheetHeader: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.md,
    justifyContent: "space-between",
  },
  sheetTitle: {
    color: colors.foreground,
    flex: 1,
    fontSize: 16,
    fontWeight: "500",
  },
  commentInput: {
    color: colors.foreground,
    fontSize: 14,
    includeFontPadding: false,
    lineHeight: 20,
    minHeight: 96,
    textAlignVertical: "top",
  },
  sheetCommentInput: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    color: colors.foreground,
    fontSize: 16,
    includeFontPadding: false,
    lineHeight: 22,
    maxHeight: 180,
    minHeight: 128,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.md,
    textAlignVertical: "top",
  },
  sheetActions: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.md,
    justifyContent: "space-between",
  },
});
