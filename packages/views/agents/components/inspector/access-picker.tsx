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
 * (the authoritative gate) rather than the legacy derived `visibility`
 * field, and offers four choices:
 *
 *   - Private (Only me)             → permission_mode "private", no targets
 *   - Public to workspace           → public_to + a workspace target
 *   - Public to specific people     → public_to + one member target each
 *   - Public to team                → DISABLED ("Coming soon"), v1-inert
 *
 * Non-editors get the read-only `<VisibilityBadge>` (the same badge path the
 * old VisibilityPicker used) so the display surface is unchanged for viewers.
 */

export type AccessChange = {
  permission_mode: AgentPermissionMode;
  invocation_targets: AgentInvocationTargetInput[];
};

type AccessKind = "private" | "workspace" | "members" | "team";

function deriveKind(
  mode: AgentPermissionMode,
  targets: AgentInvocationTarget[],
): AccessKind {
  if (mode === "private") return "private";
  if (targets.some((t) => t.target_type === "workspace")) return "workspace";
  if (targets.some((t) => t.target_type === "member")) return "members";
  if (targets.some((t) => t.target_type === "team")) return "team";
  // public_to with no resolvable target — treat as workspace for display.
  return "workspace";
}

function selectedMemberIds(targets: AgentInvocationTarget[]): string[] {
  return targets
    .filter((t) => t.target_type === "member" && t.target_id !== null)
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
   * Surfaces a one-time hint when the owner switches from Private to a public
   * mode, since sharing widens who can drive those apps through the agent.
   */
  hasComposioAllowlist?: boolean;
  onChange: (next: AccessChange) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  const currentKind = deriveKind(permissionMode, invocationTargets);
  const [draftKind, setDraftKind] = useState<AccessKind>(currentKind);
  const [showComposioHint, setShowComposioHint] = useState(false);

  if (!canEdit) {
    return <VisibilityBadge value={visibility} />;
  }

  const memberIds = selectedMemberIds(invocationTargets);
  const memberCount = memberIds.length;

  const triggerIcon =
    currentKind === "private"
      ? Lock
      : currentKind === "members"
        ? Users
        : currentKind === "team"
          ? UsersRound
          : Globe;
  const TriggerIcon = triggerIcon;

  const triggerLabel =
    currentKind === "private"
      ? t(($) => $.access.trigger_private)
      : currentKind === "workspace"
        ? t(($) => $.access.trigger_workspace)
        : currentKind === "team"
          ? t(($) => $.access.trigger_team)
          : memberCount > 0
            ? t(($) => $.access.trigger_members_count, { count: memberCount })
            : t(($) => $.access.trigger_members_empty);

  const tooltip = t(($) => $.access.tooltip);

  // A switch from Private to any public mode warrants the Composio heads-up
  // when an allowlist already exists.
  const maybeFlagComposio = (nextKind: AccessKind) => {
    if (
      hasComposioAllowlist &&
      currentKind === "private" &&
      (nextKind === "workspace" || nextKind === "members")
    ) {
      setShowComposioHint(true);
    }
  };

  const choosePrivate = () => {
    setDraftKind("private");
    setShowComposioHint(false);
    setOpen(false);
    void onChange({ permission_mode: "private", invocation_targets: [] });
  };

  const chooseWorkspace = () => {
    maybeFlagComposio("workspace");
    setDraftKind("workspace");
    setOpen(false);
    void onChange({
      permission_mode: "public_to",
      invocation_targets: [{ target_type: "workspace" }],
    });
  };

  // "Specific people" reveals the member checklist and does NOT emit until a
  // member is actually chosen — an empty member set would derive to a
  // no-grant (owner-only) state, so we wait for intent.
  const chooseMembers = () => {
    maybeFlagComposio("members");
    setDraftKind("members");
  };

  const toggleMember = (userId: string, checked: boolean) => {
    const next = new Set(memberIds);
    if (checked) next.add(userId);
    else next.delete(userId);
    void onChange({
      permission_mode: "public_to",
      invocation_targets: Array.from(next).map((id) => ({
        target_type: "member",
        target_id: id,
      })),
    });
  };

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(v) => {
        setOpen(v);
        if (v) {
          // Reset transient UI to reflect the persisted state on each open.
          setDraftKind(currentKind);
          setShowComposioHint(false);
        }
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
      <PickerItem selected={draftKind === "private"} onClick={choosePrivate}>
        <Lock className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <div className="text-left">
          <div className="font-medium">{t(($) => $.access.private_title)}</div>
          <div className="text-xs text-muted-foreground">
            {t(($) => $.access.private_desc)}
          </div>
        </div>
      </PickerItem>

      <PickerItem selected={draftKind === "workspace"} onClick={chooseWorkspace}>
        <Globe className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <div className="text-left">
          <div className="font-medium">{t(($) => $.access.workspace_title)}</div>
          <div className="text-xs text-muted-foreground">
            {t(($) => $.access.workspace_desc)}
          </div>
        </div>
      </PickerItem>

      <PickerItem selected={draftKind === "members"} onClick={chooseMembers}>
        <Users className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <div className="text-left">
          <div className="font-medium">{t(($) => $.access.members_title)}</div>
          <div className="text-xs text-muted-foreground">
            {t(($) => $.access.members_desc)}
          </div>
        </div>
      </PickerItem>

      {/* Team — reserved for a future release; shown disabled so the roadmap
          is visible without being actionable. */}
      <PickerItem selected={false} disabled onClick={() => {}}>
        <UsersRound className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <div className="text-left">
          <div className="flex items-center gap-1.5 font-medium">
            {t(($) => $.access.team_title)}
            <span className="rounded bg-muted px-1 py-0.5 text-[10px] font-medium text-muted-foreground">
              {t(($) => $.access.team_coming_soon)}
            </span>
          </div>
          <div className="text-xs text-muted-foreground">
            {t(($) => $.access.team_desc)}
          </div>
        </div>
      </PickerItem>

      {showComposioHint && (
        <div className="mx-1 mt-1 rounded-md bg-amber-500/10 px-2 py-1.5 text-xs text-amber-700 dark:text-amber-400">
          {t(($) => $.access.composio_switch_hint)}
        </div>
      )}

      {draftKind === "members" && (
        <div className="mt-1 border-t pt-1">
          <div className="px-2 pb-1 pt-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
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
                      onCheckedChange={(v) =>
                        toggleMember(m.user_id, v === true)
                      }
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
      )}
    </PropertyPicker>
  );
}
