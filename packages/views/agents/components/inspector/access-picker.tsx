"use client";

import { useEffect, useMemo, useState } from "react";
import { Globe, Loader2, Lock, Users } from "lucide-react";
import type {
  AgentInvocationTarget,
  AgentInvocationTargetInput,
  AgentPermissionMode,
  AgentVisibility,
  MemberWithUser,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
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

function selectedTargetIds(
  targets: AgentInvocationTarget[] | undefined | null,
  type: "member" | "team",
): string[] {
  return (targets ?? [])
    .filter(
      (target) => target.target_type === type && target.target_id !== null,
    )
    .map((target) => target.target_id as string);
}

/**
 * Draft-first access editor. Visibility changes are security-sensitive, so
 * choosing Shared only reveals the scope controls; nothing is persisted until
 * the owner explicitly saves the complete selection.
 */
export function AccessPicker({
  permissionMode,
  invocationTargets,
  visibility: _visibility,
  members,
  ownerId,
  canEdit = true,
  hasComposioAllowlist = false,
  onDirtyChange,
  onChange,
}: {
  permissionMode: AgentPermissionMode;
  invocationTargets: AgentInvocationTarget[] | undefined;
  visibility: AgentVisibility;
  members: MemberWithUser[];
  ownerId?: string | null;
  canEdit?: boolean;
  hasComposioAllowlist?: boolean;
  onDirtyChange?: (dirty: boolean) => void;
  onChange: (next: AccessChange) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const { t: tc } = useT("common");
  const persistedPrivate = permissionMode === "private";
  const persistedWorkspace =
    !persistedPrivate && hasWorkspaceTarget(invocationTargets);
  const persistedMembers = useMemo(
    () => selectedTargetIds(invocationTargets, "member"),
    [invocationTargets],
  );
  const teamIds = useMemo(
    () => selectedTargetIds(invocationTargets, "team"),
    [invocationTargets],
  );

  const [draftPrivate, setDraftPrivate] = useState(persistedPrivate);
  const [draftWorkspace, setDraftWorkspace] = useState(persistedWorkspace);
  const [draftMembers, setDraftMembers] = useState(persistedMembers);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setDraftPrivate(persistedPrivate);
    setDraftWorkspace(persistedWorkspace);
    setDraftMembers(persistedMembers);
  }, [persistedMembers, persistedPrivate, persistedWorkspace]);

  const editableMembers = ownerId
    ? members.filter((member) => member.user_id !== ownerId)
    : members;

  const sameMembers =
    draftMembers.length === persistedMembers.length &&
    draftMembers.every((id) => persistedMembers.includes(id));
  const dirty =
    draftPrivate !== persistedPrivate ||
    (!draftPrivate &&
      (draftWorkspace !== persistedWorkspace || !sameMembers));
  const hasSharedTarget =
    draftWorkspace || draftMembers.length > 0 || teamIds.length > 0;

  useEffect(() => {
    onDirtyChange?.(dirty);
    return () => onDirtyChange?.(false);
  }, [dirty, onDirtyChange]);

  const chooseMode = (mode: "private" | "shared") => {
    const nextPrivate = mode === "private";
    setDraftPrivate(nextPrivate);
    if (!nextPrivate && !hasSharedTarget) setDraftWorkspace(true);
  };

  const toggleMember = (userId: string, checked: boolean) => {
    setDraftMembers((current) => {
      const next = new Set(current);
      if (checked) next.add(userId);
      else next.delete(userId);
      return Array.from(next);
    });
  };

  const save = async () => {
    if (!dirty || saving || (!draftPrivate && !hasSharedTarget)) return;
    const targets: AgentInvocationTargetInput[] = [];
    if (!draftPrivate && draftWorkspace) {
      targets.push({ target_type: "workspace" });
    }
    if (!draftPrivate) {
      for (const id of draftMembers) {
        targets.push({ target_type: "member", target_id: id });
      }
      for (const id of teamIds) {
        targets.push({ target_type: "team", target_id: id });
      }
    }

    setSaving(true);
    try {
      await onChange({
        permission_mode: draftPrivate ? "private" : "public_to",
        invocation_targets: draftPrivate ? [] : targets,
      });
    } finally {
      setSaving(false);
    }
  };

  if (!canEdit) {
    const summaryLabel = persistedPrivate
      ? t(($) => $.access.trigger_private)
      : persistedWorkspace
        ? t(($) => $.access.trigger_workspace)
        : persistedMembers.length > 0
          ? t(($) => $.access.trigger_members_count, {
              count: persistedMembers.length,
            })
          : t(($) => $.access.trigger_members_empty);

    return (
      <div
        className="flex items-start gap-3 px-4 py-4"
        aria-label={t(($) => $.access.owner_only_readonly)}
        data-testid="access-readonly"
      >
        <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
          <Lock className="size-4" aria-hidden="true" />
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
    <fieldset>
      <legend className="sr-only">{t(($) => $.access.tooltip)}</legend>

      <div className="divide-y divide-surface-border">
        <AccessChoice
          name="agent-access-mode"
          value="private"
          icon={Lock}
          title={t(($) => $.access.private_title)}
          description={t(($) => $.access.private_desc)}
          selected={draftPrivate}
          onSelect={() => chooseMode("private")}
        />
        <AccessChoice
          name="agent-access-mode"
          value="shared"
          icon={Users}
          title={t(($) => $.access.shared_title)}
          description={t(($) => $.access.shared_desc)}
          selected={!draftPrivate}
          onSelect={() => chooseMode("shared")}
        />
      </div>

      {!draftPrivate ? (
        <div className="space-y-6 border-t border-surface-border bg-muted/20 px-4 py-5 sm:px-6">
          <div>
            <h4 className="text-sm font-medium">
              {t(($) => $.access.public_group)}
            </h4>
            <div className="mt-3 flex items-start gap-3 rounded-lg border bg-background p-3">
              <Checkbox
                id="agent-access-workspace"
                checked={draftWorkspace}
                onCheckedChange={(value) =>
                  setDraftWorkspace(value === true)
                }
                className="mt-0.5"
              />
              <label
                htmlFor="agent-access-workspace"
                className="flex min-w-0 flex-1 cursor-pointer items-start gap-3"
              >
                <Globe
                  className="mt-0.5 size-4 shrink-0 text-muted-foreground"
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
          </div>

          <div>
            <h4 className="text-sm font-medium">
              {t(($) => $.access.members_group)}
            </h4>
            {editableMembers.length === 0 ? (
              <p className="mt-3 text-xs text-muted-foreground">
                {t(($) => $.access.members_empty)}
              </p>
            ) : (
              <div className="mt-3 max-h-64 divide-y divide-surface-border overflow-y-auto rounded-lg border bg-background overscroll-contain">
                {editableMembers.map((member) => {
                  const id = `agent-access-member-${member.user_id}`;
                  return (
                    <div
                      key={member.user_id}
                      className="flex items-center gap-3 px-3 py-3 hover:bg-surface-hover"
                    >
                      <Checkbox
                        id={id}
                        checked={draftMembers.includes(member.user_id)}
                        onCheckedChange={(value) =>
                          toggleMember(member.user_id, value === true)
                        }
                      />
                      <label
                        htmlFor={id}
                        className="flex min-w-0 flex-1 cursor-pointer items-center gap-3"
                      >
                        <ActorAvatar
                          actorType="member"
                          actorId={member.user_id}
                          size="sm"
                        />
                        <span className="min-w-0 flex-1 truncate text-sm">
                          {member.name}
                        </span>
                      </label>
                    </div>
                  );
                })}
              </div>
            )}
          </div>

          {!hasSharedTarget ? (
            <p className="text-xs text-destructive" role="alert">
              {t(($) => $.access.shared_target_required)}
            </p>
          ) : null}

          {hasComposioAllowlist && persistedPrivate ? (
            <p className="border-l-2 border-warning pl-3 text-xs leading-5 text-muted-foreground">
              {t(($) => $.access.composio_switch_hint)}
            </p>
          ) : null}
        </div>
      ) : null}

      <div className="flex justify-end border-t border-surface-border px-4 py-3.5">
        <Button
          type="button"
          onClick={() => void save()}
          disabled={!dirty || saving || (!draftPrivate && !hasSharedTarget)}
        >
          {saving ? (
            <Loader2
              className="size-4 animate-spin motion-reduce:animate-none"
              aria-hidden="true"
            />
          ) : null}
          {tc(($) => $.save)}
        </Button>
      </div>
    </fieldset>
  );
}

function AccessChoice({
  name,
  value,
  icon: Icon,
  title,
  description,
  selected,
  onSelect,
}: {
  name: string;
  value: string;
  icon: typeof Lock;
  title: string;
  description: string;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <label className="flex min-h-16 cursor-pointer items-start gap-3 px-4 py-3.5 transition-colors hover:bg-surface-hover">
      <input
        type="radio"
        name={name}
        value={value}
        checked={selected}
        onChange={onSelect}
        className="mt-2 size-4 shrink-0 accent-foreground"
      />
      <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
        <Icon className="size-4" aria-hidden="true" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-medium">{title}</span>
        <span className="mt-0.5 block text-xs leading-5 text-muted-foreground">
          {description}
        </span>
      </span>
    </label>
  );
}
