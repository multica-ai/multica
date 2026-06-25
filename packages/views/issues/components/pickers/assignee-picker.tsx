"use client";

import { useEffect, useMemo, useState } from "react";
import { ArrowLeft, Cpu, Lock, UserMinus } from "lucide-react";
import type { Agent, AgentRuntime, IssueAssigneeType, UpdateIssueRequest } from "@multica/core/types";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { runtimeMatchesAgentCapability } from "@multica/core/agents";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, agentListOptions, squadListOptions, assigneeFrequencyOptions } from "@multica/core/workspace/queries";
import { squadLeaderRuntimeOptions } from "@multica/core/squads";
import { ActorAvatar } from "../../../common/actor-avatar";
import {
  PropertyPicker,
  PickerItem,
  PickerSection,
  PickerEmpty,
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
    role: memberRole === "owner" || memberRole === "admin" || memberRole === "member"
      ? memberRole
      : null,
  }).allowed;
}

export function AssigneePicker({
  assigneeType,
  assigneeId,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
  runtimeChoice = true,
}: {
  assigneeType: IssueAssigneeType | null;
  assigneeId: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
  runtimeChoice?: boolean;
}) {
  const { t } = useT("issues");
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const [filter, setFilter] = useState("");
  const user = useAuthStore((s) => s.user);
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { data: frequency = [] } = useQuery(assigneeFrequencyOptions(wsId));
  const { getActorName } = useActorName();
  const [runtimeChoiceAgent, setRuntimeChoiceAgent] = useState<Agent | null>(null);
  const [selectedSquadForRuntime, setSelectedSquadForRuntime] = useState<{id: string; name: string} | null>(null);
  const squadRuntimesQuery = useQuery({
    ...squadLeaderRuntimeOptions(wsId, selectedSquadForRuntime?.id ?? ""),
    enabled: !!selectedSquadForRuntime,
  });
  const squadRuntimes = squadRuntimesQuery.data ?? [];
  const squadQueryFetched = squadRuntimesQuery.isFetched;
 
  // Auto-select when squad has exactly 1 compatible runtime
  useEffect(() => {
    if (!selectedSquadForRuntime || !squadQueryFetched) return;
    if (squadRuntimes.length === 1) {
      onUpdate({
        assignee_type: "squad",
        assignee_id: selectedSquadForRuntime.id,
        runtime_id: squadRuntimes[0]!.id,
      });
      setSelectedSquadForRuntime(null);
      setOpen(false);
    }
  }, [selectedSquadForRuntime, squadRuntimes, squadQueryFetched, onUpdate, setOpen]);
 
  const currentMember = members.find((m) => m.user_id === user?.id);
  const memberRole = currentMember?.role;

  // Build a lookup map from frequency data for sorting.
  const freqMap = useMemo(() => {
    const map = new Map<string, number>();
    for (const entry of frequency) {
      map.set(`${entry.assignee_type}:${entry.assignee_id}`, entry.frequency);
    }
    return map;
  }, [frequency]);

  const getFreq = (type: string, id: string) => freqMap.get(`${type}:${id}`) ?? 0;

  const query = filter.trim().toLowerCase();
  const filteredMembers = members
    .filter((m) => m.name.toLowerCase().includes(query) || matchesPinyin(m.name, query))
    .sort((a, b) => getFreq("member", b.user_id) - getFreq("member", a.user_id));
  const filteredAgents = agents
    .filter((a) => !a.archived_at && (a.name.toLowerCase().includes(query) || matchesPinyin(a.name, query)))
    .sort((a, b) => getFreq("agent", b.id) - getFreq("agent", a.id));
  const filteredSquads = squads
    .filter((s) => !s.archived_at && (s.name.toLowerCase().includes(query) || matchesPinyin(s.name, query)))
    .sort((a, b) => getFreq("squad", b.id) - getFreq("squad", a.id));
  const compatibleRuntimesForAgent = (agent: Agent): AgentRuntime[] =>
    runtimes.filter(
      (runtime) =>
        runtime.owner_id === user?.id &&
        runtime.status === "online" &&
        runtimeMatchesAgentCapability(agent, runtime),
    );

  const isSelected = (type: string, id: string) =>
    assigneeType === type && assigneeId === id;

  const triggerLabel =
    assigneeType && assigneeId
      ? getActorName(assigneeType, assigneeId)
      : t(($) => $.pickers.assignee.trigger_unassigned);

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(v: boolean) => {
        setOpen(v);
        if (!v) {
          setFilter("");
          setSelectedSquadForRuntime(null);
          setRuntimeChoiceAgent(null);
        }
      }}
      width="w-64"
      align={align}
      searchable={!runtimeChoiceAgent && !selectedSquadForRuntime}
      searchPlaceholder={t(($) => $.pickers.assignee.search_placeholder)}
      onSearchChange={setFilter}
      triggerRender={triggerRender}
      trigger={
        customTrigger ? customTrigger : assigneeType && assigneeId ? (
          <>
            <ActorAvatar actorType={assigneeType} actorId={assigneeId} size={18} enableHoverCard showStatusDot />
            <span className="truncate">{triggerLabel}</span>
          </>
        ) : (
          <span className="text-muted-foreground">{t(($) => $.pickers.assignee.trigger_unassigned)}</span>
        )
      }
    >
      {selectedSquadForRuntime ? (
        <>
          <PickerItem selected={false} onClick={() => setSelectedSquadForRuntime(null)}>
            <ArrowLeft className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="truncate">{selectedSquadForRuntime.name}</span>
          </PickerItem>
          {squadRuntimes.length === 0 ? (
            <PickerItem selected={false} disabled onClick={() => {}} tooltip="没有兼容小队队长的本地运行时，请先安装并启动兼容的本地运行时">
              <span>没有兼容的运行时</span>
            </PickerItem>
          ) : (
            <PickerSection label={t(($) => $.pickers.assignee.runtime_group)}>
              {squadRuntimes.map((rt) => (
                <PickerItem
                  key={rt.id}
                  selected={false}
                  onClick={() => {
                    onUpdate({
                      assignee_type: "squad",
                      assignee_id: selectedSquadForRuntime.id,
                      runtime_id: rt.id,
                    });
                    setSelectedSquadForRuntime(null);
                    setOpen(false);
                  }}
                >
                  <Cpu className="h-3.5 w-3.5 text-muted-foreground" />
                  <span className="truncate">{rt.name}</span>
                </PickerItem>
              ))}
            </PickerSection>
          )}
        </>
      ) : runtimeChoiceAgent ? (
        <>
          <PickerItem selected={false} onClick={() => setRuntimeChoiceAgent(null)}>
            <ArrowLeft className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="truncate">{runtimeChoiceAgent.name}</span>
          </PickerItem>
          <PickerSection label={t(($) => $.pickers.assignee.runtime_group)}>
            {compatibleRuntimesForAgent(runtimeChoiceAgent).map((runtime) => (
              <PickerItem
                key={runtime.id}
                selected={false}
                onClick={() => {
                  onUpdate({
                    assignee_type: "agent",
                    assignee_id: runtimeChoiceAgent.id,
                    runtime_id: runtime.id,
                  });
                  setRuntimeChoiceAgent(null);
                  setOpen(false);
                }}
              >
                <Cpu className="h-3.5 w-3.5 text-muted-foreground" />
                <span className="truncate">{runtime.name}</span>
              </PickerItem>
            ))}
          </PickerSection>
        </>
      ) : (
        <>
      {/* Unassigned option — hidden when search is active */}
      {!query && (
        <PickerItem
          selected={!assigneeType && !assigneeId}
          onClick={() => {
            onUpdate({ assignee_type: null, assignee_id: null });
            setOpen(false);
          }}
        >
          <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
          <span className="text-muted-foreground">{t(($) => $.pickers.assignee.trigger_unassigned)}</span>
        </PickerItem>
      )}

      {/* Members */}
      {filteredMembers.length > 0 && (
        <PickerSection label={t(($) => $.pickers.assignee.members_group)}>
          {filteredMembers.map((m) => (
            <PickerItem
              key={m.user_id}
              selected={isSelected("member", m.user_id)}
              onClick={() => {
                onUpdate({
                  assignee_type: "member",
                  assignee_id: m.user_id,
                });
                setOpen(false);
              }}
            >
              <ActorAvatar actorType="member" actorId={m.user_id} size={18} />
              <span className="truncate">{m.name}</span>
            </PickerItem>
          ))}
        </PickerSection>
      )}

      {/* Agents */}
      {filteredAgents.length > 0 && (
        <PickerSection label={t(($) => $.pickers.assignee.agents_group)}>
          {filteredAgents.map((a) => {
            const decision = canAssignAgentToIssue(a, {
              userId: user?.id ?? null,
              role:
                memberRole === "owner" ||
                memberRole === "admin" ||
                memberRole === "member"
                  ? memberRole
                  : null,
            });
            const allowed = decision.allowed;
            return (
              <PickerItem
                key={a.id}
                selected={isSelected("agent", a.id)}
                disabled={!allowed}
                tooltip={!allowed ? decision.message : undefined}
                onClick={() => {
                  if (!allowed) return;
                  const compatibleRuntimes = compatibleRuntimesForAgent(a);
                  if (runtimeChoice && compatibleRuntimes.length > 1) {
                    setFilter("");
                    setRuntimeChoiceAgent(a);
                    return;
                  }
                  onUpdate({
                    assignee_type: "agent",
                    assignee_id: a.id,
                    ...(compatibleRuntimes[0]?.id
                      ? { runtime_id: compatibleRuntimes[0].id }
                      : {}),
                  });
                  setOpen(false);
                }}
              >
                <ActorAvatar actorType="agent" actorId={a.id} size={18} showStatusDot />
                <span className={`truncate ${allowed ? "" : "text-muted-foreground"}`}>{a.name}</span>
                {a.visibility === "private" && (
                  <Lock className="ml-auto h-3 w-3 text-muted-foreground" />
                )}
              </PickerItem>
            );
          })}
        </PickerSection>
      )}

      {/* Squads — group ownership; assigning to a squad routes the issue to
          its leader agent on the backend. */}
      {filteredSquads.length > 0 && (
        <PickerSection label={t(($) => $.pickers.assignee.squads_group)}>
          {filteredSquads.map((s) => (
            <PickerItem
              key={s.id}
              selected={isSelected("squad", s.id)}
              onClick={() => {
                if (runtimeChoice) {
                  setFilter("");
                  setSelectedSquadForRuntime({ id: s.id, name: s.name });
                  return;
                }
                onUpdate({
                  assignee_type: "squad",
                  assignee_id: s.id,
                });
                setOpen(false);
              }}
            >
              <ActorAvatar actorType="squad" actorId={s.id} size={18} />
              <span className="truncate">{s.name}</span>
            </PickerItem>
          ))}
        </PickerSection>
      )}

      {filteredMembers.length === 0 &&
        filteredAgents.length === 0 &&
        filteredSquads.length === 0 &&
        filter && <PickerEmpty />}
        </>
      )}
    </PropertyPicker>
  );
}
