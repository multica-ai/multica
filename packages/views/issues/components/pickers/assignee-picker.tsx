"use client";

import { useMemo, useState } from "react";
import { Lock, UserMinus } from "lucide-react";
import type { Agent, IssueAssigneeType, UpdateIssueRequest } from "@multica/core/types";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import { useActorName } from "@multica/core/workspace/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, agentListOptions, assigneeFrequencyOptions } from "@multica/core/workspace/queries";
import { ActorAvatar } from "../../../common/actor-avatar";
import {
  PropertyPicker,
  PickerItem,
  PickerSection,
  PickerEmpty,
} from "./property-picker";
import { useT } from "@multica/i18n/react";

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
}: {
  assigneeType: IssueAssigneeType | null;
  assigneeId: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
}) {
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const [filter, setFilter] = useState("");
  const user = useAuthStore((s) => s.user);
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: frequency = [] } = useQuery(assigneeFrequencyOptions(wsId));
  const { getActorName } = useActorName();
  const t = useT("issues");

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
    .filter((m) => m.name.toLowerCase().includes(query))
    .sort((a, b) => getFreq("member", b.user_id) - getFreq("member", a.user_id));
  const filteredAgents = agents
    .filter((a) => !a.archived_at && a.name.toLowerCase().includes(query))
    .sort((a, b) => getFreq("agent", b.id) - getFreq("agent", a.id));

  const isSelected = (type: string, id: string) =>
    assigneeType === type && assigneeId === id;

  const triggerLabel =
    assigneeType && assigneeId
      ? getActorName(assigneeType, assigneeId)
      : t("picker_unassigned");

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(v: boolean) => {
        setOpen(v);
        if (!v) setFilter("");
      }}
      width="w-52"
      align={align}
      searchable
      searchPlaceholder={t("picker_assign_to")}
      onSearchChange={setFilter}
      triggerRender={triggerRender}
      trigger={
        customTrigger ? customTrigger : assigneeType && assigneeId ? (
          <>
            <ActorAvatar actorType={assigneeType} actorId={assigneeId} size={18} enableHoverCard showStatusDot />
            <span className="truncate">{triggerLabel}</span>
          </>
        ) : (
          <span className="text-muted-foreground">{t("picker_unassigned")}</span>
        )
      }
    >
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
          <span className="text-muted-foreground">{t("picker_unassigned")}</span>
        </PickerItem>
      )}

      {/* Members */}
      {filteredMembers.length > 0 && (
        <PickerSection label={t("detail_members")}>
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
              <span>{m.name}</span>
            </PickerItem>
          ))}
        </PickerSection>
      )}

      {/* Agents */}
      {filteredAgents.length > 0 && (
        <PickerSection label={t("detail_agents")}>
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
                  onUpdate({
                    assignee_type: "agent",
                    assignee_id: a.id,
                  });
                  setOpen(false);
                }}
              >
                <ActorAvatar actorType="agent" actorId={a.id} size={18} showStatusDot />
                <span className={allowed ? "" : "text-muted-foreground"}>{a.name}</span>
                {a.visibility === "private" && (
                  <Lock className="ml-auto h-3 w-3 text-muted-foreground" />
                )}
              </PickerItem>
            );
          })}
        </PickerSection>
      )}

      {filteredMembers.length === 0 &&
        filteredAgents.length === 0 &&
        filter && <PickerEmpty />}
    </PropertyPicker>
  );
}
