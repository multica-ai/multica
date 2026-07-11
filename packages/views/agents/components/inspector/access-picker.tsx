"use client";

import { useState } from "react";
import { Check, Globe, Lock, Users } from "lucide-react";
import type {
  AgentInvocationTarget,
  AgentInvocationTargetInput,
  AgentPermissionMode,
  AgentVisibility,
  MemberWithUser,
} from "@multica/core/types";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { ActorAvatar } from "../../../common/actor-avatar";
import { useT } from "../../../i18n";

export type AccessChange = {
  permission_mode: AgentPermissionMode;
  invocation_targets: AgentInvocationTargetInput[];
};

function hasWorkspaceTarget(
  targets: AgentInvocationTarget[] | undefined | null,
): boolean {
  return (targets ?? []).some((target) => target.target_type === "workspace");
}

function selectedMemberIds(
  targets: AgentInvocationTarget[] | undefined | null,
): string[] {
  return (targets ?? [])
    .filter(
      (target) =>
        target.target_type === "member" && target.target_id !== null,
    )
    .map((target) => target.target_id as string);
}

function selectedTeamIds(
  targets: AgentInvocationTarget[] | undefined | null,
): string[] {
  return (targets ?? [])
    .filter(
      (target) => target.target_type === "team" && target.target_id !== null,
    )
    .map((target) => target.target_id as string);
}

/**
 * Expanded access editor for General settings. Permission choices stay visible
 * instead of hiding inside an inspector popover, while writes keep the same
 * owner-only and mixed-target semantics as the backend.
 */
export function AccessPicker({
  permissionMode,
  invocationTargets,
  visibility: _visibility,
  members,
  ownerId,
  canEdit = true,
  hasComposioAllowlist = false,
  onChange,
}: {
  permissionMode: AgentPermissionMode;
  invocationTargets: AgentInvocationTarget[] | undefined;
  visibility: AgentVisibility;
  members: MemberWithUser[];
  ownerId?: string | null;
  canEdit?: boolean;
  hasComposioAllowlist?: boolean;
  onChange: (next: AccessChange) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const [showComposioHint, setShowComposioHint] = useState(false);

  const isPrivate = permissionMode === "private";
  const workspaceOn = !isPrivate && hasWorkspaceTarget(invocationTargets);
  const memberIds = selectedMemberIds(invocationTargets);
  const teamIds = selectedTeamIds(invocationTargets);
  const editableMembers = ownerId
    ? members.filter((member) => member.user_id !== ownerId)
    : members;

  const summaryLabel = isPrivate
    ? t(($) => $.access.trigger_private)
    : workspaceOn
      ? t(($) => $.access.trigger_workspace)
      : memberIds.length > 0
        ? t(($) => $.access.trigger_members_count, {
            count: memberIds.length,
          })
        : t(($) => $.access.trigger_members_empty);

  const emit = (next: {
    workspace: boolean;
    members: string[];
    teams: string[];
  }) => {
    const targets: AgentInvocationTargetInput[] = [];
    if (next.workspace) targets.push({ target_type: "workspace" });
    for (const id of next.members) {
      targets.push({ target_type: "member", target_id: id });
    }
    for (const id of next.teams) {
      targets.push({ target_type: "team", target_id: id });
    }

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
    if (isPrivate) return;
    setShowComposioHint(false);
    void onChange({ permission_mode: "private", invocation_targets: [] });
  };

  const chooseShared = () => {
    if (!isPrivate) return;
    maybeFlagComposio(true);
    const hasExistingGrant =
      workspaceOn || memberIds.length > 0 || teamIds.length > 0;
    emit({
      workspace: hasExistingGrant ? workspaceOn : true,
      members: memberIds,
      teams: teamIds,
    });
  };

  const toggleWorkspace = (checked: boolean) => {
    emit({ workspace: checked, members: memberIds, teams: teamIds });
  };

  const toggleMember = (userId: string, checked: boolean) => {
    const next = new Set(memberIds);
    if (checked) next.add(userId);
    else next.delete(userId);
    emit({ workspace: workspaceOn, members: Array.from(next), teams: teamIds });
  };

  if (!canEdit) {
    return (
      <div
        className="flex items-start gap-3 border-y border-surface-border py-4"
        aria-label={t(($) => $.access.owner_only_readonly)}
        data-testid="access-readonly"
      >
        <span className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
          <Lock className="h-4 w-4" aria-hidden="true" />
        </span>
        <div className="min-w-0">
          <p className="text-sm font-medium">{summaryLabel}</p>
          <p className="mt-0.5 text-xs leading-5 text-muted-foreground">
            {t(($) => $.access.owner_only_readonly)}
          </p>
        </div>
      </div>
    );
  }

  return (
    <fieldset className="space-y-5">
      <legend className="sr-only">{t(($) => $.access.tooltip)}</legend>

      <div
        className="divide-y divide-surface-border border-y border-surface-border"
        role="radiogroup"
        aria-label={t(($) => $.access.tooltip)}
      >
        <AccessChoice
          icon={Lock}
          title={t(($) => $.access.private_title)}
          description={t(($) => $.access.private_desc)}
          selected={isPrivate}
          onSelect={choosePrivate}
        />
        <AccessChoice
          icon={Users}
          title={t(($) => $.access.shared_title)}
          description={t(($) => $.access.shared_desc)}
          selected={!isPrivate}
          onSelect={chooseShared}
        />
      </div>

      {!isPrivate ? (
        <div className="space-y-5 pl-4 sm:pl-6">
          <div>
            <h4 className="text-sm font-medium">
              {t(($) => $.access.public_group)}
            </h4>
            <label className="mt-3 flex cursor-pointer items-start gap-3 border-y border-surface-border py-3">
              <Checkbox
                checked={workspaceOn}
                onCheckedChange={(value) =>
                  toggleWorkspace(value === true)
                }
                aria-label={t(($) => $.access.workspace_title)}
                className="mt-0.5"
              />
              <Globe
                className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground"
                aria-hidden="true"
              />
              <span className="min-w-0">
                <span className="block text-sm font-medium">
                  {t(($) => $.access.workspace_title)}
                </span>
                <span className="mt-0.5 block text-xs leading-5 text-muted-foreground">
                  {t(($) => $.access.workspace_desc)}
                </span>
              </span>
            </label>
          </div>

          <div>
            <h4 className="text-sm font-medium">
              {t(($) => $.access.members_group)}
            </h4>
            {editableMembers.length === 0 ? (
              <p className="mt-3 border-y border-surface-border py-4 text-xs text-muted-foreground">
                {t(($) => $.access.members_empty)}
              </p>
            ) : (
              <div className="mt-3 max-h-64 divide-y divide-surface-border overflow-y-auto border-y border-surface-border">
                {editableMembers.map((member) => {
                  const checked = memberIds.includes(member.user_id);
                  return (
                    <label
                      key={member.user_id}
                      className="flex cursor-pointer items-center gap-3 py-3 hover:bg-surface-hover"
                    >
                      <Checkbox
                        checked={checked}
                        onCheckedChange={(value) =>
                          toggleMember(member.user_id, value === true)
                        }
                        aria-label={member.name}
                      />
                      <ActorAvatar
                        actorType="member"
                        actorId={member.user_id}
                        size="sm"
                      />
                      <span className="min-w-0 flex-1 truncate text-sm">
                        {member.name}
                      </span>
                    </label>
                  );
                })}
              </div>
            )}
          </div>
        </div>
      ) : null}

      {showComposioHint ? (
        <p
          role="status"
          className="border-l-2 border-warning pl-3 text-xs leading-5 text-muted-foreground"
        >
          {t(($) => $.access.composio_switch_hint)}
        </p>
      ) : null}
    </fieldset>
  );
}

function AccessChoice({
  icon: Icon,
  title,
  description,
  selected,
  onSelect,
}: {
  icon: typeof Lock;
  title: string;
  description: string;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className="flex w-full items-start gap-3 px-0 py-3 text-left transition-colors hover:bg-surface-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
    >
      <span className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
        <Icon className="h-4 w-4" aria-hidden="true" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-medium">{title}</span>
        <span className="mt-0.5 block text-xs leading-5 text-muted-foreground">
          {description}
        </span>
      </span>
      <span
        className={`mt-1 flex h-5 w-5 shrink-0 items-center justify-center rounded-full border ${
          selected
            ? "border-foreground bg-foreground text-background"
            : "border-input"
        }`}
        aria-hidden="true"
      >
        {selected ? <Check className="h-3 w-3" /> : null}
      </span>
    </button>
  );
}
