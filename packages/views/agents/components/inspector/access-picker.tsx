"use client";

import { useState } from "react";
import { Globe, Lock, Users, UsersRound } from "lucide-react";
import type {
  AgentInvocationTarget,
  AgentInvocationTargetInput,
  AgentPermissionMode,
  AgentVisibility,
  MemberWithUser,
} from "@multica/core/types";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  PickerItem,
  PropertyPicker,
} from "../../../issues/components/pickers";
import { ActorAvatar } from "../../../common/actor-avatar";
import { VisibilityBadge } from "../visibility-badge";
import { useT } from "../../../i18n";
import { CHIP_CLASS } from "./chip";

/**
 * AccessPicker — the owner-facing control for MUL-3963 agent invocation
 * permissions. It reads/writes `permission_mode` + `invocation_targets`
 * (the authoritative gate) rather than the legacy derived `visibility` field.
 *
 * Access is EITHER Private (only me) OR Public with a STACKABLE, MIXED
 * allow-list: the owner can combine "Everyone in workspace" + any number of
 * specific members + (future) teams on the same agent. `canInvokeAgent` on
 * the backend admits an actor when they match ANY target (OR), so the picker
 * emits the full union of every selected target and the whole set is replaced
 * on save. Team is a disabled placeholder in v1 but the structure is already
 * multi-select; any team targets that already exist are preserved on save.
 *
 * Non-editors get the read-only `<VisibilityBadge>` so the display surface is
 * unchanged for viewers.
 */

export type AccessChange = {
  permission_mode: AgentPermissionMode;
  invocation_targets: AgentInvocationTargetInput[];
};

function hasWorkspaceTarget(targets: AgentInvocationTarget[]): boolean {
  return targets.some((t) => t.target_type === "workspace");
}

function selectedMemberIds(targets: AgentInvocationTarget[]): string[] {
  return targets
    .filter((t) => t.target_type === "member" && t.target_id !== null)
    .map((t) => t.target_id as string);
}

function selectedTeamIds(targets: AgentInvocationTarget[]): string[] {
  return targets
    .filter((t) => t.target_type === "team" && t.target_id !== null)
    .map((t) => t.target_id as string);
}

export function AccessPicker({
  permissionMode,
  invocationTargets,
  visibility,
  members,
  canEdit = true,
  hasComposioAllowlist = false,
  onChange,
}: {
  permissionMode: AgentPermissionMode;
  invocationTargets: AgentInvocationTarget[];
  /** Derived visibility, used only for the read-only badge path. */
  visibility: AgentVisibility;
  members: MemberWithUser[];
  /** When false, render a read-only `<VisibilityBadge>` and skip the popover. */
  canEdit?: boolean;
  /**
   * True when the agent already has a non-empty Composio toolkit allowlist.
   * Surfaces a one-time hint when the owner shares a previously-private agent,
   * since sharing widens who can drive those apps through the agent.
   */
  hasComposioAllowlist?: boolean;
  onChange: (next: AccessChange) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  const [showComposioHint, setShowComposioHint] = useState(false);

  if (!canEdit) {
    return <VisibilityBadge value={visibility} />;
  }

  const isPrivate = permissionMode === "private";
  const workspaceOn = !isPrivate && hasWorkspaceTarget(invocationTargets);
  const memberIds = selectedMemberIds(invocationTargets);
  // Team targets aren't editable in v1, but must be preserved across saves so
  // a batch-replace never silently drops them.
  const teamIds = selectedTeamIds(invocationTargets);
  const memberCount = memberIds.length;

  // Build the union of every selected target and emit it. An empty union
  // collapses to Private (owner-only), which is the intuitive "nothing shared"
  // state rather than a public_to with no grants.
  const emit = (next: {
    workspace: boolean;
    members: string[];
    teams: string[];
  }) => {
    const targets: AgentInvocationTargetInput[] = [];
    if (next.workspace) targets.push({ target_type: "workspace" });
    for (const id of next.members)
      targets.push({ target_type: "member", target_id: id });
    for (const id of next.teams)
      targets.push({ target_type: "team", target_id: id });
    if (targets.length === 0) {
      void onChange({ permission_mode: "private", invocation_targets: [] });
      return;
    }
    void onChange({
      permission_mode: "public_to",
      invocation_targets: targets,
    });
  };

  const maybeFlagComposio = (goingPublic: boolean) => {
    if (hasComposioAllowlist && isPrivate && goingPublic) {
      setShowComposioHint(true);
    }
  };

  const choosePrivate = () => {
    setShowComposioHint(false);
    void onChange({ permission_mode: "private", invocation_targets: [] });
  };

  const toggleWorkspace = (checked: boolean) => {
    maybeFlagComposio(checked);
    emit({ workspace: checked, members: memberIds, teams: teamIds });
  };

  const toggleMember = (userId: string, checked: boolean) => {
    maybeFlagComposio(checked);
    const next = new Set(memberIds);
    if (checked) next.add(userId);
    else next.delete(userId);
    emit({ workspace: workspaceOn, members: Array.from(next), teams: teamIds });
  };

  const TriggerIcon = isPrivate
    ? Lock
    : workspaceOn
      ? Globe
      : memberCount > 0
        ? Users
        : Globe;

  const triggerLabel = isPrivate
    ? t(($) => $.access.trigger_private)
    : workspaceOn
      ? t(($) => $.access.trigger_workspace)
      : memberCount > 0
        ? t(($) => $.access.trigger_members_count, { count: memberCount })
        : t(($) => $.access.trigger_members_empty);

  const tooltip = t(($) => $.access.tooltip);

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(v) => {
        setOpen(v);
        if (v) setShowComposioHint(false);
      }}
      width="w-auto min-w-[15rem]"
      align="start"
      tooltip={tooltip}
      triggerRender={
        <button type="button" className={CHIP_CLASS} aria-label={tooltip} />
      }
      trigger={
        <>
          <TriggerIcon className="h-3 w-3 shrink-0 text-muted-foreground" />
          <span className="truncate">{triggerLabel}</span>
        </>
      }
    >
      {/* Private is the exclusive "not shared" choice: selecting it clears the
          whole allow-list. */}
      <PickerItem selected={isPrivate} onClick={choosePrivate}>
        <Lock className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <div className="text-left">
          <div className="font-medium">{t(($) => $.access.private_title)}</div>
          <div className="text-xs text-muted-foreground">
            {t(($) => $.access.private_desc)}
          </div>
        </div>
      </PickerItem>

      <div className="mt-1 border-t pt-1">
        <div className="px-2 pb-1 pt-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
          {t(($) => $.access.public_group)}
        </div>

        {/* Everyone in workspace — stackable with member/team targets. */}
        <label className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent">
          <Checkbox
            checked={workspaceOn}
            onCheckedChange={(v) => toggleWorkspace(v === true)}
            aria-label={t(($) => $.access.workspace_title)}
          />
          <Globe className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1 text-left">
            <div className="font-medium">
              {t(($) => $.access.workspace_title)}
            </div>
            <div className="truncate text-xs text-muted-foreground">
              {t(($) => $.access.workspace_desc)}
            </div>
          </div>
        </label>
      </div>

      {/* Specific people — multi-select, stacks with the workspace toggle. */}
      <div className="mt-1 border-t pt-1">
        <div className="flex items-center gap-1.5 px-2 pb-1 pt-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
          <Users className="h-3 w-3 shrink-0" />
          {t(($) => $.access.members_group)}
        </div>
        {members.length === 0 ? (
          <div className="px-2 py-2 text-xs text-muted-foreground">
            {t(($) => $.access.members_empty)}
          </div>
        ) : (
          <div className="max-h-48 overflow-y-auto">
            {members.map((m) => {
              const checked = memberIds.includes(m.user_id);
              return (
                <label
                  key={m.user_id}
                  className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent"
                >
                  <Checkbox
                    checked={checked}
                    onCheckedChange={(v) => toggleMember(m.user_id, v === true)}
                    aria-label={m.name}
                  />
                  <ActorAvatar
                    actorType="member"
                    actorId={m.user_id}
                    size={18}
                  />
                  <span className="min-w-0 flex-1 truncate">{m.name}</span>
                </label>
              );
            })}
          </div>
        )}
      </div>

      {/* Team — reserved for a future release; shown disabled so the roadmap
          is visible without being actionable. The emit logic already carries
          team targets through so nothing is lost once teams ship. */}
      <div className="mt-1 border-t pt-1">
        <div className="flex items-center gap-1.5 px-2 py-1.5 text-sm text-muted-foreground opacity-60">
          <UsersRound className="h-3.5 w-3.5 shrink-0" />
          <span className="font-medium">{t(($) => $.access.team_title)}</span>
          <span className="rounded bg-muted px-1 py-0.5 text-[10px] font-medium">
            {t(($) => $.access.team_coming_soon)}
          </span>
        </div>
      </div>

      {showComposioHint && (
        <div className="mx-1 mt-1 rounded-md bg-amber-500/10 px-2 py-1.5 text-xs text-amber-700 dark:text-amber-400">
          {t(($) => $.access.composio_switch_hint)}
        </div>
      )}
    </PropertyPicker>
  );
}
