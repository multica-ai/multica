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

type AgentsNavigation = NativeStackNavigationProp<RootStackParamList>;
type AgentScope = "mine" | "all";
type DetailTab = "overview" | "instructions" | "tasks" | "skills" | "settings" | "advanced";

const detailTabs: Array<{ id: DetailTab; label: string; icon: typeof Bot }> = [
  { id: "overview", label: "Overview", icon: Bot },
  { id: "instructions", label: "Instructions", icon: FileText },
  { id: "tasks", label: "Tasks", icon: ListTodo },
  { id: "skills", label: "Skills", icon: BookOpenText },
  { id: "settings", label: "Settings", icon: Settings },
  { id: "advanced", label: "Advanced", icon: Terminal },
];

const statusMeta: Record<AgentStatus, { label: string; color: string; muted?: boolean }> = {
  idle: { label: "Idle", color: colors.mutedForeground },
  working: { label: "Working", color: colors.success },
  blocked: { label: "Blocked", color: colors.warning },
  error: { label: "Error", color: colors.destructive },
  offline: { label: "Offline", color: colors.mutedForeground, muted: true },
};

const taskStatusMeta: Record<AgentTask["status"], { label: string; color: string }> = {
  queued: { label: "Queued", color: colors.mutedForeground },
  dispatched: { label: "Dispatched", color: colors.info },
  running: { label: "Running", color: colors.success },
  completed: { label: "Completed", color: colors.success },
  failed: { label: "Failed", color: colors.destructive },
  cancelled: { label: "Cancelled", color: colors.mutedForeground },
};

export function AgentsScreen() {
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
    Alert.alert("Archive agent?", `"${agent.name}" will stop being assignable.`, [
      { text: "Cancel", style: "cancel" },
      {
        text: "Archive",
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
        <ScreenTitleBar onBack={() => navigation.goBack()} title="Agents" />
        <EmptyState detail="Pull to retry once the connection is available." title="Unable to load agents" />
      </Screen>
    );
  }

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar
        onBack={() => navigation.goBack()}
        right={
          <Pressable
            accessibilityLabel="Create agent"
            accessibilityRole="button"
            onPress={() => setCreateOpen(true)}
            style={({ pressed }) => [styles.titleIconButton, pressed && styles.pressed]}
          >
            <Plus color={colors.foreground} size={20} />
          </Pressable>
        }
        title="Agents"
      />
      <View style={styles.toolbar}>
        <SegmentedControl
          onChange={(next) => {
            setScope(next);
            setOwnerFilter(null);
          }}
          options={[
            { label: "Mine", value: "mine" },
            { label: "All", value: "all" },
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
                {selectedOwner?.name ?? "Owner"}
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
                {showArchived ? "Archived" : `${archivedCount}`}
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
  const archived = !!agent.archived_at;
  const runtimeLabel = runtime?.name ?? (agent.runtime_mode === "cloud" ? "Cloud" : "Local");

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
            <Badge label="Archived" muted />
          ) : (
            <StatusBadge status={agent.status} />
          )}
        </View>
        <Text numberOfLines={1} style={styles.agentDescription}>
          {agent.description || "No description"}
        </Text>
        <View style={styles.agentMetaLine}>
          <RuntimeBadge mode={agent.runtime_mode} label={runtimeLabel} />
          <VisibilityBadge visibility={agent.visibility} />
          {showOwner ? <Text numberOfLines={1} style={styles.agentMetaText}>{owner?.name ?? "Unknown"}</Text> : null}
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
  const title = showArchived
    ? "No archived agents"
    : ownerFiltered
      ? "No agents for this owner"
      : scope === "mine"
        ? "No agents owned by you"
        : "No agents yet";

  return (
    <View style={styles.emptyState}>
      <Bot color={colors.mutedForeground} size={30} />
      <Text style={styles.emptyTitle}>{title}</Text>
      {!showArchived ? (
        <>
          <Text style={styles.emptyDetail}>
            {activeCount > 0
              ? "Adjust filters to see other agents."
              : "Create an agent from one of your runtimes."}
          </Text>
          <Button onPress={onCreate} style={styles.emptyButton}>
            Create agent
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
                  accessibilityLabel="Duplicate agent"
                  icon={Copy}
                  onPress={() => void onDuplicate(agent)}
                />
              ) : null}
              {isArchived && isOwner ? (
                <IconAction
                  accessibilityLabel="Restore agent"
                  icon={RotateCcw}
                  onPress={() => void onRestore(agent)}
                />
              ) : null}
              {!isArchived && isOwner ? (
                <IconAction
                  accessibilityLabel="Archive agent"
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
              This agent is archived and cannot be assigned or mentioned.
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
                    {tab.label}
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
  return (
    <View style={styles.sectionStack}>
      <View style={styles.heroCard}>
        <View style={styles.heroIcon}>
          <Bot color={colors.foreground} size={24} />
        </View>
        <View style={styles.heroText}>
          <Text numberOfLines={1} style={styles.heroTitle}>{agent.name}</Text>
          <Text style={styles.heroSubtitle}>{agent.description || "No description"}</Text>
        </View>
      </View>
      <View style={styles.infoGrid}>
        <InfoCell label="Status" value={agent.archived_at ? "Archived" : statusMeta[agent.status].label} />
        <InfoCell label="Visibility" value={agent.visibility === "workspace" ? "Workspace" : "Private"} />
        <InfoCell label="Runtime" value={runtime?.name ?? (agent.runtime_mode === "cloud" ? "Cloud" : "Local")} />
        <InfoCell label="Mode" value={agent.runtime_mode === "cloud" ? "Cloud" : "Local"} />
        <InfoCell label="Owner" value={owner?.name ?? "Unknown"} />
        <InfoCell label="Model" value={agent.model || "Default"} />
        <InfoCell label="Max tasks" value={String(agent.max_concurrent_tasks)} />
        <InfoCell label="Skills" value={String(agent.skills.length)} />
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
      setError(err instanceof Error ? err.message : "Unable to save instructions");
    } finally {
      setSaving(false);
    }
  }

  return (
    <View style={styles.sectionStack}>
      <Text style={styles.sectionTitle}>Instructions</Text>
      <Text style={styles.sectionDetail}>
        {"Define this agent's identity and working style."}
      </Text>
      <TextInput
        editable={!readOnly}
        multiline
        onChangeText={setValue}
        placeholder={readOnly ? "No instructions set" : "Write instructions..."}
        placeholderTextColor={colors.mutedForeground}
        style={[styles.textArea, styles.instructionsInput, readOnly && styles.inputReadOnly]}
        textAlignVertical="top"
        value={value}
      />
      {error ? <Text style={styles.errorText}>{error}</Text> : null}
      {!readOnly ? (
        <Button disabled={saving || value === (agent.instructions ?? "")} onPress={() => void save()}>
          {saving ? "Saving..." : "Save instructions"}
        </Button>
      ) : null}
    </View>
  );
}

function TasksTab({ agent, workspaceId }: { agent: Agent; workspaceId: string }) {
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
    return <InlineEmpty detail="Pull to retry once the connection is available." title="Unable to load tasks" />;
  }

  return (
    <View style={styles.sectionStack}>
      <View style={styles.sectionHeaderLine}>
        <View>
          <Text style={styles.sectionTitle}>Task Queue</Text>
          <Text style={styles.sectionDetail}>Assigned issue execution status.</Text>
        </View>
        {isRefetching ? <ActivityIndicator color={colors.mutedForeground} /> : null}
      </View>
      {sortedTasks.length === 0 ? (
        <InlineEmpty detail="Assign an issue to this agent to get started." title="No tasks in queue" />
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
        Refresh tasks
      </Button>
    </View>
  );
}

function TaskRow({ task, onPress }: { task: AgentTask; onPress?: () => void }) {
  const meta = taskStatusMeta[task.status];
  const content = (
    <>
      <View style={[styles.taskStatusDot, { backgroundColor: meta.color }]} />
      <View style={styles.taskText}>
        <Text numberOfLines={1} style={styles.taskTitle}>
          {task.issue_id ? `Issue ${task.issue_id.slice(0, 8)}` : "Task without linked issue"}
        </Text>
        <Text style={styles.taskMeta}>{formatTaskTime(task)}</Text>
      </View>
      <Text style={[styles.taskStatusLabel, { color: meta.color }]}>{meta.label}</Text>
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
      setError(err instanceof Error ? err.message : "Unable to update skills");
    } finally {
      setSaving(false);
      setPickerOpen(false);
    }
  }

  return (
    <View style={styles.sectionStack}>
      <View style={styles.sectionHeaderLine}>
        <View style={styles.sectionHeaderText}>
          <Text style={styles.sectionTitle}>Skills</Text>
          <Text style={styles.sectionDetail}>Workspace skills assigned to this agent.</Text>
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
          Local runtime skills are available automatically. Add workspace skills here for shared team knowledge.
        </Text>
      </View>
      {isLoading ? <InlineLoading /> : null}
      {!isLoading && agent.skills.length === 0 ? (
        <InlineEmpty detail="No workspace skills are assigned." title="No skills assigned" />
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
      setError("Name is required.");
      return;
    }
    if (!Number.isInteger(parsedMaxTasks) || parsedMaxTasks < 1) {
      setError("Max tasks must be a positive integer.");
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
      setError(err instanceof Error ? err.message : "Unable to save settings");
    } finally {
      setSaving(false);
    }
  }

  return (
    <View style={styles.sectionStack}>
      <Text style={styles.sectionTitle}>Settings</Text>
      <FormField editable={!readOnly} label="Name" onChangeText={setName} value={name} />
      <FormField
        editable={!readOnly}
        label="Description"
        onChangeText={setDescription}
        placeholder="What does this agent do?"
        value={description}
      />
      <OptionGroup label="Visibility">
        <OptionChip
          active={visibility === "private"}
          disabled={readOnly}
          icon={Lock}
          label="Private"
          onPress={() => setVisibility("private")}
        />
        <OptionChip
          active={visibility === "workspace"}
          disabled={readOnly}
          icon={Globe}
          label="Workspace"
          onPress={() => setVisibility("workspace")}
        />
      </OptionGroup>
      <OptionGroup label="Runtime">
        <PickerTrigger
          disabled={readOnly}
          label={selectedRuntime?.name ?? "No runtime"}
          meta={selectedRuntime ? getRuntimeOwnerLabel(selectedRuntime, members) : "Select"}
          onPress={() => setRuntimePickerOpen(true)}
        />
      </OptionGroup>
      <FormField editable={!readOnly} label="Model" onChangeText={setModel} placeholder="Default" value={model} />
      <FormField
        editable={!readOnly}
        keyboardType="number-pad"
        label="Max concurrent tasks"
        onChangeText={setMaxTasks}
        value={maxTasks}
      />
      {error ? <Text style={styles.errorText}>{error}</Text> : null}
      {!readOnly ? (
        <Button disabled={saving || !dirty} onPress={() => void save()}>
          {saving ? "Saving..." : "Save settings"}
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
    } catch (err) {
      setError(err instanceof Error ? err.message : "Invalid environment variables");
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
      setError(err instanceof Error ? err.message : "Unable to save advanced settings");
    } finally {
      setSaving(false);
    }
  }

  return (
    <View style={styles.sectionStack}>
      <Text style={styles.sectionTitle}>Advanced</Text>
      {agent.custom_env_copied_pending ? (
        <View style={styles.warningNotice}>
          <Text style={styles.warningNoticeText}>
            This duplicated agent needs fresh secret values before launch.
          </Text>
        </View>
      ) : null}
      <Text style={styles.optionLabel}>Environment</Text>
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
        value={agent.custom_env_redacted ? "Values are hidden for this agent." : envText}
      />
      <Text style={styles.optionLabel}>Custom args</Text>
      <TextInput
        autoCapitalize="none"
        autoCorrect={false}
        editable={!readOnly}
        multiline
        onChangeText={setArgsText}
        placeholder="One argument per line"
        placeholderTextColor={colors.mutedForeground}
        style={[styles.textArea, readOnly && styles.inputReadOnly]}
        textAlignVertical="top"
        value={argsText}
      />
      {error ? <Text style={styles.errorText}>{error}</Text> : null}
      {!readOnly ? (
        <Button disabled={saving || !dirty} onPress={() => void save()}>
          {saving ? "Saving..." : "Save advanced settings"}
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
      setError(err instanceof Error ? err.message : "Unable to create agent");
    } finally {
      setCreating(false);
    }
  }

  return (
    <Modal animationType="slide" onRequestClose={onClose} visible={open}>
      <Screen padded={false} safeArea={false}>
        <ScreenTitleBar onBack={onClose} title="New agent" />
        <KeyboardAvoidingView behavior={Platform.OS === "ios" ? "padding" : undefined} style={styles.keyboardAvoiding}>
          <ScrollView contentContainerStyle={styles.detailContent} keyboardShouldPersistTaps="handled">
            <Text style={styles.sectionTitle}>Create Agent</Text>
            <Text style={styles.sectionDetail}>Create a workspace agent from one of your runtimes.</Text>
            <FormField autoFocus label="Name" onChangeText={setName} placeholder="Deep Research Agent" value={name} />
            <FormField label="Description" onChangeText={setDescription} placeholder="What does this agent do?" value={description} />
            <OptionGroup label="Visibility">
              <OptionChip active={visibility === "private"} icon={Lock} label="Private" onPress={() => setVisibility("private")} />
              <OptionChip active={visibility === "workspace"} icon={Globe} label="Workspace" onPress={() => setVisibility("workspace")} />
            </OptionGroup>
            <OptionGroup label="Runtime">
              <PickerTrigger
                disabled={runtimesLoading || ownRuntimes.length === 0}
                label={
                  runtimesLoading
                    ? "Loading runtimes..."
                    : selectedRuntime?.name ?? "No runtime available"
                }
                meta={selectedRuntime ? getRuntimeOwnerLabel(selectedRuntime, members) : "Register a runtime first"}
                onPress={() => setRuntimePickerOpen(true)}
              />
            </OptionGroup>
            <FormField label="Model" onChangeText={setModel} placeholder="Default" value={model} />
            {ownRuntimes.length === 0 && !runtimesLoading ? (
              <View style={styles.warningNotice}>
                <Text style={styles.warningNoticeText}>
                  Register a runtime before creating an agent.
                </Text>
              </View>
            ) : null}
            {error ? <Text style={styles.errorText}>{error}</Text> : null}
            <Button disabled={creating || !name.trim() || !selectedRuntime} onPress={() => void submit()}>
              {creating ? "Creating..." : "Create agent"}
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
  return (
    <SheetModal onClose={onClose} open={open} title="Runtime">
      {runtimes.length === 0 ? (
        <Text style={styles.pickerEmpty}>No runtimes available</Text>
      ) : (
        runtimes.map((runtime) => (
          <PickerRow
            key={runtime.id}
            label={runtime.name}
            meta={`${runtime.provider} / ${getRuntimeOwnerLabel(runtime, members)}`}
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
  return (
    <SheetModal onClose={onClose} open={open} title="Owner">
      <PickerRow
        label="All owners"
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
  return (
    <SheetModal onClose={onClose} open={open} title="Add skill">
      {skills.length === 0 ? (
        <Text style={styles.pickerEmpty}>All workspace skills are already assigned.</Text>
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
  return (
    <Modal animationType="fade" onRequestClose={onClose} transparent visible={open}>
      <View style={styles.sheetRoot}>
        <Pressable onPress={onClose} style={styles.sheetBackdrop} />
        <View style={styles.sheet}>
          <View style={styles.sheetHeader}>
            <Text style={styles.sheetTitle}>{title}</Text>
            <Button onPress={onClose} variant="ghost">Close</Button>
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
  const meta = statusMeta[status];
  return (
    <View style={styles.statusBadge}>
      <View style={[styles.statusDot, { backgroundColor: meta.color, opacity: meta.muted ? 0.55 : 1 }]} />
      <Text style={[styles.statusText, { color: meta.color }]}>{meta.label}</Text>
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
  const Icon = visibility === "workspace" ? Globe : Lock;
  return (
    <View style={styles.metaBadge}>
      <Icon color={colors.mutedForeground} size={13} />
      <Text style={styles.metaBadgeText}>{visibility === "workspace" ? "Workspace" : "Private"}</Text>
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

function getRuntimeOwnerLabel(runtime: RuntimeDevice, members: MemberWithUser[]) {
  const owner = runtime.owner_id
    ? members.find((member) => member.user_id === runtime.owner_id)
    : null;
  return owner?.name ?? runtime.device_info ?? "Unknown";
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
