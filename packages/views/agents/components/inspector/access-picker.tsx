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
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import {
  PickerItem,
  PropertyPicker,
} from "../../../issues/components/pickers";
import { ActorAvatar } from "../../../common/actor-avatar";
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
 * the backend admits an actor matching ANY target (OR), so the picker emits
 * the full union of every selected target and the whole set is replaced on
 * save.
 *
 * OWNER-ONLY (MUL-3963): access is the one agent property a workspace admin
 * may NOT change — only the agent owner decides who can run their agent, and
 * the backend rejects a non-owner permission change with 403. So `canEdit`
 * here must be passed as "is the viewer the agent owner", NOT the general
 * manage permission. When `canEdit` is false the control is a static,
 * non-interactive read-only display (current value + a lock affordance +
 * a tooltip explaining only the owner can change it), the same way GitHub /
 * Notion present a permission setting a viewer can see but not edit. There is
 * deliberately no clickable trigger in that state, so a non-owner can never
 * open a picker that the backend would only bounce back.
 */

export type AccessChange = {
  permission_mode: AgentPermissionMode;
  invocation_targets: AgentInvocationTargetInput[];
};

// The helpers below defensively coerce a missing (`undefined` / `null`) list
// to an empty array. `Agent.invocation_targets` is TYPED as a required array,
// and the modern backend always serialises `[]` when there are no grants,
// but older self-host servers / stale query caches / template create
// responses (see `MinimalAgentSchema` in api/schemas.ts) can still surface an
// undefined value at runtime. Without the fallback, opening an agent detail
// page against a legacy backend crashes the whole route with
// "Cannot read properties of undefined (reading 'some')" (GH #4915).
function hasWorkspaceTarget(
  targets: AgentInvocationTarget[] | undefined | null,
): boolean {
  return (targets ?? []).some((t) => t.target_type === "workspace");
}

function selectedMemberIds(
  targets: AgentInvocationTarget[] | undefined | null,
): string[] {
  return (targets ?? [])
    .filter((t) => t.target_type === "member" && t.target_id !== null)
    .map((t) => t.target_id as string);
}

function selectedTeamIds(
  targets: AgentInvocationTarget[] | undefined | null,
): string[] {
  return (targets ?? [])
    .filter((t) => t.target_type === "team" && t.target_id !== null)
    .map((t) => t.target_id as string);
}

export function AccessPicker({
  permissionMode,
  invocationTargets,
  visibility: _visibility,
  members,
  canEdit = true,
  hasComposioAllowlist = false,
  onChange,
}: {
  permissionMode: AgentPermissionMode;
  /**
   * The agent's invocation grants. Typed loose (may be `undefined`) because
   * the schema is `optional()` and older self-host backends / template create
   * responses can omit the field even though the modern shape is a
   * required-array. The internal helpers `?? []` this before reading.
   */
  invocationTargets: AgentInvocationTarget[] | undefined;
  /**
   * Legacy derived visibility. No longer rendered directly (the read-only and
   * editable states both summarise permission_mode + targets), but kept in the
   * props so existing call sites compile unchanged.
   */
  visibility: AgentVisibility;
  members: MemberWithUser[];
  /**
   * True ONLY when the viewer is the agent owner (MUL-3963 access is
   * owner-only). When false, render the static read-only state.
   */
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

  // Display summary of the current access, shared by the read-only and
  // editable states so they never drift.
  const isPrivate = permissionMode === "private";
  const workspaceOn = !isPrivate && hasWorkspaceTarget(invocationTargets);
  const memberIds = selectedMemberIds(invocationTargets);
  // Team targets aren't editable in v1, but must be preserved across saves so
  // a batch-replace never silently drops them.
  const teamIds = selectedTeamIds(invocationTargets);
  const memberCount = memberIds.length;

  const SummaryIcon = isPrivate
    ? Lock
    : workspaceOn
      ? Globe
      : memberCount > 0
        ? Users
        : Globe;

  const summaryLabel = isPrivate
    ? t(($) => $.access.trigger_private)
    : workspaceOn
      ? t(($) => $.access.trigger_workspace)
      : memberCount > 0
        ? t(($) => $.access.trigger_members_count, { count: memberCount })
        : t(($) => $.access.trigger_members_empty);

  // Read-only state for non-owners: current value + lock + owner-only tooltip.
  // No interactive trigger is rendered, so the control can never be clicked
  // into a change the backend would reject.
  if (!canEdit) {
    const readOnlyMsg = t(($) => $.access.owner_only_readonly);
    return (
      <Tooltip>
        <TooltipTrigger
          render={
            <span
              className="inline-flex items-center gap-1 text-xs text-muted-foreground"
              aria-label={readOnlyMsg}
              data-testid="access-readonly"
            >
              <SummaryIcon className="h-3 w-3 shrink-0" />
              <span className="truncate">{summaryLabel}</span>
              <Lock className="h-3 w-3 shrink-0 opacity-60" />
            </span>
          }
        />
        <TooltipContent>{readOnlyMsg}</TooltipContent>
      </Tooltip>
    );
  }

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
          <SummaryIcon className="h-3 w-3 shrink-0 text-muted-foreground" />
          <span className="truncate">{summaryLabel}</span>
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
                    size="sm"
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
