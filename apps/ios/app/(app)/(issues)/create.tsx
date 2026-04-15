import { useState, useCallback, useMemo } from "react";
import {
  View,
  Text,
  TextInput,
  TouchableOpacity,
  ScrollView,
  Alert,
  StyleSheet,
} from "react-native";
import { useRouter, Stack } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCreateIssue } from "@multica/core/issues/mutations";
import { STATUS_CONFIG } from "@multica/core/issues/config/status";
import { PRIORITY_CONFIG } from "@multica/core/issues/config/priority";
import { STATUS_ORDER } from "@multica/core/issues/config/status";
import { PRIORITY_ORDER } from "@multica/core/issues/config/priority";
import type {
  IssueAssigneeType,
  IssuePriority,
  IssueStatus,
} from "@multica/core/types";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { StatusIcon } from "@/components/StatusIcon";
import { PriorityIcon } from "@/components/PriorityIcon";
import { Avatar } from "@/components/Avatar";
import { MentionTextInput } from "@/components/MentionTextInput";
import { createActorResolver } from "@/lib/actors";
import { colors } from "@/lib/theme";

export default function CreateIssueScreen() {
  const router = useRouter();
  const wsId = useWorkspaceId();
  const createIssue = useCreateIssue();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));

  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [status, setStatus] = useState<IssueStatus>("todo");
  const [priority, setPriority] = useState<IssuePriority>("none");
  const [assigneeType, setAssigneeType] = useState<IssueAssigneeType | null>(
    null,
  );
  const [assigneeId, setAssigneeId] = useState<string | null>(null);
  const [projectId, setProjectId] = useState<string | null>(null);
  const [showStatusPicker, setShowStatusPicker] = useState(false);
  const [showPriorityPicker, setShowPriorityPicker] = useState(false);
  const [showAssigneePicker, setShowAssigneePicker] = useState(false);
  const [showProjectPicker, setShowProjectPicker] = useState(false);

  const activeAgents = useMemo(
    () => agents.filter((agent) => !agent.archived_at),
    [agents],
  );
  const resolveActor = useMemo(
    () => createActorResolver(members, agents),
    [members, agents],
  );
  const selectedAssignee = resolveActor(assigneeType, assigneeId);
  const selectedProject = projects.find((project) => project.id === projectId);

  const handleCreate = useCallback(async () => {
    const trimmedTitle = title.trim();
    if (!trimmedTitle) {
      Alert.alert("Error", "Title is required");
      return;
    }

    try {
      await createIssue.mutateAsync({
        title: trimmedTitle,
        description: description.trim() || undefined,
        status,
        priority,
        ...(assigneeType && assigneeId
          ? { assignee_type: assigneeType, assignee_id: assigneeId }
          : {}),
        ...(projectId ? { project_id: projectId } : {}),
      });
      router.back();
    } catch (err: unknown) {
      const message =
        err instanceof Error ? err.message : "Failed to create issue";
      Alert.alert("Error", message);
    }
  }, [
    title,
    description,
    status,
    priority,
    assigneeType,
    assigneeId,
    projectId,
    createIssue,
    router,
  ]);

  return (
    <>
      <Stack.Screen
        options={{
          title: "Create",
          headerLeft: () => (
            <TouchableOpacity onPress={() => router.back()}>
              <Text style={styles.cancelText}>Cancel</Text>
            </TouchableOpacity>
          ),
          headerRight: () => (
            <TouchableOpacity
              onPress={handleCreate}
              disabled={!title.trim() || createIssue.isPending}
            >
              <Text
                style={[
                  styles.createText,
                  (!title.trim() || createIssue.isPending) &&
                    styles.createTextDisabled,
                ]}
              >
                {createIssue.isPending ? "Creating..." : "Create"}
              </Text>
            </TouchableOpacity>
          ),
        }}
      />
      <ScrollView
        style={styles.container}
        contentContainerStyle={styles.content}
        keyboardShouldPersistTaps="handled"
      >
        <TextInput
          style={styles.titleInput}
          value={title}
          onChangeText={setTitle}
          placeholder="Issue title"
          placeholderTextColor={colors.mutedForeground}
          autoFocus
          returnKeyType="next"
        />

        <MentionTextInput
          style={styles.descriptionInput}
          value={description}
          onChangeText={setDescription}
          placeholder="Add description... type @ to mention people, agents, or issues"
          placeholderTextColor={colors.mutedForeground}
          multiline
          textAlignVertical="top"
        />

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
              <StatusIcon status={status} size={14} />
              <Text style={styles.metaValueText}>
                {STATUS_CONFIG[status].label}
              </Text>
            </View>
          </TouchableOpacity>

          {showStatusPicker && (
            <View style={styles.picker}>
              {STATUS_ORDER.filter((s) => s !== "cancelled").map((s) => (
                <TouchableOpacity
                  key={s}
                  style={[
                    styles.pickerItem,
                    s === status && styles.pickerItemActive,
                  ]}
                  onPress={() => {
                    setStatus(s);
                    setShowStatusPicker(false);
                  }}
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
              <PriorityIcon priority={priority} size={14} />
              <Text style={styles.metaValueText}>
                {PRIORITY_CONFIG[priority].label}
              </Text>
            </View>
          </TouchableOpacity>

          {showPriorityPicker && (
            <View style={styles.picker}>
              {PRIORITY_ORDER.map((p) => (
                <TouchableOpacity
                  key={p}
                  style={[
                    styles.pickerItem,
                    p === priority && styles.pickerItemActive,
                  ]}
                  onPress={() => {
                    setPriority(p);
                    setShowPriorityPicker(false);
                  }}
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
              {selectedAssignee ? (
                <>
                  <Avatar
                    name={selectedAssignee.name}
                    avatarUrl={selectedAssignee.avatarUrl}
                    size={20}
                    isAgent={selectedAssignee.isAgent}
                  />
                  <Text style={styles.metaValueText}>
                    {selectedAssignee.name}
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
                  !assigneeId && styles.pickerItemActive,
                ]}
                onPress={() => {
                  setAssigneeType(null);
                  setAssigneeId(null);
                  setShowAssigneePicker(false);
                }}
              >
                <Text style={styles.pickerItemText}>Unassigned</Text>
              </TouchableOpacity>
              {members.map((member) => (
                <TouchableOpacity
                  key={member.user_id}
                  style={[
                    styles.pickerItem,
                    assigneeType === "member" &&
                      assigneeId === member.user_id &&
                      styles.pickerItemActive,
                  ]}
                  onPress={() => {
                    setAssigneeType("member");
                    setAssigneeId(member.user_id);
                    setShowAssigneePicker(false);
                  }}
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
                    assigneeType === "agent" &&
                      assigneeId === agent.id &&
                      styles.pickerItemActive,
                  ]}
                  onPress={() => {
                    setAssigneeType("agent");
                    setAssigneeId(agent.id);
                    setShowAssigneePicker(false);
                  }}
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
                style={
                  selectedProject ? styles.metaValueText : styles.metaValueMuted
                }
                numberOfLines={1}
              >
                {selectedProject?.title ?? "No project"}
              </Text>
            </View>
          </TouchableOpacity>

          {showProjectPicker && (
            <View style={styles.picker}>
              <TouchableOpacity
                style={[
                  styles.pickerItem,
                  !projectId && styles.pickerItemActive,
                ]}
                onPress={() => {
                  setProjectId(null);
                  setShowProjectPicker(false);
                }}
              >
                <Text style={styles.pickerItemText}>No project</Text>
              </TouchableOpacity>
              {projects.map((project) => (
                <TouchableOpacity
                  key={project.id}
                  style={[
                    styles.pickerItem,
                    projectId === project.id && styles.pickerItemActive,
                  ]}
                  onPress={() => {
                    setProjectId(project.id);
                    setShowProjectPicker(false);
                  }}
                >
                  <Text style={styles.pickerItemText}>{project.title}</Text>
                </TouchableOpacity>
              ))}
            </View>
          )}
        </View>
      </ScrollView>
    </>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.background,
  },
  content: {
    paddingBottom: 40,
  },
  titleInput: {
    fontSize: 20,
    fontWeight: "600",
    color: colors.foreground,
    paddingHorizontal: 16,
    paddingVertical: 16,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.border,
  },
  descriptionInput: {
    fontSize: 15,
    color: colors.foreground,
    paddingHorizontal: 16,
    paddingVertical: 12,
    minHeight: 100,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.border,
    lineHeight: 22,
  },
  metaSection: {
    paddingHorizontal: 16,
    paddingTop: 8,
  },
  metaRow: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingVertical: 12,
  },
  metaLabel: {
    fontSize: 14,
    color: colors.mutedForeground,
    width: 80,
  },
  metaValue: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    flex: 1,
    justifyContent: "flex-end",
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
  cancelText: {
    fontSize: 16,
    color: colors.foreground,
  },
  createText: {
    fontSize: 16,
    fontWeight: "600",
    color: colors.brand,
  },
  createTextDisabled: {
    opacity: 0.4,
  },
});
