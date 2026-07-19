"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Globe, Loader2, Lock, Users } from "lucide-react";
import type {
  AgentInvocationTarget,
  AgentInvocationTargetInput,
  AgentPermissionMode,
  AgentVisibility,
  MemberWithUser,
} from "@multica/core/types";
import { effectiveAccessScope } from "@multica/core/agents";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { ActorAvatar } from "../../../common/actor-avatar";
import { useT } from "../../../i18n";

export type AccessChange = {
  permission_mode: AgentPermissionMode;
  invocation_targets: AgentInvocationTargetInput[];
};

type AccessScope = "private" | "workspace" | "members";


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
 * choosing a scope only updates the draft; nothing is persisted until the
 * owner explicitly saves the complete selection.
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
  onReadyChange,
  onChange,
  hideFooter = false,
}: {
  permissionMode: AgentPermissionMode;
  invocationTargets: AgentInvocationTarget[] | undefined;
  visibility: AgentVisibility;
  members: MemberWithUser[];
  ownerId?: string | null;
  canEdit?: boolean;
  hasComposioAllowlist?: boolean;
  onDirtyChange?: (dirty: boolean) => void;
  /** Notified when the draft becomes committable / non-committable.
   *  The optional second argument is the current AccessChange when the draft
   *  is ready, so the parent can store it for immediate use (e.g. the bulk
   *  dialog's Apply button). */
  onReadyChange?: (ready: boolean, change?: AccessChange) => void;
  onChange?: (next: AccessChange) => Promise<void> | void;
  /** When true, suppress the bottom Save footer — the parent dialog owns
   *  the apply trigger (e.g. the bulk "Set access scope" dialog). */
  hideFooter?: boolean;
}) {
  const { t } = useT("agents");
  const { t: tc } = useT("common");
  // Centralized derivation (MUL-3963): map canonical scope to picker's
  // local draft key (private / workspace / members).
  const canonical = effectiveAccessScope(permissionMode, invocationTargets);
  const persistedPrivate = permissionMode === "private";
  const persistedWorkspace = canonical === "workspace";
  const persistedScope: AccessScope = persistedPrivate
    ? "private"
    : persistedWorkspace
      ? "workspace"
      : "members";
  const persistedMembers = useMemo(
    () => selectedTargetIds(invocationTargets, "member"),
    [invocationTargets],
  );
  const teamIds = useMemo(
    () => selectedTargetIds(invocationTargets, "team"),
    [invocationTargets],
  );

  const [draftScope, setDraftScope] = useState<AccessScope>(persistedScope);
  const [draftMembers, setDraftMembers] = useState(persistedMembers);
  const [saving, setSaving] = useState(false);
  /** Tracks whether the user has explicitly selected a scope (clicked any
   *  radio). Once true, the picker considers the draft "ready" even if the
   *  selection matches the persisted default — applying the default to a
   *  bulk selection is still a valid action. */
  const [hasInteracted, setHasInteracted] = useState(false);

  /** Wrapper that marks the picker as interacted and updates the draft scope. */
  const selectDraftScope = (scope: AccessScope) => {
    setHasInteracted(true);
    setDraftScope(scope);
  };

  // Only reset the draft when the PERSISTED values actually change by value,
  // not by reference. A prop like invocationTargets={[]} creates a new array
  // on every parent render, which would otherwise cause this effect to fire
  // on every render and reset the user's in-progress edits.
  const prevPersistedScopeRef = useRef(persistedScope);
  const prevPersistedMembersRef = useRef(persistedMembers);
  useEffect(() => {
    const scopeChanged = persistedScope !== prevPersistedScopeRef.current;
    const membersChanged =
      persistedMembers.length !== prevPersistedMembersRef.current.length ||
      persistedMembers.some(
        (id, i) => id !== prevPersistedMembersRef.current[i],
      );
    if (scopeChanged || membersChanged) {
      setDraftScope(persistedScope);
      setDraftMembers(persistedMembers);
      prevPersistedScopeRef.current = persistedScope;
      prevPersistedMembersRef.current = persistedMembers;
    }
  }, [persistedScope, persistedMembers]);

  const editableMembers = ownerId
    ? members.filter((member) => member.user_id !== ownerId)
    : members;

  const sameMembers =
    draftMembers.length === persistedMembers.length &&
    draftMembers.every((id) => persistedMembers.includes(id));
  const dirty =
    draftScope !== persistedScope ||
    (draftScope === "members" && !sameMembers);
  const hasMemberTarget = draftMembers.length > 0;

  useEffect(() => {
    onDirtyChange?.(dirty);
    return () => onDirtyChange?.(false);
  }, [dirty, onDirtyChange]);

  /** The change the current draft would commit, or null when the draft is not
   *  committable (specific-people with zero targets). Memoized by draft value:
   *  a parent that stores this in state re-renders the picker with an identical
   *  reference, so the notify effect below stays quiet. */
  const draftChange = useMemo<AccessChange | null>(() => {
    if (draftScope === "private") {
      return { permission_mode: "private", invocation_targets: [] };
    }
    const targets: AgentInvocationTargetInput[] = [];
    if (draftScope === "workspace") {
      targets.push({ target_type: "workspace" });
    }
    if (draftScope === "members") {
      if (draftMembers.length === 0) return null;
      for (const id of draftMembers) {
        targets.push({ target_type: "member", target_id: id });
      }
      for (const id of teamIds) {
        targets.push({ target_type: "team", target_id: id });
      }
    }
    return { permission_mode: "public_to", invocation_targets: targets };
  }, [draftScope, draftMembers, teamIds]);

  /** Ready once the user has explicitly selected a scope (hasInteracted) AND
   *  the draft is committable. Does NOT require the draft to differ from the
   *  persisted state — applying the default scope to a bulk selection is a
   *  valid action. */
  const readyChange = hasInteracted ? draftChange : null;

  // Notify on value change, never on render. `onReadyChange` must be stable in
  // the caller; combined with the memoized `readyChange` that keeps a parent
  // storing the change in state from re-triggering this effect. Deliberately no
  // cleanup: clearing the parent whenever a dependency changes is what turned
  // this into an update loop. The parent owns resetting its own state.
  useEffect(() => {
    onReadyChange?.(readyChange !== null, readyChange ?? undefined);
  }, [readyChange, onReadyChange]);

  const toggleMember = (userId: string, checked: boolean) => {
    setDraftMembers((current) => {
      const next = new Set(current);
      if (checked) next.add(userId);
      else next.delete(userId);
      return Array.from(next);
    });
  };

  const save = async () => {
    if (!onChange || saving || !draftChange) return;
    setSaving(true);
    try {
      await onChange(draftChange);
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
          selected={draftScope === "private"}
          onSelect={() => selectDraftScope("private")}
        />
        <AccessChoice
          name="agent-access-mode"
          value="workspace"
          icon={Globe}
          title={t(($) => $.access.workspace_title)}
          description={t(($) => $.access.workspace_desc)}
          selected={draftScope === "workspace"}
          onSelect={() => selectDraftScope("workspace")}
        />
        <AccessChoice
          name="agent-access-mode"
          value="members"
          icon={Users}
          title={t(($) => $.access.members_title)}
          description={t(($) => $.access.members_desc)}
          selected={draftScope === "members"}
          onSelect={() => selectDraftScope("members")}
        />
      </div>

      {draftScope === "members" ? (
        <div className="border-t border-surface-border bg-muted/20 px-4 py-5 sm:px-6">
          <div>
            {editableMembers.length === 0 ? (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.access.members_empty)}
              </p>
            ) : (
              <div className="max-h-64 divide-y divide-surface-border overflow-y-auto rounded-lg border bg-background overscroll-contain">
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

          {!hasMemberTarget ? (
            <p className="mt-3 text-xs text-destructive" role="alert">
              {t(($) => $.access.shared_target_required)}
            </p>
          ) : null}
        </div>
      ) : null}

      {hasComposioAllowlist && persistedPrivate && draftScope !== "private" ? (
        <div className="border-t border-surface-border bg-muted/20 px-4 py-4 sm:px-6">
          <p className="border-l-2 border-warning pl-3 text-xs leading-5 text-muted-foreground">
            {t(($) => $.access.composio_switch_hint)}
          </p>
        </div>
      ) : null}

      {hideFooter ? null : (
        <div className="flex justify-end border-t border-surface-border px-4 py-3.5">
          <Button
            type="button"
            onClick={() => void save()}
            disabled={
              !onChange || !dirty || saving || (draftScope === "members" && !hasMemberTarget)
            }
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
      )}
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
