import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  Keyboard,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  useWindowDimensions,
  View,
} from "react-native";
import type { NativeStackScreenProps } from "@react-navigation/native-stack";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import { useCoreQuery } from "@multica/core/provider";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import {
  issueLabelsOptions,
  labelListOptions,
  useAttachLabel,
  useDetachLabel,
} from "@multica/core/labels";
import { useProjectList } from "@multica/core/projects/hooks";
import {
  useChildIssueProgress,
  useChildIssues,
  useIssueDetail,
  useOptionalIssueDetail,
} from "@multica/core/issues/hooks";
import { ALL_STATUSES, PRIORITY_ORDER } from "@multica/core/issues/config";
import { useActorName } from "@multica/core/workspace/hooks";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { canAssignAgentToIssue, isAgentSelectable } from "@multica/core/permissions";
import type {
  Agent,
  IssueAssigneeType,
  IssuePriority,
  IssueStatus,
  Label,
  MemberWithUser,
  Project,
  SearchIssueResult,
} from "@multica/core/types";
import { Button, EmptyState, LoadingState, Screen } from "../../components/ui/primitives";
import { ScreenTitleBar } from "../../components/ui/screen-title-bar";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";
import {
  DatePickerModal,
  dateInputToRfc3339,
  formatDateLabel,
  normalizeDateInput,
  normalizeDueDateInput,
} from "./date-picker-modal";
import { formatAgentStatus, formatIssuePriority, formatIssueStatus } from "../../i18n/format";

type IssuePropertiesProps = NativeStackScreenProps<RootStackParamList, "IssueProperties">;
type Translate = (key: string, options?: Record<string, unknown>) => string;
type AssigneeOption =
  | { type: "member"; id: string; label: string; subtitle?: string }
  | { type: "agent"; id: string; label: string; subtitle?: string };

const SERVER_ISSUE_SEARCH_LIMIT = 20;
const SERVER_SEARCH_DEBOUNCE_MS = 150;

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

export function IssuePropertiesScreen({ navigation, route }: IssuePropertiesProps) {
  const { issueId } = route.params;
  const { t } = useTranslation();
  const insets = useSafeAreaInsets();
  const userId = useAuthStore((state) => state.user?.id);
  const { workspace } = useMobileWorkspace();
  const { getActorName } = useActorName();
  const { data: issue, isError, isLoading } = useIssueDetail(workspace.id, issueId);
  const { data: parentIssue, isLoading: parentIssueLoading } = useOptionalIssueDetail(
    workspace.id,
    issue?.parent_issue_id,
  );
  const { data: children = [] } = useChildIssues(workspace.id, issueId);
  const { data: childProgress } = useChildIssueProgress(workspace.id);
  const { data: members = [] } = useCoreQuery(memberListOptions(workspace.id));
  const { data: agents = [] } = useCoreQuery(agentListOptions(workspace.id));
  const { data: projects = [] } = useProjectList(workspace.id);
  const labelListScope = useMemo(
    () => ({ projectId: issue?.project_id ?? null }),
    [issue?.project_id],
  );
  const { data: allLabels = [] } = useCoreQuery(labelListOptions(workspace.id, labelListScope));
  const { data: attachedLabels = [] } = useCoreQuery(issueLabelsOptions(workspace.id, issueId));
  const updateIssue = useUpdateIssue();
  const attachLabel = useAttachLabel(issueId, labelListScope);
  const detachLabel = useDetachLabel(issueId);
  const [assigneePickerOpen, setAssigneePickerOpen] = useState(false);
  const [assigneeError, setAssigneeError] = useState<string | null>(null);
  const [projectPickerOpen, setProjectPickerOpen] = useState(false);
  const [projectError, setProjectError] = useState<string | null>(null);
  const [parentPickerOpen, setParentPickerOpen] = useState(false);
  const [parentError, setParentError] = useState<string | null>(null);
  const [startDatePickerOpen, setStartDatePickerOpen] = useState(false);
  const [dueDatePickerOpen, setDueDatePickerOpen] = useState(false);
  const [labelPickerOpen, setLabelPickerOpen] = useState(false);
  const [startDateError, setStartDateError] = useState<string | null>(null);
  const [dueDateError, setDueDateError] = useState<string | null>(null);
  const [labelError, setLabelError] = useState<string | null>(null);

  const currentMemberRole = useMemo(
    () => members.find((member) => member.user_id === userId)?.role ?? null,
    [members, userId],
  );

  const assigneeLabel = issue?.assignee_type && issue.assignee_id
    ? getActorName(issue.assignee_type, issue.assignee_id)
    : t("issues.unassigned");
  const currentProject = issue?.project_id
    ? projects.find((project) => project.id === issue.project_id)
    : undefined;
  const projectLabel = issue?.project_id
    ? currentProject ? formatProjectLabel(currentProject) : t("issues.unknown_project")
    : t("issues.no_project");
  const attachedLabelIds = useMemo(
    () => new Set(attachedLabels.map((label) => label.id)),
    [attachedLabels],
  );
  const labelSaving = attachLabel.isPending || detachLabel.isPending;

  const changeStatus = useCallback(async (status: IssueStatus) => {
    if (!issue || status === issue.status) return;
    await updateIssue.mutateAsync({ id: issue.id, status });
  }, [issue, updateIssue]);

  const changePriority = useCallback(async (priority: IssuePriority) => {
    if (!issue || priority === issue.priority) return;
    await updateIssue.mutateAsync({ id: issue.id, priority });
  }, [issue, updateIssue]);

  const changeAssignee = useCallback(async (
    assigneeType: IssueAssigneeType | null,
    assigneeId: string | null,
  ) => {
    if (!issue) return;
    if (assigneeType === issue.assignee_type && assigneeId === issue.assignee_id) {
      setAssigneePickerOpen(false);
      return;
    }
    setAssigneeError(null);
    try {
      await updateIssue.mutateAsync({
        id: issue.id,
        assignee_type: assigneeType,
        assignee_id: assigneeId,
      });
      setAssigneePickerOpen(false);
    } catch (err) {
      setAssigneeError(err instanceof Error ? err.message : t("issues.unable_to_save_issue"));
    }
  }, [issue, t, updateIssue]);

  const changeProject = useCallback(async (projectId: string | null) => {
    if (!issue) return;
    if (projectId === issue.project_id) {
      setProjectPickerOpen(false);
      return;
    }
    setProjectError(null);
    try {
      await updateIssue.mutateAsync({
        id: issue.id,
        project_id: projectId,
      });
      setProjectPickerOpen(false);
    } catch (err) {
      setProjectError(err instanceof Error ? err.message : t("issues.unable_to_save_issue"));
    }
  }, [issue, t, updateIssue]);

  const changeStartDate = useCallback(async (startDate: string | null) => {
    if (!issue || startDate === normalizeDateInput(issue.start_date)) return;
    const dueDate = normalizeDateInput(issue.due_date);
    if (startDate && dueDate && startDate > dueDate) {
      setStartDateError(t("issues.start_date_after_due_date"));
      return;
    }
    setStartDateError(null);
    try {
      await updateIssue.mutateAsync({
        id: issue.id,
        start_date: startDate ? dateInputToRfc3339(startDate) : null,
      });
    } catch (err) {
      setStartDateError(err instanceof Error ? err.message : t("issues.unable_to_save_issue"));
    }
  }, [issue, t, updateIssue]);

  const changeDueDate = useCallback(async (dueDate: string | null) => {
    if (!issue || dueDate === normalizeDueDateInput(issue.due_date)) return;
    const startDate = normalizeDateInput(issue.start_date);
    if (dueDate && startDate && dueDate < startDate) {
      setDueDateError(t("issues.due_date_before_start_date"));
      return;
    }
    setDueDateError(null);
    try {
      await updateIssue.mutateAsync({
        id: issue.id,
        due_date: dueDate ? dateInputToRfc3339(dueDate) : null,
      });
    } catch (err) {
      setDueDateError(err instanceof Error ? err.message : t("issues.unable_to_save_issue"));
    }
  }, [issue, t, updateIssue]);

  const toggleLabel = useCallback(async (labelId: string) => {
    setLabelError(null);
    try {
      if (attachedLabelIds.has(labelId)) {
        await detachLabel.mutateAsync(labelId);
      } else {
        await attachLabel.mutateAsync(labelId);
      }
    } catch (err) {
      setLabelError(err instanceof Error ? err.message : t("issues.unable_to_save_issue"));
    }
  }, [attachLabel, attachedLabelIds, detachLabel, t]);

  const changeParentIssue = useCallback(async (parentIssueId: string | null) => {
    if (!issue) return;
    if (parentIssueId === issue.parent_issue_id) {
      setParentPickerOpen(false);
      return;
    }
    setParentError(null);
    try {
      await updateIssue.mutateAsync({
        id: issue.id,
        parent_issue_id: parentIssueId,
      });
      setParentPickerOpen(false);
    } catch (err) {
      setParentError(err instanceof Error ? err.message : t("issues.unable_to_save_issue"));
    }
  }, [issue, t, updateIssue]);

  if (isLoading) return <LoadingState />;
  if (isError || !issue) return <EmptyState title={t("issues.unable_to_load_properties")} />;

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar onBack={() => navigation.goBack()} title={`${issue.identifier} ${t("issues.properties")}`} />
      <ScrollView
        contentContainerStyle={[
          styles.propertiesContent,
          { paddingBottom: Math.max(insets.bottom, spacing.lg) },
        ]}
      >
        <View style={styles.propertiesBlock}>
          <View style={styles.propertiesBlockHeader}>
            <Text style={styles.propertiesBlockTitle}>{t("issues.properties")}</Text>
            <Text style={styles.metadataSummary}>
              {formatIssueStatus(t, issue.status)} / {formatIssuePriority(t, issue.priority)}
            </Text>
          </View>
          <View style={styles.metadataBody}>
            <Property label={t("issues.status")}>
              <OptionRow>
                {ALL_STATUSES.map((status) => (
                  <Chip
                    active={issue.status === status}
                    key={status}
                    label={formatIssueStatus(t, status)}
                    onPress={() => void changeStatus(status)}
                  />
                ))}
              </OptionRow>
            </Property>
            <Property label={t("issues.priority")}>
              <OptionRow>
                {PRIORITY_ORDER.map((priority) => (
                  <Chip
                    active={issue.priority === priority}
                    key={priority}
                    label={formatIssuePriority(t, priority)}
                    onPress={() => void changePriority(priority)}
                  />
                ))}
              </OptionRow>
            </Property>
            <Property label={t("issues.assignee")}>
              <Pressable
                accessibilityRole="button"
                disabled={updateIssue.isPending}
                onPress={() => {
                  setAssigneeError(null);
                  setAssigneePickerOpen(true);
                }}
                style={({ pressed }) => [
                  styles.propertySelectTrigger,
                  pressed && styles.buttonPressed,
                  updateIssue.isPending && styles.disabledAction,
                ]}
              >
                <Text
                  numberOfLines={1}
                  style={[
                    styles.propertySelectText,
                    !issue.assignee_id && styles.propertySelectPlaceholder,
                  ]}
                >
                  {assigneeLabel}
                </Text>
                <Text style={styles.propertySelectMeta}>
                  {updateIssue.isPending ? t("issues.saving") : t("issues.select")}
                </Text>
              </Pressable>
            </Property>
            <Property label={t("issues.project")}>
              <Pressable
                accessibilityRole="button"
                disabled={updateIssue.isPending}
                onPress={() => {
                  setProjectError(null);
                  setProjectPickerOpen(true);
                }}
                style={({ pressed }) => [
                  styles.propertySelectTrigger,
                  pressed && styles.buttonPressed,
                  updateIssue.isPending && styles.disabledAction,
                ]}
              >
                <Text
                  numberOfLines={1}
                  style={[
                    styles.propertySelectText,
                    !issue.project_id && styles.propertySelectPlaceholder,
                  ]}
                >
                  {projectLabel}
                </Text>
                <Text style={styles.propertySelectMeta}>
                  {updateIssue.isPending ? t("issues.saving") : t("issues.select")}
                </Text>
              </Pressable>
            </Property>
            <Property label={t("issues.creator")}>
              <Text style={styles.value}>
                {getActorName(issue.creator_type, issue.creator_id)}
              </Text>
            </Property>
            <Property label={t("issues.start_date")}>
              <Pressable
                accessibilityRole="button"
                disabled={updateIssue.isPending}
                onPress={() => {
                  setStartDateError(null);
                  setStartDatePickerOpen(true);
                }}
                style={({ pressed }) => [
                  styles.dueDateTrigger,
                  pressed && styles.buttonPressed,
                  updateIssue.isPending && styles.disabledAction,
                ]}
              >
                <Text
                  numberOfLines={1}
                  style={[
                    styles.dueDateTriggerText,
                    !issue.start_date && styles.dueDatePlaceholder,
                  ]}
                >
                  {issue.start_date ? formatDateLabel(issue.start_date) : t("issues.no_start_date")}
                </Text>
                <Text style={styles.dueDateTriggerMeta}>
                  {updateIssue.isPending ? t("issues.saving") : t("issues.select")}
                </Text>
              </Pressable>
              {startDateError ? <Text style={styles.errorText}>{startDateError}</Text> : null}
            </Property>
            <Property label={t("issues.due_date")}>
              <Pressable
                accessibilityRole="button"
                disabled={updateIssue.isPending}
                onPress={() => {
                  setDueDateError(null);
                  setDueDatePickerOpen(true);
                }}
                style={({ pressed }) => [
                  styles.dueDateTrigger,
                  pressed && styles.buttonPressed,
                  updateIssue.isPending && styles.disabledAction,
                ]}
              >
                <Text
                  numberOfLines={1}
                  style={[
                    styles.dueDateTriggerText,
                    !issue.due_date && styles.dueDatePlaceholder,
                  ]}
                >
                  {issue.due_date ? formatDateLabel(issue.due_date) : t("issues.no_due_date")}
                </Text>
                <Text style={styles.dueDateTriggerMeta}>
                  {updateIssue.isPending ? t("issues.saving") : t("issues.select")}
                </Text>
              </Pressable>
              {dueDateError ? <Text style={styles.errorText}>{dueDateError}</Text> : null}
            </Property>
            <Property label={t("issues.labels")}>
              <Pressable
                accessibilityRole="button"
                disabled={labelSaving}
                onPress={() => {
                  setLabelError(null);
                  setLabelPickerOpen(true);
                }}
                style={({ pressed }) => [
                  styles.labelTrigger,
                  pressed && styles.buttonPressed,
                  labelSaving && styles.disabledAction,
                ]}
              >
                {attachedLabels.length > 0 ? (
                  <View style={styles.labelChipList}>
                    {attachedLabels.map((label) => (
                      <IssueLabelChip key={label.id} label={label} />
                    ))}
                  </View>
                ) : (
                  <Text style={[styles.propertySelectText, styles.propertySelectPlaceholder]}>
                    {t("issues.no_labels")}
                  </Text>
                )}
                <Text style={styles.propertySelectMeta}>
                  {labelSaving ? t("issues.saving") : t("issues.select")}
                </Text>
              </Pressable>
              {labelError ? <Text style={styles.errorText}>{labelError}</Text> : null}
            </Property>
          </View>
        </View>

        <View style={styles.propertiesBlock}>
          <View style={styles.propertiesBlockHeader}>
            <Text style={styles.propertiesBlockTitle}>{t("issues.parent_issue")}</Text>
          </View>
          <View style={styles.relationList}>
            <Pressable
              accessibilityRole="button"
              disabled={updateIssue.isPending}
              onPress={() => {
                setParentError(null);
                setParentPickerOpen(true);
              }}
              style={({ pressed }) => [
                styles.childRow,
                pressed && styles.buttonPressed,
                updateIssue.isPending && styles.disabledAction,
              ]}
            >
              {parentIssue ? (
                <>
                  <Text style={styles.childIdentifier}>{parentIssue.identifier}</Text>
                  <Text style={styles.childTitle}>{parentIssue.title}</Text>
                </>
              ) : (
                <Text
                  numberOfLines={1}
                  style={[
                    styles.childTitle,
                    !issue.parent_issue_id && styles.propertySelectPlaceholder,
                  ]}
                >
                  {issue.parent_issue_id
                    ? parentIssueLoading
                      ? t("issues.loading_parent_issue")
                      : t("issues.unable_to_load_parent_issue")
                    : t("issues.no_parent_issue")}
                </Text>
              )}
              <Text style={styles.attachmentMeta}>
                {updateIssue.isPending ? t("issues.saving") : t("issues.select")}
              </Text>
            </Pressable>
            {issue.parent_issue_id ? (
              <Button
                disabled={!parentIssue}
                onPress={() => navigation.push("IssueDetail", { issueId: issue.parent_issue_id! })}
                variant="secondary"
              >
                {t("common.open")}
              </Button>
            ) : null}
            {parentError ? <Text style={styles.errorText}>{parentError}</Text> : null}
          </View>
        </View>

        {children.length > 0 ? (
          <View style={styles.propertiesBlock}>
            <View style={styles.propertiesBlockHeader}>
              <Text style={styles.propertiesBlockTitle}>{t("issues.child_issues")}</Text>
              <Text style={styles.stickySectionCount}>{children.length}</Text>
            </View>
            <View style={styles.relationList}>
              {children.map((child) => (
                <Pressable
                  key={child.id}
                  onPress={() => navigation.push("IssueDetail", { issueId: child.id })}
                  style={({ pressed }) => [
                    styles.childRow,
                    pressed && styles.buttonPressed,
                  ]}
                >
                  <Text style={styles.childIdentifier}>{child.identifier}</Text>
                  <Text style={styles.childTitle}>{child.title}</Text>
                  {childProgress?.get(child.id) ? (
                    <Text style={styles.attachmentMeta}>
                      {t("issues.child_progress", {
                        done: childProgress.get(child.id)?.done,
                        total: childProgress.get(child.id)?.total,
                      })}
                    </Text>
                  ) : null}
                </Pressable>
              ))}
            </View>
          </View>
        ) : null}
      </ScrollView>
      <DatePickerModal
        disabledDateMessage={
          issue.due_date
            ? t("issues.start_date_after_due_date")
            : undefined
        }
        maxDate={issue.due_date}
        onChange={(startDate) => void changeStartDate(startDate)}
        onClose={() => setStartDatePickerOpen(false)}
        open={startDatePickerOpen}
        title={t("issues.start_date")}
        value={issue.start_date}
      />
      <DatePickerModal
        disabledDateMessage={
          issue.start_date
            ? t("issues.due_date_before_start_date")
            : undefined
        }
        minDate={issue.start_date}
        onChange={(dueDate) => void changeDueDate(dueDate)}
        onClose={() => setDueDatePickerOpen(false)}
        open={dueDatePickerOpen}
        title={t("issues.due_date")}
        value={issue.due_date}
      />
      <LabelPickerSheet
        allLabels={allLabels}
        attachedLabelIds={attachedLabelIds}
        bottomInset={insets.bottom}
        error={labelError}
        onClose={() => {
          setLabelPickerOpen(false);
          setLabelError(null);
        }}
        onToggle={(labelId) => void toggleLabel(labelId)}
        open={labelPickerOpen}
        saving={labelSaving}
      />
      <AssigneePickerSheet
        agents={agents}
        bottomInset={insets.bottom}
        currentAssigneeId={issue.assignee_id}
        currentAssigneeType={issue.assignee_type}
        currentMemberRole={currentMemberRole}
        error={assigneeError}
        members={members}
        onChange={(assigneeType, assigneeId) => void changeAssignee(assigneeType, assigneeId)}
        onClose={() => {
          setAssigneePickerOpen(false);
          setAssigneeError(null);
        }}
        open={assigneePickerOpen}
        saving={updateIssue.isPending}
        userId={userId ?? null}
      />
      <ProjectPickerSheet
        bottomInset={insets.bottom}
        currentProjectId={issue.project_id}
        error={projectError}
        onChange={(projectId) => void changeProject(projectId)}
        onClose={() => {
          setProjectPickerOpen(false);
          setProjectError(null);
        }}
        open={projectPickerOpen}
        projects={projects}
        saving={updateIssue.isPending}
      />
      <ParentIssuePickerSheet
        bottomInset={insets.bottom}
        childrenIds={children.map((child) => child.id)}
        currentParentIssueId={issue.parent_issue_id}
        currentIssueId={issue.id}
        error={parentError}
        onChange={(parentIssueId) => void changeParentIssue(parentIssueId)}
        onClose={() => {
          setParentPickerOpen(false);
          setParentError(null);
        }}
        open={parentPickerOpen}
        saving={updateIssue.isPending}
      />
    </Screen>
  );
}

function LabelPickerSheet({
  allLabels,
  attachedLabelIds,
  bottomInset,
  error,
  onClose,
  onToggle,
  open,
  saving,
}: {
  allLabels: Label[];
  attachedLabelIds: Set<string>;
  bottomInset: number;
  error: string | null;
  onClose: () => void;
  onToggle: (labelId: string) => void;
  open: boolean;
  saving: boolean;
}) {
  const { t } = useTranslation();
  const [query, setQuery] = useState("");
  const keyboardHeight = useKeyboardHeight(open);
  const { height: windowHeight } = useWindowDimensions();
  const sheetMaxHeight = Math.max(0, windowHeight - keyboardHeight - spacing.xl);
  const sheetBottomPadding = keyboardHeight > 0 ? spacing.md : Math.max(bottomInset, spacing.md);

  useEffect(() => {
    if (!open) setQuery("");
  }, [open]);

  const normalizedQuery = normalizeSearch(query);
  const filteredLabels = useMemo(
    () => allLabels.filter((label) => {
      if (!normalizedQuery) return true;
      return normalizeSearch(label.name).includes(normalizedQuery);
    }),
    [allLabels, normalizedQuery],
  );

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
            <Text style={styles.sheetTitle}>{t("issues.labels")}</Text>
            <Button disabled={saving} onPress={onClose} variant="ghost">
              {t("common.close")}
            </Button>
          </View>
          <TextInput
            autoCapitalize="none"
            autoCorrect={false}
            editable={!saving}
            onChangeText={setQuery}
            placeholder={t("issues.search_labels")}
            placeholderTextColor={colors.mutedForeground}
            style={styles.assigneeSearchInput}
            value={query}
          />
          {error ? <Text style={styles.errorText}>{error}</Text> : null}
          <ScrollView
            keyboardShouldPersistTaps="handled"
            style={styles.assigneePickerList}
          >
            {filteredLabels.length > 0 ? (
              <View style={styles.assigneeSection}>
                {filteredLabels.map((label) => (
                  <LabelOptionRow
                    active={attachedLabelIds.has(label.id)}
                    disabled={saving}
                    key={label.id}
                    label={label}
                    onPress={() => onToggle(label.id)}
                  />
                ))}
              </View>
            ) : (
              <Text style={styles.assigneeEmptyText}>
                {allLabels.length === 0 ? t("issues.no_labels") : t("common.no_results")}
              </Text>
            )}
          </ScrollView>
        </View>
      </View>
    </Modal>
  );
}

function LabelOptionRow({
  active,
  disabled,
  label,
  onPress,
}: {
  active: boolean;
  disabled: boolean;
  label: Label;
  onPress: () => void;
}) {
  const { t } = useTranslation();

  return (
    <Pressable
      accessibilityRole="button"
      disabled={disabled}
      onPress={onPress}
      style={({ pressed }) => [
        styles.assigneeOption,
        active && styles.assigneeOptionActive,
        pressed && styles.buttonPressed,
        disabled && styles.disabledAction,
      ]}
    >
      <View style={[styles.labelOptionSwatch, { backgroundColor: label.color }]} />
      <Text
        numberOfLines={1}
        style={[styles.assigneeOptionLabel, styles.labelOptionName, active && styles.assigneeOptionLabelActive]}
      >
        {label.name}
      </Text>
      {active ? <Text style={styles.labelOptionSelected}>{t("common.selected")}</Text> : null}
    </Pressable>
  );
}

function IssueLabelChip({ label }: { label: Label }) {
  return (
    <View style={styles.issueLabelChip}>
      <View style={[styles.issueLabelChipDot, { backgroundColor: label.color }]} />
      <Text numberOfLines={1} style={styles.issueLabelChipText}>
        {label.name}
      </Text>
    </View>
  );
}

function AssigneePickerSheet({
  agents,
  bottomInset,
  currentAssigneeId,
  currentAssigneeType,
  currentMemberRole,
  error,
  members,
  onChange,
  onClose,
  open,
  saving,
  userId,
}: {
  agents: Agent[];
  bottomInset: number;
  currentAssigneeId: string | null;
  currentAssigneeType: IssueAssigneeType | null;
  currentMemberRole: MemberWithUser["role"] | null;
  error: string | null;
  members: MemberWithUser[];
  onChange: (assigneeType: IssueAssigneeType | null, assigneeId: string | null) => void;
  onClose: () => void;
  open: boolean;
  saving: boolean;
  userId: string | null;
}) {
  const { t } = useTranslation();
  const [query, setQuery] = useState("");
  const keyboardHeight = useKeyboardHeight(open);
  const { height: windowHeight } = useWindowDimensions();
  const sheetMaxHeight = Math.max(0, windowHeight - keyboardHeight - spacing.xl);
  const sheetBottomPadding = keyboardHeight > 0 ? spacing.md : Math.max(bottomInset, spacing.md);

  useEffect(() => {
    if (!open) setQuery("");
  }, [open]);

  const normalizedQuery = query.trim().toLowerCase();
  const memberOptions = useMemo<AssigneeOption[]>(
    () => members
      .filter((member) => {
        const haystack = `${member.name} ${member.email}`.toLowerCase();
        return !normalizedQuery || haystack.includes(normalizedQuery);
      })
      .map((member) => ({
        type: "member",
        id: member.user_id,
        label: member.name,
        subtitle: member.email,
      })),
    [members, normalizedQuery],
  );
  const agentOptions = useMemo<AssigneeOption[]>(
    () => agents
      .filter((agent) => isAgentSelectable(agent, userId))
      .filter((agent) => canAssignAgentToIssue(agent, {
        userId,
        role: currentMemberRole,
      }).allowed)
      .filter((agent) => {
        const haystack = agent.name.toLowerCase();
        return !normalizedQuery || haystack.includes(normalizedQuery);
      })
      .map((agent) => ({
        type: "agent",
        id: agent.id,
        label: agent.name,
        subtitle: formatAgentStatus(t, agent.status),
      })),
    [agents, currentMemberRole, normalizedQuery, t, userId],
  );
  const hasResults = memberOptions.length > 0 || agentOptions.length > 0;

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
            <Text style={styles.sheetTitle}>{t("issues.assignee")}</Text>
            <Button disabled={saving} onPress={onClose} variant="ghost">
              {t("common.close")}
            </Button>
          </View>
          <TextInput
            autoCapitalize="none"
            autoCorrect={false}
            editable={!saving}
            onChangeText={setQuery}
            placeholder={t("issues.search_assignees")}
            placeholderTextColor={colors.mutedForeground}
            style={styles.assigneeSearchInput}
            value={query}
          />
          {error ? <Text style={styles.errorText}>{error}</Text> : null}
          <ScrollView
            keyboardShouldPersistTaps="handled"
            style={styles.assigneePickerList}
          >
            <AssigneeOptionRow
              active={!currentAssigneeType && !currentAssigneeId}
              disabled={saving}
              label={t("issues.unassigned")}
              onPress={() => onChange(null, null)}
            />
            {memberOptions.length > 0 ? (
              <AssigneeOptionSection
                currentAssigneeId={currentAssigneeId}
                currentAssigneeType={currentAssigneeType}
                disabled={saving}
                label={t("issues.members")}
                onChange={onChange}
                options={memberOptions}
              />
            ) : null}
            {agentOptions.length > 0 ? (
              <AssigneeOptionSection
                currentAssigneeId={currentAssigneeId}
                currentAssigneeType={currentAssigneeType}
                disabled={saving}
                label={t("issues.agents")}
                onChange={onChange}
                options={agentOptions}
              />
            ) : null}
            {!hasResults ? (
              <Text style={styles.assigneeEmptyText}>{t("common.no_results")}</Text>
            ) : null}
          </ScrollView>
        </View>
      </View>
    </Modal>
  );
}

function ProjectPickerSheet({
  bottomInset,
  currentProjectId,
  error,
  onChange,
  onClose,
  open,
  projects,
  saving,
}: {
  bottomInset: number;
  currentProjectId: string | null;
  error: string | null;
  onChange: (projectId: string | null) => void;
  onClose: () => void;
  open: boolean;
  projects: Project[];
  saving: boolean;
}) {
  const { t } = useTranslation();
  const [query, setQuery] = useState("");
  const keyboardHeight = useKeyboardHeight(open);
  const { height: windowHeight } = useWindowDimensions();
  const sheetMaxHeight = Math.max(0, windowHeight - keyboardHeight - spacing.xl);
  const sheetBottomPadding = keyboardHeight > 0 ? spacing.md : Math.max(bottomInset, spacing.md);

  useEffect(() => {
    if (!open) setQuery("");
  }, [open]);

  const normalizedQuery = normalizeSearch(query);
  const projectOptions = useMemo(
    () => projects.filter((project) => {
      if (!normalizedQuery) return true;
      return normalizeSearch(formatProjectLabel(project)).includes(normalizedQuery);
    }),
    [normalizedQuery, projects],
  );
  const showNoProject = !normalizedQuery || normalizeSearch(t("issues.no_project")).includes(normalizedQuery);
  const hasResults = showNoProject || projectOptions.length > 0;

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
            <Text style={styles.sheetTitle}>{t("issues.project")}</Text>
            <Button disabled={saving} onPress={onClose} variant="ghost">
              {t("common.close")}
            </Button>
          </View>
          <TextInput
            autoCapitalize="none"
            autoCorrect={false}
            editable={!saving}
            onChangeText={setQuery}
            placeholder={t("issues.search_projects")}
            placeholderTextColor={colors.mutedForeground}
            style={styles.assigneeSearchInput}
            value={query}
          />
          {error ? <Text style={styles.errorText}>{error}</Text> : null}
          <ScrollView
            keyboardShouldPersistTaps="handled"
            style={styles.assigneePickerList}
          >
            {showNoProject ? (
              <AssigneeOptionRow
                active={!currentProjectId}
                disabled={saving}
                label={t("issues.no_project")}
                onPress={() => onChange(null)}
              />
            ) : null}
            {projectOptions.length > 0 ? (
              <View style={styles.assigneeSection}>
                <Text style={styles.assigneeSectionTitle}>{t("issues.project")}</Text>
                {projectOptions.map((project) => (
                  <AssigneeOptionRow
                    active={currentProjectId === project.id}
                    disabled={saving}
                    key={project.id}
                    label={formatProjectLabel(project)}
                    onPress={() => onChange(project.id)}
                  />
                ))}
              </View>
            ) : null}
            {!hasResults ? (
              <Text style={styles.assigneeEmptyText}>{t("common.no_results")}</Text>
            ) : null}
          </ScrollView>
        </View>
      </View>
    </Modal>
  );
}

function ParentIssuePickerSheet({
  bottomInset,
  childrenIds,
  currentIssueId,
  currentParentIssueId,
  error,
  onChange,
  onClose,
  open,
  saving,
}: {
  bottomInset: number;
  childrenIds: string[];
  currentIssueId: string;
  currentParentIssueId: string | null;
  error: string | null;
  onChange: (parentIssueId: string | null) => void;
  onClose: () => void;
  open: boolean;
  saving: boolean;
}) {
  const { t } = useTranslation();
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchIssueResult[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [hasSearched, setHasSearched] = useState(false);
  const [isError, setIsError] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const keyboardHeight = useKeyboardHeight(open);
  const { height: windowHeight } = useWindowDimensions();
  const sheetMaxHeight = Math.max(0, windowHeight - keyboardHeight - spacing.xl);
  const sheetBottomPadding = keyboardHeight > 0 ? spacing.md : Math.max(bottomInset, spacing.md);
  const excludedIssueIds = useMemo(
    () => new Set([currentIssueId, ...childrenIds]),
    [childrenIds, currentIssueId],
  );

  useEffect(() => {
    if (open) return undefined;
    setQuery("");
    setResults([]);
    setIsLoading(false);
    setHasSearched(false);
    setIsError(false);
    if (debounceRef.current) clearTimeout(debounceRef.current);
    abortRef.current?.abort();
    return undefined;
  }, [open]);

  useEffect(() => () => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    abortRef.current?.abort();
  }, []);

  const runSearch = useCallback((value: string) => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    abortRef.current?.abort();

    const trimmed = value.trim();
    if (!trimmed) {
      setResults([]);
      setIsLoading(false);
      setHasSearched(false);
      setIsError(false);
      return;
    }

    setIsLoading(true);
    setIsError(false);
    debounceRef.current = setTimeout(() => {
      const controller = new AbortController();
      abortRef.current = controller;

      void (async () => {
        try {
          const res = await api.searchIssues({
            q: trimmed,
            limit: SERVER_ISSUE_SEARCH_LIMIT,
            include_closed: true,
            signal: controller.signal,
          });
          if (!controller.signal.aborted) {
            setResults(res.issues);
            setHasSearched(true);
          }
        } catch {
          if (!controller.signal.aborted) {
            setResults([]);
            setIsError(true);
            setHasSearched(true);
          }
        } finally {
          if (!controller.signal.aborted) {
            setIsLoading(false);
          }
        }
      })();
    }, SERVER_SEARCH_DEBOUNCE_MS);
  }, []);

  const handleChangeText = useCallback((value: string) => {
    setQuery(value);
    runSearch(value);
  }, [runSearch]);

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
            <Text style={styles.sheetTitle}>{t("issues.parent_issue")}</Text>
            <Button disabled={saving} onPress={onClose} variant="ghost">
              {t("common.close")}
            </Button>
          </View>
          <TextInput
            autoCapitalize="none"
            autoCorrect={false}
            editable={!saving}
            onChangeText={handleChangeText}
            placeholder={t("issues.search_parent_issues")}
            placeholderTextColor={colors.mutedForeground}
            style={styles.assigneeSearchInput}
            value={query}
          />
          {error ? <Text style={styles.errorText}>{error}</Text> : null}
          {isError ? <Text style={styles.errorText}>{t("issues.unable_to_search")}</Text> : null}
          <ScrollView
            keyboardShouldPersistTaps="handled"
            style={styles.assigneePickerList}
          >
            <AssigneeOptionRow
              active={!currentParentIssueId}
              disabled={saving || !currentParentIssueId}
              label={t("issues.no_parent_issue")}
              onPress={() => onChange(null)}
            />
            {!hasSearched && !query.trim() ? (
              <Text style={styles.assigneeEmptyText}>{t("issues.search_parent_empty")}</Text>
            ) : null}
            {results.length > 0 ? (
              <View style={styles.assigneeSection}>
                <Text style={styles.assigneeSectionTitle}>{t("issues.issue")}</Text>
                {results.map((result) => {
                  const excluded = excludedIssueIds.has(result.id);
                  return (
                    <ParentIssueOptionRow
                      active={currentParentIssueId === result.id}
                      disabled={saving || excluded}
                      excluded={excluded}
                      issue={result}
                      key={result.id}
                      onPress={() => onChange(result.id)}
                    />
                  );
                })}
              </View>
            ) : null}
            {isLoading ? (
              <View style={styles.parentSearchLoading}>
                <ActivityIndicator color={colors.mutedForeground} size="small" />
                <Text style={styles.attachmentMeta}>{t("issues.searching")}</Text>
              </View>
            ) : null}
            {!isError && !isLoading && hasSearched && results.length === 0 ? (
              <Text style={styles.assigneeEmptyText}>{t("issues.no_matching")}</Text>
            ) : null}
          </ScrollView>
        </View>
      </View>
    </Modal>
  );
}

function ParentIssueOptionRow({
  active,
  disabled,
  excluded,
  issue,
  onPress,
}: {
  active: boolean;
  disabled: boolean;
  excluded: boolean;
  issue: SearchIssueResult;
  onPress: () => void;
}) {
  const { t } = useTranslation();
  const statusColor = getIssueStatusColor(issue.status);

  return (
    <Pressable
      accessibilityRole="button"
      disabled={disabled}
      onPress={onPress}
      style={({ pressed }) => [
        styles.assigneeOption,
        active && styles.assigneeOptionActive,
        pressed && styles.buttonPressed,
        disabled && styles.disabledAction,
      ]}
    >
      <View style={styles.assigneeOptionTextGroup}>
        <View style={styles.parentIssueOptionHeader}>
          <View style={[styles.parentIssueStatusDot, { backgroundColor: statusColor }]} />
          <Text style={styles.childIdentifier}>{issue.identifier}</Text>
          <Text
            numberOfLines={1}
            style={[styles.assigneeOptionLabel, active && styles.assigneeOptionLabelActive]}
          >
            {issue.title}
          </Text>
        </View>
        <Text style={[styles.assigneeOptionSubtitle, { color: statusColor }]}>
          {excludedParentIssueReason(excluded, active, t) ?? formatIssueStatus(t, issue.status)}
        </Text>
      </View>
    </Pressable>
  );
}

function AssigneeOptionSection({
  currentAssigneeId,
  currentAssigneeType,
  disabled,
  label,
  onChange,
  options,
}: {
  currentAssigneeId: string | null;
  currentAssigneeType: IssueAssigneeType | null;
  disabled: boolean;
  label: string;
  onChange: (assigneeType: IssueAssigneeType, assigneeId: string) => void;
  options: AssigneeOption[];
}) {
  return (
    <View style={styles.assigneeSection}>
      <Text style={styles.assigneeSectionTitle}>{label}</Text>
      {options.map((option) => (
        <AssigneeOptionRow
          active={currentAssigneeType === option.type && currentAssigneeId === option.id}
          disabled={disabled}
          key={`${option.type}:${option.id}`}
          label={option.label}
          onPress={() => onChange(option.type, option.id)}
          subtitle={option.subtitle}
        />
      ))}
    </View>
  );
}

function AssigneeOptionRow({
  active,
  disabled,
  label,
  onPress,
  subtitle,
}: {
  active: boolean;
  disabled: boolean;
  label: string;
  onPress: () => void;
  subtitle?: string;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      disabled={disabled}
      onPress={onPress}
      style={({ pressed }) => [
        styles.assigneeOption,
        active && styles.assigneeOptionActive,
        pressed && styles.buttonPressed,
        disabled && styles.disabledAction,
      ]}
    >
      <View style={styles.assigneeOptionTextGroup}>
        <Text
          numberOfLines={1}
          style={[styles.assigneeOptionLabel, active && styles.assigneeOptionLabelActive]}
        >
          {label}
        </Text>
        {subtitle ? (
          <Text numberOfLines={1} style={styles.assigneeOptionSubtitle}>
            {subtitle}
          </Text>
        ) : null}
      </View>
    </Pressable>
  );
}

function formatProjectLabel(project: Pick<Project, "icon" | "title">) {
  return `${project.icon ?? ""}${project.icon ? " " : ""}${project.title}`;
}

function normalizeSearch(value: string) {
  return value.trim().toLowerCase().replace(/\s+/g, "");
}

function getIssueStatusColor(status: IssueStatus) {
  switch (status) {
    case "in_progress":
      return colors.warning;
    case "in_review":
      return colors.success;
    case "done":
      return colors.info;
    case "blocked":
      return colors.destructive;
    default:
      return colors.mutedForeground;
  }
}

function excludedParentIssueReason(excluded: boolean, active: boolean, t: Translate) {
  if (!excluded || active) return null;
  return t("issues.parent_issue_not_selectable");
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

const styles = StyleSheet.create({
  content: {
    gap: spacing.xl,
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
  section: {
    gap: spacing.sm,
  },
  sectionSeparated: {
    borderTopColor: colors.border,
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingTop: spacing.lg,
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
  relationList: {
    gap: spacing.sm,
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
    marginTop: -spacing.lg,
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
    marginTop: -spacing.lg,
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
