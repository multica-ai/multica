"use client";

import { useMemo, useState } from "react";
import { Lock, UserMinus } from "@multica/ui/icons";
import type { Agent, IssueAssigneeType, MemberRole, MemberWithUser, Squad, UpdateIssueRequest } from "@multica/core/types";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { isAgentRuntimeBound } from "@multica/core/agents";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import { useActorName } from "@multica/core/workspace/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { useAssigneePicksStore, selectAssigneePicks, rankAssigneePicks } from "@multica/core/issues/stores/assignee-picks-store";
import { ActorAvatar } from "../../../common/actor-avatar";
import { DeferredPopup } from "../../../common/deferred-popup";
import {
  PropertyPicker,
  PickerPanel,
  PickerItem,
  PickerSection,
  PickerEmpty,
  PICKER_TRIGGER_CLASS,
} from "./property-picker";
import { useT } from "../../../i18n";
import { matchesPinyin } from "../../../editor/extensions/pinyin-match";

/**
 * Legacy boolean shape kept around for callers (e.g. `use-issue-actions.ts`)
 * that haven't migrated to the new `canAssignAgentToIssue` Decision API yet.
 * Internally redirects to the canonical rule so behaviour stays in sync.
 */
export function canAssignAgent(
  agent: Agent,
  userId: string | undefined,
  memberRole: string | undefined,
): boolean {
  return canAssignAgentToIssue(agent, {
    userId: userId ?? null,
    role: toMemberRole(memberRole),
  }).allowed;
}

// Keeps "guest": the rules refuse a guest outright, while an unknown role
// would fall through to the owner and member-grant checks.
function toMemberRole(role: string | undefined): MemberRole | null {
  return role === "owner" || role === "admin" || role === "member" || role === "guest" ? role : null;
}

interface AssigneePickerProps {
  assigneeType: IssueAssigneeType | null;
  assigneeId: string | null;
  /**
   * `true` when a batch selection spans different assignees ("mixed"): no row
   * is checked, including the unassigned row. Distinct from `assigneeType` /
   * `assigneeId` both being `null`, which means every selected issue is
   * genuinely unassigned and the unassigned row should be checked.
   */
  mixed?: boolean;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement<Record<string, unknown>>;
  /** Hover tooltip naming the property; hidden while the picker is open. */
  tooltip?: React.ReactNode;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
  /**
   * Whether a pick feeds the local ranking right away. The create-issue
   * dialog turns this off and records only the assignee it submits.
   */
  recordPicks?: boolean;
}

/**
 * Mounting the real picker subscribes to members/agents/squads
 * queries — multiplied per board card / list row that cost froze tab
 * switches. Uncontrolled callers that bring their own trigger content get a
 * deferred lookalike trigger instead; the picker mounts on first interaction.
 * The default trigger needs `getActorName` (a members/agents subscription
 * itself), so trigger-less callers stay eager.
 */
export function AssigneePicker(props: AssigneePickerProps) {
  const hasDeferredTriggerContent =
    props.trigger !== undefined || props.triggerRender?.props.children != null;
  const canDefer =
    props.open === undefined &&
    props.onOpenChange === undefined &&
    hasDeferredTriggerContent;
  if (!canDefer) {
    return <AssigneePickerImpl {...props} />;
  }
  return (
    <DeferredPopup
      trigger={props.trigger}
      triggerRender={props.triggerRender}
      triggerClassName={PICKER_TRIGGER_CLASS}
      tooltip={props.tooltip}
    >
      {(open, onOpenChange) => (
        <AssigneePickerImpl {...props} open={open} onOpenChange={onOpenChange} />
      )}
    </DeferredPopup>
  );
}

function AssigneePickerImpl({
  assigneeType,
  assigneeId,
  mixed = false,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  tooltip,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
  recordPicks = true,
}: AssigneePickerProps) {
  const { t } = useT("issues");
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const [filter, setFilter] = useState("");
  const choices = useAssigneeChoices(filter);
  const { getActorName } = useActorName();

  const triggerLabel =
    assigneeType && assigneeId
      ? getActorName(assigneeType, assigneeId)
      : t(($) => $.pickers.assignee.trigger_unassigned);

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(v: boolean) => {
        setOpen(v);
        if (!v) setFilter("");
      }}
      width="w-64"
      align={align}
      searchable
      searchPlaceholder={t(($) => $.pickers.assignee.search_placeholder)}
      onSearchChange={setFilter}
      triggerRender={triggerRender}
      tooltip={tooltip}
      trigger={
        customTrigger ? customTrigger : assigneeType && assigneeId ? (
          <>
            <ActorAvatar actorType={assigneeType} actorId={assigneeId} size="sm" enableHoverCard showStatusDot />
            <span className="truncate">{triggerLabel}</span>
          </>
        ) : (
          <span className="text-muted-foreground">{t(($) => $.pickers.assignee.trigger_unassigned)}</span>
        )
      }
    >
      <AssigneeOptions
        choices={choices}
        filter={filter}
        assigneeType={assigneeType}
        assigneeId={assigneeId}
        mixed={mixed}
        recordPicks={recordPicks}
        onPick={(updates) => {
          onUpdate(updates);
          setOpen(false);
        }}
      />
    </PropertyPicker>
  );
}

/**
 * The assignee picker's search box and list without the popover, for hosting
 * inside a menu. `onPicked` runs after the choice is applied so the host can
 * close itself.
 */
export function AssigneePickerPanel({
  assigneeType,
  assigneeId,
  onUpdate,
  onPicked,
}: {
  assigneeType: IssueAssigneeType | null;
  assigneeId: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  onPicked: () => void;
}) {
  const { t } = useT("issues");
  const [filter, setFilter] = useState("");
  const choices = useAssigneeChoices(filter);

  return (
    <PickerPanel
      searchable
      autoFocusSearch
      searchPlaceholder={t(($) => $.pickers.assignee.search_placeholder)}
      onSearchChange={setFilter}
    >
      <AssigneeOptions
        choices={choices}
        filter={filter}
        assigneeType={assigneeType}
        assigneeId={assigneeId}
        onPick={(updates) => {
          onUpdate(updates);
          onPicked();
        }}
      />
    </PickerPanel>
  );
}

/**
 * Records a manual pick in this device's per-user, per-workspace queue that
 * ranks the picker. Exported for the create-issue dialog, which records only
 * the assignee it finally submits.
 */
export function useRecordAssigneePick() {
  const user = useAuthStore((s) => s.user);
  const wsId = useWorkspaceId();
  const recordPick = useAssigneePicksStore((s) => s.recordPick);
  return (type: IssueAssigneeType, id: string) => {
    if (user?.id && wsId) recordPick(user.id, wsId, { type, id });
  };
}

function useAssigneeChoices(filter: string) {
  const user = useAuthStore((s) => s.user);
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const picks = useAssigneePicksStore(selectAssigneePicks(user?.id, wsId));
  const ranked = useMemo(() => rankAssigneePicks(picks), [picks]);

  const currentMember = members.find((m) => m.user_id === user?.id);
  const memberRole = currentMember?.role;

  const freqMap = useMemo(() => {
    const map = new Map<string, number>();
    for (const entry of ranked) map.set(`${entry.type}:${entry.id}`, entry.count);
    return map;
  }, [ranked]);

  const getFreq = (type: string, id: string) => freqMap.get(`${type}:${id}`) ?? 0;

  // Only actors this user can pick are listed: agents they may invoke that
  // have a runtime, and squads whose leader is such an agent (the server
  // checks the leader's invoke permission). Anything else would be a row
  // that can't be clicked, or a pick the server rejects.
  const role = toMemberRole(memberRole);
  const assignableAgents = agents.filter(
    (a) =>
      !a.archived_at &&
      isAgentRuntimeBound(a) &&
      canAssignAgentToIssue(a, { userId: user?.id ?? null, role }).allowed,
  );
  const assignableAgentIds = new Set(assignableAgents.map((a) => a.id));
  const assignableSquads = squads.filter(
    (s) => !s.archived_at && assignableAgentIds.has(s.leader_id),
  );

  const query = filter.trim().toLowerCase();
  const filteredMembers = members
    .filter((m) => m.name.toLowerCase().includes(query) || matchesPinyin(m.name, query))
    .sort((a, b) => getFreq("member", b.user_id) - getFreq("member", a.user_id));
  const filteredAgents = assignableAgents
    .filter((a) => a.name.toLowerCase().includes(query) || matchesPinyin(a.name, query))
    .sort((a, b) => getFreq("agent", b.id) - getFreq("agent", a.id));
  const filteredSquads = assignableSquads
    .filter((s) => s.name.toLowerCase().includes(query) || matchesPinyin(s.name, query))
    .sort((a, b) => getFreq("squad", b.id) - getFreq("squad", a.id));
  // Recent: the top 3 actors this user picked most often on this device in
  // this workspace. Entries whose actor is gone or no longer pickable —
  // member removed, agent/squad archived or hidden above — are skipped so the
  // next one moves up. Hidden while searching so Enter still picks the first
  // match.
  const recent: RecentAssignee[] = [];
  if (!query) {
    for (const entry of ranked) {
      if (recent.length >= 3) break;
      if (entry.type === "member") {
        const member = members.find((m) => m.user_id === entry.id);
        if (member) recent.push({ type: "member", member });
      } else if (entry.type === "agent") {
        const agent = assignableAgents.find((a) => a.id === entry.id);
        if (agent) recent.push({ type: "agent", agent });
      } else if (entry.type === "squad") {
        const squad = assignableSquads.find((s) => s.id === entry.id);
        if (squad) recent.push({ type: "squad", squad });
      }
    }
  }

  return {
    recent,
    filteredMembers,
    filteredAgents,
    filteredSquads,
  };
}

type RecentAssignee =
  | { type: "member"; member: MemberWithUser }
  | { type: "agent"; agent: Agent }
  | { type: "squad"; squad: Squad };

function AssigneeOptions({
  choices,
  filter,
  assigneeType,
  assigneeId,
  mixed = false,
  recordPicks = true,
  onPick,
}: {
  choices: ReturnType<typeof useAssigneeChoices>;
  filter: string;
  assigneeType: IssueAssigneeType | null;
  assigneeId: string | null;
  mixed?: boolean;
  recordPicks?: boolean;
  onPick: (updates: Partial<UpdateIssueRequest>) => void;
}) {
  const { t } = useT("issues");
  const { recent, filteredMembers, filteredAgents, filteredSquads } = choices;
  const recordPick = useRecordAssigneePick();

  const isSelected = (type: string, id: string) =>
    assigneeType === type && assigneeId === id;

  // One click is one pick, single issue or batch. Re-picking the current
  // assignee changes nothing, so it does not count.
  const pickActor = (type: IssueAssigneeType, id: string) => {
    if (recordPicks && !isSelected(type, id)) recordPick(type, id);
    onPick({ assignee_type: type, assignee_id: id });
  };

  // Shared by Recent and the per-type groups so selection and the private
  // lock render identically wherever an actor shows up.
  const renderMember = (m: MemberWithUser) => (
    <PickerItem
      key={m.user_id}
      selected={isSelected("member", m.user_id)}
      onClick={() => pickActor("member", m.user_id)}
    >
      <ActorAvatar actorType="member" actorId={m.user_id} size="sm" />
      <span className="truncate">{m.name}</span>
    </PickerItem>
  );

  const renderAgent = (a: Agent) => (
    <PickerItem
      key={a.id}
      selected={isSelected("agent", a.id)}
      onClick={() => pickActor("agent", a.id)}
    >
      <ActorAvatar actorType="agent" actorId={a.id} size="sm" showStatusDot />
      <span className="truncate">{a.name}</span>
      {a.visibility === "private" && (
        <Lock className="ml-auto h-3 w-3 text-muted-foreground" />
      )}
    </PickerItem>
  );

  const renderSquad = (s: Squad) => (
    <PickerItem
      key={s.id}
      selected={isSelected("squad", s.id)}
      onClick={() => pickActor("squad", s.id)}
    >
      <ActorAvatar actorType="squad" actorId={s.id} size="sm" />
      <span className="truncate">{s.name}</span>
    </PickerItem>
  );

  return (
    <>
      {/* Unassigned — always the first row, search active or not. Every
          picker in the app puts the empty value there, so "clear this field"
          never moves. */}
      <PickerItem
        emptyValue
        selected={!mixed && !assigneeType && !assigneeId}
        onClick={() => onPick({ assignee_type: null, assignee_id: null })}
      >
        <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
        <span className="text-muted-foreground">{t(($) => $.pickers.assignee.trigger_unassigned)}</span>
      </PickerItem>

      {/* Recent — empty (and hidden) while searching or without history. The
          same actors still appear in their own groups below. */}
      {recent.length > 0 && (
        <PickerSection label={t(($) => $.pickers.assignee.recent_group)}>
          {recent.map((r) =>
            r.type === "member"
              ? renderMember(r.member)
              : r.type === "agent"
                ? renderAgent(r.agent)
                : renderSquad(r.squad),
          )}
        </PickerSection>
      )}

      {filteredMembers.length > 0 && (
        <PickerSection label={t(($) => $.pickers.assignee.members_group)}>
          {filteredMembers.map(renderMember)}
        </PickerSection>
      )}

      {filteredAgents.length > 0 && (
        <PickerSection label={t(($) => $.pickers.assignee.agents_group)}>
          {filteredAgents.map(renderAgent)}
        </PickerSection>
      )}

      {/* Squads — group ownership; assigning to a squad routes the issue to
          its leader agent on the backend. */}
      {filteredSquads.length > 0 && (
        <PickerSection label={t(($) => $.pickers.assignee.squads_group)}>
          {filteredSquads.map(renderSquad)}
        </PickerSection>
      )}

      {filteredMembers.length === 0 &&
        filteredAgents.length === 0 &&
        filteredSquads.length === 0 &&
        filter && <PickerEmpty />}
    </>
  );
}
