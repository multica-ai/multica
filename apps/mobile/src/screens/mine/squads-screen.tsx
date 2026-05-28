import { useEffect, useMemo, useState, type ComponentProps, type ReactNode } from "react";
import {
  Alert,
  FlatList,
  Image,
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import { useNavigation } from "@react-navigation/native";
import type { NativeStackNavigationProp } from "@react-navigation/native-stack";
import { useTranslation } from "react-i18next";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { isAgentSelectable } from "@multica/core/permissions";
import { useCoreQuery, useCoreQueryClient } from "@multica/core/provider";
import {
  agentListOptions,
  memberListOptions,
  squadListOptions,
  squadMemberStatusOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import type {
  Agent,
  CreateSquadRequest,
  MemberWithUser,
  Squad,
  SquadMember,
  SquadMemberStatus,
  SquadMemberType,
  UpdateSquadRequest,
} from "@multica/core/types";
import {
  Bot,
  CheckCircle2,
  FileText,
  Plus,
  Settings,
  Trash2,
  Users,
  X,
} from "lucide-react-native";
import { Button, EmptyState, LoadingState, Screen } from "../../components/ui/primitives";
import { ScreenTitleBar } from "../../components/ui/screen-title-bar";
import { formatAgentStatus } from "../../i18n/format";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";

type SquadsNavigation = NativeStackNavigationProp<RootStackParamList>;
type SquadScope = "mine" | "all";
type SquadTab = "members" | "instructions" | "settings";
type MemberCandidate = {
  id: string;
  type: SquadMemberType;
  label: string;
  meta?: string;
};

const tabs: Array<{ id: SquadTab; labelKey: string; icon: typeof Users }> = [
  { id: "members", labelKey: "squads.members", icon: Users },
  { id: "instructions", labelKey: "squads.instructions", icon: FileText },
  { id: "settings", labelKey: "squads.settings", icon: Settings },
];

const statusColor: Record<NonNullable<SquadMemberStatus["status"]>, string> = {
  working: colors.success,
  idle: colors.mutedForeground,
  offline: colors.mutedForeground,
  unstable: colors.warning,
};

export function SquadsScreen() {
  const { t } = useTranslation();
  const navigation = useNavigation<SquadsNavigation>();
  const qc = useCoreQueryClient();
  const userId = useAuthStore((state) => state.user?.id ?? null);
  const { workspace } = useMobileWorkspace();
  const [scope, setScope] = useState<SquadScope>("mine");
  const [query, setQuery] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const {
    data: squads = [],
    isError,
    isLoading,
    isRefetching,
    refetch,
  } = useCoreQuery(squadListOptions(workspace.id));
  const { data: agents = [] } = useCoreQuery(agentListOptions(workspace.id));
  const { data: members = [] } = useCoreQuery(memberListOptions(workspace.id));
  const currentRole = members.find((member) => member.user_id === userId)?.role ?? null;
  const canManage = currentRole === "owner" || currentRole === "admin";

  const agentsById = useMemo(() => new Map(agents.map((agent) => [agent.id, agent])), [agents]);
  const membersById = useMemo(
    () => new Map(members.map((member) => [member.user_id, member])),
    [members],
  );
  const filteredSquads = useMemo(() => {
    const normalizedQuery = normalizeSearch(query);
    return squads.filter((squad) => {
      if (scope === "mine" && squad.creator_id !== userId) return false;
      if (!normalizedQuery) return true;
      return normalizeSearch(`${squad.name} ${squad.description}`).includes(normalizedQuery);
    });
  }, [query, scope, squads, userId]);
  const selectedSquad = selectedId
    ? squads.find((squad) => squad.id === selectedId) ?? null
    : null;

  function invalidateSquads(squadId?: string) {
    void qc.invalidateQueries({ queryKey: workspaceKeys.squads(workspace.id) });
    if (squadId) {
      void qc.invalidateQueries({ queryKey: [...workspaceKeys.squads(workspace.id), squadId, "members"] });
    }
  }

  async function createSquad(data: CreateSquadRequest, initialMembers: MemberCandidate[]) {
    const squad = await api.createSquad(data);
    if (initialMembers.length > 0) {
      await Promise.allSettled(
        initialMembers.map((member) =>
          api.addSquadMember(squad.id, {
            member_type: member.type,
            member_id: member.id,
          }),
        ),
      );
    }
    invalidateSquads(squad.id);
    setSelectedId(squad.id);
  }

  async function updateSquad(squadId: string, data: UpdateSquadRequest) {
    await api.updateSquad(squadId, data);
    invalidateSquads(squadId);
  }

  async function archiveSquad(squad: Squad) {
    Alert.alert(t("squads.archive_title"), t("squads.archive_description", { name: squad.name }), [
      { text: t("common.cancel"), style: "cancel" },
      {
        text: t("squads.archive"),
        style: "destructive",
        onPress: () => {
          void api.deleteSquad(squad.id).then(() => {
            setSelectedId(null);
            invalidateSquads(squad.id);
          });
        },
      },
    ]);
  }

  if (isLoading) return <LoadingState />;
  if (isError) {
    return (
      <Screen padded={false} safeArea={false}>
        <ScreenTitleBar onBack={() => navigation.goBack()} title={t("squads.title")} />
        <EmptyState detail={t("common.pull_to_retry")} title={t("squads.unable_to_load")} />
      </Screen>
    );
  }

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar
        onBack={() => navigation.goBack()}
        right={
          canManage ? (
            <Pressable
              accessibilityLabel={t("squads.create")}
              accessibilityRole="button"
              onPress={() => setCreateOpen(true)}
              style={({ pressed }) => [styles.titleIconButton, pressed && styles.pressed]}
            >
              <Plus color={colors.foreground} size={20} />
            </Pressable>
          ) : undefined
        }
        title={t("squads.title")}
      />
      <View style={styles.toolbar}>
        <SegmentedControl
          onChange={setScope}
          options={[
            { label: t("squads.scope_mine"), value: "mine" },
            { label: t("squads.scope_all"), value: "all" },
          ]}
          value={scope}
        />
        <TextInput
          autoCapitalize="none"
          autoCorrect={false}
          onChangeText={setQuery}
          placeholder={t("squads.search")}
          placeholderTextColor={colors.mutedForeground}
          style={styles.searchInput}
          value={query}
        />
      </View>
      <FlatList
        contentContainerStyle={filteredSquads.length === 0 ? styles.emptyList : styles.list}
        data={filteredSquads}
        keyExtractor={(squad) => squad.id}
        ListEmptyComponent={
          <SquadsEmpty
            canManage={canManage}
            filtered={!!query.trim()}
            onCreate={() => setCreateOpen(true)}
          />
        }
        onRefresh={() => void refetch()}
        refreshing={isRefetching && !isLoading}
        renderItem={({ item }) => (
          <SquadRow
            creator={membersById.get(item.creator_id) ?? null}
            leader={agentsById.get(item.leader_id) ?? null}
            squad={item}
            onPress={() => setSelectedId(item.id)}
          />
        )}
      />
      <CreateSquadModal
        agents={agents}
        members={members}
        onClose={() => setCreateOpen(false)}
        onCreate={createSquad}
        open={createOpen}
        userId={userId}
      />
      <SquadDetailModal
        agents={agents}
        canManage={canManage}
        members={members}
        onArchive={archiveSquad}
        onClose={() => setSelectedId(null)}
        onRefresh={() => invalidateSquads(selectedSquad?.id)}
        onUpdate={updateSquad}
        open={!!selectedSquad}
        squad={selectedSquad}
        userId={userId}
        workspaceId={workspace.id}
      />
    </Screen>
  );
}

function SquadRow({
  creator,
  leader,
  squad,
  onPress,
}: {
  creator: MemberWithUser | null;
  leader: Agent | null;
  squad: Squad;
  onPress: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [styles.card, pressed && styles.pressed]}
    >
      <SquadAvatar squad={squad} size={42} />
      <View style={styles.cardText}>
        <View style={styles.cardTitleLine}>
          <Text numberOfLines={1} style={styles.cardTitle}>{squad.name}</Text>
          {squad.archived_at ? <Badge label={t("squads.archived")} muted /> : null}
        </View>
        <Text numberOfLines={1} style={styles.cardDescription}>
          {squad.description || t("squads.no_description")}
        </Text>
        <Text numberOfLines={1} style={styles.cardMeta}>
          {t("squads.leader")}: {leader?.name ?? t("common.unknown")} / {t("squads.creator")}: {creator?.name ?? t("common.unknown")}
        </Text>
      </View>
    </Pressable>
  );
}

function SquadsEmpty({
  canManage,
  filtered,
  onCreate,
}: {
  canManage: boolean;
  filtered: boolean;
  onCreate: () => void;
}) {
  const { t } = useTranslation();
  return (
    <View style={styles.emptyState}>
      <Users color={colors.mutedForeground} size={30} />
      <Text style={styles.emptyTitle}>
        {filtered ? t("common.no_results") : t("squads.no_squads")}
      </Text>
      <Text style={styles.emptyDetail}>
        {filtered ? t("squads.empty_filtered") : t("squads.empty_detail")}
      </Text>
      {canManage && !filtered ? (
        <Button onPress={onCreate} style={styles.emptyButton}>
          {t("squads.create")}
        </Button>
      ) : null}
    </View>
  );
}

function SquadDetailModal({
  agents,
  canManage,
  members,
  onArchive,
  onClose,
  onRefresh,
  onUpdate,
  open,
  squad,
  userId,
  workspaceId,
}: {
  agents: Agent[];
  canManage: boolean;
  members: MemberWithUser[];
  onArchive: (squad: Squad) => void;
  onClose: () => void;
  onRefresh: () => void;
  onUpdate: (squadId: string, data: UpdateSquadRequest) => Promise<void>;
  open: boolean;
  squad: Squad | null;
  userId: string | null;
  workspaceId: string;
}) {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState<SquadTab>("members");
  const { data: squadMembers = [], refetch: refetchMembers } = useCoreQuery({
    queryKey: squad ? [...workspaceKeys.squads(workspaceId), squad.id, "members"] : ["squad-members", "none"],
    queryFn: () => api.listSquadMembers(squad?.id ?? ""),
    enabled: !!squad?.id,
  });
  const { data: statusResp } = useCoreQuery(
    squadMemberStatusOptions(workspaceId, squad?.id ?? ""),
  );
  const statusById = useMemo(() => {
    const map = new Map<string, SquadMemberStatus>();
    for (const status of statusResp?.members ?? []) map.set(`${status.member_type}:${status.member_id}`, status);
    return map;
  }, [statusResp]);

  useEffect(() => {
    if (!open) setActiveTab("members");
  }, [open]);

  if (!squad) return null;

  async function refreshMembers() {
    await refetchMembers();
    onRefresh();
  }

  return (
    <Modal animationType="slide" onRequestClose={onClose} visible={open}>
      <Screen padded={false} safeArea={false}>
        <ScreenTitleBar
          onBack={onClose}
          right={
            canManage && !squad.archived_at ? (
              <Pressable
                accessibilityLabel={t("squads.archive")}
                accessibilityRole="button"
                onPress={() => onArchive(squad)}
                style={({ pressed }) => [styles.titleIconButton, pressed && styles.pressed]}
              >
                <Trash2 color={colors.destructive} size={19} />
              </Pressable>
            ) : undefined
          }
          title={squad.name}
        />
        <View style={styles.detailHero}>
          <SquadAvatar squad={squad} size={54} />
          <View style={styles.detailHeroText}>
            <Text numberOfLines={1} style={styles.detailTitle}>{squad.name}</Text>
            <Text style={styles.detailSubtitle}>{squad.description || t("squads.no_description")}</Text>
          </View>
        </View>
        <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.tabsScroller}>
          <View style={styles.tabsRow}>
            {tabs.map((tab) => {
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
        <ScrollView contentContainerStyle={styles.detailContent} style={styles.detailScroll}>
          {activeTab === "members" ? (
            <MembersTab
              agents={agents}
              canManage={canManage && !squad.archived_at}
              members={members}
              onChanged={() => void refreshMembers()}
              squad={squad}
              squadMembers={squadMembers}
              statusById={statusById}
              userId={userId}
            />
          ) : null}
          {activeTab === "instructions" ? (
            <InstructionsTab
              canEdit={canManage && !squad.archived_at}
              onUpdate={onUpdate}
              squad={squad}
            />
          ) : null}
          {activeTab === "settings" ? (
            <SettingsTab
              canEdit={canManage && !squad.archived_at}
              onUpdate={onUpdate}
              squad={squad}
            />
          ) : null}
        </ScrollView>
      </Screen>
    </Modal>
  );
}

function MembersTab({
  agents,
  canManage,
  members,
  onChanged,
  squad,
  squadMembers,
  statusById,
  userId,
}: {
  agents: Agent[];
  canManage: boolean;
  members: MemberWithUser[];
  onChanged: () => void;
  squad: Squad;
  squadMembers: SquadMember[];
  statusById: Map<string, SquadMemberStatus>;
  userId: string | null;
}) {
  const { t } = useTranslation();
  const [addOpen, setAddOpen] = useState(false);
  const [roleMember, setRoleMember] = useState<SquadMember | null>(null);
  const agentsById = useMemo(() => new Map(agents.map((agent) => [agent.id, agent])), [agents]);
  const membersById = useMemo(
    () => new Map(members.map((member) => [member.user_id, member])),
    [members],
  );

  async function removeMember(member: SquadMember) {
    await api.removeSquadMember(squad.id, {
      member_type: member.member_type,
      member_id: member.member_id,
    });
    onChanged();
  }

  async function setLeader(agentId: string) {
    await api.updateSquad(squad.id, { leader_id: agentId });
    onChanged();
  }

  async function updateRole(member: SquadMember, role: string) {
    await api.updateSquadMemberRole(squad.id, {
      member_type: member.member_type,
      member_id: member.member_id,
      role,
    });
    onChanged();
  }

  return (
    <View style={styles.sectionStack}>
      <View style={styles.sectionHeaderLine}>
        <View style={styles.sectionHeaderText}>
          <Text style={styles.sectionTitle}>{t("squads.members")}</Text>
          <Text style={styles.sectionDetail}>{t("squads.members_detail")}</Text>
        </View>
        {canManage ? (
          <Pressable
            accessibilityRole="button"
            onPress={() => setAddOpen(true)}
            style={({ pressed }) => [styles.smallActionButton, pressed && styles.pressed]}
          >
            <Plus color={colors.foreground} size={16} />
          </Pressable>
        ) : null}
      </View>
      {squadMembers.length === 0 ? (
        <InlineEmpty title={t("squads.no_members")} />
      ) : (
        squadMembers.map((member) => {
          const agent = member.member_type === "agent" ? agentsById.get(member.member_id) : null;
          const human = member.member_type === "member" ? membersById.get(member.member_id) : null;
          const name = agent?.name ?? human?.name ?? t("common.unknown");
          return (
            <MemberRow
              canManage={canManage}
              isLeader={member.member_type === "agent" && member.member_id === squad.leader_id}
              key={`${member.member_type}-${member.member_id}`}
              member={member}
              name={name}
              onEditRole={() => setRoleMember(member)}
              onRemove={() => void removeMember(member)}
              onSetLeader={member.member_type === "agent" ? () => void setLeader(member.member_id) : undefined}
              status={statusById.get(`${member.member_type}:${member.member_id}`)}
              typeLabel={member.member_type === "agent" ? t("issues.agent") : t("issues.member")}
            />
          );
        })
      )}
      <AddMemberModal
        agents={agents}
        members={members}
        onAdded={onChanged}
        onClose={() => setAddOpen(false)}
        open={addOpen}
        squad={squad}
        squadMembers={squadMembers}
        userId={userId}
      />
      <RoleEditorModal
        member={roleMember}
        onClose={() => setRoleMember(null)}
        onSave={(role) => {
          if (roleMember) void updateRole(roleMember, role);
          setRoleMember(null);
        }}
      />
    </View>
  );
}

function MemberRow({
  canManage,
  isLeader,
  member,
  name,
  onEditRole,
  onRemove,
  onSetLeader,
  status,
  typeLabel,
}: {
  canManage: boolean;
  isLeader: boolean;
  member: SquadMember;
  name: string;
  onEditRole: () => void;
  onRemove: () => void;
  onSetLeader?: () => void;
  status?: SquadMemberStatus;
  typeLabel: string;
}) {
  const { t } = useTranslation();
  return (
    <View style={styles.memberRow}>
      <View style={styles.memberIcon}>
        {member.member_type === "agent" ? (
          <Bot color={colors.foreground} size={18} />
        ) : (
          <Users color={colors.foreground} size={18} />
        )}
      </View>
      <View style={styles.memberText}>
        <View style={styles.memberTitleLine}>
          <Text numberOfLines={1} style={styles.memberName}>{name}</Text>
          {isLeader ? <Badge label={t("squads.leader")} /> : null}
        </View>
        <Text numberOfLines={2} style={styles.memberMeta}>
          {typeLabel}{member.role ? ` / ${member.role}` : ""}
        </Text>
        {status?.status ? (
          <Text style={[styles.memberStatus, { color: statusColor[status.status] }]}>
            {t(`squads.status.${status.status}`)}{status.active_issues.length > 0 ? ` / ${status.active_issues[0]?.identifier}` : ""}
          </Text>
        ) : null}
      </View>
      {canManage ? (
        <View style={styles.memberActions}>
          {onSetLeader && !isLeader ? (
            <IconButton icon={CheckCircle2} onPress={onSetLeader} />
          ) : null}
          <IconButton icon={FileText} onPress={onEditRole} />
          {!isLeader ? <IconButton destructive icon={X} onPress={onRemove} /> : null}
        </View>
      ) : null}
    </View>
  );
}

function InstructionsTab({
  canEdit,
  onUpdate,
  squad,
}: {
  canEdit: boolean;
  onUpdate: (squadId: string, data: UpdateSquadRequest) => Promise<void>;
  squad: Squad;
}) {
  const { t } = useTranslation();
  const [value, setValue] = useState(squad.instructions ?? "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setValue(squad.instructions ?? "");
    setError(null);
  }, [squad.id, squad.instructions]);

  async function save() {
    setSaving(true);
    setError(null);
    try {
      await onUpdate(squad.id, { instructions: value });
    } catch (err) {
      setError(err instanceof Error ? err.message : t("squads.unable_to_save"));
    } finally {
      setSaving(false);
    }
  }

  return (
    <View style={styles.sectionStack}>
      <Text style={styles.sectionTitle}>{t("squads.instructions")}</Text>
      <Text style={styles.sectionDetail}>{t("squads.instructions_detail")}</Text>
      <TextInput
        editable={canEdit}
        multiline
        onChangeText={setValue}
        placeholder={t("squads.instructions_placeholder")}
        placeholderTextColor={colors.mutedForeground}
        style={[styles.textArea, !canEdit && styles.inputReadOnly]}
        textAlignVertical="top"
        value={value}
      />
      {error ? <Text style={styles.errorText}>{error}</Text> : null}
      {canEdit ? (
        <Button disabled={saving || value === (squad.instructions ?? "")} onPress={() => void save()}>
          {saving ? t("squads.saving") : t("common.save")}
        </Button>
      ) : null}
    </View>
  );
}

function SettingsTab({
  canEdit,
  onUpdate,
  squad,
}: {
  canEdit: boolean;
  onUpdate: (squadId: string, data: UpdateSquadRequest) => Promise<void>;
  squad: Squad;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState(squad.name);
  const [description, setDescription] = useState(squad.description ?? "");
  const [avatarUrl, setAvatarUrl] = useState(squad.avatar_url ?? "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const dirty =
    name !== squad.name ||
    description !== (squad.description ?? "") ||
    avatarUrl !== (squad.avatar_url ?? "");

  useEffect(() => {
    setName(squad.name);
    setDescription(squad.description ?? "");
    setAvatarUrl(squad.avatar_url ?? "");
    setError(null);
  }, [squad]);

  async function save() {
    if (!name.trim()) {
      setError(t("squads.name_required"));
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await onUpdate(squad.id, {
        name: name.trim(),
        description: description.trim(),
        avatar_url: avatarUrl.trim() || undefined,
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : t("squads.unable_to_save"));
    } finally {
      setSaving(false);
    }
  }

  return (
    <View style={styles.sectionStack}>
      <Text style={styles.sectionTitle}>{t("squads.settings")}</Text>
      <FormField editable={canEdit} label={t("squads.name")} onChangeText={setName} value={name} />
      <FormField editable={canEdit} label={t("squads.description")} onChangeText={setDescription} value={description} />
      <FormField editable={canEdit} label={t("squads.avatar_url")} onChangeText={setAvatarUrl} value={avatarUrl} />
      {error ? <Text style={styles.errorText}>{error}</Text> : null}
      {canEdit ? (
        <Button disabled={saving || !dirty} onPress={() => void save()}>
          {saving ? t("squads.saving") : t("common.save")}
        </Button>
      ) : null}
    </View>
  );
}

function CreateSquadModal({
  agents,
  members,
  onClose,
  onCreate,
  open,
  userId,
}: {
  agents: Agent[];
  members: MemberWithUser[];
  onClose: () => void;
  onCreate: (data: CreateSquadRequest, initialMembers: MemberCandidate[]) => Promise<void>;
  open: boolean;
  userId: string | null;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [avatarUrl, setAvatarUrl] = useState("");
  const [leaderId, setLeaderId] = useState("");
  const [selectedMembers, setSelectedMembers] = useState<MemberCandidate[]>([]);
  const [memberPickerOpen, setMemberPickerOpen] = useState(false);
  const [leaderPickerOpen, setLeaderPickerOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const leader = agents.find((agent) => agent.id === leaderId) ?? null;
  const activeAgents = useMemo(
    () => agents.filter((agent) => !agent.archived_at && agent.runtime_id && isAgentSelectable(agent, userId)),
    [agents, userId],
  );

  useEffect(() => {
    if (!open) {
      setName("");
      setDescription("");
      setAvatarUrl("");
      setLeaderId("");
      setSelectedMembers([]);
      setError(null);
      setCreating(false);
    }
  }, [open]);

  async function submit() {
    if (!name.trim() || !leaderId || creating) return;
    setCreating(true);
    setError(null);
    try {
      await onCreate(
        {
          name: name.trim(),
          description: description.trim() || undefined,
          leader_id: leaderId,
          avatar_url: avatarUrl.trim() || undefined,
        },
        selectedMembers.filter((member) => !(member.type === "agent" && member.id === leaderId)),
      );
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("squads.unable_to_create"));
    } finally {
      setCreating(false);
    }
  }

  return (
    <Modal animationType="slide" onRequestClose={onClose} visible={open}>
      <Screen padded={false} safeArea={false}>
        <ScreenTitleBar onBack={onClose} title={t("squads.new_squad")} />
        <KeyboardAvoidingView behavior={Platform.OS === "ios" ? "padding" : undefined} style={styles.keyboardAvoiding}>
          <ScrollView contentContainerStyle={styles.detailContent} keyboardShouldPersistTaps="handled">
            <Text style={styles.sectionTitle}>{t("squads.create_title")}</Text>
            <Text style={styles.sectionDetail}>{t("squads.create_detail")}</Text>
            <FormField autoFocus label={t("squads.name")} onChangeText={setName} placeholder={t("squads.name_placeholder")} value={name} />
            <FormField label={t("squads.description")} onChangeText={setDescription} placeholder={t("squads.description_placeholder")} value={description} />
            <FormField autoCapitalize="none" label={t("squads.avatar_url")} onChangeText={setAvatarUrl} placeholder="https://..." value={avatarUrl} />
            <PickerTrigger
              label={leader?.name ?? t("squads.select_leader")}
              meta={leader ? formatAgentStatus(t, leader.status) : t("squads.leader_hint")}
              onPress={() => setLeaderPickerOpen(true)}
            />
            <View style={styles.selectedMembers}>
              <View style={styles.sectionHeaderLine}>
                <Text style={styles.optionLabel}>{t("squads.initial_members")}</Text>
                <Pressable
                  accessibilityRole="button"
                  onPress={() => setMemberPickerOpen(true)}
                  style={({ pressed }) => [styles.smallActionButton, pressed && styles.pressed]}
                >
                  <Plus color={colors.foreground} size={16} />
                </Pressable>
              </View>
              {selectedMembers.length === 0 ? (
                <Text style={styles.mutedText}>{t("squads.no_initial_members")}</Text>
              ) : (
                selectedMembers.map((member) => (
                  <View key={`${member.type}-${member.id}`} style={styles.chipRow}>
                    <Text numberOfLines={1} style={styles.chipRowText}>{member.label}</Text>
                    <IconButton
                      icon={X}
                      onPress={() => setSelectedMembers((items) => items.filter((item) => item.type !== member.type || item.id !== member.id))}
                    />
                  </View>
                ))
              )}
            </View>
            {error ? <Text style={styles.errorText}>{error}</Text> : null}
            <Button disabled={creating || !name.trim() || !leaderId} onPress={() => void submit()}>
              {creating ? t("squads.creating") : t("squads.create")}
            </Button>
          </ScrollView>
        </KeyboardAvoidingView>
        <CandidatePickerModal
          candidates={activeAgents.map((agent) => ({
            id: agent.id,
            type: "agent" as const,
            label: agent.name,
            meta: formatAgentStatus(t, agent.status),
          }))}
          onClose={() => setLeaderPickerOpen(false)}
          onSelect={(candidate) => {
            setLeaderId(candidate.id);
            setSelectedMembers((items) => items.filter((item) => !(item.type === "agent" && item.id === candidate.id)));
            setLeaderPickerOpen(false);
          }}
          open={leaderPickerOpen}
          selectedKey={leaderId ? `agent:${leaderId}` : null}
          title={t("squads.leader")}
        />
        <CandidatePickerModal
          candidates={[
            ...activeAgents
              .filter((agent) => agent.id !== leaderId)
              .map((agent) => ({ id: agent.id, type: "agent" as const, label: agent.name, meta: t("issues.agent") })),
            ...members.map((member) => ({ id: member.user_id, type: "member" as const, label: member.name, meta: member.email })),
          ]}
          onClose={() => setMemberPickerOpen(false)}
          onSelect={(candidate) => {
            setSelectedMembers((items) => {
              if (items.some((item) => item.type === candidate.type && item.id === candidate.id)) return items;
              return [...items, candidate];
            });
          }}
          open={memberPickerOpen}
          selectedKeys={new Set(selectedMembers.map((member) => `${member.type}:${member.id}`))}
          title={t("squads.add_member")}
        />
      </Screen>
    </Modal>
  );
}

function AddMemberModal({
  agents,
  members,
  onAdded,
  onClose,
  open,
  squad,
  squadMembers,
  userId,
}: {
  agents: Agent[];
  members: MemberWithUser[];
  onAdded: () => void;
  onClose: () => void;
  open: boolean;
  squad: Squad;
  squadMembers: SquadMember[];
  userId: string | null;
}) {
  const { t } = useTranslation();
  const existing = new Set(squadMembers.map((member) => `${member.member_type}:${member.member_id}`));
  const candidates: MemberCandidate[] = [
    ...agents
      .filter((agent) => !agent.archived_at && isAgentSelectable(agent, userId) && !existing.has(`agent:${agent.id}`))
      .map((agent) => ({ id: agent.id, type: "agent" as const, label: agent.name, meta: t("issues.agent") })),
    ...members
      .filter((member) => !existing.has(`member:${member.user_id}`))
      .map((member) => ({ id: member.user_id, type: "member" as const, label: member.name, meta: member.email })),
  ];

  return (
    <CandidatePickerModal
      candidates={candidates}
      onClose={onClose}
      onSelect={(candidate) => {
        void api.addSquadMember(squad.id, {
          member_type: candidate.type,
          member_id: candidate.id,
        }).then(onAdded);
        onClose();
      }}
      open={open}
      title={t("squads.add_member")}
    />
  );
}

function RoleEditorModal({
  member,
  onClose,
  onSave,
}: {
  member: SquadMember | null;
  onClose: () => void;
  onSave: (role: string) => void;
}) {
  const { t } = useTranslation();
  const [value, setValue] = useState("");

  useEffect(() => {
    setValue(member?.role ?? "");
  }, [member]);

  return (
    <SheetModal onClose={onClose} open={!!member} title={t("squads.role")}>
      <TextInput
        autoFocus
        onChangeText={setValue}
        placeholder={t("squads.role_placeholder")}
        placeholderTextColor={colors.mutedForeground}
        style={styles.input}
        value={value}
      />
      <Button onPress={() => onSave(value.trim())}>{t("common.save")}</Button>
    </SheetModal>
  );
}

function CandidatePickerModal({
  candidates,
  onClose,
  onSelect,
  open,
  selectedKey,
  selectedKeys,
  title,
}: {
  candidates: MemberCandidate[];
  onClose: () => void;
  onSelect: (candidate: MemberCandidate) => void;
  open: boolean;
  selectedKey?: string | null;
  selectedKeys?: Set<string>;
  title: string;
}) {
  const { t } = useTranslation();
  const [query, setQuery] = useState("");

  useEffect(() => {
    if (!open) setQuery("");
  }, [open]);

  const normalizedQuery = normalizeSearch(query);
  const filtered = candidates.filter((candidate) => {
    if (!normalizedQuery) return true;
    return normalizeSearch(`${candidate.label} ${candidate.meta ?? ""}`).includes(normalizedQuery);
  });

  return (
    <SheetModal onClose={onClose} open={open} title={title}>
      <TextInput
        autoCapitalize="none"
        autoCorrect={false}
        onChangeText={setQuery}
        placeholder={t("common.search")}
        placeholderTextColor={colors.mutedForeground}
        style={styles.input}
        value={query}
      />
      {filtered.length === 0 ? (
        <Text style={styles.pickerEmpty}>{t("common.no_results")}</Text>
      ) : (
        filtered.map((candidate) => {
          const key = `${candidate.type}:${candidate.id}`;
          return (
            <PickerRow
              key={key}
              label={candidate.label}
              meta={candidate.meta}
              onPress={() => onSelect(candidate)}
              selected={selectedKey === key || selectedKeys?.has(key) === true}
            />
          );
        })
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
  children: ReactNode;
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
          <ScrollView contentContainerStyle={styles.sheetContent} keyboardShouldPersistTaps="handled">
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
  editable = true,
  label,
  ...props
}: ComponentProps<typeof TextInput> & { label: string }) {
  return (
    <View style={styles.formField}>
      <Text style={styles.optionLabel}>{label}</Text>
      <TextInput
        editable={editable}
        placeholderTextColor={colors.mutedForeground}
        {...props}
        style={[styles.input, !editable && styles.inputReadOnly, props.style]}
      />
    </View>
  );
}

function PickerTrigger({
  label,
  meta,
  onPress,
}: {
  label: string;
  meta?: string;
  onPress: () => void;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [styles.pickerTrigger, pressed && styles.pressed]}
    >
      <View style={styles.pickerTriggerText}>
        <Text numberOfLines={1} style={styles.pickerTriggerLabel}>{label}</Text>
        {meta ? <Text numberOfLines={1} style={styles.pickerTriggerMeta}>{meta}</Text> : null}
      </View>
      <Text style={styles.pickerTriggerAction}>›</Text>
    </Pressable>
  );
}

function PickerRow({
  label,
  meta,
  onPress,
  selected,
}: {
  label: string;
  meta?: string;
  onPress: () => void;
  selected: boolean;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [
        styles.pickerRow,
        selected && styles.pickerRowSelected,
        pressed && styles.pressed,
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

function SquadAvatar({ squad, size }: { squad: Squad; size: number }) {
  const initials = squad.name.trim().slice(0, 2).toUpperCase() || "SQ";
  if (squad.avatar_url) {
    return (
      <Image
        accessibilityIgnoresInvertColors
        source={{ uri: squad.avatar_url }}
        style={[styles.avatarImage, { borderRadius: size / 2, height: size, width: size }]}
      />
    );
  }
  return (
    <View style={[styles.avatarFallback, { borderRadius: size / 2, height: size, width: size }]}>
      <Text style={styles.avatarText}>{initials}</Text>
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

function IconButton({
  destructive,
  icon: Icon,
  onPress,
}: {
  destructive?: boolean;
  icon: typeof X;
  onPress: () => void;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [styles.iconButton, pressed && styles.pressed]}
    >
      <Icon color={destructive ? colors.destructive : colors.foreground} size={16} />
    </Pressable>
  );
}

function InlineEmpty({ title }: { title: string }) {
  return (
    <View style={styles.inlineState}>
      <Users color={colors.mutedForeground} size={24} />
      <Text style={styles.emptyTitle}>{title}</Text>
    </View>
  );
}

function normalizeSearch(value: string) {
  return value.trim().toLowerCase().replace(/\s+/g, "");
}

const styles = StyleSheet.create({
  avatarFallback: {
    alignItems: "center",
    backgroundColor: colors.muted,
    justifyContent: "center",
  },
  avatarImage: {
    backgroundColor: colors.muted,
  },
  avatarText: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "700",
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
  card: {
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
  cardDescription: {
    color: colors.mutedForeground,
    fontSize: 12,
    marginTop: 3,
  },
  cardMeta: {
    color: colors.mutedForeground,
    fontSize: 12,
    marginTop: spacing.sm,
  },
  cardText: {
    flex: 1,
    minWidth: 0,
  },
  cardTitle: {
    color: colors.foreground,
    flex: 1,
    fontSize: 15,
    fontWeight: "600",
  },
  cardTitleLine: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
  },
  chipRow: {
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
  chipRowText: {
    color: colors.foreground,
    flex: 1,
    fontSize: 13,
    fontWeight: "500",
  },
  detailContent: {
    gap: spacing.lg,
    padding: spacing.lg,
    paddingBottom: spacing.xl,
  },
  detailHero: {
    alignItems: "center",
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.md,
    padding: spacing.lg,
  },
  detailHeroText: {
    flex: 1,
    minWidth: 0,
  },
  detailScroll: {
    flex: 1,
  },
  detailSubtitle: {
    color: colors.mutedForeground,
    fontSize: 13,
    lineHeight: 18,
  },
  detailTitle: {
    color: colors.foreground,
    fontSize: 18,
    fontWeight: "700",
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
  formField: {
    gap: spacing.xs,
  },
  iconButton: {
    alignItems: "center",
    borderRadius: radii.md,
    height: 32,
    justifyContent: "center",
    width: 32,
  },
  inlineState: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderStyle: "dashed",
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.sm,
    padding: spacing.xl,
  },
  input: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    color: colors.foreground,
    fontSize: 14,
    minHeight: 44,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  inputReadOnly: {
    opacity: 0.65,
  },
  keyboardAvoiding: {
    flex: 1,
  },
  list: {
    padding: spacing.lg,
    paddingTop: 0,
  },
  memberActions: {
    flexDirection: "row",
  },
  memberIcon: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    height: 38,
    justifyContent: "center",
    width: 38,
  },
  memberMeta: {
    color: colors.mutedForeground,
    fontSize: 12,
    lineHeight: 17,
  },
  memberName: {
    color: colors.foreground,
    flex: 1,
    fontSize: 14,
    fontWeight: "600",
  },
  memberRow: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.md,
    padding: spacing.md,
  },
  memberStatus: {
    fontSize: 12,
    fontWeight: "600",
    marginTop: 2,
  },
  memberText: {
    flex: 1,
    minWidth: 0,
  },
  memberTitleLine: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
  },
  mutedText: {
    color: colors.mutedForeground,
    fontSize: 13,
  },
  optionLabel: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "600",
  },
  pickerEmpty: {
    color: colors.mutedForeground,
    fontSize: 13,
    padding: spacing.md,
    textAlign: "center",
  },
  pickerRow: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.sm,
    padding: spacing.md,
  },
  pickerRowLabel: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "600",
  },
  pickerRowMeta: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
  pickerRowSelected: {
    borderColor: colors.primary,
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
    padding: spacing.md,
  },
  pickerTriggerAction: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "600",
  },
  pickerTriggerLabel: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "600",
  },
  pickerTriggerMeta: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
  pickerTriggerText: {
    flex: 1,
    minWidth: 0,
  },
  pressed: {
    opacity: 0.75,
  },
  searchInput: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    color: colors.foreground,
    flex: 1,
    fontSize: 13,
    minHeight: 38,
    paddingHorizontal: spacing.md,
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
    fontWeight: "700",
  },
  segmented: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    flexDirection: "row",
    padding: 3,
  },
  segment: {
    borderRadius: radii.sm,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  segmentActive: {
    backgroundColor: colors.card,
  },
  segmentText: {
    color: colors.mutedForeground,
    fontSize: 13,
    fontWeight: "600",
  },
  segmentTextActive: {
    color: colors.foreground,
  },
  selectedMembers: {
    gap: spacing.sm,
  },
  sheet: {
    backgroundColor: colors.background,
    borderTopLeftRadius: radii.md,
    borderTopRightRadius: radii.md,
    maxHeight: "82%",
    paddingBottom: spacing.lg,
  },
  sheetBackdrop: {
    flex: 1,
  },
  sheetContent: {
    gap: spacing.sm,
    padding: spacing.lg,
  },
  sheetHeader: {
    alignItems: "center",
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    justifyContent: "space-between",
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
  },
  sheetRoot: {
    backgroundColor: "rgba(0,0,0,0.35)",
    flex: 1,
    justifyContent: "flex-end",
  },
  sheetTitle: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "700",
  },
  smallActionButton: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    height: 34,
    justifyContent: "center",
    width: 34,
  },
  tabChip: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    flexDirection: "row",
    gap: spacing.xs,
    height: 34,
    justifyContent: "center",
    minWidth: 108,
    paddingHorizontal: spacing.md,
  },
  tabChipActive: {
    backgroundColor: colors.primary,
  },
  tabChipText: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "600",
    includeFontPadding: false,
    lineHeight: 16,
  },
  tabChipTextActive: {
    color: colors.primaryForeground,
  },
  tabsRow: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
    padding: spacing.lg,
    paddingBottom: spacing.sm,
  },
  tabsScroller: {
    backgroundColor: colors.background,
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexGrow: 0,
    flexShrink: 0,
    height: 58,
    zIndex: 2,
  },
  textArea: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    color: colors.foreground,
    fontSize: 14,
    minHeight: 180,
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
    flexDirection: "row",
    gap: spacing.sm,
    padding: spacing.lg,
  },
});
