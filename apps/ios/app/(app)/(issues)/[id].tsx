import { useState, useCallback, useMemo } from "react";
import {
  View,
  Text,
  ScrollView,
  TouchableOpacity,
  KeyboardAvoidingView,
  Platform,
  Alert,
  StyleSheet,
  ActivityIndicator,
} from "react-native";
import { useLocalSearchParams, Stack } from "expo-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWSEvent } from "@multica/core/realtime";
import {
  issueDetailOptions,
  issueKeys,
  issueTimelineOptions,
} from "@multica/core/issues/queries";
import { useUpdateIssue, useCreateComment } from "@multica/core/issues/mutations";
import { STATUS_CONFIG } from "@multica/core/issues/config/status";
import { PRIORITY_CONFIG } from "@multica/core/issues/config/priority";
import { STATUS_ORDER } from "@multica/core/issues/config/status";
import { PRIORITY_ORDER } from "@multica/core/issues/config/priority";
import type {
  IssueAssigneeType,
  IssuePriority,
  IssueStatus,
  TimelineEntry,
} from "@multica/core/types";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { StatusIcon } from "@/components/StatusIcon";
import { PriorityIcon } from "@/components/PriorityIcon";
import { Avatar } from "@/components/Avatar";
import { AgentLiveLog } from "@/components/AgentLiveLog";
import { MarkdownText } from "@/components/MarkdownText";
import { MentionTextInput } from "@/components/MentionTextInput";
import { ScreenHeader } from "@/components/ScreenHeader";
import { createActorResolver } from "@/lib/actors";
import { colors } from "@/lib/theme";

export default function IssueDetailScreen() {
  const { id, returnTo, returnLabel } = useLocalSearchParams<{
    id: string;
    returnTo?: string;
    returnLabel?: string;
  }>();
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

  const { data: issue, isLoading } = useQuery(issueDetailOptions(wsId, id!));
  const { data: timeline = [] } = useQuery(issueTimelineOptions(id!));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const updateIssue = useUpdateIssue();
  const createComment = useCreateComment(id!);

  const [commentText, setCommentText] = useState("");
  const [showStatusPicker, setShowStatusPicker] = useState(false);
  const [showPriorityPicker, setShowPriorityPicker] = useState(false);
  const [showAssigneePicker, setShowAssigneePicker] = useState(false);
  const [showProjectPicker, setShowProjectPicker] = useState(false);

  const comments = useMemo(
    () => timeline.filter((e: TimelineEntry) => e.type === "comment"),
    [timeline],
  );

  const activeAgents = useMemo(
    () => agents.filter((agent) => !agent.archived_at),
    [agents],
  );

  const resolveActor = useMemo(
    () => createActorResolver(members, agents),
    [members, agents],
  );

  const projectMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const project of projects) map.set(project.id, project.title);
    return map;
  }, [projects]);

  const refreshIssueState = useCallback(() => {
    qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, id!) });
    qc.invalidateQueries({ queryKey: issueKeys.timeline(id!) });
    qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
    qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  }, [id, qc, wsId]);

  useWSEvent(
    "issue:updated",
    useCallback(
      (payload: unknown) => {
        const p = payload as { issue?: { id?: string } };
        if (p.issue?.id === id) refreshIssueState();
      },
      [id, refreshIssueState],
    ),
  );

  useWSEvent(
    "comment:created",
    useCallback(
      (payload: unknown) => {
        const p = payload as { comment?: { issue_id?: string } };
        if (p.comment?.issue_id === id) refreshIssueState();
      },
      [id, refreshIssueState],
    ),
  );

  useWSEvent(
    "comment:updated",
    useCallback(
      (payload: unknown) => {
        const p = payload as { comment?: { issue_id?: string } };
        if (p.comment?.issue_id === id) refreshIssueState();
      },
      [id, refreshIssueState],
    ),
  );

  useWSEvent(
    "comment:deleted",
    useCallback(
      (payload: unknown) => {
        const p = payload as { issue_id?: string };
        if (p.issue_id === id) refreshIssueState();
      },
      [id, refreshIssueState],
    ),
  );

  useWSEvent(
    "activity:created",
    useCallback(
      (payload: unknown) => {
        const p = payload as { issue_id?: string };
        if (p.issue_id === id) refreshIssueState();
      },
      [id, refreshIssueState],
    ),
  );

  const refreshOnTaskEvent = useCallback(
    (payload: unknown) => {
      const p = payload as { issue_id?: string };
      if (p.issue_id === id) refreshIssueState();
    },
    [id, refreshIssueState],
  );

  useWSEvent("task:dispatch", refreshOnTaskEvent);
  useWSEvent("task:completed", refreshOnTaskEvent);
  useWSEvent("task:failed", refreshOnTaskEvent);
  useWSEvent("task:cancelled", refreshOnTaskEvent);

  const handleStatusChange = useCallback(
    (status: IssueStatus) => {
      updateIssue.mutate({ id: id!, status });
      setShowStatusPicker(false);
    },
    [id, updateIssue],
  );

  const handlePriorityChange = useCallback(
    (priority: IssuePriority) => {
      updateIssue.mutate({ id: id!, priority });
      setShowPriorityPicker(false);
    },
    [id, updateIssue],
  );

  const handleAssigneeChange = useCallback(
    (assigneeType: IssueAssigneeType | null, assigneeId: string | null) => {
      updateIssue.mutate({
        id: id!,
        assignee_type: assigneeType,
        assignee_id: assigneeId,
      });
      setShowAssigneePicker(false);
    },
    [id, updateIssue],
  );

  const handleProjectChange = useCallback(
    (projectId: string | null) => {
      updateIssue.mutate({ id: id!, project_id: projectId });
      setShowProjectPicker(false);
    },
    [id, updateIssue],
  );

  const handleComment = useCallback(async () => {
    const text = commentText.trim();
    if (!text) return;
    try {
      await createComment.mutateAsync({ content: text });
      setCommentText("");
    } catch {
      Alert.alert("Error", "Failed to post comment");
    }
  }, [commentText, createComment]);

  if (isLoading || !issue) {
    const loadingBackLabel = returnLabel ?? "Issues";
    const loadingFallback = returnTo ?? "/(app)/(issues)";
    return (
      <>
        <Stack.Screen options={{ headerShown: false }} />
        <ScreenHeader
          title="Issue"
          backLabel={loadingBackLabel}
          fallback={loadingFallback}
          preferFallback={Boolean(returnTo)}
        />
        <View style={styles.loadingContainer}>
          <ActivityIndicator size="large" />
        </View>
      </>
    );
  }

  const statusConfig = STATUS_CONFIG[issue.status];
  const priorityConfig = PRIORITY_CONFIG[issue.priority];
  const assignee = resolveActor(issue.assignee_type, issue.assignee_id);
  const projectTitle = issue.project_id ? projectMap.get(issue.project_id) : null;
  const backLabel = returnLabel ?? "Issues";
  const fallback = returnTo ?? "/(app)/(issues)";

  return (
    <>
      <Stack.Screen options={{ headerShown: false }} />
      <ScreenHeader
        title={issue.identifier}
        subtitle={issue.title}
        backLabel={backLabel}
        fallback={fallback}
        preferFallback={Boolean(returnTo)}
      />
      <KeyboardAvoidingView
        style={styles.container}
        behavior={Platform.OS === "ios" ? "padding" : undefined}
        keyboardVerticalOffset={100}
      >
        <ScrollView
          style={styles.scroll}
          contentContainerStyle={styles.scrollContent}
        >
          {/* Title */}
          <Text style={styles.title}>{issue.title}</Text>

          {/* Metadata row */}
          <View style={styles.metaSection}>
            {/* Status */}
            <TouchableOpacity
              style={styles.metaRow}
              onPress={() => {
                setShowStatusPicker(!showStatusPicker);
                setShowPriorityPicker(false);
                setShowAssigneePicker(false);
                setShowProjectPicker(false);
              }}
            >
              <Text style={styles.metaLabel}>Status</Text>
              <View style={styles.metaValue}>
                <StatusIcon status={issue.status} size={14} />
                <Text style={styles.metaValueText}>{statusConfig.label}</Text>
              </View>
            </TouchableOpacity>

            {showStatusPicker && (
              <View style={styles.picker}>
                {STATUS_ORDER.map((s) => (
                  <TouchableOpacity
                    key={s}
                    style={[
                      styles.pickerItem,
                      s === issue.status && styles.pickerItemActive,
                    ]}
                    onPress={() => handleStatusChange(s)}
                  >
                    <StatusIcon status={s} size={14} />
                    <Text style={styles.pickerItemText}>
                      {STATUS_CONFIG[s].label}
                    </Text>
                  </TouchableOpacity>
                ))}
              </View>
            )}

            {/* Priority */}
            <TouchableOpacity
              style={styles.metaRow}
              onPress={() => {
                setShowPriorityPicker(!showPriorityPicker);
                setShowStatusPicker(false);
                setShowAssigneePicker(false);
                setShowProjectPicker(false);
              }}
            >
              <Text style={styles.metaLabel}>Priority</Text>
              <View style={styles.metaValue}>
                <PriorityIcon priority={issue.priority} size={14} />
                <Text style={styles.metaValueText}>{priorityConfig.label}</Text>
              </View>
            </TouchableOpacity>

            {showPriorityPicker && (
              <View style={styles.picker}>
                {PRIORITY_ORDER.map((p) => (
                  <TouchableOpacity
                    key={p}
                    style={[
                      styles.pickerItem,
                      p === issue.priority && styles.pickerItemActive,
                    ]}
                    onPress={() => handlePriorityChange(p)}
                  >
                    <PriorityIcon priority={p} size={14} />
                    <Text style={styles.pickerItemText}>
                      {PRIORITY_CONFIG[p].label}
                    </Text>
                  </TouchableOpacity>
                ))}
              </View>
            )}

            {/* Assignee */}
            <TouchableOpacity
              style={styles.metaRow}
              onPress={() => {
                setShowAssigneePicker(!showAssigneePicker);
                setShowStatusPicker(false);
                setShowPriorityPicker(false);
                setShowProjectPicker(false);
              }}
            >
              <Text style={styles.metaLabel}>Assignee</Text>
              <View style={styles.metaValue}>
                {assignee ? (
                  <>
                    <Avatar
                      name={assignee.name}
                      avatarUrl={assignee.avatarUrl}
                      size={20}
                      isAgent={assignee.isAgent}
                    />
                    <Text style={styles.metaValueText}>
                      {assignee.name}
                    </Text>
                  </>
                ) : (
                  <Text style={styles.metaValueMuted}>Unassigned</Text>
                )}
              </View>
            </TouchableOpacity>

            {showAssigneePicker && (
              <View style={styles.picker}>
                <TouchableOpacity
                  style={[
                    styles.pickerItem,
                    !issue.assignee_id && styles.pickerItemActive,
                  ]}
                  onPress={() => handleAssigneeChange(null, null)}
                >
                  <Text style={styles.pickerItemText}>Unassigned</Text>
                </TouchableOpacity>
                {members.map((member) => (
                  <TouchableOpacity
                    key={member.user_id}
                    style={[
                      styles.pickerItem,
                      issue.assignee_type === "member" &&
                        issue.assignee_id === member.user_id &&
                        styles.pickerItemActive,
                    ]}
                    onPress={() => handleAssigneeChange("member", member.user_id)}
                  >
                    <Avatar
                      name={member.name}
                      avatarUrl={member.avatar_url}
                      size={18}
                    />
                    <Text style={styles.pickerItemText}>{member.name}</Text>
                  </TouchableOpacity>
                ))}
                {activeAgents.map((agent) => (
                  <TouchableOpacity
                    key={agent.id}
                    style={[
                      styles.pickerItem,
                      issue.assignee_type === "agent" &&
                        issue.assignee_id === agent.id &&
                        styles.pickerItemActive,
                    ]}
                    onPress={() => handleAssigneeChange("agent", agent.id)}
                  >
                    <Avatar
                      name={agent.name}
                      avatarUrl={agent.avatar_url}
                      size={18}
                      isAgent
                    />
                    <Text style={styles.pickerItemText}>{agent.name}</Text>
                  </TouchableOpacity>
                ))}
              </View>
            )}

            {/* Project */}
            <TouchableOpacity
              style={styles.metaRow}
              onPress={() => {
                setShowProjectPicker(!showProjectPicker);
                setShowStatusPicker(false);
                setShowPriorityPicker(false);
                setShowAssigneePicker(false);
              }}
            >
              <Text style={styles.metaLabel}>Project</Text>
              <View style={styles.metaValue}>
                <Text
                  style={projectTitle ? styles.metaValueText : styles.metaValueMuted}
                  numberOfLines={1}
                >
                  {projectTitle ?? "No project"}
                </Text>
              </View>
            </TouchableOpacity>

            {showProjectPicker && (
              <View style={styles.picker}>
                <TouchableOpacity
                  style={[
                    styles.pickerItem,
                    !issue.project_id && styles.pickerItemActive,
                  ]}
                  onPress={() => handleProjectChange(null)}
                >
                  <Text style={styles.pickerItemText}>No project</Text>
                </TouchableOpacity>
                {projects.map((project) => (
                  <TouchableOpacity
                    key={project.id}
                    style={[
                      styles.pickerItem,
                      issue.project_id === project.id && styles.pickerItemActive,
                    ]}
                    onPress={() => handleProjectChange(project.id)}
                  >
                    <Text style={styles.pickerItemText}>{project.title}</Text>
                  </TouchableOpacity>
                ))}
              </View>
            )}
          </View>

          {/* Agent execution */}
          <View style={styles.section}>
            <AgentLiveLog
              issueId={issue.id}
              getAgentName={(agentId) =>
                agentId
                  ? resolveActor("agent", agentId)?.name ?? "Agent"
                  : "Agent"
              }
              getAgentAvatar={(agentId) =>
                agentId
                  ? resolveActor("agent", agentId)?.avatarUrl ?? null
                  : null
              }
            />
          </View>

          {/* Description */}
          {issue.description && (
            <View style={styles.section}>
              <Text style={styles.sectionTitle}>Description</Text>
              <MarkdownText content={issue.description} />
            </View>
          )}

          {/* Comments */}
          <View style={styles.section}>
            <Text style={styles.sectionTitle}>
              Comments ({comments.length})
            </Text>
            {comments.length > 0 ? (
              comments.map((entry: TimelineEntry) => {
                const author = resolveActor(entry.actor_type, entry.actor_id);
                return (
                  <View key={entry.id} style={styles.comment}>
                    <Avatar
                      name={author?.name}
                      avatarUrl={author?.avatarUrl}
                      size={28}
                      isAgent={author?.isAgent}
                    />
                    <View style={styles.commentContent}>
                      <View style={styles.commentHeader}>
                        <Text style={styles.commentAuthor}>
                          {author?.name ?? "Unknown"}
                        </Text>
                        <Text style={styles.commentTime}>
                          {formatRelativeTime(entry.created_at)}
                        </Text>
                      </View>
                      {entry.content ? (
                        <MarkdownText content={entry.content} />
                      ) : null}
                    </View>
                  </View>
                );
              })
            ) : (
              <Text style={styles.emptyText}>No comments yet</Text>
            )}
          </View>
        </ScrollView>

        {/* Comment input */}
        <View style={styles.commentInput}>
          <MentionTextInput
            style={styles.commentTextInput}
            value={commentText}
            onChangeText={setCommentText}
            placeholder="Add a comment..."
            placeholderTextColor={colors.mutedForeground}
            multiline
          />
          <TouchableOpacity
            onPress={handleComment}
            disabled={!commentText.trim()}
            style={[
              styles.sendButton,
              !commentText.trim() && styles.sendButtonDisabled,
            ]}
          >
            <Text style={styles.sendButtonText}>Send</Text>
          </TouchableOpacity>
        </View>
      </KeyboardAvoidingView>
    </>
  );
}

function formatRelativeTime(dateStr: string): string {
  const now = Date.now();
  const diff = now - new Date(dateStr).getTime();
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  return `${days}d`;
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.background,
  },
  loadingContainer: {
    flex: 1,
    justifyContent: "center",
    alignItems: "center",
    backgroundColor: colors.background,
  },
  scroll: {
    flex: 1,
  },
  scrollContent: {
    paddingBottom: 20,
  },
  title: {
    fontSize: 22,
    fontWeight: "700",
    color: colors.foreground,
    paddingHorizontal: 16,
    paddingTop: 16,
    paddingBottom: 12,
  },
  metaSection: {
    paddingHorizontal: 16,
    paddingBottom: 16,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.border,
  },
  metaRow: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingVertical: 10,
  },
  metaLabel: {
    fontSize: 14,
    color: colors.mutedForeground,
    width: 80,
  },
  metaValue: {
    flex: 1,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "flex-end",
    gap: 6,
  },
  metaValueText: {
    fontSize: 14,
    color: colors.foreground,
    fontWeight: "500",
  },
  metaValueMuted: {
    fontSize: 14,
    color: colors.mutedForeground,
  },
  picker: {
    backgroundColor: colors.secondary,
    borderRadius: 8,
    padding: 4,
    marginBottom: 8,
  },
  pickerItem: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingVertical: 8,
    paddingHorizontal: 12,
    borderRadius: 6,
  },
  pickerItemActive: {
    backgroundColor: colors.background,
  },
  pickerItemText: {
    fontSize: 14,
    color: colors.foreground,
  },
  section: {
    paddingHorizontal: 16,
    paddingTop: 16,
  },
  sectionTitle: {
    fontSize: 14,
    fontWeight: "600",
    color: colors.mutedForeground,
    marginBottom: 8,
    textTransform: "uppercase",
    letterSpacing: 0.5,
  },
  description: {
    fontSize: 15,
    color: colors.foreground,
    lineHeight: 22,
  },
  comment: {
    flexDirection: "row",
    gap: 10,
    paddingVertical: 10,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.border,
  },
  commentContent: {
    flex: 1,
  },
  commentHeader: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    marginBottom: 4,
  },
  commentAuthor: {
    fontSize: 14,
    fontWeight: "600",
    color: colors.foreground,
  },
  commentTime: {
    fontSize: 12,
    color: colors.mutedForeground,
  },
  commentBody: {
    fontSize: 14,
    color: colors.foreground,
    lineHeight: 20,
  },
  emptyText: {
    paddingVertical: 16,
    fontSize: 14,
    color: colors.mutedForeground,
  },
  commentInput: {
    flexDirection: "row",
    alignItems: "flex-end",
    paddingHorizontal: 16,
    paddingVertical: 8,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: colors.border,
    backgroundColor: colors.background,
    gap: 8,
  },
  commentTextInput: {
    flex: 1,
    minHeight: 36,
    maxHeight: 100,
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: 8,
    paddingHorizontal: 12,
    paddingVertical: 8,
    fontSize: 14,
    color: colors.foreground,
  },
  sendButton: {
    paddingHorizontal: 14,
    paddingVertical: 8,
    borderRadius: 8,
    backgroundColor: colors.foreground,
  },
  sendButtonDisabled: {
    opacity: 0.3,
  },
  sendButtonText: {
    fontSize: 14,
    fontWeight: "600",
    color: colors.background,
  },
});
