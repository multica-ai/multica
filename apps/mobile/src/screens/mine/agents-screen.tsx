import { useEffect, useMemo, useState } from "react";
import {
  ActivityIndicator,
  Alert,
  FlatList,
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
  type TextInputProps,
} from "react-native";
import { useNavigation } from "@react-navigation/native";
import type { NativeStackNavigationProp } from "@react-navigation/native-stack";
import { useTranslation } from "react-i18next";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useCoreQuery, useCoreQueryClient } from "@multica/core/provider";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import type {
  Agent,
  AgentRuntimeMode,
  AgentStatus,
  AgentTask,
  AgentVisibility,
  CreateAgentRequest,
  MemberWithUser,
  RuntimeDevice,
  Skill,
  UpdateAgentRequest,
} from "@multica/core/types";
import {
  agentListOptions,
  memberListOptions,
  skillListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import {
  Archive,
  BookOpenText,
  Bot,
  CheckCircle2,
  ChevronDown,
  Cloud,
  Copy,
  FileText,
  Globe,
  ListTodo,
  Lock,
  Monitor,
  Plus,
  RotateCcw,
  Settings,
  Terminal,
  Trash2,
  X,
} from "lucide-react-native";
import { Button, EmptyState, LoadingState, Screen } from "../../components/ui/primitives";
import { ScreenTitleBar } from "../../components/ui/screen-title-bar";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";
import {
  formatAgentStatus,
  formatAgentTaskStatus,
  formatAgentVisibility,
  formatRuntimeMode,
} from "../../i18n/format";

type AgentsNavigation = NativeStackNavigationProp<RootStackParamList>;
type AgentScope = "mine" | "all";
type DetailTab = "overview" | "instructions" | "tasks" | "skills" | "settings" | "advanced";

const detailTabs: Array<{ id: DetailTab; labelKey: string; icon: typeof Bot }> = [
  { id: "overview", labelKey: "agents.overview", icon: Bot },
  { id: "instructions", labelKey: "agents.instructions", icon: FileText },
  { id: "tasks", labelKey: "agents.tasks", icon: ListTodo },
  { id: "skills", labelKey: "agents.skills", icon: BookOpenText },
  { id: "settings", labelKey: "agents.settings", icon: Settings },
  { id: "advanced", labelKey: "agents.advanced", icon: Terminal },
];

const statusMeta: Record<AgentStatus, { color: string; muted?: boolean }> = {
  idle: { color: colors.mutedForeground },
  working: { color: colors.success },
  blocked: { color: colors.warning },
  error: { color: colors.destructive },
  offline: { color: colors.mutedForeground, muted: true },
};

const taskStatusMeta: Record<AgentTask["status"], { color: string }> = {
  queued: { color: colors.mutedForeground },
  dispatched: { color: colors.info },
  running: { color: colors.success },
  completed: { color: colors.success },
  failed: { color: colors.destructive },
  cancelled: { color: colors.mutedForeground },
};

export function AgentsScreen() {
  const { t } = useTranslation();
  const navigation = useNavigation<AgentsNavigation>();
  const qc = useCoreQueryClient();
  const currentUser = useAuthStore((state) => state.user);
  const { workspace } = useMobileWorkspace();
  const [scope, setScope] = useState<AgentScope>("mine");
  const [ownerFilter, setOwnerFilter] = useState<string | null>(null);
  const [showArchived, setShowArchived] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [ownerPickerOpen, setOwnerPickerOpen] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const {
    data: agents = [],
    isError,
    isLoading,
    isRefetching,
    refetch,
  } = useCoreQuery(agentListOptions(workspace.id, scope === "mine" ? "me" : undefined));
  const { data: runtimes = [], isLoading: runtimesLoading } = useCoreQuery(
    runtimeListOptions(workspace.id),
  );
  const { data: members = [] } = useCoreQuery(memberListOptions(workspace.id));

  const ownerOptions = useMemo(() => {
    const ownerIds = Array.from(
      new Set(agents.map((agent) => agent.owner_id).filter(Boolean) as string[]),
    );
    return ownerIds
      .map((ownerId) => members.find((member) => member.user_id === ownerId))
      .filter(Boolean) as MemberWithUser[];
  }, [agents, members]);

  const filteredAgents = useMemo(() => {
    const byOwner =
      scope === "all" && ownerFilter
        ? agents.filter((agent) => agent.owner_id === ownerFilter)
        : agents;
    return byOwner.filter((agent) => (showArchived ? !!agent.archived_at : !agent.archived_at));
  }, [agents, ownerFilter, scope, showArchived]);

  const archivedCount = useMemo(() => {
    const byOwner =
      scope === "all" && ownerFilter
        ? agents.filter((agent) => agent.owner_id === ownerFilter)
        : agents;
    return byOwner.filter((agent) => !!agent.archived_at).length;
  }, [agents, ownerFilter, scope]);

  const selectedAgent = selectedId
    ? agents.find((agent) => agent.id === selectedId) ?? null
    : null;
  const activeCount = agents.filter((agent) => !agent.archived_at).length;
  const selectedOwner = ownerFilter
    ? members.find((member) => member.user_id === ownerFilter) ?? null
    : null;

  function invalidateAgents() {
    void qc.invalidateQueries({ queryKey: workspaceKeys.agents(workspace.id) });
  }

  async function createAgent(data: CreateAgentRequest) {
    const agent = await api.createAgent(data);
    invalidateAgents();
    setSelectedId(agent.id);
  }

  async function updateAgent(agentId: string, data: UpdateAgentRequest) {
    await api.updateAgent(agentId, data);
    invalidateAgents();
  }

  async function archiveAgent(agent: Agent) {
    Alert.alert(t("agents.archive_title"), t("agents.archive_description", { name: agent.name }), [
      { text: t("common.cancel"), style: "cancel" },
      {
        text: t("agents.archive"),
        style: "destructive",
        onPress: () => {
          void api.archiveAgent(agent.id).then(invalidateAgents);
        },
      },
    ]);
  }

  async function restoreAgent(agent: Agent) {
    await api.restoreAgent(agent.id);
    invalidateAgents();
  }

  async function duplicateAgent(agent: Agent) {
    const copied = await api.copyAgent(agent.id);
    invalidateAgents();
    setShowArchived(false);
    setSelectedId(copied.id);
  }

  if (isLoading) return <LoadingState />;
  if (isError) {
    return (
      <Screen padded={false} safeArea={false}>
        <ScreenTitleBar onBack={() => navigation.goBack()} title={t("agents.title")} />
        <EmptyState detail={t("common.pull_to_retry")} title={t("agents.unable_to_load")} />
      </Screen>
    );
  }

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar
        onBack={() => navigation.goBack()}
        right={
          <Pressable
            accessibilityLabel={t("agents.create")}
            accessibilityRole="button"
            onPress={() => setCreateOpen(true)}
            style={({ pressed }) => [styles.titleIconButton, pressed && styles.pressed]}
          >
            <Plus color={colors.foreground} size={20} />
          </Pressable>
        }
        title={t("agents.title")}
      />
      <View style={styles.toolbar}>
        <SegmentedControl
          onChange={(next) => {
            setScope(next);
            setOwnerFilter(null);
          }}
          options={[
            { label: t("agents.scope_mine"), value: "mine" },
            { label: t("agents.scope_all"), value: "all" },
          ]}
          value={scope}
        />
        <View style={styles.toolbarActions}>
          {scope === "all" && ownerOptions.length > 1 ? (
            <Pressable
              accessibilityRole="button"
              onPress={() => setOwnerPickerOpen(true)}
              style={({ pressed }) => [styles.filterButton, pressed && styles.pressed]}
            >
              <Text numberOfLines={1} style={styles.filterButtonText}>
                {selectedOwner?.name ?? t("agents.owner")}
              </Text>
              <ChevronDown color={colors.mutedForeground} size={14} />
            </Pressable>
          ) : null}
          {archivedCount > 0 ? (
            <Pressable
              accessibilityRole="button"
              onPress={() => setShowArchived((value) => !value)}
              style={({ pressed }) => [
                styles.filterButton,
                showArchived && styles.filterButtonActive,
                pressed && styles.pressed,
              ]}
            >
              <Archive color={showArchived ? colors.primaryForeground : colors.mutedForeground} size={14} />
              <Text
                style={[
                  styles.filterButtonText,
                  showArchived && styles.filterButtonTextActive,
                ]}
              >
                {showArchived ? t("agents.archived") : `${archivedCount}`}
              </Text>
            </Pressable>
          ) : null}
        </View>
      </View>
      <FlatList
        contentContainerStyle={filteredAgents.length === 0 ? styles.emptyList : styles.list}
        data={filteredAgents}
        keyExtractor={(agent) => agent.id}
        ListEmptyComponent={
          <AgentsEmpty
            activeCount={activeCount}
            ownerFiltered={!!ownerFilter}
            scope={scope}
            showArchived={showArchived}
            onCreate={() => setCreateOpen(true)}
          />
        }
        onRefresh={() => {
          void refetch();
        }}
        refreshing={isRefetching && !isLoading}
        renderItem={({ item }) => (
          <AgentRow
            agent={item}
            owner={members.find((member) => member.user_id === item.owner_id) ?? null}
            runtime={runtimes.find((runtime) => runtime.id === item.runtime_id) ?? null}
            showOwner={scope === "all"}
            onPress={() => setSelectedId(item.id)}
          />
        )}
      />
      <OwnerPickerModal
        members={ownerOptions}
        onChange={setOwnerFilter}
        onClose={() => setOwnerPickerOpen(false)}
        open={ownerPickerOpen}
        value={ownerFilter}
      />
      <CreateAgentModal
        currentUserId={currentUser?.id ?? null}
        members={members}
        onClose={() => setCreateOpen(false)}
        onCreate={createAgent}
        open={createOpen}
        runtimes={runtimes}
        runtimesLoading={runtimesLoading}
      />
      <AgentDetailModal
        agent={selectedAgent}
        currentUserId={currentUser?.id ?? null}
        members={members}
        onArchive={archiveAgent}
        onClose={() => setSelectedId(null)}
        onDuplicate={duplicateAgent}
        onRestore={restoreAgent}
        onUpdate={updateAgent}
        open={!!selectedAgent}
        runtimes={runtimes}
        workspaceId={workspace.id}
      />
    </Screen>
  );
}

function AgentRow({
  agent,
  owner,
  runtime,
  showOwner,
  onPress,
}: {
  agent: Agent;
  owner: MemberWithUser | null;
  runtime: RuntimeDevice | null;
  showOwner: boolean;
  onPress: () => void;
}) {
  const { t } = useTranslation();
  const archived = !!agent.archived_at;
  const runtimeLabel = runtime?.name ?? formatRuntimeMode(t, agent.runtime_mode);

  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [styles.agentCard, pressed && styles.pressed]}
    >
      <View style={[styles.agentAvatar, archived && styles.archivedDim]}>
        <Bot color={colors.foreground} size={20} />
      </View>
      <View style={styles.agentCardText}>
        <View style={styles.agentTitleLine}>
          <Text
            numberOfLines={1}
            style={[styles.agentName, archived && styles.archivedText]}
          >
            {agent.name}
          </Text>
          {archived ? (
            <Badge label={t("agents.archived")} muted />
          ) : (
            <StatusBadge status={agent.status} />
          )}
        </View>
        <Text numberOfLines={1} style={styles.agentDescription}>
          {agent.description || t("agents.no_description")}
        </Text>
        <View style={styles.agentMetaLine}>
          <RuntimeBadge mode={agent.runtime_mode} label={runtimeLabel} />
          <VisibilityBadge visibility={agent.visibility} />
          {showOwner ? <Text numberOfLines={1} style={styles.agentMetaText}>{owner?.name ?? t("common.unknown")}</Text> : null}
        </View>
      </View>
    </Pressable>
  );
}

function AgentsEmpty({
  activeCount,
  ownerFiltered,
  scope,
  showArchived,
  onCreate,
}: {
  activeCount: number;
  ownerFiltered: boolean;
  scope: AgentScope;
  showArchived: boolean;
  onCreate: () => void;
}) {
  const { t } = useTranslation();
  const title = showArchived
    ? t("agents.no_archived")
    : ownerFiltered
      ? t("agents.no_for_owner")
      : scope === "mine"
        ? t("agents.no_mine")
        : t("agents.no_agents");

  return (
    <View style={styles.emptyState}>
      <Bot color={colors.mutedForeground} size={30} />
      <Text style={styles.emptyTitle}>{title}</Text>
      {!showArchived ? (
        <>
          <Text style={styles.emptyDetail}>
            {activeCount > 0
              ? t("agents.empty_detail_filtered")
              : t("agents.empty_detail_create")}
          </Text>
          <Button onPress={onCreate} style={styles.emptyButton}>
            {t("agents.create")}
          </Button>
        </>
      ) : null}
    </View>
  );
}

function AgentDetailModal({
  agent,
  currentUserId,
  members,
  onArchive,
  onClose,
  onDuplicate,
  onRestore,
  onUpdate,
  open,
  runtimes,
  workspaceId,
}: {
  agent: Agent | null;
  currentUserId: string | null;
  members: MemberWithUser[];
  onArchive: (agent: Agent) => void | Promise<void>;
  onClose: () => void;
  onDuplicate: (agent: Agent) => void | Promise<void>;
  onRestore: (agent: Agent) => void | Promise<void>;
  onUpdate: (agentId: string, data: UpdateAgentRequest) => Promise<void>;
  open: boolean;
  runtimes: RuntimeDevice[];
  workspaceId: string;
}) {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState<DetailTab>("overview");
  const runtime = agent
    ? runtimes.find((item) => item.id === agent.runtime_id) ?? null
    : null;
  const isArchived = !!agent?.archived_at;
  const isOwner = !!agent && !!currentUserId && agent.owner_id === currentUserId;
  const membership = members.find((member) => member.user_id === currentUserId);
  const canDuplicate =
    !!agent &&
    !isArchived &&
    !!currentUserId &&
    !!membership &&
    (agent.owner_id === currentUserId ||
      membership.role === "owner" ||
      membership.role === "admin");

  useEffect(() => {
    if (!open) setActiveTab("overview");
  }, [open]);

  if (!agent) return null;

  return (
    <Modal animationType="slide" onRequestClose={onClose} visible={open}>
      <Screen padded={false} safeArea={false}>
        <ScreenTitleBar
          onBack={onClose}
          right={
            <View style={styles.detailHeaderActions}>
              {canDuplicate ? (
                <IconAction
                  accessibilityLabel={t("agents.duplicate")}
                  icon={Copy}
                  onPress={() => void onDuplicate(agent)}
                />
              ) : null}
              {isArchived && isOwner ? (
                <IconAction
                  accessibilityLabel={t("agents.restore")}
                  icon={RotateCcw}
                  onPress={() => void onRestore(agent)}
                />
              ) : null}
              {!isArchived && isOwner ? (
                <IconAction
                  accessibilityLabel={t("agents.archive")}
                  destructive
                  icon={Trash2}
                  onPress={() => void onArchive(agent)}
                />
              ) : null}
            </View>
          }
          title={agent.name}
        />
        {isArchived ? (
          <View style={styles.archiveBanner}>
            <Archive color={colors.mutedForeground} size={14} />
            <Text style={styles.archiveBannerText}>
              {t("agents.archive_banner")}
            </Text>
          </View>
        ) : null}
        <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.tabsScroller}>
          <View style={styles.tabsRow}>
            {detailTabs.map((tab) => {
              const Icon = tab.icon;
              const active = activeTab === tab.id;
              return (
                <Pressable
                  accessibilityRole="button"
                  key={tab.id}
                  onPress={() => setActiveTab(tab.id)}
                  style={({ pressed }) => [
                    styles.tabChip,
                    active && styles.tabChipActive,
                    pressed && styles.pressed,
                  ]}
                >
                  <Icon color={active ? colors.primaryForeground : colors.mutedForeground} size={15} />
                  <Text style={[styles.tabChipText, active && styles.tabChipTextActive]}>
                    {t(tab.labelKey)}
                  </Text>
                </Pressable>
              );
            })}
          </View>
        </ScrollView>
        <ScrollView contentContainerStyle={styles.detailContent}>
          {activeTab === "overview" ? (
            <OverviewTab agent={agent} owner={members.find((m) => m.user_id === agent.owner_id) ?? null} runtime={runtime} />
          ) : null}
          {activeTab === "instructions" ? (
            <InstructionsTab agent={agent} readOnly={!isOwner || isArchived} onUpdate={onUpdate} />
          ) : null}
          {activeTab === "tasks" ? (
            <TasksTab agent={agent} workspaceId={workspaceId} />
          ) : null}
          {activeTab === "skills" ? (
            <SkillsTab agent={agent} readOnly={!isOwner || isArchived} workspaceId={workspaceId} />
          ) : null}
          {activeTab === "settings" ? (
            <SettingsTab
              agent={agent}
              currentUserId={currentUserId}
              members={members}
              readOnly={!isOwner || isArchived}
              runtimes={runtimes}
              onUpdate={onUpdate}
            />
          ) : null}
          {activeTab === "advanced" ? (
            <AdvancedTab agent={agent} readOnly={!isOwner || isArchived} onUpdate={onUpdate} />
          ) : null}
        </ScrollView>
      </Screen>
    </Modal>
  );
}

function OverviewTab({
  agent,
  owner,
  runtime,
}: {
  agent: Agent;
  owner: MemberWithUser | null;
  runtime: RuntimeDevice | null;
}) {
  const { t } = useTranslation();
  return (
    <View style={styles.sectionStack}>
      <View style={styles.heroCard}>
        <View style={styles.heroIcon}>
          <Bot color={colors.foreground} size={24} />
        </View>
        <View style={styles.heroText}>
          <Text numberOfLines={1} style={styles.heroTitle}>{agent.name}</Text>
          <Text style={styles.heroSubtitle}>{agent.description || t("agents.no_description")}</Text>
        </View>
      </View>
      <View style={styles.infoGrid}>
        <InfoCell label={t("agents.status")} value={agent.archived_at ? t("agents.archived") : formatAgentStatus(t, agent.status)} />
        <InfoCell label={t("agents.visibility_label")} value={formatAgentVisibility(t, agent.visibility)} />
        <InfoCell label={t("agents.runtime")} value={runtime?.name ?? formatRuntimeMode(t, agent.runtime_mode)} />
        <InfoCell label={t("agents.mode")} value={formatRuntimeMode(t, agent.runtime_mode)} />
        <InfoCell label={t("agents.owner")} value={owner?.name ?? t("common.unknown")} />
        <InfoCell label={t("agents.model")} value={agent.model || t("agents.default_model")} />
        <InfoCell label={t("agents.max_tasks")} value={String(agent.max_concurrent_tasks)} />
        <InfoCell label={t("agents.skills")} value={String(agent.skills.length)} />
      </View>
    </View>
  );
}

function InstructionsTab({
  agent,
  readOnly,
  onUpdate,
}: {
  agent: Agent;
  readOnly: boolean;
  onUpdate: (agentId: string, data: UpdateAgentRequest) => Promise<void>;
}) {
  const { t } = useTranslation();
  const [value, setValue] = useState(agent.instructions ?? "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setValue(agent.instructions ?? "");
    setError(null);
  }, [agent.id, agent.instructions]);

  async function save() {
    if (readOnly || value === (agent.instructions ?? "")) return;
    setSaving(true);
    setError(null);
    try {
      await onUpdate(agent.id, { instructions: value });
    } catch (err) {
      setError(err instanceof Error ? err.message : t("agents.unable_to_save_instructions"));
    } finally {
      setSaving(false);
    }
  }

  return (
    <View style={styles.sectionStack}>
      <Text style={styles.sectionTitle}>{t("agents.instructions")}</Text>
      <Text style={styles.sectionDetail}>
        {t("agents.instructions_detail")}
      </Text>
      <TextInput
        editable={!readOnly}
        multiline
        onChangeText={setValue}
        placeholder={readOnly ? t("agents.no_instructions") : t("agents.write_instructions")}
        placeholderTextColor={colors.mutedForeground}
        style={[styles.textArea, styles.instructionsInput, readOnly && styles.inputReadOnly]}
        textAlignVertical="top"
        value={value}
      />
      {error ? <Text style={styles.errorText}>{error}</Text> : null}
      {!readOnly ? (
        <Button disabled={saving || value === (agent.instructions ?? "")} onPress={() => void save()}>
          {saving ? t("agents.saving") : t("agents.save_instructions")}
        </Button>
      ) : null}
    </View>
  );
}

function TasksTab({ agent, workspaceId }: { agent: Agent; workspaceId: string }) {
  const { t } = useTranslation();
  const navigation = useNavigation<AgentsNavigation>();
  const { data: tasks = [], isError, isLoading, isRefetching, refetch } = useCoreQuery({
    queryKey: ["workspaces", workspaceId, "agents", agent.id, "tasks"],
    queryFn: () => api.listAgentTasks(agent.id),
  });

  const sortedTasks = useMemo(() => {
    const activeOrder = ["running", "dispatched", "queued"];
    return [...tasks].sort((a, b) => {
      const aActive = activeOrder.indexOf(a.status);
      const bActive = activeOrder.indexOf(b.status);
      if (aActive !== -1 && bActive === -1) return -1;
      if (aActive === -1 && bActive !== -1) return 1;
      if (aActive !== -1 && bActive !== -1) return aActive - bActive;
      return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
    });
  }, [tasks]);

  if (isLoading) return <InlineLoading />;
  if (isError) {
    return <InlineEmpty detail={t("common.pull_to_retry")} title={t("agents.unable_to_load_tasks")} />;
  }

  return (
    <View style={styles.sectionStack}>
      <View style={styles.sectionHeaderLine}>
        <View>
          <Text style={styles.sectionTitle}>{t("agents.task_queue")}</Text>
          <Text style={styles.sectionDetail}>{t("agents.task_queue_detail")}</Text>
        </View>
        {isRefetching ? <ActivityIndicator color={colors.mutedForeground} /> : null}
      </View>
      {sortedTasks.length === 0 ? (
        <InlineEmpty detail={t("agents.no_tasks_detail")} title={t("agents.no_tasks")} />
      ) : (
        sortedTasks.map((task) => (
          <TaskRow
            key={task.id}
            onPress={
              task.issue_id
                ? () => navigation.navigate("IssueDetail", { issueId: task.issue_id })
                : undefined
            }
            task={task}
          />
        ))
      )}
      <Button onPress={() => void refetch()} variant="secondary">
        {t("agents.refresh_tasks")}
      </Button>
    </View>
  );
}

function TaskRow({ task, onPress }: { task: AgentTask; onPress?: () => void }) {
  const { t } = useTranslation();
  const meta = taskStatusMeta[task.status];
  const content = (
    <>
      <View style={[styles.taskStatusDot, { backgroundColor: meta.color }]} />
      <View style={styles.taskText}>
        <Text numberOfLines={1} style={styles.taskTitle}>
          {task.issue_id ? `${t("issues.issue")} ${task.issue_id.slice(0, 8)}` : t("agents.task_without_issue")}
        </Text>
        <Text style={styles.taskMeta}>{formatTaskTime(task)}</Text>
      </View>
      <Text style={[styles.taskStatusLabel, { color: meta.color }]}>{formatAgentTaskStatus(t, task.status)}</Text>
    </>
  );

  if (!onPress) return <View style={styles.taskRow}>{content}</View>;

  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [styles.taskRow, pressed && styles.pressed]}
    >
      {content}
    </Pressable>
  );
}

function SkillsTab({
  agent,
  readOnly,
  workspaceId,
}: {
  agent: Agent;
  readOnly: boolean;
  workspaceId: string;
}) {
  const { t } = useTranslation();
  const qc = useCoreQueryClient();
  const { data: workspaceSkills = [], isLoading } = useCoreQuery(skillListOptions(workspaceId));
  const [pickerOpen, setPickerOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const agentSkillIds = useMemo(() => new Set(agent.skills.map((skill) => skill.id)), [agent.skills]);
  const availableSkills = workspaceSkills.filter((skill) => !agentSkillIds.has(skill.id));

  async function setSkills(skillIds: string[]) {
    setSaving(true);
    setError(null);
    try {
      await api.setAgentSkills(agent.id, { skill_ids: skillIds });
      void qc.invalidateQueries({ queryKey: workspaceKeys.agents(workspaceId) });
    } catch (err) {
      setError(err instanceof Error ? err.message : t("agents.unable_to_update_skills"));
    } finally {
      setSaving(false);
      setPickerOpen(false);
    }
  }

  return (
    <View style={styles.sectionStack}>
      <View style={styles.sectionHeaderLine}>
        <View style={styles.sectionHeaderText}>
          <Text style={styles.sectionTitle}>{t("agents.skills")}</Text>
          <Text style={styles.sectionDetail}>{t("agents.skills_detail")}</Text>
        </View>
        {!readOnly ? (
          <Pressable
            accessibilityRole="button"
            disabled={saving || availableSkills.length === 0}
            onPress={() => setPickerOpen(true)}
            style={({ pressed }) => [
              styles.smallActionButton,
              (saving || availableSkills.length === 0) && styles.disabled,
              pressed && styles.pressed,
            ]}
          >
            <Plus color={colors.foreground} size={16} />
          </Pressable>
        ) : null}
      </View>
      <View style={styles.infoNotice}>
        <Text style={styles.infoNoticeText}>
          {t("agents.skills_notice")}
        </Text>
      </View>
      {isLoading ? <InlineLoading /> : null}
      {!isLoading && agent.skills.length === 0 ? (
        <InlineEmpty detail={t("agents.no_skills_detail")} title={t("agents.no_skills")} />
      ) : null}
      {agent.skills.map((skill) => (
        <SkillRow
          key={skill.id}
          readOnly={readOnly || saving}
          skill={skill}
          onRemove={() => void setSkills(agent.skills.filter((item) => item.id !== skill.id).map((item) => item.id))}
        />
      ))}
      {error ? <Text style={styles.errorText}>{error}</Text> : null}
      <SkillPickerModal
        onClose={() => setPickerOpen(false)}
        onSelect={(skillId) => void setSkills([...agent.skills.map((skill) => skill.id), skillId])}
        open={pickerOpen}
        saving={saving}
        skills={availableSkills}
      />
    </View>
  );
}

function SkillRow({
  skill,
  readOnly,
  onRemove,
}: {
  skill: Skill;
  readOnly: boolean;
  onRemove: () => void;
}) {
  return (
    <View style={styles.skillRow}>
      <View style={styles.skillIcon}>
        <BookOpenText color={colors.mutedForeground} size={17} />
      </View>
      <View style={styles.skillText}>
        <Text numberOfLines={1} style={styles.skillName}>{skill.name}</Text>
        {skill.description ? (
          <Text numberOfLines={1} style={styles.skillDescription}>{skill.description}</Text>
        ) : null}
      </View>
      {!readOnly ? (
        <Pressable
          accessibilityRole="button"
          onPress={onRemove}
          style={({ pressed }) => [styles.removeButton, pressed && styles.pressed]}
        >
          <X color={colors.destructive} size={16} />
        </Pressable>
      ) : null}
    </View>
  );
}

function SettingsTab({
  agent,
  currentUserId,
  members,
  readOnly,
  runtimes,
  onUpdate,
}: {
  agent: Agent;
  currentUserId: string | null;
  members: MemberWithUser[];
  readOnly: boolean;
  runtimes: RuntimeDevice[];
  onUpdate: (agentId: string, data: UpdateAgentRequest) => Promise<void>;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState(agent.name);
  const [description, setDescription] = useState(agent.description ?? "");
  const [visibility, setVisibility] = useState<AgentVisibility>(agent.visibility);
  const [runtimeId, setRuntimeId] = useState(agent.runtime_id);
  const [model, setModel] = useState(agent.model ?? "");
  const [maxTasks, setMaxTasks] = useState(String(agent.max_concurrent_tasks));
  const [runtimePickerOpen, setRuntimePickerOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const ownRuntimes = currentUserId
    ? runtimes.filter((runtime) => runtime.owner_id === currentUserId)
    : runtimes;
  const selectedRuntime = runtimes.find((runtime) => runtime.id === runtimeId) ?? null;
  const dirty =
    name !== agent.name ||
    description !== (agent.description ?? "") ||
    visibility !== agent.visibility ||
    runtimeId !== agent.runtime_id ||
    model !== (agent.model ?? "") ||
    Number(maxTasks) !== agent.max_concurrent_tasks;

  useEffect(() => {
    setName(agent.name);
    setDescription(agent.description ?? "");
    setVisibility(agent.visibility);
    setRuntimeId(agent.runtime_id);
    setModel(agent.model ?? "");
    setMaxTasks(String(agent.max_concurrent_tasks));
    setError(null);
  }, [agent]);

  async function save() {
    const parsedMaxTasks = Number(maxTasks);
    if (!name.trim()) {
      setError(t("agents.name_required"));
      return;
    }
    if (!Number.isInteger(parsedMaxTasks) || parsedMaxTasks < 1) {
      setError(t("agents.max_tasks_invalid"));
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await onUpdate(agent.id, {
        name: name.trim(),
        description: description.trim(),
        visibility,
        runtime_id: runtimeId,
        model: model.trim(),
        max_concurrent_tasks: parsedMaxTasks,
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : t("agents.unable_to_save_settings"));
    } finally {
      setSaving(false);
    }
  }

  return (
    <View style={styles.sectionStack}>
      <Text style={styles.sectionTitle}>{t("agents.settings")}</Text>
      <FormField editable={!readOnly} label={t("agents.name")} onChangeText={setName} value={name} />
      <FormField
        editable={!readOnly}
        label={t("agents.description")}
        onChangeText={setDescription}
        placeholder={t("agents.description_placeholder")}
        value={description}
      />
      <OptionGroup label={t("agents.visibility_label")}>
        <OptionChip
          active={visibility === "private"}
          disabled={readOnly}
          icon={Lock}
          label={t("agents.visibility.private")}
          onPress={() => setVisibility("private")}
        />
        <OptionChip
          active={visibility === "workspace"}
          disabled={readOnly}
          icon={Globe}
          label={t("agents.visibility.workspace")}
          onPress={() => setVisibility("workspace")}
        />
      </OptionGroup>
      <OptionGroup label={t("agents.runtime")}>
        <PickerTrigger
          disabled={readOnly}
          label={selectedRuntime?.name ?? t("agents.no_runtime")}
          meta={selectedRuntime ? getRuntimeOwnerLabel(selectedRuntime, members, t) : t("agents.select")}
          onPress={() => setRuntimePickerOpen(true)}
        />
      </OptionGroup>
      <FormField editable={!readOnly} label={t("agents.model")} onChangeText={setModel} placeholder={t("agents.default_model")} value={model} />
      <FormField
        editable={!readOnly}
        keyboardType="number-pad"
        label={t("agents.max_concurrent_tasks")}
        onChangeText={setMaxTasks}
        value={maxTasks}
      />
      {error ? <Text style={styles.errorText}>{error}</Text> : null}
      {!readOnly ? (
        <Button disabled={saving || !dirty} onPress={() => void save()}>
          {saving ? t("agents.saving") : t("agents.save_settings")}
        </Button>
      ) : null}
      <RuntimePickerModal
        onClose={() => setRuntimePickerOpen(false)}
        onSelect={setRuntimeId}
        open={runtimePickerOpen}
        runtimes={ownRuntimes}
        selectedId={runtimeId}
        members={members}
      />
    </View>
  );
}

function AdvancedTab({
  agent,
  readOnly,
  onUpdate,
}: {
  agent: Agent;
  readOnly: boolean;
  onUpdate: (agentId: string, data: UpdateAgentRequest) => Promise<void>;
}) {
  const { t } = useTranslation();
  const [envText, setEnvText] = useState(envMapToText(agent.custom_env ?? {}));
  const [argsText, setArgsText] = useState((agent.custom_args ?? []).join("\n"));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const envReadOnly = readOnly || agent.custom_env_redacted;
  const dirty =
    envText !== envMapToText(agent.custom_env ?? {}) ||
    argsText !== (agent.custom_args ?? []).join("\n");

  useEffect(() => {
    setEnvText(envMapToText(agent.custom_env ?? {}));
    setArgsText((agent.custom_args ?? []).join("\n"));
    setError(null);
  }, [agent]);

  async function save() {
    let customEnv: Record<string, string>;
    try {
      customEnv = parseEnvText(envText);
    } catch {
      setError(t("agents.invalid_environment"));
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await onUpdate(agent.id, {
        custom_env: envReadOnly ? undefined : customEnv,
        custom_args: argsText
          .split("\n")
          .map((line) => line.trim())
          .filter(Boolean),
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : t("agents.unable_to_save_advanced"));
    } finally {
      setSaving(false);
    }
  }

  return (
    <View style={styles.sectionStack}>
      <Text style={styles.sectionTitle}>{t("agents.advanced")}</Text>
      {agent.custom_env_copied_pending ? (
        <View style={styles.warningNotice}>
          <Text style={styles.warningNoticeText}>
            {t("agents.copied_secret_notice")}
          </Text>
        </View>
      ) : null}
      <Text style={styles.optionLabel}>{t("agents.environment")}</Text>
      <TextInput
        autoCapitalize="none"
        autoCorrect={false}
        editable={!envReadOnly}
        multiline
        onChangeText={setEnvText}
        placeholder="ANTHROPIC_API_KEY=..."
        placeholderTextColor={colors.mutedForeground}
        style={[styles.textArea, envReadOnly && styles.inputReadOnly]}
        textAlignVertical="top"
        value={agent.custom_env_redacted ? t("agents.hidden_values") : envText}
      />
      <Text style={styles.optionLabel}>{t("agents.custom_args")}</Text>
      <TextInput
        autoCapitalize="none"
        autoCorrect={false}
        editable={!readOnly}
        multiline
        onChangeText={setArgsText}
        placeholder={t("agents.one_argument_per_line")}
        placeholderTextColor={colors.mutedForeground}
        style={[styles.textArea, readOnly && styles.inputReadOnly]}
        textAlignVertical="top"
        value={argsText}
      />
      {error ? <Text style={styles.errorText}>{error}</Text> : null}
      {!readOnly ? (
        <Button disabled={saving || !dirty} onPress={() => void save()}>
          {saving ? t("agents.saving") : t("agents.save_advanced")}
        </Button>
      ) : null}
    </View>
  );
}

function CreateAgentModal({
  currentUserId,
  members,
  onClose,
  onCreate,
  open,
  runtimes,
  runtimesLoading,
}: {
  currentUserId: string | null;
  members: MemberWithUser[];
  onClose: () => void;
  onCreate: (data: CreateAgentRequest) => Promise<void>;
  open: boolean;
  runtimes: RuntimeDevice[];
  runtimesLoading: boolean;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<AgentVisibility>("private");
  const [runtimeId, setRuntimeId] = useState("");
  const [model, setModel] = useState("");
  const [runtimePickerOpen, setRuntimePickerOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const ownRuntimes = useMemo(
    () => (currentUserId ? runtimes.filter((runtime) => runtime.owner_id === currentUserId) : runtimes),
    [currentUserId, runtimes],
  );
  const selectedRuntime = runtimes.find((runtime) => runtime.id === runtimeId) ?? ownRuntimes[0] ?? null;

  useEffect(() => {
    if (open && !runtimeId && ownRuntimes[0]) setRuntimeId(ownRuntimes[0].id);
    if (!open) {
      setName("");
      setDescription("");
      setVisibility("private");
      setModel("");
      setError(null);
      setCreating(false);
    }
  }, [open, ownRuntimes, runtimeId]);

  async function submit() {
    if (!name.trim() || !selectedRuntime || creating) return;
    setCreating(true);
    setError(null);
    try {
      await onCreate({
        name: name.trim(),
        description: description.trim(),
        runtime_id: selectedRuntime.id,
        visibility,
        model: model.trim() || undefined,
      });
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("agents.unable_to_create"));
    } finally {
      setCreating(false);
    }
  }

  return (
    <Modal animationType="slide" onRequestClose={onClose} visible={open}>
      <Screen padded={false} safeArea={false}>
        <ScreenTitleBar onBack={onClose} title={t("agents.new_agent")} />
        <KeyboardAvoidingView behavior={Platform.OS === "ios" ? "padding" : undefined} style={styles.keyboardAvoiding}>
          <ScrollView contentContainerStyle={styles.detailContent} keyboardShouldPersistTaps="handled">
            <Text style={styles.sectionTitle}>{t("agents.create_title")}</Text>
            <Text style={styles.sectionDetail}>{t("agents.create_detail")}</Text>
            <FormField autoFocus label={t("agents.name")} onChangeText={setName} placeholder={t("agents.name_placeholder")} value={name} />
            <FormField label={t("agents.description")} onChangeText={setDescription} placeholder={t("agents.description_placeholder")} value={description} />
            <OptionGroup label={t("agents.visibility_label")}>
              <OptionChip active={visibility === "private"} icon={Lock} label={t("agents.visibility.private")} onPress={() => setVisibility("private")} />
              <OptionChip active={visibility === "workspace"} icon={Globe} label={t("agents.visibility.workspace")} onPress={() => setVisibility("workspace")} />
            </OptionGroup>
            <OptionGroup label={t("agents.runtime")}>
              <PickerTrigger
                disabled={runtimesLoading || ownRuntimes.length === 0}
                label={
                  runtimesLoading
                    ? t("agents.loading_runtimes")
                    : selectedRuntime?.name ?? t("agents.no_runtime_available")
                }
                meta={selectedRuntime ? getRuntimeOwnerLabel(selectedRuntime, members, t) : t("agents.register_runtime_first")}
                onPress={() => setRuntimePickerOpen(true)}
              />
            </OptionGroup>
            <FormField label={t("agents.model")} onChangeText={setModel} placeholder={t("agents.default_model")} value={model} />
            {ownRuntimes.length === 0 && !runtimesLoading ? (
              <View style={styles.warningNotice}>
                <Text style={styles.warningNoticeText}>
                  {t("agents.register_runtime_notice")}
                </Text>
              </View>
            ) : null}
            {error ? <Text style={styles.errorText}>{error}</Text> : null}
            <Button disabled={creating || !name.trim() || !selectedRuntime} onPress={() => void submit()}>
              {creating ? t("agents.creating") : t("agents.create")}
            </Button>
          </ScrollView>
        </KeyboardAvoidingView>
        <RuntimePickerModal
          onClose={() => setRuntimePickerOpen(false)}
          onSelect={setRuntimeId}
          open={runtimePickerOpen}
          runtimes={ownRuntimes}
          selectedId={selectedRuntime?.id ?? ""}
          members={members}
        />
      </Screen>
    </Modal>
  );
}

function RuntimePickerModal({
  members,
  onClose,
  onSelect,
  open,
  runtimes,
  selectedId,
}: {
  members: MemberWithUser[];
  onClose: () => void;
  onSelect: (runtimeId: string) => void;
  open: boolean;
  runtimes: RuntimeDevice[];
  selectedId: string;
}) {
  const { t } = useTranslation();
  return (
    <SheetModal onClose={onClose} open={open} title={t("agents.runtime")}>
      {runtimes.length === 0 ? (
        <Text style={styles.pickerEmpty}>{t("agents.no_runtimes_available")}</Text>
      ) : (
        runtimes.map((runtime) => (
          <PickerRow
            key={runtime.id}
            label={runtime.name}
            meta={`${runtime.provider} / ${getRuntimeOwnerLabel(runtime, members, t)}`}
            onPress={() => {
              onSelect(runtime.id);
              onClose();
            }}
            selected={selectedId === runtime.id}
          />
        ))
      )}
    </SheetModal>
  );
}

function OwnerPickerModal({
  members,
  onChange,
  onClose,
  open,
  value,
}: {
  members: MemberWithUser[];
  onChange: (ownerId: string | null) => void;
  onClose: () => void;
  open: boolean;
  value: string | null;
}) {
  const { t } = useTranslation();
  return (
    <SheetModal onClose={onClose} open={open} title={t("agents.owner")}>
      <PickerRow
        label={t("agents.all_owners")}
        onPress={() => {
          onChange(null);
          onClose();
        }}
        selected={!value}
      />
      {members.map((member) => (
        <PickerRow
          key={member.user_id}
          label={member.name}
          meta={member.email}
          onPress={() => {
            onChange(member.user_id);
            onClose();
          }}
          selected={value === member.user_id}
        />
      ))}
    </SheetModal>
  );
}

function SkillPickerModal({
  onClose,
  onSelect,
  open,
  saving,
  skills,
}: {
  onClose: () => void;
  onSelect: (skillId: string) => void;
  open: boolean;
  saving: boolean;
  skills: Skill[];
}) {
  const { t } = useTranslation();
  return (
    <SheetModal onClose={onClose} open={open} title={t("agents.add_skill")}>
      {skills.length === 0 ? (
        <Text style={styles.pickerEmpty}>{t("agents.all_skills_assigned")}</Text>
      ) : (
        skills.map((skill) => (
          <PickerRow
            disabled={saving}
            key={skill.id}
            label={skill.name}
            meta={skill.description}
            onPress={() => onSelect(skill.id)}
            selected={false}
          />
        ))
      )}
    </SheetModal>
  );
}

function SheetModal({
  children,
  onClose,
  open,
  title,
}: {
  children: React.ReactNode;
  onClose: () => void;
  open: boolean;
  title: string;
}) {
  const { t } = useTranslation();
  return (
    <Modal animationType="fade" onRequestClose={onClose} transparent visible={open}>
      <View style={styles.sheetRoot}>
        <Pressable onPress={onClose} style={styles.sheetBackdrop} />
        <View style={styles.sheet}>
          <View style={styles.sheetHeader}>
            <Text style={styles.sheetTitle}>{title}</Text>
            <Button onPress={onClose} variant="ghost">{t("common.close")}</Button>
          </View>
          <ScrollView contentContainerStyle={styles.sheetContent}>
            {children}
          </ScrollView>
        </View>
      </View>
    </Modal>
  );
}

function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
}: {
  options: Array<{ label: string; value: T }>;
  value: T;
  onChange: (value: T) => void;
}) {
  return (
    <View style={styles.segmented}>
      {options.map((option) => {
        const active = value === option.value;
        return (
          <Pressable
            accessibilityRole="button"
            key={option.value}
            onPress={() => onChange(option.value)}
            style={({ pressed }) => [
              styles.segment,
              active && styles.segmentActive,
              pressed && styles.pressed,
            ]}
          >
            <Text style={[styles.segmentText, active && styles.segmentTextActive]}>
              {option.label}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}

function FormField({
  label,
  editable = true,
  style,
  ...props
}: TextInputProps & { label: string }) {
  return (
    <View style={styles.formField}>
      <Text style={styles.optionLabel}>{label}</Text>
      <TextInput
        editable={editable}
        placeholderTextColor={colors.mutedForeground}
        {...props}
        style={[styles.input, !editable && styles.inputReadOnly, style]}
      />
    </View>
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
  disabled,
  icon: Icon,
  label,
  onPress,
}: {
  active: boolean;
  disabled?: boolean;
  icon?: typeof Bot;
  label: string;
  onPress: () => void;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      disabled={disabled}
      onPress={onPress}
      style={({ pressed }) => [
        styles.optionChip,
        active && styles.optionChipActive,
        disabled && styles.disabled,
        pressed && !disabled && styles.pressed,
      ]}
    >
      {Icon ? <Icon color={active ? colors.primaryForeground : colors.mutedForeground} size={15} /> : null}
      <Text style={[styles.optionChipText, active && styles.optionChipTextActive]}>{label}</Text>
    </Pressable>
  );
}

function PickerTrigger({
  disabled,
  label,
  meta,
  onPress,
}: {
  disabled?: boolean;
  label: string;
  meta?: string;
  onPress: () => void;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      disabled={disabled}
      onPress={onPress}
      style={({ pressed }) => [
        styles.pickerTrigger,
        disabled && styles.disabled,
        pressed && !disabled && styles.pressed,
      ]}
    >
      <View style={styles.pickerTriggerTextWrap}>
        <Text numberOfLines={1} style={styles.pickerTriggerLabel}>{label}</Text>
        {meta ? <Text numberOfLines={1} style={styles.pickerTriggerMeta}>{meta}</Text> : null}
      </View>
      <ChevronDown color={colors.mutedForeground} size={16} />
    </Pressable>
  );
}

function PickerRow({
  disabled,
  label,
  meta,
  onPress,
  selected,
}: {
  disabled?: boolean;
  label: string;
  meta?: string;
  onPress: () => void;
  selected: boolean;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      disabled={disabled}
      onPress={onPress}
      style={({ pressed }) => [
        styles.pickerRow,
        selected && styles.pickerRowSelected,
        disabled && styles.disabled,
        pressed && !disabled && styles.pressed,
      ]}
    >
      <View style={styles.pickerRowText}>
        <Text numberOfLines={1} style={styles.pickerRowLabel}>{label}</Text>
        {meta ? <Text numberOfLines={1} style={styles.pickerRowMeta}>{meta}</Text> : null}
      </View>
      {selected ? <CheckCircle2 color={colors.success} size={18} /> : null}
    </Pressable>
  );
}

function StatusBadge({ status }: { status: AgentStatus }) {
  const { t } = useTranslation();
  const meta = statusMeta[status];
  return (
    <View style={styles.statusBadge}>
      <View style={[styles.statusDot, { backgroundColor: meta.color, opacity: meta.muted ? 0.55 : 1 }]} />
      <Text style={[styles.statusText, { color: meta.color }]}>{formatAgentStatus(t, status)}</Text>
    </View>
  );
}

function RuntimeBadge({ mode, label }: { mode: AgentRuntimeMode; label: string }) {
  const Icon = mode === "cloud" ? Cloud : Monitor;
  return (
    <View style={styles.metaBadge}>
      <Icon color={colors.mutedForeground} size={13} />
      <Text numberOfLines={1} style={styles.metaBadgeText}>{label}</Text>
    </View>
  );
}

function VisibilityBadge({ visibility }: { visibility: AgentVisibility }) {
  const { t } = useTranslation();
  const Icon = visibility === "workspace" ? Globe : Lock;
  return (
    <View style={styles.metaBadge}>
      <Icon color={colors.mutedForeground} size={13} />
      <Text style={styles.metaBadgeText}>{formatAgentVisibility(t, visibility)}</Text>
    </View>
  );
}

function Badge({ label, muted }: { label: string; muted?: boolean }) {
  return (
    <View style={[styles.badge, muted && styles.badgeMuted]}>
      <Text style={[styles.badgeText, muted && styles.badgeTextMuted]}>{label}</Text>
    </View>
  );
}

function InfoCell({ label, value }: { label: string; value: string }) {
  return (
    <View style={styles.infoCell}>
      <Text style={styles.infoLabel}>{label}</Text>
      <Text numberOfLines={2} style={styles.infoValue}>{value}</Text>
    </View>
  );
}

function IconAction({
  accessibilityLabel,
  destructive,
  icon: Icon,
  onPress,
}: {
  accessibilityLabel: string;
  destructive?: boolean;
  icon: typeof Bot;
  onPress: () => void;
}) {
  return (
    <Pressable
      accessibilityLabel={accessibilityLabel}
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [styles.titleIconButton, pressed && styles.pressed]}
    >
      <Icon color={destructive ? colors.destructive : colors.foreground} size={19} />
    </Pressable>
  );
}

function InlineLoading() {
  return (
    <View style={styles.inlineState}>
      <ActivityIndicator color={colors.foreground} />
    </View>
  );
}

function InlineEmpty({ title, detail }: { title: string; detail?: string }) {
  return (
    <View style={styles.inlineState}>
      <Bot color={colors.mutedForeground} size={24} />
      <Text style={styles.emptyTitle}>{title}</Text>
      {detail ? <Text style={styles.emptyDetail}>{detail}</Text> : null}
    </View>
  );
}

function getRuntimeOwnerLabel(
  runtime: RuntimeDevice,
  members: MemberWithUser[],
  t: (key: string, options?: Record<string, unknown>) => string,
) {
  const owner = runtime.owner_id
    ? members.find((member) => member.user_id === runtime.owner_id)
    : null;
  return owner?.name ?? runtime.device_info ?? t("common.unknown");
}

function formatTaskTime(task: AgentTask) {
  const source =
    task.status === "running" && task.started_at
      ? task.started_at
      : task.status === "dispatched" && task.dispatched_at
        ? task.dispatched_at
        : (task.status === "completed" || task.status === "failed") && task.completed_at
          ? task.completed_at
          : task.created_at;
  return new Date(source).toLocaleString();
}

function envMapToText(env: Record<string, string>) {
  return Object.entries(env)
    .map(([key, value]) => `${key}=${value}`)
    .join("\n");
}

function parseEnvText(value: string) {
  const result: Record<string, string> = {};
  for (const rawLine of value.split("\n")) {
    const line = rawLine.trim();
    if (!line) continue;
    const index = line.indexOf("=");
    if (index <= 0) throw new Error(`Invalid env line: ${line}`);
    const key = line.slice(0, index).trim();
    const envValue = line.slice(index + 1);
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(key)) throw new Error(`Invalid env key: ${key}`);
    result[key] = envValue;
  }
  return result;
}

const styles = StyleSheet.create({
  archiveBanner: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.sm,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
  },
  archiveBannerText: {
    color: colors.mutedForeground,
    flex: 1,
    fontSize: 12,
  },
  agentAvatar: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    height: 40,
    justifyContent: "center",
    width: 40,
  },
  agentCard: {
    alignItems: "flex-start",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.md,
    marginBottom: spacing.sm,
    padding: spacing.md,
  },
  agentCardText: {
    flex: 1,
    minWidth: 0,
  },
  agentDescription: {
    color: colors.mutedForeground,
    fontSize: 12,
    marginTop: 3,
  },
  agentMetaLine: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.xs,
    marginTop: spacing.sm,
  },
  agentMetaText: {
    color: colors.mutedForeground,
    fontSize: 12,
    maxWidth: 110,
  },
  agentName: {
    color: colors.foreground,
    flex: 1,
    fontSize: 15,
    fontWeight: "600",
  },
  agentTitleLine: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
  },
  archivedDim: {
    opacity: 0.45,
  },
  archivedText: {
    color: colors.mutedForeground,
  },
  badge: {
    backgroundColor: colors.primary,
    borderRadius: radii.sm,
    paddingHorizontal: spacing.xs,
    paddingVertical: 2,
  },
  badgeMuted: {
    backgroundColor: colors.muted,
  },
  badgeText: {
    color: colors.primaryForeground,
    fontSize: 11,
    fontWeight: "600",
  },
  badgeTextMuted: {
    color: colors.mutedForeground,
  },
  detailContent: {
    gap: spacing.lg,
    padding: spacing.lg,
    paddingBottom: spacing.xl,
  },
  detailHeaderActions: {
    flexDirection: "row",
    gap: spacing.xs,
  },
  disabled: {
    opacity: 0.45,
  },
  emptyButton: {
    marginTop: spacing.sm,
  },
  emptyDetail: {
    color: colors.mutedForeground,
    fontSize: 13,
    lineHeight: 18,
    textAlign: "center",
  },
  emptyList: {
    flexGrow: 1,
  },
  emptyState: {
    alignItems: "center",
    flex: 1,
    gap: spacing.sm,
    justifyContent: "center",
    padding: spacing.xl,
  },
  emptyTitle: {
    color: colors.foreground,
    fontSize: 15,
    fontWeight: "600",
  },
  errorText: {
    color: colors.destructive,
    fontSize: 13,
    lineHeight: 18,
  },
  filterButton: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    flexDirection: "row",
    gap: spacing.xs,
    minHeight: 32,
    paddingHorizontal: spacing.sm,
  },
  filterButtonActive: {
    backgroundColor: colors.primary,
  },
  filterButtonText: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "600",
    maxWidth: 80,
  },
  filterButtonTextActive: {
    color: colors.primaryForeground,
  },
  formField: {
    gap: spacing.xs,
  },
  heroCard: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.md,
    padding: spacing.md,
  },
  heroIcon: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    height: 48,
    justifyContent: "center",
    width: 48,
  },
  heroSubtitle: {
    color: colors.mutedForeground,
    fontSize: 13,
    lineHeight: 18,
  },
  heroText: {
    flex: 1,
    minWidth: 0,
  },
  heroTitle: {
    color: colors.foreground,
    fontSize: 17,
    fontWeight: "600",
  },
  infoCell: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexBasis: "48%",
    flexGrow: 1,
    gap: spacing.xs,
    minHeight: 72,
    padding: spacing.md,
  },
  infoGrid: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.sm,
  },
  infoLabel: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  infoNotice: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    padding: spacing.md,
  },
  infoNoticeText: {
    color: colors.mutedForeground,
    fontSize: 12,
    lineHeight: 18,
  },
  infoValue: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "600",
  },
  inlineState: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderStyle: "dashed",
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.sm,
    justifyContent: "center",
    minHeight: 150,
    padding: spacing.lg,
  },
  input: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    color: colors.foreground,
    fontSize: 15,
    minHeight: 44,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  inputReadOnly: {
    backgroundColor: colors.muted,
    color: colors.mutedForeground,
  },
  instructionsInput: {
    minHeight: 260,
  },
  keyboardAvoiding: {
    flex: 1,
  },
  list: {
    padding: spacing.lg,
    paddingBottom: spacing.xl,
  },
  metaBadge: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.sm,
    flexDirection: "row",
    gap: 3,
    maxWidth: 150,
    paddingHorizontal: spacing.xs,
    paddingVertical: 3,
  },
  metaBadgeText: {
    color: colors.mutedForeground,
    fontSize: 11,
    fontWeight: "500",
  },
  optionChip: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    flexDirection: "row",
    gap: spacing.xs,
    minHeight: 36,
    paddingHorizontal: spacing.md,
  },
  optionChipActive: {
    backgroundColor: colors.primary,
  },
  optionChipText: {
    color: colors.foreground,
    fontSize: 13,
    fontWeight: "500",
  },
  optionChipTextActive: {
    color: colors.primaryForeground,
  },
  optionGroup: {
    gap: spacing.xs,
  },
  optionLabel: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "600",
  },
  optionRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.sm,
  },
  pickerEmpty: {
    color: colors.mutedForeground,
    fontSize: 13,
    padding: spacing.lg,
    textAlign: "center",
  },
  pickerRow: {
    alignItems: "center",
    borderRadius: radii.md,
    flexDirection: "row",
    gap: spacing.md,
    minHeight: 52,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  pickerRowLabel: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
  },
  pickerRowMeta: {
    color: colors.mutedForeground,
    fontSize: 12,
    marginTop: 2,
  },
  pickerRowSelected: {
    backgroundColor: colors.muted,
  },
  pickerRowText: {
    flex: 1,
    minWidth: 0,
  },
  pickerTrigger: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.sm,
    minHeight: 52,
    paddingHorizontal: spacing.md,
  },
  pickerTriggerLabel: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "500",
  },
  pickerTriggerMeta: {
    color: colors.mutedForeground,
    fontSize: 12,
    marginTop: 2,
  },
  pickerTriggerTextWrap: {
    flex: 1,
    minWidth: 0,
  },
  pressed: {
    opacity: 0.72,
  },
  removeButton: {
    alignItems: "center",
    borderRadius: radii.md,
    height: 34,
    justifyContent: "center",
    width: 34,
  },
  sectionDetail: {
    color: colors.mutedForeground,
    fontSize: 13,
    lineHeight: 18,
  },
  sectionHeaderLine: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.md,
    justifyContent: "space-between",
  },
  sectionHeaderText: {
    flex: 1,
    minWidth: 0,
  },
  sectionStack: {
    gap: spacing.md,
  },
  sectionTitle: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "600",
  },
  segmented: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    flexDirection: "row",
    padding: 2,
  },
  segment: {
    borderRadius: radii.sm,
    minHeight: 30,
    paddingHorizontal: spacing.md,
    justifyContent: "center",
  },
  segmentActive: {
    backgroundColor: colors.card,
  },
  segmentText: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "600",
  },
  segmentTextActive: {
    color: colors.foreground,
  },
  sheet: {
    backgroundColor: colors.card,
    borderTopLeftRadius: radii.md,
    borderTopRightRadius: radii.md,
    maxHeight: "72%",
    paddingBottom: spacing.lg,
  },
  sheetBackdrop: {
    flex: 1,
  },
  sheetContent: {
    paddingHorizontal: spacing.sm,
    paddingBottom: spacing.lg,
  },
  sheetHeader: {
    alignItems: "center",
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    justifyContent: "space-between",
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
  },
  sheetRoot: {
    backgroundColor: "rgba(0,0,0,0.25)",
    flex: 1,
    justifyContent: "flex-end",
  },
  sheetTitle: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "600",
  },
  skillDescription: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
  skillIcon: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    height: 36,
    justifyContent: "center",
    width: 36,
  },
  skillName: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "600",
  },
  skillRow: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.md,
    padding: spacing.md,
  },
  skillText: {
    flex: 1,
    minWidth: 0,
  },
  smallActionButton: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    height: 36,
    justifyContent: "center",
    width: 36,
  },
  statusBadge: {
    alignItems: "center",
    flexDirection: "row",
    gap: 4,
  },
  statusDot: {
    borderRadius: 4,
    height: 8,
    width: 8,
  },
  statusText: {
    fontSize: 11,
    fontWeight: "600",
  },
  tabChip: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    flexDirection: "row",
    gap: spacing.xs,
    height: 34,
    paddingHorizontal: spacing.md,
  },
  tabChipActive: {
    backgroundColor: colors.primary,
  },
  tabChipText: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "600",
  },
  tabChipTextActive: {
    color: colors.primaryForeground,
  },
  tabsRow: {
    flexDirection: "row",
    gap: spacing.sm,
    padding: spacing.md,
  },
  tabsScroller: {
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexGrow: 0,
  },
  taskMeta: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
  taskRow: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.md,
    padding: spacing.md,
  },
  taskStatusDot: {
    borderRadius: 5,
    height: 10,
    width: 10,
  },
  taskStatusLabel: {
    fontSize: 12,
    fontWeight: "600",
  },
  taskText: {
    flex: 1,
    minWidth: 0,
  },
  taskTitle: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "600",
  },
  textArea: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    color: colors.foreground,
    fontSize: 14,
    lineHeight: 20,
    minHeight: 140,
    padding: spacing.md,
  },
  titleIconButton: {
    alignItems: "center",
    borderRadius: radii.md,
    height: 36,
    justifyContent: "center",
    width: 36,
  },
  toolbar: {
    alignItems: "center",
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    justifyContent: "space-between",
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
  },
  toolbarActions: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
  },
  warningNotice: {
    backgroundColor: "#fff7ed",
    borderColor: "#fed7aa",
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    padding: spacing.md,
  },
  warningNoticeText: {
    color: colors.warning,
    fontSize: 12,
    lineHeight: 18,
  },
});
