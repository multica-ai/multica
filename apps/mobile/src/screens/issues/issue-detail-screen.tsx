import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  Clipboard,
  Keyboard,
  KeyboardAvoidingView,
  Linking,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  useWindowDimensions,
  View,
  type GestureResponderEvent,
  type NativeSyntheticEvent,
  type TextInputProps,
  type TextInputKeyPressEventData,
  type TextInputSelectionChangeEventData,
} from "react-native";
import * as ImagePicker from "expo-image-picker";
import { LinearGradient } from "expo-linear-gradient";
import { FlashList, type FlashListRef } from "@shopify/flash-list";
import type { NativeStackScreenProps } from "@react-navigation/native-stack";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { MoreHorizontal } from "lucide-react-native";
import Svg, { Path } from "react-native-svg";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import {
  useCreateComment,
  useDeleteComment,
  useUpdateComment,
  useUpdateIssue,
} from "@multica/core/issues/mutations";
import {
  useIssueAttachments,
  useIssueDetail,
  useIssueList,
  useIssueSubscribers,
  useIssueTaskRuns,
  useIssueTimelineEntries,
  useLiveIssueTasks,
} from "@multica/core/issues/hooks";
import {
  useActorName,
  useWorkspaceMentionTargets,
  type WorkspaceMentionTarget,
} from "@multica/core/workspace/hooks";
import {
  issueToMentionTarget,
  mergeMentionTargets,
} from "@multica/core/workspace/mentions";
import type {
  AgentTask,
  Attachment,
  IssuePriority,
  IssueStatus,
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
import { ImagePreviewModal } from "./image-preview-modal";
import {
  createDraftCommentAttachment,
  type DraftCommentAttachment,
} from "./comment-attachment-drafts";
import { TaskMessageRow } from "./task-transcript-components";
import {
  formatAgentTaskStatus,
  formatIssuePriority,
  formatIssueStatus,
} from "../../i18n/format";

type Props = NativeStackScreenProps<RootStackParamList, "IssueDetail">;
type TimelineProps = NativeStackScreenProps<RootStackParamList, "IssueTimeline">;
type TaskRunsProps = NativeStackScreenProps<RootStackParamList, "IssueTaskRuns">;
type DocumentPickerModule = typeof import("expo-document-picker");
declare const require: (moduleName: string) => unknown;
const emptyTimeline: TimelineEntry[] = [];
type DetailListItem = CommentListRow | { key: string; kind: "error"; message: string };
type CommentThread = {
  root: TimelineEntry;
  replies: TimelineEntry[];
};
type CommentListRow =
  | { key: string; kind: "root"; entry: TimelineEntry; rootId: string; expanded: boolean }
  | { key: string; kind: "reply"; entry: TimelineEntry; rootId: string; expanded: boolean; isLastReply: boolean }
  | { key: string; kind: "footer"; rootId: string };
type AttachmentPreviewState = {
  attachment: Attachment;
  imageAttachments?: Attachment[];
  imageIndex?: number;
  textContent?: string;
  error?: string;
  loading?: boolean;
};
type Translate = (key: string, options?: Record<string, unknown>) => string;
const MAX_MENTION_SUGGESTIONS = 20;
const SERVER_ISSUE_SEARCH_LIMIT = 20;
const SERVER_SEARCH_DEBOUNCE_MS = 150;
const TEXT_PREVIEW_MAX_BYTES = 1_000_000;
const COLLAPSED_COMMENT_HEIGHT = 160;
const COMMENT_PREVIEW_MAX_CHARS = 700;
const COMMENT_COLLAPSE_MIN_CHARS = 420;
const COMMENT_COLLAPSE_MIN_LINES = 8;
const COMMENT_CARD_FADE_COLORS = ["rgba(255, 255, 255, 0)", "rgba(255, 255, 255, 0.86)", colors.card] as const;
const COMMENT_REPLY_FADE_COLORS = ["rgba(241, 241, 240, 0)", "rgba(241, 241, 240, 0.88)", colors.muted] as const;

function useKeyboardHeight(enabled: boolean) {
  const { height: windowHeight } = useWindowDimensions();
  const [keyboardHeight, setKeyboardHeight] = useState(0);

  useEffect(() => {
    if (!enabled) {
      setKeyboardHeight(0);
      return undefined;
    }

    const showEvent = Platform.OS === "ios" ? "keyboardWillChangeFrame" : "keyboardDidShow";
    const hideEvent = Platform.OS === "ios" ? "keyboardWillHide" : "keyboardDidHide";
    const showSub = Keyboard.addListener(showEvent, (event) => {
      setKeyboardHeight(Math.max(0, windowHeight - event.endCoordinates.screenY));
    });
    const hideSub = Keyboard.addListener(hideEvent, () => {
      setKeyboardHeight(0);
    });

    return () => {
      showSub.remove();
      hideSub.remove();
    };
  }, [enabled, windowHeight]);

  return keyboardHeight;
}

export function IssueDetailScreen({ navigation, route }: Props) {
  const { issueId } = route.params;
  const { t } = useTranslation();
  const insets = useSafeAreaInsets();
  const userId = useAuthStore((state) => state.user?.id);
  const { workspace } = useMobileWorkspace();
  const { getActorName } = useActorName();
  const mentionTargets = useWorkspaceMentionTargets(workspace.id);
  const { data: issue, isError, isLoading } = useIssueDetail(workspace.id, issueId);
  const { data: allIssues = [] } = useIssueList(workspace.id);
  const issueMentionTargets = useMemo(
    () => allIssues.map(issueToMentionTarget),
    [allIssues],
  );
  const { data: attachments = [], refetch: refetchAttachments } = useIssueAttachments(workspace.id, issueId);
  const {
    isSubscribed,
    isToggling: togglingSubscription,
    loading: subscribersLoading,
    toggleSubscribe,
  } = useIssueSubscribers(workspace.id, issueId, userId);
  const {
    tasks: liveTasks,
    cancellingTaskIds,
    cancelTask: cancelLiveTask,
  } = useLiveIssueTasks(workspace.id, issueId);
  const { data: taskRuns = [] } = useIssueTaskRuns(workspace.id, issueId);
  const { data: timelineData } = useIssueTimelineEntries(workspace.id, issueId);
  const timeline = Array.isArray(timelineData) ? timelineData : emptyTimeline;
  const createComment = useCreateComment(issueId);
  const updateComment = useUpdateComment(issueId);
  const deleteComment = useDeleteComment(issueId);
  const updateIssue = useUpdateIssue();
  const titleInputRef = useRef<TextInput | null>(null);
  const listRef = useRef<FlashListRef<DetailListItem> | null>(null);
  const [comment, setComment] = useState("");
  const [commentAttachments, setCommentAttachments] = useState<DraftCommentAttachment[]>([]);
  const [replyTargetId, setReplyTargetId] = useState<string | null>(null);
  const [editingCommentId, setEditingCommentId] = useState<string | null>(null);
  const [editingContent, setEditingContent] = useState("");
  const [editingTitle, setEditingTitle] = useState(false);
  const [titleDraft, setTitleDraft] = useState("");
  const [descriptionSheetOpen, setDescriptionSheetOpen] = useState(false);
  const [descriptionDraft, setDescriptionDraft] = useState("");
  const [issueEditError, setIssueEditError] = useState<string | null>(null);
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [commentError, setCommentError] = useState<string | null>(null);
  const [issueMenuOpen, setIssueMenuOpen] = useState(false);
  const [commentSheetOpen, setCommentSheetOpen] = useState(false);
  const [liveTaskError, setLiveTaskError] = useState<string | null>(null);
  const [attachmentPreview, setAttachmentPreview] = useState<AttachmentPreviewState | null>(null);
  const previewAbortRef = useRef<AbortController | null>(null);

  useEffect(() => () => {
    previewAbortRef.current?.abort();
  }, []);

  useEffect(() => {
    setEditingTitle(false);
    setTitleDraft(issue?.title ?? "");
    setDescriptionSheetOpen(false);
    setDescriptionDraft(issue?.description ?? "");
    setIssueEditError(null);
  }, [issue?.description, issue?.id, issue?.title]);

  useEffect(() => {
    if (!editingTitle) return;
    const timer = setTimeout(() => {
      titleInputRef.current?.focus();
    }, 0);
    return () => clearTimeout(timer);
  }, [editingTitle]);

  const comments = useMemo(
    () => timeline
      .filter((entry: TimelineEntry) => entry.type === "comment")
      .sort((a: TimelineEntry, b: TimelineEntry) => a.created_at.localeCompare(b.created_at)),
    [timeline],
  );
  const commentThreads = useMemo(() => buildCommentThreads(comments), [comments]);
  const baseCommentRows = useMemo(() => buildCommentRows(commentThreads), [commentThreads]);
  const [commentRows, setCommentRows] = useState<CommentListRow[]>([]);
  const activities = useMemo(
    () => timeline
      .filter((entry: TimelineEntry) => entry.type === "activity")
      .sort((a: TimelineEntry, b: TimelineEntry) => a.created_at.localeCompare(b.created_at)),
    [timeline],
  );

  useEffect(() => {
    setCommentRows((currentRows) => mergeCommentRows(currentRows, baseCommentRows));
  }, [baseCommentRows]);

  const openCommentComposer = useCallback(() => {
    setReplyTargetId(null);
    setComment("");
    setCommentAttachments([]);
    setUploadError(null);
    setCommentError(null);
    setCommentSheetOpen(true);
  }, []);

  const openReplyComposer = useCallback((parentId: string) => {
    setReplyTargetId(parentId);
    setComment("");
    setCommentAttachments([]);
    setUploadError(null);
    setCommentError(null);
    setCommentSheetOpen(true);
  }, []);

  const submitComment = useCallback(async () => {
    const content = comment.trim();
    if (!content || createComment.isPending || uploading) return;
    setUploading(true);
    setUploadError(null);
    setCommentError(null);
    const uploadedAttachments: Attachment[] = [];
    try {
      for (const draft of commentAttachments) {
        const attachment = await uploadMobileAsset(api, draft, { issueId });
        uploadedAttachments.push(attachment);
      }
      await createComment.mutateAsync({
        content,
        parentId: replyTargetId ?? undefined,
        attachmentIds: uploadedAttachments.map((attachment) => attachment.id),
      });
      setComment("");
      setCommentAttachments([]);
      setReplyTargetId(null);
      await refetchAttachments();
      setCommentSheetOpen(false);
    } catch (err) {
      await Promise.allSettled(uploadedAttachments.map((attachment) => api.deleteAttachment(attachment.id)));
      setUploadError(err instanceof Error ? err.message : t("issues.unable_to_send_comment"));
      setCommentError(err instanceof Error ? err.message : t("issues.unable_to_send_comment"));
      await refetchAttachments();
    } finally {
      setUploading(false);
    }
  }, [comment, commentAttachments, createComment, issueId, refetchAttachments, replyTargetId, t, uploading]);

  const saveCommentEdit = useCallback(async (commentId: string) => {
    const content = editingContent.trim();
    if (!content || updateComment.isPending) return;
    setCommentError(null);
    try {
      await updateComment.mutateAsync({ commentId, content });
      setEditingCommentId(null);
      setEditingContent("");
    } catch (err) {
      setCommentError(err instanceof Error ? err.message : t("issues.unable_to_save_comment"));
    }
  }, [editingContent, t, updateComment]);

  const removeComment = useCallback(async (commentId: string) => {
    if (deleteComment.isPending) return;
    setCommentError(null);
    try {
      await deleteComment.mutateAsync(commentId);
    } catch (err) {
      setCommentError(err instanceof Error ? err.message : t("issues.unable_to_delete_comment"));
    }
  }, [deleteComment, t]);

  const closeCommentSheet = useCallback(() => {
    setCommentSheetOpen(false);
    setReplyTargetId(null);
    setCommentAttachments([]);
    setUploadError(null);
  }, []);

  const addCommentAttachment = useCallback((asset: MobileUploadAsset) => {
    setCommentAttachments((items) => [
      ...items,
      createDraftCommentAttachment(asset, items.length),
    ]);
  }, []);

  const removeCommentAttachment = useCallback((attachmentId: string) => {
    setCommentAttachments((items) => items.filter((attachment) => attachment.id !== attachmentId));
  }, []);

  const uploadAttachment = useCallback(async (asset: MobileUploadAsset, target: "issue" | "comment") => {
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
      setUploadError(err instanceof Error ? err.message : t("issues.upload_failed"));
    } finally {
      setUploading(false);
    }
  }, [addCommentAttachment, issueId, refetchAttachments, t]);

  const pickDocument = useCallback(async (target: "issue" | "comment") => {
    setUploadError(null);

    let DocumentPicker: DocumentPickerModule;
    try {
      DocumentPicker = require("expo-document-picker") as DocumentPickerModule;
    } catch (err) {
      setUploadError(formatDocumentPickerError(err, t));
      return;
    }

    let result: Awaited<ReturnType<DocumentPickerModule["getDocumentAsync"]>>;
    try {
      result = await DocumentPicker.getDocumentAsync({
        copyToCacheDirectory: true,
        multiple: true,
        base64: false,
      });
    } catch (err) {
      setUploadError(formatDocumentPickerError(err, t));
      return;
    }

    if (result.canceled) return;
    for (const asset of result.assets) {
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
  }, [t, uploadAttachment]);

  const pickImage = useCallback(async (target: "issue" | "comment") => {
    const result = await ImagePicker.launchImageLibraryAsync({
      mediaTypes: ["images"],
      allowsMultipleSelection: true,
      quality: 1,
    });
    if (result.canceled) return;
    for (const asset of result.assets) {
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
  }, [uploadAttachment]);

  const startCommentEdit = useCallback((commentId: string, content: string) => {
    setEditingCommentId(commentId);
    setEditingContent(content);
  }, []);

  const copyCommentContent = useCallback(async (content: string) => {
    setCommentError(null);
    try {
      Clipboard.setString(content);
    } catch (err) {
      setCommentError(formatClipboardError(err, t));
    }
  }, [t]);

  const cancelCommentEdit = useCallback(() => {
    setEditingCommentId(null);
    setEditingContent("");
  }, []);

  const deleteCommentById = useCallback((commentId: string) => {
    void removeComment(commentId);
  }, [removeComment]);

  const saveCommentEditById = useCallback((commentId: string) => {
    void saveCommentEdit(commentId);
  }, [saveCommentEdit]);

  const copyCommentByContent = useCallback((content: string) => {
    void copyCommentContent(content);
  }, [copyCommentContent]);

  const openIssueMention = useCallback((targetIssueId: string) => {
    navigation.push("IssueDetail", { issueId: targetIssueId });
  }, [navigation]);

  const startTitleEdit = useCallback(() => {
    if (!issue || updateIssue.isPending) return;
    setIssueEditError(null);
    setTitleDraft(issue.title);
    setEditingTitle(true);
  }, [issue, updateIssue.isPending]);

  const saveTitleEdit = useCallback(async () => {
    if (!issue || updateIssue.isPending || !editingTitle) return;
    const title = titleDraft.trim();
    if (!title) {
      setTitleDraft(issue.title);
      setEditingTitle(false);
      return;
    }
    if (title === issue.title) {
      setEditingTitle(false);
      return;
    }
    setIssueEditError(null);
    try {
      await updateIssue.mutateAsync({ id: issue.id, title });
      setEditingTitle(false);
    } catch (err) {
      setIssueEditError(err instanceof Error ? err.message : t("issues.unable_to_save_issue"));
    }
  }, [editingTitle, issue, t, titleDraft, updateIssue]);

  const openDescriptionEditor = useCallback(() => {
    if (!issue || updateIssue.isPending) return;
    setIssueEditError(null);
    setDescriptionDraft(issue.description ?? "");
    setDescriptionSheetOpen(true);
  }, [issue, updateIssue.isPending]);

  const closeDescriptionEditor = useCallback(() => {
    setDescriptionSheetOpen(false);
    setDescriptionDraft("");
    setIssueEditError(null);
  }, []);

  const saveDescriptionEdit = useCallback(async () => {
    if (!issue || updateIssue.isPending) return;
    const description = descriptionDraft.replace(/\s+$/, "");
    if (description === (issue.description ?? "")) {
      setDescriptionSheetOpen(false);
      return;
    }
    setIssueEditError(null);
    try {
      await updateIssue.mutateAsync({ id: issue.id, description });
      setDescriptionSheetOpen(false);
      setDescriptionDraft("");
    } catch (err) {
      setIssueEditError(err instanceof Error ? err.message : t("issues.unable_to_save_issue"));
    }
  }, [descriptionDraft, issue, t, updateIssue]);

  const stopLiveTask = useCallback(async (taskId: string) => {
    setLiveTaskError(null);
    try {
      await cancelLiveTask(taskId);
    } catch (err) {
      setLiveTaskError(err instanceof Error ? err.message : t("issues.unable_to_stop_agent"));
    }
  }, [cancelLiveTask, t]);

  const closeAttachmentPreview = useCallback(() => {
    previewAbortRef.current?.abort();
    previewAbortRef.current = null;
    setAttachmentPreview(null);
  }, []);

  const openAttachmentPreview = useCallback(async (attachment: Attachment, attachmentGroup: Attachment[] = [attachment]) => {
    previewAbortRef.current?.abort();
    previewAbortRef.current = null;

    if (isImageAttachment(attachment)) {
      const imageAttachments = attachmentGroup.filter(isImageAttachment);
      const imageIndex = Math.max(0, imageAttachments.findIndex((item) => item.id === attachment.id));
      setAttachmentPreview({ attachment, imageAttachments, imageIndex });
      return;
    }

    if (!isTextPreviewAttachment(attachment)) {
      setAttachmentPreview({ attachment });
      return;
    }

    if (attachment.size_bytes > TEXT_PREVIEW_MAX_BYTES) {
      setAttachmentPreview({
        attachment,
        error: t("issues.file_too_large_preview"),
      });
      return;
    }

    setAttachmentPreview({ attachment, loading: true });
    const controller = new AbortController();
    previewAbortRef.current = controller;
    try {
      const response = await fetch(attachment.download_url || attachment.url, {
        signal: controller.signal,
      });
      if (!response.ok) {
        throw new Error(`${t("issues.unable_to_load_preview")} (${response.status})`);
      }
      const textContent = await response.text();
      if (controller.signal.aborted) return;
      setAttachmentPreview({ attachment, textContent });
    } catch (err) {
      if (controller.signal.aborted) return;
      setAttachmentPreview({
        attachment,
        error: err instanceof Error ? err.message : t("issues.unable_to_load_preview"),
      });
    } finally {
      if (previewAbortRef.current === controller) {
        previewAbortRef.current = null;
      }
    }
  }, [t]);

  const expandCommentRow = useCallback((commentId: string) => {
    setCommentRows((rows) => rows.map((row) => {
      if (row.kind === "footer" || row.entry.id !== commentId || row.expanded) return row;
      return { ...row, expanded: true };
    }));
  }, []);

  const renderListItem = useCallback(({ item }: { item: DetailListItem }) => {
    if (item.kind === "error") {
      return (
        <View style={styles.commentRowThreadStart}>
          <Text style={styles.errorText}>{item.message}</Text>
        </View>
      );
    }

    if (item.kind === "footer") {
      return (
        <View style={styles.commentRowThreadContinuation}>
          <ThreadReplyFooter
            onReply={() => openReplyComposer(item.rootId)}
          />
        </View>
      );
    }

    const isEditingEntry = editingCommentId === item.entry.id;
    return (
      <View style={item.kind === "root" ? styles.commentRowThreadStart : styles.commentRowThreadContinuation}>
        <TimelineItem
          entry={item.entry}
          editingCommentId={isEditingEntry ? editingCommentId : null}
          editingContent={isEditingEntry ? editingContent : ""}
          onOpenAttachment={openAttachmentPreview}
          onCancelEdit={cancelCommentEdit}
          onChangeEdit={setEditingContent}
          onDelete={deleteCommentById}
          onReply={item.kind === "root" ? openReplyComposer : undefined}
          onSaveEdit={saveCommentEditById}
          onStartEdit={startCommentEdit}
          onCopyComment={copyCommentByContent}
          onIssueMentionPress={openIssueMention}
          resolveActorName={getActorName}
          userId={userId}
          mentionTargets={mentionTargets}
          issueMentionTargets={issueMentionTargets}
          expanded={item.expanded}
          onExpandComment={expandCommentRow}
          variant={item.kind === "root" ? "threadRoot" : "reply"}
          isLastReply={item.kind === "reply" ? item.isLastReply : false}
        />
      </View>
    );
  }, [
    editingCommentId,
    editingContent,
    expandCommentRow,
    getActorName,
    mentionTargets,
    issueMentionTargets,
    openAttachmentPreview,
    openReplyComposer,
    cancelCommentEdit,
    copyCommentByContent,
    deleteCommentById,
    openIssueMention,
    saveCommentEditById,
    startCommentEdit,
    userId,
  ]);

  const scrollToLatestComment = useCallback(() => {
    listRef.current?.scrollToEnd({ animated: true });
  }, []);

  const listHeader = useMemo(() => {
    if (!issue) return null;
    return (
      <View style={styles.listHeader}>
        <View style={styles.section}>
          {editingTitle ? (
            <TextInput
              ref={titleInputRef}
              autoCapitalize="sentences"
              autoCorrect
              blurOnSubmit
              editable={!updateIssue.isPending}
              onBlur={() => void saveTitleEdit()}
              onChangeText={setTitleDraft}
              onSubmitEditing={() => void saveTitleEdit()}
              placeholder={t("issues.title_placeholder")}
              placeholderTextColor={colors.mutedForeground}
              returnKeyType="done"
              style={styles.issueTitleInput}
              value={titleDraft}
            />
          ) : (
            <Pressable
              accessibilityHint={t("issues.edit_title_hint")}
              accessibilityRole="button"
              onPress={startTitleEdit}
              style={({ pressed }) => [
                styles.editableTitle,
                pressed && styles.buttonPressed,
              ]}
            >
              <Text style={styles.issueBodyTitle}>{issue.title}</Text>
            </Pressable>
          )}
          {issueEditError ? <Text style={styles.errorText}>{issueEditError}</Text> : null}
          <Pressable
            accessibilityHint={t("issues.edit_description_hint")}
            accessibilityRole="button"
            onPress={openDescriptionEditor}
            style={({ pressed }) => [
              styles.editableDescription,
              pressed && styles.buttonPressed,
            ]}
          >
            <View style={styles.descriptionHeader}>
              <Text style={styles.descriptionLabel}>{t("issues.description")}</Text>
              <Text style={styles.editHintText}>{t("issues.tap_to_edit")}</Text>
            </View>
            {issue.description ? (
              <MarkdownText
                content={issue.description}
                onIssueMentionPress={(targetIssueId) => {
                  navigation.push("IssueDetail", { issueId: targetIssueId });
                }}
              />
            ) : (
              <Text style={styles.emptyText}>{t("issues.no_description")}</Text>
            )}
          </Pressable>
          <View style={styles.issueEngagementRow}>
            <Pressable
              accessibilityRole="button"
              disabled={!userId || subscribersLoading || togglingSubscription}
              onPress={toggleSubscribe}
              style={({ pressed }) => [
                styles.subscribeButton,
                isSubscribed && styles.subscribeButtonActive,
                (!userId || subscribersLoading || togglingSubscription) && styles.subscribeButtonDisabled,
                pressed && styles.buttonPressed,
              ]}
            >
              <Text style={[
                styles.subscribeButtonText,
                isSubscribed && styles.subscribeButtonTextActive,
              ]}>
                {isSubscribed ? t("issues.subscribed") : t("issues.subscribe")}
              </Text>
            </Pressable>
            <IssueShortcutButton
              count={activities.length}
              label={t("issues.timeline")}
              onPress={() => navigation.navigate("IssueTimeline", { issueId })}
            />
            <IssueShortcutButton
              count={taskRuns.length}
              label={t("issues.agent_transcript")}
              onPress={() => navigation.navigate("IssueTaskRuns", { issueId })}
            />
          </View>
        </View>

        <View style={[styles.section, styles.sectionSeparated]}>
          <View style={[styles.sectionHeader, styles.sectionHeaderSurface]}>
            <View style={styles.sectionTitleGroup}>
              <View style={styles.sectionTitleAccent} />
              <Text style={styles.sectionTitle}>{t("issues.attachments")}</Text>
            </View>
            <View style={styles.inlineActions}>
              <Button
                disabled={uploading}
                onPress={() => void pickImage("issue")}
                style={styles.sectionHeaderActionButton}
                variant="secondary"
              >
                {t("issues.image")}
              </Button>
              <Button
                disabled={uploading}
                onPress={() => void pickDocument("issue")}
                style={styles.sectionHeaderActionButton}
                variant="secondary"
              >
                {t("issues.file")}
              </Button>
            </View>
          </View>
          {uploadError ? <Text style={styles.errorText}>{uploadError}</Text> : null}
          <View style={styles.attachmentSectionPanel}>
            <AttachmentList attachments={attachments} onOpen={openAttachmentPreview} />
          </View>
        </View>

        <View style={styles.sectionSeparated}>
          <View style={[styles.commentListHeader, styles.sectionHeaderSurface]}>
            <View style={styles.stickySectionTitleGroup}>
              <View style={styles.sectionTitleAccent} />
              <Text style={styles.sectionTitle}>{t("issues.comments")}</Text>
              <Text style={styles.stickySectionCount}>{comments.length}</Text>
            </View>
            <Pressable
              accessibilityRole="button"
              onPress={scrollToLatestComment}
              style={({ pressed }) => [
                styles.jumpToLatestButton,
                pressed && styles.buttonPressed,
              ]}
            >
              <Text style={styles.metadataToggle}>{t("issues.jump_to_latest_comment")}</Text>
            </Pressable>
          </View>
        </View>
      </View>
    );
  }, [
    activities.length,
    attachments,
    comments.length,
    editingTitle,
    issue,
    issueEditError,
    isSubscribed,
    issueId,
    navigation,
    openAttachmentPreview,
    openDescriptionEditor,
    pickDocument,
    pickImage,
    saveTitleEdit,
    scrollToLatestComment,
    startTitleEdit,
    subscribersLoading,
    t,
    taskRuns.length,
    titleDraft,
    toggleSubscribe,
    togglingSubscription,
    uploadError,
    uploading,
    updateIssue.isPending,
    userId,
  ]);

  const commentItems = useMemo<DetailListItem[]>(() => {
    const items: DetailListItem[] = commentError
      ? [{ key: "comments-error", kind: "error", message: commentError }]
      : [];
    if (comments.length === 0) return items;
    items.push(...commentRows);
    return items;
  }, [commentError, commentRows, comments.length]);

  const openIssueProperties = useCallback(() => {
    setIssueMenuOpen(false);
    navigation.navigate("IssueProperties", { issueId });
  }, [issueId, navigation]);
  const openCreateChildIssue = useCallback(() => {
    if (!issue) return;
    setIssueMenuOpen(false);
    navigation.navigate("CreateIssue", {
      parentIssueId: issue.id,
      parentIssueIdentifier: issue.identifier,
    });
  }, [issue, navigation]);

  if (isLoading) return <LoadingState />;
  if (isError || !issue) return <EmptyState title={t("issues.unable_to_load")} />;

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar
        onBack={() => navigation.goBack()}
        right={(
          <HeaderIconButton
            label={t("issues.issue_actions")}
            onPress={() => setIssueMenuOpen(true)}
          >
            <MoreHorizontal color={colors.foreground} size={20} />
          </HeaderIconButton>
        )}
        title={issue.identifier}
      />
      <IssueActionsMenu
        onClose={() => setIssueMenuOpen(false)}
        onCreateChildIssue={openCreateChildIssue}
        onOpenProperties={openIssueProperties}
        open={issueMenuOpen}
        topInset={insets.top}
      />
      {liveTasks.length > 0 ? (
        <IssueLiveAgentCard
          cancellingTaskIds={cancellingTaskIds}
          error={liveTaskError}
          getActorName={getActorName}
          onStop={(taskId) => void stopLiveTask(taskId)}
          tasks={liveTasks}
        />
      ) : null}
      <KeyboardAvoidingView
        behavior={Platform.OS === "ios" ? "padding" : "height"}
        keyboardVerticalOffset={0}
        style={styles.keyboardAvoidingContent}
      >
        <FlashList
          automaticallyAdjustKeyboardInsets={Platform.OS === "ios"}
          contentContainerStyle={[
            styles.issueDetailContent,
            editingCommentId && styles.contentEditingComment,
          ]}
          data={commentItems}
          drawDistance={1200}
          getItemType={getDetailListItemType}
          keyboardShouldPersistTaps="handled"
          keyExtractor={(item) => item.key}
          ListEmptyComponent={<Text style={styles.emptyText}>{t("issues.no_comments")}</Text>}
          ListHeaderComponent={listHeader}
          ref={listRef}
          removeClippedSubviews={Platform.OS === "android"}
          renderItem={renderListItem}
        />

        {!editingCommentId ? (
          <Pressable
            accessibilityLabel={t("issues.add_comment")}
            accessibilityRole="button"
            onPress={openCommentComposer}
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
        issueMentionTargets={issueMentionTargets}
        placeholder={replyTargetId ? t("issues.reply_in_thread") : t("issues.add_comment")}
        submitLabel={replyTargetId ? t("issues.send_reply") : t("issues.send")}
        title={replyTargetId ? t("issues.reply_in_thread") : t("issues.add_comment")}
      />
      <DescriptionEditSheet
        bottomInset={insets.bottom}
        error={issueEditError}
        issueMentionTargets={issueMentionTargets}
        mentionTargets={mentionTargets}
        onChangeDescription={setDescriptionDraft}
        onClose={closeDescriptionEditor}
        onSubmit={() => void saveDescriptionEdit()}
        open={descriptionSheetOpen}
        saving={updateIssue.isPending}
        value={descriptionDraft}
      />
      <AttachmentPreviewModal
        onClose={closeAttachmentPreview}
        open={Boolean(attachmentPreview)}
        preview={attachmentPreview}
      />
    </Screen>
  );
}

export function IssueTimelineScreen({ navigation, route }: TimelineProps) {
  const { t } = useTranslation();
  const { issueId } = route.params;
  const { workspace } = useMobileWorkspace();
  const { getActorName } = useActorName();
  const { data: timelineData, isError, isLoading } = useIssueTimelineEntries(workspace.id, issueId);
  const activities = useMemo(
    () => (Array.isArray(timelineData) ? timelineData : emptyTimeline)
      .filter((entry: TimelineEntry) => entry.type === "activity")
      .sort((a: TimelineEntry, b: TimelineEntry) => a.created_at.localeCompare(b.created_at)),
    [timelineData],
  );

  if (isLoading) return <LoadingState />;
  if (isError) return <EmptyState title={t("issues.unable_to_load_timeline")} />;

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar onBack={() => navigation.goBack()} title={t("issues.issue_timeline")} />
      <FlashList
        contentContainerStyle={styles.content}
        data={activities}
        ItemSeparatorComponent={ListItemSeparator}
        keyExtractor={(item) => item.id}
        ListEmptyComponent={<Text style={styles.emptyText}>{t("issues.no_timeline_events")}</Text>}
        renderItem={({ item }) => (
          <TimelineItem
            entry={item}
            resolveActorName={getActorName}
          />
        )}
      />
    </Screen>
  );
}

export function IssueTaskRunsScreen({ navigation, route }: TaskRunsProps) {
  const { t } = useTranslation();
  const { issueId } = route.params;
  const { workspace } = useMobileWorkspace();
  const { data: taskRuns = [], isError, isLoading } = useIssueTaskRuns(workspace.id, issueId);

  if (isLoading) return <LoadingState />;
  if (isError) return <EmptyState title={t("transcript.unable_to_load")} />;

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar onBack={() => navigation.goBack()} title={t("issues.issue_agent_runs")} />
      <FlashList
        contentContainerStyle={styles.content}
        data={taskRuns}
        ItemSeparatorComponent={ListItemSeparator}
        keyExtractor={(item) => item.id}
        ListEmptyComponent={<Text style={styles.emptyText}>{t("issues.no_agent_runs")}</Text>}
        renderItem={({ item }) => (
          <TaskRunHeader
            onPress={() => navigation.push("IssueTaskTranscript", { issueId, taskId: item.id })}
            task={item}
          />
        )}
      />
    </Screen>
  );
}

function IssueActionsMenu({
  onClose,
  onCreateChildIssue,
  onOpenProperties,
  open,
  topInset,
}: {
  onClose: () => void;
  onCreateChildIssue: () => void;
  onOpenProperties: () => void;
  open: boolean;
  topInset: number;
}) {
  const { t } = useTranslation();
  if (!open) return null;
  return (
    <Modal animationType="fade" onRequestClose={onClose} transparent visible>
      <View style={styles.menuModalOverlay}>
        <Pressable style={StyleSheet.absoluteFill} onPress={onClose} />
        <View style={[
          styles.commentDropdown,
          styles.issueActionsDropdown,
          { top: Math.max(topInset, spacing.sm) + 44 },
        ]}>
          <DropdownItem label={t("issues.action_properties")} onPress={onOpenProperties} />
          <DropdownItem label={t("issues.create_child_action")} onPress={onCreateChildIssue} />
        </View>
      </View>
    </Modal>
  );
}

function IssueShortcutButton({
  count,
  label,
  onPress,
}: {
  count: number;
  label: string;
  onPress: () => void;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [
        styles.issueShortcutButton,
        pressed && styles.buttonPressed,
      ]}
    >
      <Text style={styles.issueShortcutLabel}>{label}</Text>
      <Text style={styles.issueShortcutCount}>{count}</Text>
    </Pressable>
  );
}

function ListItemSeparator() {
  return <View style={styles.listItemSeparator} />;
}

function CommentSheet({
  attachments,
  bottomInset,
  comment,
  createPending,
  issueMentionTargets,
  mentionTargets,
  onChangeComment,
  onClose,
  onPickDocument,
  onPickImage,
  onRemoveAttachment,
  onSubmit,
  open,
  placeholder,
  submitLabel,
  title,
  uploadError,
  uploading,
}: {
  attachments: DraftCommentAttachment[];
  bottomInset: number;
  comment: string;
  createPending: boolean;
  issueMentionTargets: WorkspaceMentionTarget[];
  mentionTargets: WorkspaceMentionTarget[];
  onChangeComment: (content: string) => void;
  onClose: () => void;
  onPickDocument: () => void;
  onPickImage: () => void;
  onRemoveAttachment: (attachmentId: string) => void;
  onSubmit: () => void;
  open: boolean;
  placeholder: string;
  submitLabel: string;
  title: string;
  uploadError: string | null;
  uploading: boolean;
}) {
  const { t } = useTranslation();
  const canSubmit = comment.trim().length > 0 && !createPending && !uploading;
  const keyboardHeight = useKeyboardHeight(open);
  const { height: windowHeight } = useWindowDimensions();
  const sheetMaxHeight = Math.max(0, windowHeight - keyboardHeight - spacing.xl);
  const sheetBottomPadding = keyboardHeight > 0 ? spacing.md : Math.max(bottomInset, spacing.md);

  return (
    <Modal
      animationType="fade"
      onRequestClose={onClose}
      transparent
      visible={open}
    >
      <View style={styles.sheetKeyboardView}>
        <Pressable style={styles.sheetBackdrop} onPress={onClose} />
        <View style={[
          styles.sheet,
          {
            marginBottom: keyboardHeight,
            maxHeight: sheetMaxHeight,
            paddingBottom: sheetBottomPadding,
          },
        ]}>
          <View style={styles.sheetHandle} />
          <View style={styles.sheetHeader}>
            <Text style={styles.sheetTitle}>{title}</Text>
            <Button onPress={onClose} variant="ghost">
              {t("common.close")}
            </Button>
          </View>
          <MentionTextInput
            autoFocus
            issueMentionTargets={issueMentionTargets}
            mentionTargets={mentionTargets}
            multiline
            onChangeText={onChangeComment}
            placeholder={placeholder}
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
                {t("issues.image")}
              </Button>
              <Button disabled={uploading} onPress={onPickDocument} variant="secondary">
                {t("issues.file")}
              </Button>
            </View>
            <Button disabled={!canSubmit} onPress={onSubmit}>
              {submitLabel}
            </Button>
          </View>
        </View>
      </View>
    </Modal>
  );
}

function DescriptionEditSheet({
  bottomInset,
  error,
  issueMentionTargets,
  mentionTargets,
  onChangeDescription,
  onClose,
  onSubmit,
  open,
  saving,
  value,
}: {
  bottomInset: number;
  error: string | null;
  issueMentionTargets: WorkspaceMentionTarget[];
  mentionTargets: WorkspaceMentionTarget[];
  onChangeDescription: (description: string) => void;
  onClose: () => void;
  onSubmit: () => void;
  open: boolean;
  saving: boolean;
  value: string;
}) {
  const { t } = useTranslation();
  const keyboardHeight = useKeyboardHeight(open);
  const { height: windowHeight } = useWindowDimensions();
  const sheetMaxHeight = Math.max(0, windowHeight - keyboardHeight - spacing.xl);
  const sheetBottomPadding = keyboardHeight > 0 ? spacing.md : Math.max(bottomInset, spacing.md);

  return (
    <Modal
      animationType="fade"
      onRequestClose={onClose}
      transparent
      visible={open}
    >
      <View style={styles.sheetKeyboardView}>
        <Pressable style={styles.sheetBackdrop} onPress={onClose} />
        <View style={[
          styles.sheet,
          {
            marginBottom: keyboardHeight,
            maxHeight: sheetMaxHeight,
            paddingBottom: sheetBottomPadding,
          },
        ]}>
          <View style={styles.sheetHandle} />
          <View style={styles.sheetHeader}>
            <Text style={styles.sheetTitle}>{t("issues.edit_description")}</Text>
            <Button disabled={saving} onPress={onClose} variant="ghost">
              {t("common.cancel")}
            </Button>
          </View>
          <MentionTextInput
            autoFocus
            editable={!saving}
            issueMentionTargets={issueMentionTargets}
            mentionTargets={mentionTargets}
            multiline
            onChangeText={onChangeDescription}
            placeholder={t("issues.description_placeholder")}
            placeholderTextColor={colors.mutedForeground}
            scrollEnabled
            style={styles.descriptionSheetInput}
            value={value}
          />
          {error ? <Text style={styles.errorText}>{error}</Text> : null}
          <View style={styles.sheetActions}>
            <Text style={styles.sheetHelperText}>{t("issues.markdown_supported")}</Text>
            <Button disabled={saving} onPress={onSubmit}>
              {saving ? t("issues.saving") : t("common.save")}
            </Button>
          </View>
        </View>
      </View>
    </Modal>
  );
}

function MentionTextInput({
  issueMentionTargets = [],
  mentionTargets,
  onChangeText,
  onKeyPress,
  onSelectionChange,
  value,
  ...props
}: TextInputProps & {
  issueMentionTargets?: WorkspaceMentionTarget[];
  mentionTargets: WorkspaceMentionTarget[];
  onChangeText: (text: string) => void;
  value: string;
}) {
  const { t } = useTranslation();
  const [selection, setSelection] = useState({ start: value.length, end: value.length });
  const mentionQuery = getActiveMentionQuery(value, selection.start);
  const normalizedQuery = mentionQuery?.query.trim() ?? "";
  const [serverIssueTargets, setServerIssueTargets] = useState<WorkspaceMentionTarget[]>([]);
  const [searchedIssueQuery, setSearchedIssueQuery] = useState("");
  const [isSearchingIssues, setIsSearchingIssues] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState(0);

  useEffect(() => {
    setServerIssueTargets([]);
    setSearchedIssueQuery("");

    if (!normalizedQuery) {
      setIsSearchingIssues(false);
      return undefined;
    }

    let cancelled = false;
    const controller = new AbortController();
    setIsSearchingIssues(true);

    const timer = setTimeout(() => {
      void (async () => {
        try {
          const res = await api.searchIssues({
            q: normalizedQuery,
            limit: SERVER_ISSUE_SEARCH_LIMIT,
            include_closed: true,
            signal: controller.signal,
          });
          if (!cancelled && !controller.signal.aborted) {
            setServerIssueTargets(res.issues.map(issueToMentionTarget));
          }
        } catch {
          // Keep local suggestions when search is aborted or unavailable.
        } finally {
          if (!cancelled && !controller.signal.aborted) {
            setSearchedIssueQuery(normalizedQuery);
            setIsSearchingIssues(false);
          }
        }
      })();
    }, SERVER_SEARCH_DEBOUNCE_MS);

    return () => {
      cancelled = true;
      clearTimeout(timer);
      controller.abort();
    };
  }, [normalizedQuery]);

  const suggestions = useMemo(
    () => {
      const issueTargets = mergeMentionTargets(
        filterMentionTargets(issueMentionTargets, normalizedQuery),
        searchedIssueQuery === normalizedQuery ? serverIssueTargets : [],
      );
      return mergeMentionTargets(
        filterMentionTargets(mentionTargets, normalizedQuery),
        issueTargets,
      ).slice(0, MAX_MENTION_SUGGESTIONS);
    },
    [
      issueMentionTargets,
      mentionTargets,
      normalizedQuery,
      searchedIssueQuery,
      serverIssueTargets,
    ],
  );
  const showSuggestions = Boolean(mentionQuery);
  const isWaitingForServer =
    normalizedQuery !== "" &&
    (isSearchingIssues || searchedIssueQuery !== normalizedQuery);
  const selectedTarget = suggestions[selectedIndex];
  const selectedKey = selectedTarget ? mentionTargetKey(selectedTarget) : null;

  useEffect(() => {
    setSelectedIndex(0);
  }, [normalizedQuery, suggestions.length]);

  function handleKeyPress(event: NativeSyntheticEvent<TextInputKeyPressEventData>) {
    if (showSuggestions && suggestions.length > 0) {
      if (event.nativeEvent.key === "ArrowUp") {
        setSelectedIndex((index) => (index + suggestions.length - 1) % suggestions.length);
        return;
      }
      if (event.nativeEvent.key === "ArrowDown") {
        setSelectedIndex((index) => (index + 1) % suggestions.length);
        return;
      }
      if (event.nativeEvent.key === "Enter") {
        const target = suggestions[selectedIndex];
        if (target) {
          insertMention(target);
          return;
        }
      }
    }
    onKeyPress?.(event);
  }

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
        onKeyPress={handleKeyPress}
        onSelectionChange={handleSelectionChange}
        selection={selection}
        value={value}
      />
      {showSuggestions ? (
        <ScrollView
          keyboardShouldPersistTaps="always"
          nestedScrollEnabled
          style={styles.mentionSuggestions}
        >
          {suggestions.length > 0 ? (
            <>
              <MentionSuggestionGroup
                items={suggestions.filter((target) => target.type !== "issue")}
                label={t("issues.users")}
                onSelect={insertMention}
                selectedKey={selectedKey}
              />
              <MentionSuggestionGroup
                items={suggestions.filter((target) => target.type === "issue")}
                label={t("nav.issues")}
                onSelect={insertMention}
                selectedKey={selectedKey}
              />
            </>
          ) : (
            <Text style={styles.mentionEmptyText}>
              {isWaitingForServer ? t("issues.searching") : t("common.no_results")}
            </Text>
          )}
        </ScrollView>
      ) : null}
    </View>
  );
}

function MentionSuggestionGroup({
  items,
  label,
  onSelect,
  selectedKey,
}: {
  items: WorkspaceMentionTarget[];
  label: string;
  onSelect: (target: WorkspaceMentionTarget) => void;
  selectedKey: string | null;
}) {
  const { t } = useTranslation();
  if (items.length === 0) return null;
  return (
    <View>
      <Text style={styles.mentionGroupLabel}>{label}</Text>
      {items.map((target) => {
        const isClosedIssue = target.status === "done" || target.status === "cancelled";
        const selected = mentionTargetKey(target) === selectedKey;
        return (
          <Pressable
            key={`${target.type}-${target.id}`}
            onPress={() => onSelect(target)}
            style={[
              styles.mentionSuggestionRow,
              selected && styles.mentionSuggestionRowSelected,
            ]}
          >
            <View style={styles.mentionAvatar}>
              <Text style={styles.mentionAvatarText}>
                {target.type === "issue" ? "#" : target.type === "agent" ? "A" : "@"}
              </Text>
            </View>
            <View style={styles.mentionSuggestionTextGroup}>
              <Text
                numberOfLines={1}
                style={[
                  styles.mentionSuggestionName,
                  isClosedIssue && styles.mentionSuggestionClosed,
                ]}
              >
                {target.label}
              </Text>
              <Text
                numberOfLines={1}
                style={[
                  styles.mentionSuggestionType,
                  isClosedIssue && styles.mentionSuggestionClosed,
                ]}
              >
                {target.type === "issue"
                  ? target.description ?? t("issues.issue")
                  : target.type === "agent"
                    ? t("issues.agent")
                    : target.type === "squad"
                      ? t("issues.squad")
                      : target.type === "all"
                        ? t("issues.all_members")
                        : t("issues.member")}
              </Text>
            </View>
          </Pressable>
        );
      })}
    </View>
  );
}

function mentionTargetKey(target: WorkspaceMentionTarget) {
  return `${target.type}:${target.id}`;
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
      return (
        target.label.toLowerCase().includes(q) ||
        target.description?.toLowerCase().includes(q)
      );
    })
    .slice(0, MAX_MENTION_SUGGESTIONS);
}

function formatMentionMarkdown(target: WorkspaceMentionTarget) {
  if (target.type === "issue") {
    return `[${target.label}](mention://${target.type}/${target.id})`;
  }
  const label = target.type === "all" ? "@All members" : `@${target.label}`;
  return `[${label}](mention://${target.type}/${target.id})`;
}

function buildCommentThreads(comments: TimelineEntry[]): CommentThread[] {
  const byId = new Map(comments.map((comment) => [comment.id, comment]));
  const rootById = new Map<string, TimelineEntry>();

  function findRoot(comment: TimelineEntry): TimelineEntry {
    const seen = new Set<string>();
    let current = comment;
    while (current.parent_id && byId.has(current.parent_id) && !seen.has(current.id)) {
      seen.add(current.id);
      current = byId.get(current.parent_id)!;
    }
    return current;
  }

  for (const comment of comments) {
    rootById.set(comment.id, findRoot(comment));
  }

  const threadByRootId = new Map<string, CommentThread>();
  for (const comment of comments) {
    const root = rootById.get(comment.id) ?? comment;
    let thread = threadByRootId.get(root.id);
    if (!thread) {
      thread = { root, replies: [] };
      threadByRootId.set(root.id, thread);
    }
    if (comment.id !== root.id) {
      thread.replies.push(comment);
    }
  }

  return Array.from(threadByRootId.values())
    .sort((a, b) => a.root.created_at.localeCompare(b.root.created_at))
    .map((thread) => ({
      ...thread,
      replies: thread.replies.sort((a, b) => a.created_at.localeCompare(b.created_at)),
    }));
}

function buildCommentRows(threads: CommentThread[]): CommentListRow[] {
  return threads.flatMap((thread) => [
    {
      key: `${thread.root.id}:root`,
      kind: "root" as const,
      entry: thread.root,
      expanded: false,
      rootId: thread.root.id,
    },
    ...thread.replies.map((reply, index) => ({
      key: `${reply.id}:reply`,
      kind: "reply" as const,
      entry: reply,
      expanded: false,
      rootId: thread.root.id,
      isLastReply: index === thread.replies.length - 1,
    })),
    {
      key: `${thread.root.id}:footer`,
      kind: "footer" as const,
      rootId: thread.root.id,
    },
  ]);
}

function mergeCommentRows(currentRows: CommentListRow[], nextRows: CommentListRow[]): CommentListRow[] {
  if (currentRows.length === 0) return nextRows;

  const currentByCommentId = new Map<string, Extract<CommentListRow, { entry: TimelineEntry }>>();
  for (const row of currentRows) {
    if (row.kind !== "footer") currentByCommentId.set(row.entry.id, row);
  }

  return nextRows.map((row) => {
    if (row.kind === "footer") return row;
    const current = currentByCommentId.get(row.entry.id);
    if (!current?.expanded) return row;
    return { ...row, expanded: true };
  });
}

function getDetailListItemType(item: DetailListItem) {
  switch (item.kind) {
    case "root":
      return "comment-root";
    case "reply":
      return "comment-reply";
    case "footer":
      return "comment-footer";
    case "error":
      return "comment-error";
  }
}

function shouldCollapseComment(content: string) {
  if (content.length > COMMENT_COLLAPSE_MIN_CHARS) return true;
  const lines = content.replace(/\r\n/g, "\n").split("\n");
  if (lines.length > COMMENT_COLLAPSE_MIN_LINES) return true;
  const denseMarkdownLines = lines.filter((line) => {
    const trimmed = line.trim();
    return (
      trimmed.startsWith("```") ||
      trimmed.startsWith(">") ||
      /^#{1,6}\s+/.test(trimmed) ||
      /^[-*]\s+/.test(trimmed) ||
      /^\d+[.)]\s+/.test(trimmed)
    );
  });
  return denseMarkdownLines.length > 5;
}

function createCommentPreview(content: string) {
  const normalized = content.replace(/\r\n/g, "\n").trimEnd();
  const lines = normalized.split("\n");
  const previewLines: string[] = [];
  let charCount = 0;
  let openCodeFence = false;

  for (const line of lines) {
    const nextCount = charCount + line.length + (previewLines.length > 0 ? 1 : 0);
    if (previewLines.length >= COMMENT_COLLAPSE_MIN_LINES || nextCount > COMMENT_PREVIEW_MAX_CHARS) {
      break;
    }
    previewLines.push(line);
    charCount = nextCount;
    if (line.trim().startsWith("```")) {
      openCodeFence = !openCodeFence;
    }
  }

  let preview = previewLines.join("\n").trimEnd();
  if (!preview) return normalized.slice(0, COMMENT_PREVIEW_MAX_CHARS).trimEnd();

  if (openCodeFence) {
    preview = `${preview}\n\`\`\``;
  }
  if (preview.length < normalized.length) {
    preview = `${preview}\n...`;
  }
  return preview;
}


function ThreadReplyFooter({
  onReply,
}: {
  onReply: () => void;
}) {
  const { t } = useTranslation();
  return (
    <View style={styles.threadReplyFooter}>
      <Pressable
        accessibilityLabel={t("issues.reply")}
        accessibilityRole="button"
        onPress={onReply}
        style={({ pressed }) => [
          styles.threadReplyButton,
          pressed && styles.buttonPressed,
        ]}
      >
        <Text style={styles.threadReplyButtonText}>{t("issues.reply")}</Text>
      </Pressable>
    </View>
  );
}

const TimelineItem = memo(function TimelineItem({
  editingCommentId,
  editingContent,
  entry,
  expanded = false,
  isLastReply,
  onCancelEdit,
  onChangeEdit,
  onCopyComment,
  onDelete,
  onExpandComment,
  onOpenAttachment,
  onIssueMentionPress,
  onReply,
  onSaveEdit,
  onStartEdit,
  resolveActorName,
  userId,
  issueMentionTargets,
  mentionTargets,
  variant = "card",
}: {
  editingCommentId?: string | null;
  editingContent?: string;
  entry: TimelineEntry;
  expanded?: boolean;
  onCancelEdit?: () => void;
  onChangeEdit?: (content: string) => void;
  onCopyComment?: (content: string) => void;
  onDelete?: (commentId: string) => void;
  onExpandComment?: (commentId: string) => void;
  onOpenAttachment?: (attachment: Attachment, attachmentGroup: Attachment[]) => void;
  onIssueMentionPress?: (issueId: string) => void;
  onReply?: (commentId: string) => void;
  onSaveEdit?: (commentId: string) => void;
  onStartEdit?: (commentId: string, content: string) => void;
  resolveActorName: (type: string, id: string) => string;
  userId?: string;
  issueMentionTargets?: WorkspaceMentionTarget[];
  mentionTargets?: WorkspaceMentionTarget[];
  variant?: "card" | "threadRoot" | "reply";
  isLastReply?: boolean;
}) {
  const { t } = useTranslation();
  const actor = resolveActorName(entry.actor_type, entry.actor_id);
  const isOwnComment = entry.type === "comment" && entry.actor_type === "member" && entry.actor_id === userId;
  const isEditing = editingCommentId === entry.id;
  const [openMenu, setOpenMenu] = useState<"actions" | null>(null);
  const [actionsMenuAnchor, setActionsMenuAnchor] = useState<{ x: number; y: number } | null>(null);
  const { height: windowHeight, width: windowWidth } = useWindowDimensions();
  const body = entry.type === "comment"
    ? entry.content
    : formatActivity(entry, resolveActorName, t);
  const isComment = entry.type === "comment";
  const hasCommentActions = isComment;
  const commentContent = entry.content ?? "";
  const fadeColors = variant === "reply" ? COMMENT_REPLY_FADE_COLORS : COMMENT_CARD_FADE_COLORS;
  const canCollapse = isComment && !isEditing && shouldCollapseComment(commentContent);
  const isCommentCollapsed = canCollapse && !expanded;
  const renderedCommentContent = useMemo(
    () => isCommentCollapsed ? createCommentPreview(commentContent) : commentContent,
    [commentContent, isCommentCollapsed],
  );

  function openActionsMenuAtPress(event: GestureResponderEvent) {
    if (!isComment || isEditing || !hasCommentActions) return;
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
        {onReply ? (
          <DropdownItem
            label={t("issues.reply")}
            onPress={() => {
              onReply(entry.id);
              closeActionsMenu();
            }}
          />
        ) : null}
        <DropdownItem
          label={t("issues.copy")}
          onPress={() => {
            onCopyComment?.(entry.content ?? "");
            closeActionsMenu();
          }}
        />
        {isOwnComment ? (
          <>
            <DropdownItem
              label={t("issues.edit")}
              onPress={() => {
                onStartEdit?.(entry.id, entry.content ?? "");
                closeActionsMenu();
              }}
            />
            <DropdownItem
              destructive
              label={t("issues.delete")}
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
    const menuHeight = 44 + (onReply ? 44 : 0) + (isOwnComment ? 72 : 0);
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

  const content = (
    <>
      <View style={isComment ? styles.commentHeader : styles.timelineHeader}>
        <View style={styles.timelineActorGroup}>
          <Text style={styles.timelineActor}>{actor}</Text>
          <Text style={styles.timelineDate}>{formatDate(entry.created_at)}</Text>
        </View>
        {isComment ? (
          <View style={styles.commentHeaderActions}>
            <HeaderIconButton
              label={t("issues.issue_actions")}
              disabled={!hasCommentActions}
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
        ) : null}
      </View>
      {isEditing ? (
        <View style={styles.editBox}>
          <MentionTextInput
            autoFocus
            issueMentionTargets={issueMentionTargets ?? []}
            mentionTargets={mentionTargets ?? []}
            multiline
            onChangeText={onChangeEdit ?? (() => {})}
            style={styles.commentInput}
            value={editingContent ?? ""}
          />
          <View style={styles.inlineActions}>
            <Button onPress={() => onSaveEdit?.(entry.id)}>{t("common.save")}</Button>
            <Button onPress={() => {
              setOpenMenu(null);
              setActionsMenuAnchor(null);
              onCancelEdit?.();
            }} variant="secondary">
              {t("common.cancel")}
            </Button>
          </View>
        </View>
      ) : entry.type === "comment" ? (
        <View style={[styles.commentBodyWrap, isCommentCollapsed && styles.commentBodyCollapsed]}>
          <MarkdownText
            content={renderedCommentContent}
            onIssueMentionPress={onIssueMentionPress}
          />
          {isCommentCollapsed ? (
            <LinearGradient
              colors={fadeColors}
              pointerEvents="box-none"
              style={styles.commentFadeOverlay}
            >
              <Pressable
                accessibilityLabel={t("issues.expand_comment")}
                accessibilityRole="button"
                onPress={() => onExpandComment?.(entry.id)}
                style={({ pressed }) => [
                  styles.expandCommentButton,
                  pressed && styles.buttonPressed,
                ]}
              >
                <ExpandCommentIcon />
              </Pressable>
            </LinearGradient>
          ) : null}
        </View>
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
    </>
  );

  return (
    <Pressable
      delayLongPress={320}
      onLongPress={isComment && !isEditing ? openActionsMenuAtPress : undefined}
      style={[
        styles.timelineItem,
        variant === "threadRoot" && styles.timelineItemThreadRoot,
        variant === "reply" && styles.timelineItemReply,
      ]}
    >
      {renderActionsMenuModal()}
      {variant === "reply" ? (
        <View style={[
          styles.replyInner,
          !isLastReply && styles.replyInnerSeparator,
        ]}>
          {content}
        </View>
      ) : content}
    </Pressable>
  );
});

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
      {typeof children === "string" || typeof children === "number" ? (
        <Text style={styles.headerIconButtonText}>{children}</Text>
      ) : children}
    </Pressable>
  );
}

function ExpandCommentIcon() {
  return (
    <Svg fill="none" height={22} viewBox="0 0 24 24" width={22}>
      <Path
        d="M7 10.5L12 15.5L17 10.5"
        stroke={colors.foreground}
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth={2.4}
      />
      <Path
        d="M7 5.5L12 10.5L17 5.5"
        stroke={colors.foreground}
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeOpacity={0.72}
        strokeWidth={2.4}
      />
    </Svg>
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
  onOpen: (attachment: Attachment, attachmentGroup: Attachment[]) => void;
  onRemove?: (attachmentId: string) => void;
  removingAttachmentId?: string | null;
}) {
  const { t } = useTranslation();
  if (attachments.length === 0) {
    if (compact) return null;
    return <Text style={styles.emptyText}>{t("issues.no_attachments")}</Text>;
  }

  return (
    <View style={styles.attachmentList}>
      {attachments.map((attachment) => (
        <Pressable
          key={attachment.id}
          onPress={() => onOpen(attachment, attachments)}
          style={({ pressed }) => [
            styles.attachmentRow,
            pressed && styles.buttonPressed,
          ]}
        >
          <View style={styles.attachmentContent}>
            <Text style={styles.attachmentName}>{attachment.filename}</Text>
            <Text style={styles.attachmentMeta}>
              {attachmentMetaLabel(attachment, t)}
            </Text>
          </View>
          {onRemove ? (
            <Pressable
              accessibilityLabel={t("issues.remove_attachment", { name: attachment.filename })}
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
                {removingAttachmentId === attachment.id ? "..." : t("issues.remove")}
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
  const { t } = useTranslation();
  return (
    <View style={styles.attachmentList}>
      {attachments.map((attachment) => (
        <View key={attachment.id} style={styles.attachmentRow}>
          <View style={styles.attachmentContent}>
            <Text style={styles.attachmentName}>{attachment.name}</Text>
            <Text style={styles.attachmentMeta}>
              {formatBytes(attachment.size ?? 0)} / {attachment.mimeType || t("issues.file")}
            </Text>
          </View>
          <Pressable
            accessibilityLabel={t("issues.remove_attachment", { name: attachment.name })}
            accessibilityRole="button"
            hitSlop={8}
            onPress={() => onRemove(attachment.id)}
            style={({ pressed }) => [
              styles.attachmentRemoveButton,
              pressed && styles.buttonPressed,
            ]}
          >
            <Text style={styles.attachmentRemoveText}>{t("issues.remove")}</Text>
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
  const { t } = useTranslation();
  const attachment = preview?.attachment;
  const url = attachment ? attachment.download_url || attachment.url : "";
  const canPreviewImage = Boolean(attachment && isImageAttachment(attachment));
  const canPreviewText = Boolean(attachment && isTextPreviewAttachment(attachment));
  const imageAttachments = useMemo(() => (
    preview?.imageAttachments?.length ? preview.imageAttachments : attachment && canPreviewImage ? [attachment] : []
  ), [attachment, canPreviewImage, preview?.imageAttachments]);

  if (canPreviewImage) {
    return (
      <ImagePreviewModal
        imageAttachments={imageAttachments}
        initialIndex={preview?.imageIndex ?? 0}
        onClose={onClose}
        open={open}
      />
    );
  }

  return (
    <Modal animationType="slide" onRequestClose={onClose} transparent visible={open}>
      <View style={styles.previewModalRoot}>
        <View style={styles.previewModal}>
          <View style={styles.previewHeader}>
            <View style={styles.previewTitleGroup}>
              <Text numberOfLines={1} style={styles.previewTitle}>
                {attachment?.filename ?? t("issues.attachment")}
              </Text>
              {attachment ? (
                <Text style={styles.previewMeta}>
                  {formatBytes(attachment.size_bytes)} / {attachment.content_type || t("issues.file")}
                </Text>
              ) : null}
            </View>
            {attachment ? (
              <View style={styles.previewActions}>
                <Button onPress={() => void Linking.openURL(url)} variant="secondary">
                  {t("common.open")}
                </Button>
                <Button onPress={onClose} variant="ghost">
                  {t("common.close")}
                </Button>
              </View>
            ) : null}
          </View>

          <View style={styles.previewBody}>
            {attachment && canPreviewText ? (
              preview?.loading ? (
                <View style={styles.previewCentered}>
                  <ActivityIndicator />
                  <Text style={styles.attachmentMeta}>{t("issues.loading_preview")}</Text>
                </View>
              ) : (
                <ScrollView contentContainerStyle={styles.previewTextContent}>
                  <Text selectable style={styles.previewText}>
                    {preview?.error ?? preview?.textContent ?? t("issues.no_preview_available")}
                  </Text>
                </ScrollView>
              )
            ) : null}

            {attachment && !canPreviewText ? (
              <View style={styles.previewCentered}>
                <Text style={styles.previewUnsupportedTitle}>{t("issues.preview_unavailable")}</Text>
                <Text style={styles.previewUnsupportedBody}>
                  {t("issues.preview_unsupported_body")}
                </Text>
                <Button onPress={() => void Linking.openURL(url)} variant="secondary">
                  {t("issues.open_externally")}
                </Button>
              </View>
            ) : null}

            {attachment && preview?.error && !canPreviewText ? (
              <View style={styles.previewCentered}>
                <Text style={styles.errorText}>{preview.error}</Text>
                <Button onPress={() => void Linking.openURL(url)} variant="secondary">
                  {t("issues.open_externally")}
                </Button>
              </View>
            ) : null}
          </View>
        </View>
      </View>
    </Modal>
  );
}

function IssueLiveAgentCard({
  cancellingTaskIds,
  error,
  getActorName,
  onStop,
  tasks,
}: {
  cancellingTaskIds: Set<string>;
  error: string | null;
  getActorName: (type: string, id: string) => string;
  onStop: (taskId: string) => void;
  tasks: Array<{ task: AgentTask; messages: TaskMessagePayload[] }>;
}) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const primary = tasks[0];
  if (!primary) return null;

  const agentName = getActorName("agent", primary.task.agent_id);
  const toolCount = primary.messages.filter((message) => message.type === "tool_use").length;
  const extraCount = Math.max(0, tasks.length - 1);
  const statusText = [
    error ? t("common.error_with_message", { message: error }) : null,
    toolCount > 0 ? t("issues.tools_count", { count: toolCount }) : null,
    extraCount > 0 ? t("issues.more_count", { count: extraCount }) : null,
  ].filter(Boolean).join(" · ");

  return (
    <View style={styles.liveCard}>
      <Pressable
        accessibilityRole="button"
        onPress={() => setExpanded(true)}
        style={({ pressed }) => [styles.liveCardHeader, pressed && styles.buttonPressed]}
      >
        <ActivityIndicator color={colors.primary} size="small" />
        <View style={styles.liveCardTextGroup}>
          <Text numberOfLines={1} style={styles.liveCardTitle}>
            {t("issues.agent_working", { name: agentName })} ·{" "}
            <LiveElapsed task={primary.task} />
            {statusText ? ` · ${statusText}` : ""}
          </Text>
        </View>
        <Pressable
          disabled={cancellingTaskIds.has(primary.task.id)}
          onPress={(event) => {
            event.stopPropagation();
            onStop(primary.task.id);
          }}
          style={({ pressed }) => [
            styles.liveStopButton,
            pressed && styles.buttonPressed,
            cancellingTaskIds.has(primary.task.id) && styles.headerIconButtonDisabled,
          ]}
        >
          <Text style={styles.liveStopButtonText}>
            {cancellingTaskIds.has(primary.task.id) ? t("issues.stopping") : t("issues.stop")}
          </Text>
        </Pressable>
      </Pressable>

      <Modal animationType="slide" onRequestClose={() => setExpanded(false)} visible={expanded}>
        <View style={styles.liveModal}>
          <View style={styles.previewHeader}>
            <View style={styles.previewTitleGroup}>
              <Text style={styles.previewTitle}>{t("issues.agent_live_transcript")}</Text>
              <Text style={styles.previewMeta}>
                {tasks.length === 1 ? t("issues.active_run_one") : t("issues.active_run_other", { count: tasks.length })}
              </Text>
            </View>
            <Button onPress={() => setExpanded(false)} variant="secondary">{t("common.close")}</Button>
          </View>
          <ScrollView contentContainerStyle={styles.liveModalContent}>
            {tasks.map(({ task, messages }, index) => (
              <View key={task.id} style={styles.liveTaskGroup}>
                <View style={styles.timelineHeader}>
                  <View style={styles.timelineActorGroup}>
                    <Text style={styles.timelineActor}>{getActorName("agent", task.agent_id)}</Text>
                    <Text style={styles.timelineDate}>
                      <LiveElapsed task={task} /> · {formatAgentTaskStatus(t, task.status)}
                    </Text>
                  </View>
                  <Button
                    disabled={cancellingTaskIds.has(task.id)}
                    onPress={() => onStop(task.id)}
                    variant="secondary"
                  >
                    {cancellingTaskIds.has(task.id) ? t("issues.stopping") : t("issues.stop")}
                  </Button>
                </View>
                {messages.length === 0 ? (
                  <Text style={styles.emptyText}>
                    {t("issues.live_log_unavailable")}
                  </Text>
                ) : (
                  messages.map((message) => (
                    <TaskMessageRow key={`${task.id}-${message.seq}`} message={message} />
                  ))
                )}
                {index < tasks.length - 1 ? <View style={styles.liveTaskSeparator} /> : null}
              </View>
            ))}
          </ScrollView>
        </View>
      </Modal>
    </View>
  );
}

function LiveElapsed({ task }: { task: AgentTask }) {
  const start = task.started_at ?? task.dispatched_at ?? task.created_at;
  const [elapsed, setElapsed] = useState(() => formatElapsed(start));

  useEffect(() => {
    setElapsed(formatElapsed(start));
    const interval = setInterval(() => setElapsed(formatElapsed(start)), 1000);
    return () => clearInterval(interval);
  }, [start]);

  return <>{elapsed}</>;
}

const TaskRunHeader = memo(function TaskRunHeader({
  onPress,
  task,
}: {
  onPress: () => void;
  task: AgentTask;
}) {
  const { t } = useTranslation();
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [styles.taskCard, pressed && styles.buttonPressed]}
    >
      <View style={styles.timelineHeader}>
        <Text style={styles.timelineActor}>{t("issues.run_title", { id: task.id.slice(0, 8) })}</Text>
        <Text style={styles.timelineDate}>{formatAgentTaskStatus(t, task.status)}</Text>
      </View>
      {task.error ? <Text style={styles.errorText}>{task.error}</Text> : null}
    </Pressable>
  );
});

function formatDate(date: string | null | undefined) {
  if (!date) return "-";
  return new Date(date).toLocaleDateString();
}

function formatElapsed(date: string) {
  const elapsed = Math.max(0, Date.now() - new Date(date).getTime());
  const seconds = Math.floor(elapsed / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}m`;
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

function attachmentPreviewLabel(attachment: Attachment, t: Translate) {
  if (isImageAttachment(attachment)) return t("issues.tap_to_view");
  if (isTextPreviewAttachment(attachment)) return t("issues.tap_to_preview");
  return t("issues.tap_to_open");
}

function attachmentMetaLabel(attachment: Attachment, t: Translate) {
  const fileInfo = `${formatBytes(attachment.size_bytes)} / ${attachment.content_type || t("issues.file")}`;
  if (isImageAttachment(attachment)) return fileInfo;
  return `${fileInfo} / ${attachmentPreviewLabel(attachment, t)}`;
}

function formatDocumentPickerError(err: unknown, t: Translate) {
  const message = err instanceof Error ? err.message : String(err);
  if (message.includes("ExpoDocumentPicker")) {
    return t("issues.file_picker_rebuild");
  }
  return message || t("issues.file_picker_unavailable");
}

function formatClipboardError(err: unknown, t: Translate) {
  const message = err instanceof Error ? err.message : String(err);
  return message || t("issues.unable_to_copy_comment");
}

function statusLabel(status: string, t: Translate) {
  return formatIssueStatus(t, status as IssueStatus) ?? status;
}

function priorityLabel(priority: string, t: Translate) {
  return formatIssuePriority(t, priority as IssuePriority) ?? priority;
}

function formatActivity(
  entry: TimelineEntry,
  resolveActorName: (type: string, id: string) => string,
  t: Translate,
) {
  const details = (entry.details ?? {}) as Record<string, string>;
  switch (entry.action) {
    case "created":
      return t("issues.activity_created");
    case "status_changed":
      return t("issues.activity_status_changed", {
        from: statusLabel(details.from ?? "?", t),
        to: statusLabel(details.to ?? "?", t),
      });
    case "priority_changed":
      return t("issues.activity_priority_changed", {
        from: priorityLabel(details.from ?? "?", t),
        to: priorityLabel(details.to ?? "?", t),
      });
    case "assignee_changed": {
      const toName = details.to_id && details.to_type
        ? resolveActorName(details.to_type, details.to_id)
        : null;
      if (toName) return t("issues.activity_assigned_to", { name: toName });
      if (details.from_id && !details.to_id) return t("issues.activity_removed_assignee");
      return t("issues.activity_changed_assignee");
    }
    case "due_date_changed":
      return details.to
        ? t("issues.activity_set_due_date", { date: formatDate(details.to) })
        : t("issues.activity_removed_due_date");
    case "description_updated":
      return t("issues.activity_description_updated");
    case "title_changed":
      return t("issues.activity_title_changed");
    case "task_completed":
      return t("issues.activity_task_completed");
    case "task_failed":
      return t("issues.activity_task_failed");
    default:
      return entry.action ?? t("issues.activity_updated");
  }
}

const styles = StyleSheet.create({
  content: {
    padding: spacing.lg,
    paddingBottom: 96,
  },
  listItemSeparator: {
    height: spacing.md,
  },
  issueDetailContent: {
    padding: spacing.lg,
    paddingBottom: 96,
  },
  contentEditingComment: {
    paddingBottom: 240,
  },
  propertiesContent: {
    gap: spacing.lg,
    padding: spacing.lg,
  },
  keyboardAvoidingContent: {
    flex: 1,
  },
  listHeader: {
    gap: spacing.xl,
  },
  section: {
    gap: spacing.sm,
  },
  sectionSeparated: {
    borderTopColor: colors.border,
    borderTopWidth: 1,
    paddingTop: spacing.xl,
  },
  sectionHeader: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.md,
    justifyContent: "space-between",
  },
  sectionHeaderSurface: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    minHeight: 58,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.md,
  },
  sectionHeaderActionButton: {
    minHeight: 32,
    paddingHorizontal: spacing.sm,
  },
  sectionTitleGroup: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
    minHeight: 32,
  },
  sectionTitleAccent: {
    backgroundColor: colors.foreground,
    borderRadius: 2,
    height: 18,
    width: 3,
  },
  stickySectionHeader: {
    alignItems: "center",
    backgroundColor: colors.background,
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderTopColor: colors.border,
    borderTopWidth: StyleSheet.hairlineWidth,
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
    backgroundColor: colors.muted,
    borderRadius: 10,
    color: colors.foreground,
    fontSize: 12,
    fontWeight: "600",
    minWidth: 22,
    overflow: "hidden",
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
    textAlign: "center",
  },
  commentListHeader: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.md,
    justifyContent: "space-between",
  },
  jumpToLatestButton: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.sm,
    justifyContent: "center",
    minHeight: 32,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
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
  editableTitle: {
    borderRadius: radii.sm,
    marginHorizontal: -spacing.xs,
    marginBottom: spacing.xs,
    paddingHorizontal: spacing.xs,
    paddingVertical: spacing.xs,
  },
  issueBodyTitle: {
    color: colors.foreground,
    fontSize: 22,
    fontWeight: "700",
    lineHeight: 29,
  },
  issueTitleInput: {
    backgroundColor: colors.muted,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    color: colors.foreground,
    fontSize: 22,
    fontWeight: "700",
    includeFontPadding: false,
    lineHeight: 29,
    marginBottom: spacing.xs,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  editableDescription: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.xs,
    marginHorizontal: -spacing.sm,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.md,
  },
  descriptionHeader: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
    justifyContent: "space-between",
  },
  descriptionLabel: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  editHintText: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
  emptyText: {
    color: colors.mutedForeground,
    fontSize: 14,
  },
  issueEngagementRow: {
    alignItems: "center",
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.sm,
  },
  subscribeButton: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    justifyContent: "center",
    minHeight: 36,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  subscribeButtonActive: {
    backgroundColor: colors.primary,
    borderColor: colors.primary,
  },
  subscribeButtonDisabled: {
    opacity: 0.6,
  },
  subscribeButtonText: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "600",
  },
  subscribeButtonTextActive: {
    color: colors.primaryForeground,
  },
  issueShortcutButton: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.xs,
    minHeight: 36,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  issueShortcutLabel: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "600",
  },
  issueShortcutCount: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "600",
  },
  errorText: {
    color: colors.destructive,
    fontSize: 14,
    lineHeight: 20,
  },
  propertiesBlock: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.md,
    padding: spacing.md,
  },
  propertiesBlockHeader: {
    alignItems: "center",
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.sm,
    justifyContent: "space-between",
    marginHorizontal: -spacing.md,
    paddingBottom: spacing.md,
    paddingHorizontal: spacing.md,
  },
  propertiesBlockTitle: {
    color: colors.foreground,
    flex: 1,
    fontSize: 16,
    fontWeight: "600",
  },
  property: {
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
  propertySelectTrigger: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.sm,
    height: 44,
    justifyContent: "space-between",
    paddingHorizontal: spacing.md,
    width: "100%",
  },
  propertySelectText: {
    color: colors.foreground,
    flex: 1,
    fontSize: 14,
    fontWeight: "500",
  },
  propertySelectPlaceholder: {
    color: colors.mutedForeground,
  },
  propertySelectMeta: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  dueDateTrigger: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.sm,
    height: 44,
    justifyContent: "space-between",
    paddingHorizontal: spacing.md,
    width: "100%",
  },
  dueDateTriggerText: {
    color: colors.foreground,
    flex: 1,
    fontSize: 14,
    fontWeight: "500",
  },
  dueDatePlaceholder: {
    color: colors.mutedForeground,
  },
  dueDateTriggerMeta: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  labelTrigger: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.sm,
    justifyContent: "space-between",
    minHeight: 44,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    width: "100%",
  },
  labelChipList: {
    flex: 1,
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.xs,
    minWidth: 0,
  },
  issueLabelChip: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.xs,
    maxWidth: "100%",
    paddingHorizontal: spacing.sm,
    paddingVertical: 4,
  },
  issueLabelChipDot: {
    borderRadius: 4,
    height: 8,
    width: 8,
  },
  issueLabelChipText: {
    color: colors.foreground,
    flexShrink: 1,
    fontSize: 12,
    fontWeight: "500",
  },
  disabledAction: {
    opacity: 0.6,
  },
  value: {
    color: colors.foreground,
    fontSize: 14,
  },
  assigneeSearchInput: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    color: colors.foreground,
    fontSize: 14,
    marginBottom: spacing.md,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  assigneePickerList: {
    maxHeight: 380,
  },
  assigneeSection: {
    gap: spacing.xs,
    marginTop: spacing.md,
  },
  assigneeSectionTitle: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "600",
    textTransform: "uppercase",
  },
  assigneeOption: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    minHeight: 44,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  assigneeOptionActive: {
    borderColor: colors.primary,
  },
  assigneeOptionTextGroup: {
    flex: 1,
    gap: 2,
    minWidth: 0,
  },
  assigneeOptionLabel: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
  },
  assigneeOptionLabelActive: {
    color: colors.primary,
  },
  assigneeOptionSubtitle: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
  assigneeEmptyText: {
    color: colors.mutedForeground,
    fontSize: 14,
    paddingVertical: spacing.lg,
    textAlign: "center",
  },
  parentSearchLoading: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
    justifyContent: "center",
    paddingVertical: spacing.lg,
  },
  parentIssueOptionHeader: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
    minWidth: 0,
  },
  parentIssueStatusDot: {
    borderRadius: 4,
    height: 8,
    width: 8,
  },
  labelOptionSwatch: {
    borderRadius: 6,
    height: 12,
    width: 12,
  },
  labelOptionName: {
    flex: 1,
    minWidth: 0,
  },
  labelOptionSelected: {
    color: colors.primary,
    fontSize: 12,
    fontWeight: "600",
    marginLeft: "auto",
  },
  sectionTitle: {
    color: colors.foreground,
    fontSize: 17,
    fontWeight: "700",
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
  relationList: {
    gap: spacing.sm,
  },
  commentRowThreadStart: {
    marginTop: spacing.md,
  },
  commentRowThreadContinuation: {
    marginTop: 0,
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
  threadReplyFooter: {
    backgroundColor: colors.card,
    borderBottomLeftRadius: radii.md,
    borderBottomRightRadius: radii.md,
    borderColor: colors.border,
    borderTopLeftRadius: 0,
    borderTopRightRadius: 0,
    borderTopColor: colors.border,
    borderWidth: StyleSheet.hairlineWidth,
    borderTopWidth: StyleSheet.hairlineWidth,
    padding: spacing.md,
  },
  threadReplyButton: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    minHeight: 40,
    justifyContent: "center",
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  threadReplyButtonText: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
  },
  timelineItemThreadRoot: {
    borderBottomLeftRadius: 0,
    borderBottomRightRadius: 0,
    borderBottomWidth: 0,
  },
  timelineItemReply: {
    borderBottomWidth: 0,
    borderColor: colors.border,
    borderRadius: 0,
    borderWidth: StyleSheet.hairlineWidth,
    borderTopWidth: StyleSheet.hairlineWidth,
    gap: 0,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  replyInner: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    gap: spacing.sm,
    padding: spacing.md,
  },
  replyInnerSeparator: {
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
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
  issueActionsDropdown: {
    minWidth: 156,
    position: "absolute",
    right: spacing.lg,
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
  commentBodyWrap: {
    position: "relative",
  },
  commentBodyCollapsed: {
    height: COLLAPSED_COMMENT_HEIGHT,
    overflow: "hidden",
  },
  commentFadeOverlay: {
    alignItems: "center",
    bottom: 0,
    justifyContent: "flex-end",
    left: 0,
    minHeight: 96,
    paddingTop: spacing.xl,
    paddingBottom: spacing.xs,
    position: "absolute",
    right: 0,
  },
  expandCommentButton: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: 18,
    borderWidth: StyleSheet.hairlineWidth,
    height: 36,
    justifyContent: "center",
    shadowColor: "#000000",
    shadowOffset: { height: 2, width: 0 },
    shadowOpacity: 0.12,
    shadowRadius: 6,
    width: 36,
    elevation: 4,
  },
  editBox: {
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
    maxHeight: 224,
    overflow: "hidden",
  },
  mentionEmptyText: {
    color: colors.mutedForeground,
    fontSize: 13,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  mentionGroupLabel: {
    color: colors.mutedForeground,
    fontSize: 11,
    fontWeight: "600",
    paddingHorizontal: spacing.md,
    paddingTop: spacing.sm,
    textTransform: "uppercase",
  },
  mentionSuggestionRow: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
    minHeight: 48,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  mentionSuggestionRowSelected: {
    backgroundColor: colors.muted,
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
  mentionSuggestionClosed: {
    opacity: 0.55,
    textDecorationLine: "line-through",
  },
  attachmentList: {
    gap: spacing.sm,
  },
  attachmentSectionPanel: {
    backgroundColor: colors.muted,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    padding: spacing.sm,
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
    minHeight: 64,
    padding: spacing.md,
    shadowColor: "#000000",
    shadowOffset: { height: 1, width: 0 },
    shadowOpacity: 0.04,
    shadowRadius: 2,
    elevation: 1,
  },
  attachmentContent: {
    flex: 1,
    gap: spacing.xs,
    minWidth: 0,
  },
  attachmentName: {
    color: colors.foreground,
    fontSize: 15,
    fontWeight: "600",
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
  previewModalRoot: {
    flex: 1,
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
  liveCard: {
    backgroundColor: colors.background,
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.xs,
  },
  liveCardHeader: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
    minHeight: 36,
  },
  liveCardTextGroup: {
    flex: 1,
    minWidth: 0,
  },
  liveCardTitle: {
    color: colors.foreground,
    fontSize: 13,
    fontWeight: "600",
    lineHeight: 18,
  },
  liveStopButton: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.sm,
    justifyContent: "center",
    minHeight: 28,
    paddingHorizontal: spacing.sm,
  },
  liveStopButtonText: {
    color: colors.foreground,
    fontSize: 11,
    fontWeight: "600",
  },
  liveModal: {
    backgroundColor: colors.background,
    flex: 1,
    paddingTop: Platform.OS === "ios" ? 56 : spacing.lg,
  },
  liveModalContent: {
    gap: spacing.lg,
    padding: spacing.lg,
    paddingBottom: 48,
  },
  liveTaskGroup: {
    gap: spacing.md,
  },
  liveTaskSeparator: {
    backgroundColor: colors.border,
    height: StyleSheet.hairlineWidth,
    marginTop: spacing.sm,
  },
  taskCard: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.sm,
    padding: spacing.md,
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
  descriptionSheetInput: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    color: colors.foreground,
    fontSize: 16,
    includeFontPadding: false,
    lineHeight: 22,
    maxHeight: 280,
    minHeight: 220,
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
  sheetHelperText: {
    color: colors.mutedForeground,
    flex: 1,
    fontSize: 12,
  },
});
