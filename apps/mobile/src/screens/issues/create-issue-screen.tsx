import { useEffect, useMemo, useState } from "react";
import {
  KeyboardAvoidingView,
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
  type KeyboardEvent,
} from "react-native";
import * as ImagePicker from "expo-image-picker";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import type { NativeStackScreenProps } from "@react-navigation/native-stack";
import { useTranslation } from "react-i18next";
import { api } from "@multica/core/api";
import { BOARD_STATUSES, PRIORITY_ORDER } from "@multica/core/issues/config";
import { useCreateIssue } from "@multica/core/issues/mutations";
import { useProjectList } from "@multica/core/projects/hooks";
import { useWorkspaceMentionTargets } from "@multica/core/workspace/hooks";
import type { IssueAssigneeType, IssuePriority, IssueStatus } from "@multica/core/types";
import { Button, Screen } from "../../components/ui/primitives";
import { ScreenTitleBar } from "../../components/ui/screen-title-bar";
import { formatIssuePriority, formatIssueStatus } from "../../i18n/format";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { uploadMobileAsset, type MobileUploadAsset } from "../../platform/upload";
import { colors, radii, spacing } from "../../theme/tokens";
import {
  createDraftCommentAttachment,
  type DraftCommentAttachment,
} from "./comment-attachment-drafts";
import {
  DatePickerModal,
  dateInputToRfc3339,
  formatDueDateLabel,
  isValidDateInput,
} from "./date-picker-modal";

type Props = NativeStackScreenProps<RootStackParamList, "CreateIssue">;
type DocumentPickerModule = typeof import("expo-document-picker");
declare const require: (moduleName: string) => unknown;

export function CreateIssueScreen({ navigation, route }: Props) {
  const { t } = useTranslation();
  const insets = useSafeAreaInsets();
  const createIssue = useCreateIssue();
  const { workspace } = useMobileWorkspace();
  const parentIssueId = route.params?.parentIssueId;
  const parentIssueIdentifier = route.params?.parentIssueIdentifier;
  const isChildIssue = Boolean(parentIssueId);
  const mentionTargets = useWorkspaceMentionTargets(workspace.id);
  const { data: projects = [] } = useProjectList(workspace.id);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [status, setStatus] = useState<IssueStatus>("todo");
  const [priority, setPriority] = useState<IssuePriority>("none");
  const [assignee, setAssignee] = useState<{ type: IssueAssigneeType; id: string } | null>(null);
  const [assigneePickerOpen, setAssigneePickerOpen] = useState(false);
  const [dueDate, setDueDate] = useState("");
  const [datePickerOpen, setDatePickerOpen] = useState(false);
  const [projectId, setProjectId] = useState<string | null>(null);
  const [projectPickerOpen, setProjectPickerOpen] = useState(false);
  const [attachments, setAttachments] = useState<DraftCommentAttachment[]>([]);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const assigneeOptions = useMemo(
    () => mentionTargets.filter((target): target is typeof target & { type: IssueAssigneeType } =>
      target.type === "member" || target.type === "agent",
    ),
    [mentionTargets],
  );
  const currentAssigneeLabel = assignee
    ? assigneeOptions.find((item) => item.type === assignee.type && item.id === assignee.id)?.label ?? t("common.unknown")
    : t("issues.unassigned");
  const currentProjectLabel = projectId
    ? projects.find((project) => project.id === projectId)?.title ?? t("issues.unknown_project")
    : t("issues.no_project");

  async function submit() {
    const trimmedTitle = title.trim();
    const trimmedDueDate = dueDate.trim();
    if (!trimmedTitle || createIssue.isPending || uploading) return;
    if (trimmedDueDate && !isValidDateInput(trimmedDueDate)) {
      setError(t("issues.invalid_due_date"));
      return;
    }

    setError(null);
    setUploading(true);
    const uploadedAttachmentIds: string[] = [];
    try {
      for (const attachment of attachments) {
        const uploaded = await uploadMobileAsset(api, attachment);
        uploadedAttachmentIds.push(uploaded.id);
      }
      const issue = await createIssue.mutateAsync({
        title: trimmedTitle,
        description: description.trim() || undefined,
        status,
        priority,
        assignee_type: assignee?.type,
        assignee_id: assignee?.id,
        due_date: trimmedDueDate ? dateInputToRfc3339(trimmedDueDate) : undefined,
        parent_issue_id: parentIssueId,
        project_id: projectId ?? undefined,
        attachment_ids: uploadedAttachmentIds.length > 0 ? uploadedAttachmentIds : undefined,
      });
      navigation.replace("IssueDetail", { issueId: issue.id });
    } catch (err) {
      await Promise.allSettled(uploadedAttachmentIds.map((id) => api.deleteAttachment(id)));
      setError(err instanceof Error ? err.message : t("issues.unable_to_create"));
    } finally {
      setUploading(false);
    }
  }

  function addAttachment(asset: MobileUploadAsset) {
    setAttachments((items) => [...items, createDraftCommentAttachment(asset, items.length)]);
  }

  function removeAttachment(attachmentId: string) {
    setAttachments((items) => items.filter((attachment) => attachment.id !== attachmentId));
  }

  async function pickDocument() {
    setError(null);
    let DocumentPicker: DocumentPickerModule;
    try {
      DocumentPicker = require("expo-document-picker") as DocumentPickerModule;
    } catch (err) {
      setError(formatDocumentPickerError(err, t));
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
      setError(formatDocumentPickerError(err, t));
      return;
    }

    if (result.canceled) return;
    const asset = result.assets[0];
    if (!asset) return;
    addAttachment({
      uri: asset.uri,
      name: asset.name,
      mimeType: asset.mimeType,
      size: asset.size,
    });
  }

  async function pickImage() {
    setError(null);
    const result = await ImagePicker.launchImageLibraryAsync({
      mediaTypes: ["images"],
      quality: 1,
    });
    if (result.canceled) return;
    const asset = result.assets[0];
    if (!asset) return;
    addAttachment({
      uri: asset.uri,
      name: asset.fileName ?? "image",
      mimeType: asset.mimeType,
      size: asset.fileSize,
    });
  }

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar
        onBack={() => navigation.goBack()}
        title={isChildIssue ? t("issues.new_child_issue") : t("issues.new_issue")}
      />
      <KeyboardAvoidingView
        behavior={Platform.OS === "ios" ? "padding" : undefined}
        style={styles.keyboardAvoiding}
      >
        <ScrollView
          contentContainerStyle={[
            styles.content,
            { paddingBottom: Math.max(insets.bottom, spacing.lg) },
          ]}
          keyboardShouldPersistTaps="handled"
        >
          <Text style={styles.workspaceName}>{workspace.name}</Text>
          {isChildIssue && parentIssueIdentifier ? (
            <View style={styles.parentBanner}>
              <Text style={styles.parentBannerLabel}>{t("issues.parent")}</Text>
              <Text numberOfLines={1} style={styles.parentBannerValue}>{parentIssueIdentifier}</Text>
            </View>
          ) : null}
          <TextInput
            autoFocus
            onChangeText={setTitle}
            placeholder={t("issues.title_placeholder")}
            placeholderTextColor={colors.mutedForeground}
            returnKeyType="next"
            style={styles.titleInput}
            value={title}
          />
          <TextInput
            multiline
            onChangeText={setDescription}
            placeholder={t("issues.description_placeholder")}
            placeholderTextColor={colors.mutedForeground}
            style={styles.descriptionInput}
            value={description}
          />
          <OptionGroup label={t("issues.status")}>
            {BOARD_STATUSES.map((item) => (
              <OptionChip
                active={status === item}
                key={item}
                label={formatIssueStatus(t, item)}
                onPress={() => setStatus(item)}
              />
            ))}
          </OptionGroup>
          <OptionGroup label={t("issues.priority")}>
            {PRIORITY_ORDER.map((item) => (
              <OptionChip
                active={priority === item}
                key={item}
                label={formatIssuePriority(t, item)}
                onPress={() => setPriority(item)}
              />
            ))}
          </OptionGroup>
          <OptionGroup label={t("issues.assignee")}>
            <PickerTrigger
              label={currentAssigneeLabel}
              muted={!assignee}
              onPress={() => setAssigneePickerOpen(true)}
            />
          </OptionGroup>
          <OptionGroup label={t("issues.due_date")}>
            <Pressable
              accessibilityRole="button"
              onPress={() => setDatePickerOpen(true)}
              style={({ pressed }) => [
                styles.datePickerTrigger,
                pressed && styles.optionChipPressed,
              ]}
            >
              <Text style={[styles.datePickerTriggerText, !dueDate && styles.datePickerPlaceholder]}>
                {dueDate ? formatDueDateLabel(dueDate) : t("issues.no_due_date")}
              </Text>
            </Pressable>
          </OptionGroup>
          <OptionGroup label={t("issues.project")}>
            <PickerTrigger
              label={currentProjectLabel}
              muted={!projectId}
              onPress={() => setProjectPickerOpen(true)}
            />
          </OptionGroup>
          <OptionGroup label={t("issues.attachments")}>
            <View style={styles.inlineActions}>
              <Button disabled={uploading} onPress={() => void pickImage()} variant="secondary">
                {t("issues.image")}
              </Button>
              <Button disabled={uploading} onPress={() => void pickDocument()} variant="secondary">
                {t("issues.file")}
              </Button>
            </View>
            {attachments.length > 0 && (
              <DraftAttachmentList attachments={attachments} onRemove={removeAttachment} />
            )}
          </OptionGroup>
          {error ? <Text style={styles.error}>{error}</Text> : null}
          <Button disabled={!title.trim() || createIssue.isPending || uploading} onPress={() => void submit()}>
            {createIssue.isPending || uploading
              ? t("issues.creating")
              : isChildIssue ? t("issues.create_child") : t("issues.create")}
          </Button>
        </ScrollView>
      </KeyboardAvoidingView>
      <DatePickerModal
        onChange={(value) => setDueDate(value ?? "")}
        onClose={() => setDatePickerOpen(false)}
        open={datePickerOpen}
        value={dueDate}
      />
      <AssigneePickerModal
        assignee={assignee}
        onChange={setAssignee}
        onClose={() => setAssigneePickerOpen(false)}
        open={assigneePickerOpen}
        options={assigneeOptions}
      />
      <ProjectPickerModal
        onChange={setProjectId}
        onClose={() => setProjectPickerOpen(false)}
        open={projectPickerOpen}
        projects={projects}
        value={projectId}
      />
    </Screen>
  );
}

function PickerTrigger({
  label,
  muted,
  onPress,
}: {
  label: string;
  muted?: boolean;
  onPress: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [
        styles.pickerTrigger,
        pressed && styles.optionChipPressed,
      ]}
    >
      <Text numberOfLines={1} style={[styles.pickerTriggerText, muted && styles.pickerTriggerPlaceholder]}>
        {label}
      </Text>
      <Text style={styles.pickerTriggerMeta}>{t("issues.select")}</Text>
    </Pressable>
  );
}

function AssigneePickerModal({
  assignee,
  onChange,
  onClose,
  open,
  options,
}: {
  assignee: { type: IssueAssigneeType; id: string } | null;
  onChange: (value: { type: IssueAssigneeType; id: string } | null) => void;
  onClose: () => void;
  open: boolean;
  options: Array<{ id: string; label: string; type: IssueAssigneeType }>;
}) {
  const { t } = useTranslation();
  const [query, setQuery] = useState("");

  useEffect(() => {
    if (!open) setQuery("");
  }, [open]);

  const filteredMembers = options.filter((item) => item.type === "member" && fuzzyMatch(item.label, query));
  const filteredAgents = options.filter((item) => item.type === "agent" && fuzzyMatch(item.label, query));
  const showUnassigned = !query.trim() || fuzzyMatch(t("issues.unassigned"), query) || fuzzyMatch(t("issues.no_assignee"), query);
  const hasResults = showUnassigned || filteredMembers.length > 0 || filteredAgents.length > 0;

  function select(value: { type: IssueAssigneeType; id: string } | null) {
    onChange(value);
    onClose();
  }

  return (
    <SearchPickerModal
      onChangeQuery={setQuery}
      onClose={onClose}
      open={open}
      placeholder={t("issues.assign_to")}
      query={query}
      title={t("issues.assignee")}
    >
      {showUnassigned ? (
        <PickerRow
          label={t("issues.unassigned")}
          muted
          onPress={() => select(null)}
          selected={!assignee}
        />
      ) : null}
      {filteredMembers.length > 0 ? (
        <PickerSection label={t("issues.members")}>
          {filteredMembers.map((item) => (
            <PickerRow
              key={`member-${item.id}`}
              label={item.label}
              onPress={() => select({ type: "member", id: item.id })}
              selected={assignee?.type === "member" && assignee.id === item.id}
            />
          ))}
        </PickerSection>
      ) : null}
      {filteredAgents.length > 0 ? (
        <PickerSection label={t("issues.agents")}>
          {filteredAgents.map((item) => (
            <PickerRow
              key={`agent-${item.id}`}
              label={item.label}
              onPress={() => select({ type: "agent", id: item.id })}
              selected={assignee?.type === "agent" && assignee.id === item.id}
            />
          ))}
        </PickerSection>
      ) : null}
      {!hasResults ? <Text style={styles.pickerEmpty}>{t("common.no_results")}</Text> : null}
    </SearchPickerModal>
  );
}

function ProjectPickerModal({
  onChange,
  onClose,
  open,
  projects,
  value,
}: {
  onChange: (projectId: string | null) => void;
  onClose: () => void;
  open: boolean;
  projects: Array<{ id: string; title: string; icon: string | null }>;
  value: string | null;
}) {
  const { t } = useTranslation();
  const [query, setQuery] = useState("");

  useEffect(() => {
    if (!open) setQuery("");
  }, [open]);

  const filtered = projects.filter((project) =>
    fuzzyMatch(`${project.icon ?? ""} ${project.title}`, query),
  );
  const showNoProject = !query.trim() || fuzzyMatch(t("issues.no_project"), query);
  const hasResults = showNoProject || filtered.length > 0;

  function select(projectId: string | null) {
    onChange(projectId);
    onClose();
  }

  return (
    <SearchPickerModal
      onChangeQuery={setQuery}
      onClose={onClose}
      open={open}
      placeholder={t("issues.filter_projects")}
      query={query}
      title={t("issues.project")}
    >
      {showNoProject ? (
        <PickerRow
          label={t("issues.no_project")}
          muted
          onPress={() => select(null)}
          selected={!value}
        />
      ) : null}
      {filtered.map((project) => (
        <PickerRow
          key={project.id}
          label={`${project.icon ?? ""}${project.icon ? " " : ""}${project.title}`}
          onPress={() => select(project.id)}
          selected={value === project.id}
        />
      ))}
      {!hasResults ? <Text style={styles.pickerEmpty}>{t("common.no_results")}</Text> : null}
    </SearchPickerModal>
  );
}

function SearchPickerModal({
  children,
  onChangeQuery,
  onClose,
  open,
  placeholder,
  query,
  title,
}: {
  children: React.ReactNode;
  onChangeQuery: (query: string) => void;
  onClose: () => void;
  open: boolean;
  placeholder: string;
  query: string;
  title: string;
}) {
  const { t } = useTranslation();
  const keyboardHeight = useKeyboardHeight();
  const { height: windowHeight } = useWindowDimensions();
  const maxSheetHeight = Math.max(
    220,
    windowHeight - keyboardHeight - spacing.lg * 2,
  );

  return (
    <Modal animationType="fade" onRequestClose={onClose} transparent visible={open}>
      <View style={styles.searchPickerRoot}>
        <Pressable onPress={onClose} style={styles.searchPickerBackdrop} />
        <View style={[
          styles.searchPickerSheet,
          { marginBottom: keyboardHeight, maxHeight: maxSheetHeight },
        ]}>
          <View style={styles.searchPickerHeader}>
            <Text style={styles.searchPickerTitle}>{title}</Text>
            <Button onPress={onClose} variant="ghost">
              {t("common.close")}
            </Button>
          </View>
          <TextInput
            autoCapitalize="none"
            autoCorrect={false}
            autoFocus
            onChangeText={onChangeQuery}
            placeholder={placeholder}
            placeholderTextColor={colors.mutedForeground}
            style={styles.searchPickerInput}
            value={query}
          />
          <ScrollView
            contentContainerStyle={styles.searchPickerList}
            keyboardShouldPersistTaps="handled"
          >
            {children}
          </ScrollView>
        </View>
      </View>
    </Modal>
  );
}

function PickerSection({ children, label }: { children: React.ReactNode; label: string }) {
  return (
    <View style={styles.pickerSection}>
      <Text style={styles.pickerSectionLabel}>{label}</Text>
      {children}
    </View>
  );
}

function PickerRow({
  label,
  muted,
  onPress,
  selected,
}: {
  label: string;
  muted?: boolean;
  onPress: () => void;
  selected: boolean;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [
        styles.pickerRow,
        pressed && styles.optionChipPressed,
      ]}
    >
      <Text numberOfLines={1} style={[styles.pickerRowLabel, muted && styles.pickerRowMuted]}>
        {label}
      </Text>
      {selected ? <Text style={styles.pickerRowCheck}>✓</Text> : null}
    </Pressable>
  );
}

function OptionGroup({ children, label }: { children: React.ReactNode; label: string }) {
  return (
    <View style={styles.optionGroup}>
      <Text style={styles.optionLabel}>{label}</Text>
      <View style={styles.optionRow}>{children}</View>
    </View>
  );
}

function OptionChip({
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
      style={({ pressed }) => [
        styles.optionChip,
        active && styles.optionChipActive,
        pressed && styles.optionChipPressed,
      ]}
    >
      <Text style={[styles.optionChipText, active && styles.optionChipTextActive]}>{label}</Text>
    </Pressable>
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
            <Text numberOfLines={1} style={styles.attachmentName}>{attachment.name}</Text>
            <Text style={styles.attachmentMeta}>
              {formatBytes(attachment.size ?? 0)} / {attachment.mimeType || "file"}
            </Text>
          </View>
          <Pressable
            accessibilityLabel={t("issues.remove_attachment", { name: attachment.name })}
            accessibilityRole="button"
            hitSlop={8}
            onPress={() => onRemove(attachment.id)}
            style={({ pressed }) => [
              styles.attachmentRemoveButton,
              pressed && styles.optionChipPressed,
            ]}
          >
            <Text style={styles.attachmentRemoveText}>{t("issues.remove")}</Text>
          </Pressable>
        </View>
      ))}
    </View>
  );
}

function formatBytes(bytes: number) {
  if (!bytes) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** index;
  return `${value >= 10 || index === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[index]}`;
}

function formatDocumentPickerError(err: unknown, t: (key: string) => string) {
  const message = err instanceof Error ? err.message : String(err);
  if (message.includes("ExpoDocumentPicker")) {
    return t("issues.document_picker_unavailable");
  }
  return message || t("issues.file_picker_unavailable");
}

function fuzzyMatch(value: string, query: string) {
  const needle = normalizeSearch(query);
  if (!needle) return true;

  const haystack = normalizeSearch(value);
  if (haystack.includes(needle)) return true;

  let index = 0;
  for (const char of haystack) {
    if (char === needle[index]) index += 1;
    if (index === needle.length) return true;
  }
  return false;
}

function normalizeSearch(value: string) {
  return value.trim().toLowerCase().replace(/\s+/g, "");
}

function useKeyboardHeight() {
  const [height, setHeight] = useState(0);

  useEffect(() => {
    function show(event: KeyboardEvent) {
      setHeight(event.endCoordinates.height);
    }

    function hide() {
      setHeight(0);
    }

    const showSubscription = Keyboard.addListener(
      Platform.OS === "ios" ? "keyboardWillShow" : "keyboardDidShow",
      show,
    );
    const hideSubscription = Keyboard.addListener(
      Platform.OS === "ios" ? "keyboardWillHide" : "keyboardDidHide",
      hide,
    );

    return () => {
      showSubscription.remove();
      hideSubscription.remove();
    };
  }, []);

  return height;
}

const styles = StyleSheet.create({
  keyboardAvoiding: {
    flex: 1,
  },
  content: {
    gap: spacing.md,
    padding: spacing.lg,
  },
  workspaceName: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  parentBanner: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.sm,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  parentBannerLabel: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  parentBannerValue: {
    color: colors.foreground,
    flex: 1,
    fontSize: 13,
    fontWeight: "600",
  },
  titleInput: {
    color: colors.foreground,
    fontSize: 22,
    fontWeight: "600",
    includeFontPadding: false,
    lineHeight: 28,
    minHeight: 48,
    padding: 0,
  },
  descriptionInput: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    color: colors.foreground,
    fontSize: 16,
    includeFontPadding: false,
    lineHeight: 22,
    minHeight: 132,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.md,
    textAlignVertical: "top",
  },
  pickerTrigger: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.md,
    height: 44,
    justifyContent: "space-between",
    paddingHorizontal: spacing.md,
    width: "100%",
  },
  pickerTriggerText: {
    color: colors.foreground,
    flex: 1,
    fontSize: 15,
    fontWeight: "500",
  },
  pickerTriggerPlaceholder: {
    color: colors.mutedForeground,
  },
  pickerTriggerMeta: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  searchPickerRoot: {
    flex: 1,
    justifyContent: "flex-end",
  },
  searchPickerBackdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: "rgba(0, 0, 0, 0.42)",
  },
  searchPickerSheet: {
    backgroundColor: colors.background,
    borderColor: colors.border,
    borderTopLeftRadius: radii.md,
    borderTopRightRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    padding: spacing.md,
  },
  searchPickerHeader: {
    alignItems: "center",
    flexDirection: "row",
    justifyContent: "space-between",
    marginBottom: spacing.sm,
  },
  searchPickerTitle: {
    color: colors.foreground,
    fontSize: 18,
    fontWeight: "600",
  },
  searchPickerInput: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    color: colors.foreground,
    fontSize: 16,
    height: 44,
    includeFontPadding: false,
    marginBottom: spacing.sm,
    paddingHorizontal: spacing.md,
  },
  searchPickerList: {
    gap: spacing.xs,
    paddingBottom: spacing.md,
  },
  pickerSection: {
    gap: spacing.xs,
    paddingTop: spacing.sm,
  },
  pickerSectionLabel: {
    color: colors.mutedForeground,
    fontSize: 11,
    fontWeight: "700",
    paddingHorizontal: spacing.sm,
    textTransform: "uppercase",
  },
  pickerRow: {
    alignItems: "center",
    borderRadius: radii.md,
    flexDirection: "row",
    gap: spacing.md,
    minHeight: 44,
    paddingHorizontal: spacing.sm,
  },
  pickerRowLabel: {
    color: colors.foreground,
    flex: 1,
    fontSize: 15,
    fontWeight: "500",
  },
  pickerRowMuted: {
    color: colors.mutedForeground,
  },
  pickerRowCheck: {
    color: colors.mutedForeground,
    fontSize: 16,
    fontWeight: "700",
  },
  pickerEmpty: {
    color: colors.mutedForeground,
    fontSize: 14,
    paddingVertical: spacing.lg,
    textAlign: "center",
  },
  datePickerTrigger: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    height: 44,
    justifyContent: "space-between",
    paddingHorizontal: spacing.md,
    width: "100%",
  },
  datePickerTriggerText: {
    color: colors.foreground,
    fontSize: 15,
    fontWeight: "500",
  },
  datePickerPlaceholder: {
    color: colors.mutedForeground,
  },
  inlineActions: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.sm,
  },
  optionGroup: {
    gap: spacing.sm,
  },
  optionLabel: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  optionRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.sm,
  },
  optionChip: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  optionChipActive: {
    backgroundColor: colors.primary,
    borderColor: colors.primary,
  },
  optionChipPressed: {
    opacity: 0.8,
  },
  optionChipText: {
    color: colors.foreground,
    fontSize: 13,
    fontWeight: "500",
  },
  optionChipTextActive: {
    color: colors.primaryForeground,
  },
  error: {
    color: colors.destructive,
    fontSize: 14,
  },
  attachmentList: {
    gap: spacing.sm,
    width: "100%",
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
});
