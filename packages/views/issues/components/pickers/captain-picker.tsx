"use client";

import { useMemo, useState } from "react";
import { Lock, UserMinus } from "lucide-react";
import type { IssueCaptainType, UpdateIssueRequest } from "@multica/core/types";
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
import { useT } from "../../../i18n";

// Captain is an agent-only routing field on the issue. v1 enforces
// captain_type === "agent" on the backend, so this picker omits the Members
// section that AssigneePicker has. Frequency sorting reuses the assignee
// frequency table — captain choices are correlated enough with assignee
// patterns that mining a separate table would be overkill in v1.
export function CaptainPicker({
  captainType,
  captainId,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
}: {
  captainType: IssueCaptainType | null;
  captainId: string | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
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
  const { data: frequency = [] } = useQuery(assigneeFrequencyOptions(wsId));
  const { getActorName } = useActorName();

  const currentMember = members.find((m) => m.user_id === user?.id);
  const memberRole = currentMember?.role;

  const freqMap = useMemo(() => {
    const map = new Map<string, number>();
    for (const entry of frequency) {
      map.set(`${entry.assignee_type}:${entry.assignee_id}`, entry.frequency);
    }
    return map;
  }, [frequency]);

  const getFreq = (id: string) => freqMap.get(`agent:${id}`) ?? 0;

  const query = filter.trim().toLowerCase();
  const filteredAgents = agents
    .filter((a) => !a.archived_at && a.name.toLowerCase().includes(query))
    .sort((a, b) => getFreq(b.id) - getFreq(a.id));

  const isSelected = (id: string) =>
    captainType === "agent" && captainId === id;

  const triggerLabel =
    captainType && captainId
      ? getActorName(captainType, captainId)
      : t(($) => $.pickers.captain.trigger_no_captain);

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
      searchPlaceholder={t(($) => $.pickers.captain.search_placeholder)}
      onSearchChange={setFilter}
      triggerRender={triggerRender}
      trigger={
        customTrigger ? customTrigger : captainType && captainId ? (
          <>
            <ActorAvatar actorType={captainType} actorId={captainId} size={18} enableHoverCard showStatusDot />
            <span className="truncate">{triggerLabel}</span>
          </>
        ) : (
          <span className="text-muted-foreground">{t(($) => $.pickers.captain.trigger_no_captain)}</span>
        )
      }
    >
      {/* No captain option — hidden when search is active */}
      {!query && (
        <PickerItem
          selected={!captainType && !captainId}
          onClick={() => {
            onUpdate({ captain_type: null, captain_id: null });
            setOpen(false);
          }}
        >
          <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
          <span className="text-muted-foreground">{t(($) => $.pickers.captain.trigger_no_captain)}</span>
        </PickerItem>
      )}

      {/* Agents only — captain is agent-only in v1 */}
      {filteredAgents.length > 0 && (
        <PickerSection label={t(($) => $.pickers.captain.agents_group)}>
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
                selected={isSelected(a.id)}
                disabled={!allowed}
                tooltip={!allowed ? decision.message : undefined}
                onClick={() => {
                  if (!allowed) return;
                  onUpdate({
                    captain_type: "agent",
                    captain_id: a.id,
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

      {filteredAgents.length === 0 && filter && <PickerEmpty />}
    </PropertyPicker>
  );
}
