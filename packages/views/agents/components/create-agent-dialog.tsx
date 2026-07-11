"use client";

import { useState } from "react";
import { Globe, Lock, Users } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { ModelDropdown } from "./model-dropdown";
import { RuntimePicker, isRuntimeUsableForUser } from "./runtime-picker";
import { InstructionsEditor } from "./instructions-editor";
import { SkillMultiSelect } from "./skill-multi-select";
import { AvatarUploadControl } from "../../common/avatar-upload-control";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useFeatureEnabled } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type {
  Agent,
  AgentInvocationTargetInput,
  AgentPermissionMode,
  AgentVisibility,
  RuntimeDevice,
  MemberWithUser,
  CreateAgentRequest,
} from "@multica/core/types";
import { isImeComposing } from "@multica/core/utils";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";
import {
  AGENT_DESCRIPTION_MAX_LENGTH,
  VISIBILITY_DESCRIPTION,
  VISIBILITY_LABEL,
} from "@multica/core/agents";
import { ActorAvatar } from "../../common/actor-avatar";
import { CharCounter } from "./char-counter";
import { useT } from "../../i18n";

export function CreateAgentDialog({
  runtimes,
  runtimesLoading,
  members,
  currentUserId,
  template,
  squadId,
  onClose,
  onCreate,
}: {
  runtimes: RuntimeDevice[];
  runtimesLoading?: boolean;
  members: MemberWithUser[];
  currentUserId: string | null;
  // When provided, the dialog opens in "Duplicate" mode: the visible
  // fields (name / description / runtime / visibility / model) are
  // pre-populated from this agent, and the hidden fields
  // (instructions / custom_args / custom_env / max_concurrent_tasks)
  // are forwarded to the create call so the new agent is a true clone.
  // Skills are copied separately by the caller after createAgent
  // succeeds — they're not part of CreateAgentRequest.
  template?: Agent | null;
  // When set, every successful create is followed by
  // addSquadMember(squadId, agent) so the new agent joins this squad.
  // If the squad-join call fails the agent still exists and the dialog
  // surfaces a warning toast — the user can add it manually from the
  // Members tab.
  squadId?: string;
  onClose: () => void;
  // Returns the created Agent so the dialog can run a follow-up
  // setAgentSkills with the IDs the user picked in the form. Pre-skill-
  // section callers can keep returning `void`; the dialog tolerates a
  // falsy return (no follow-up runs).
  onCreate: (data: CreateAgentRequest) => Promise<Agent | void>;
}) {
  const { t } = useT("agents");
  const isDuplicate = !!template;
  const queryClient = useQueryClient();
  const wsId = useWorkspaceId();
  // MUL-4010: rolls out the private / public_to access model in the create
  // flow to match the AccessPicker on the agent detail page. Shares the
  // `composio_mcp_apps` switch with the Composio rollout — the MUL-3963
  // permission model exists to gate Composio sharing, so both surfaces flip
  // together. Defaults OFF so production stays on the legacy Workspace /
  // Personal toggle until Composio is greenlit.
  const accessPickerEnabled = useFeatureEnabled(COMPOSIO_MCP_APPS_FLAG, false);

  // Name defaults: duplicate uses "<original> copy". Manual-create starts blank.
  const [name, setName] = useState(
    template ? `${template.name}${t(($) => $.create_dialog.duplicate_copy_suffix)}` : "",
  );
  const [description, setDescription] = useState(template?.description ?? "");
  // Legacy visibility state. Kept as the source of truth when
  // `accessPickerEnabled` is false; only used to seed the new access state
  // when the flag flips on for a duplicate.
  const [visibility, setVisibility] = useState<AgentVisibility>(
    template?.visibility ?? "workspace",
  );

  // New access state (MUL-3963 aligned). When duplicating, seed from the
  // template so the clone lands with the source agent's grants; otherwise
  // default to public_to + workspace, matching the legacy "Workspace" default
  // so a plain "click Create" produces the same result as before.
  const [permissionMode, setPermissionMode] = useState<AgentPermissionMode>(
    template?.permission_mode ?? "public_to",
  );
  const [workspaceTargetOn, setWorkspaceTargetOn] = useState<boolean>(() => {
    if (template) {
      return (template.invocation_targets ?? []).some(
        (tgt) => tgt.target_type === "workspace",
      );
    }
    return true;
  });
  const [selectedMemberIds, setSelectedMemberIds] = useState<Set<string>>(
    () =>
      new Set(
        (template?.invocation_targets ?? [])
          .filter((tgt) => tgt.target_type === "member" && tgt.target_id)
          .map((tgt) => tgt.target_id as string),
      ),
  );

  // Team targets on the template are preserved across the create so we don't
  // silently drop a grant type the picker doesn't expose yet (mirrors
  // AccessPicker's `teamIds` pass-through).
  const templateTeamTargets: AgentInvocationTargetInput[] = (
    template?.invocation_targets ?? []
  )
    .filter((tgt) => tgt.target_type === "team" && tgt.target_id)
    .map((tgt) => ({
      target_type: "team" as const,
      target_id: tgt.target_id as string,
    }));

  const [model, setModel] = useState(template?.model ?? "");
  const [instructions, setInstructions] = useState(template?.instructions ?? "");
  const [avatarUrl, setAvatarUrl] = useState<string | null>(template?.avatar_url ?? null);
  const [selectedSkillIds, setSelectedSkillIds] = useState<Set<string>>(
    () => new Set(template?.skills.map((s) => s.id) ?? []),
  );
  const [creating, setCreating] = useState(false);

  // Duplicate-mode pre-fill: clone lands on the source agent's runtime so
  // the user doesn't have to re-pick. Skipped when that runtime is now
  // locked for the caller (Create would 403). Empty fallback hands the
  // job to RuntimePicker — it owns filter state, so it's the only place
  // that knows which runtimes are visible right now.
  const [selectedRuntimeId, setSelectedRuntimeId] = useState(() => {
    const templateRuntime = template?.runtime_id
      ? runtimes.find((r) => r.id === template.runtime_id)
      : undefined;
    if (templateRuntime && isRuntimeUsableForUser(templateRuntime, currentUserId)) {
      return templateRuntime.id;
    }
    return "";
  });

  const selectedRuntime = runtimes.find((d) => d.id === selectedRuntimeId) ?? null;
  // Defense-in-depth: even if a locked runtime somehow ends up selected
  // (e.g. duplicate of an agent whose template runtime is now locked, and
  // the workspace has no usable fallback), gate Create on it so we don't
  // submit a request the backend will reject with 403.
  const selectedRuntimeLocked =
    selectedRuntime != null &&
    !isRuntimeUsableForUser(selectedRuntime, currentUserId);
  const accessSelectionInvalid =
    accessPickerEnabled &&
    permissionMode === "public_to" &&
    !workspaceTargetOn &&
    selectedMemberIds.size === 0 &&
    templateTeamTargets.length === 0;

  // Shared squad-join follow-up. Returns nothing — the caller has
  // already shown its create-success toast; we only need to surface a
  // warning when the agent landed but the squad-join failed. Cache
  // invalidation for the squad's members list rides along so the
  // Members tab re-renders without a manual refetch.
  const attachToSquad = async (agentId: string, displayName: string) => {
    if (!squadId) return;
    try {
      await api.addSquadMember(squadId, {
        member_type: "agent",
        member_id: agentId,
      });
      if (wsId) {
        queryClient.invalidateQueries({
          queryKey: [...workspaceKeys.squads(wsId), squadId, "members"],
        });
        queryClient.invalidateQueries({
          queryKey: [...workspaceKeys.squads(wsId), squadId],
        });
      }
    } catch (err) {
      toast.warning(
        t(($) => $.create_dialog.squad_join_failed_toast, {
          name: displayName,
          error: err instanceof Error ? err.message : "unknown error",
        }),
      );
    }
  };

  const handleSubmit = async () => {
    if (
      !name.trim() ||
      !selectedRuntime ||
      selectedRuntimeLocked ||
      accessSelectionInvalid
    ) {
      return;
    }
    setCreating(true);

    try {
      const trimmedInstructions = instructions.trim();
      const data: CreateAgentRequest = {
        name: name.trim(),
        description: description.trim(),
        runtime_id: selectedRuntime.id,
        model: model.trim() || undefined,
        instructions: trimmedInstructions || undefined,
        avatar_url: avatarUrl ?? undefined,
      };
      if (accessPickerEnabled) {
        // New MUL-3963 shape: send the authoritative permission fields and
        // let the backend derive the legacy `visibility` field. Mirror the
        // AccessPicker `emit` normalisation — a public_to with zero targets
        // collapses to private as defense in depth so the backend never sees
        // an "empty public" request. Team targets from the template survive.
        const invocationTargets: AgentInvocationTargetInput[] = [];
        if (permissionMode === "public_to") {
          if (workspaceTargetOn) {
            invocationTargets.push({ target_type: "workspace" });
          } else {
            for (const id of selectedMemberIds) {
              invocationTargets.push({ target_type: "member", target_id: id });
            }
            for (const tgt of templateTeamTargets) {
              invocationTargets.push(tgt);
            }
          }
        }
        const collapseToPrivate =
          permissionMode === "public_to" && invocationTargets.length === 0;
        data.permission_mode = collapseToPrivate ? "private" : permissionMode;
        data.invocation_targets = collapseToPrivate ? [] : invocationTargets;
      } else {
        // Legacy path: send the visibility toggle unchanged. The backend
        // maps this to permission_mode + invocation_targets server-side.
        data.visibility = visibility;
      }
      if (template) {
        // Duplicate path: forward the hidden config fields the source
        // agent had so the clone is functional out of the box (args /
        // concurrency). Skills flow through the dialog form. As of
        // MUL-2600 the agent resource shape no longer carries
        // custom_env values, so duplication cannot copy env at all —
        // the user has to re-set env on the clone via the env tab
        // (which now goes through the audited `/env` endpoint). The
        // dialog's create call still accepts custom_env at create
        // time, but the source values aren't available here.
        if (template.custom_args.length) data.custom_args = template.custom_args;
        if (template.max_concurrent_tasks) {
          data.max_concurrent_tasks = template.max_concurrent_tasks;
        }
      }
      const createdAgent = await onCreate(data);
      // Follow-up: attach selected skills to the newly created agent.
      // onCreate returns the created Agent for this path; if the caller
      // doesn't return it we fall back to skipping (preserves
      // backward compatibility with non-skill-aware callers).
      if (createdAgent && selectedSkillIds.size > 0) {
        try {
          await api.setAgentSkills(createdAgent.id, {
            skill_ids: [...selectedSkillIds],
          });
          if (wsId) {
            queryClient.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
          }
        } catch (skillErr) {
          // Non-fatal: agent exists, skills can be added on the detail
          // page. Surface as a warning toast so the user knows.
          toast.warning(
            t(($) => $.create_dialog.skill_attach_failed_toast, {
              error:
                skillErr instanceof Error ? skillErr.message : "unknown error",
            }),
          );
        }
      }
      // Squad context: attach the agent after skills land so the
      // squad's Members tab shows the agent with its skills already
      // in place. Atomicity is best-effort by design (see plan in
      // MUL-2178) — a partial failure surfaces a warning toast and
      // the user can retry from the Add Member dialog.
      if (createdAgent && squadId) {
        await attachToSquad(createdAgent.id, createdAgent.name);
      }
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.create_dialog.create_failed_toast));
      setCreating(false);
    }
  };

  const headerTitle = isDuplicate
    ? t(($) => $.create_dialog.title_duplicate)
    : t(($) => $.create_dialog.title_create);

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="p-0 gap-0 flex flex-col overflow-hidden !top-1/2 !left-1/2 !-translate-x-1/2 !-translate-y-1/2 !w-full !max-w-2xl !h-[85vh]">
        <DialogHeader className="border-b px-5 py-3 space-y-0">
          <DialogTitle className="text-base font-semibold">{headerTitle}</DialogTitle>
          {isDuplicate && template && (
            <DialogDescription className="mt-1 text-xs">
              {t(($) => $.create_dialog.description_duplicate, { name: template.name })}
            </DialogDescription>
          )}
          {!isDuplicate && (
            <DialogDescription className="mt-1 text-xs">
              {t(($) => $.create_dialog.description_create)}
            </DialogDescription>
          )}
        </DialogHeader>

        <div className="flex-1 overflow-y-auto p-5">
          <div className="space-y-4 min-w-0">
            {/* Identity row: avatar (left) + name & description stack
                (right). The avatar visually anchors the identity of
                what the user is creating; pairing it with the Name
                field reads as "this is the agent's face + name",
                same shape as detail-page header so the affordance is
                instantly familiar. */}
            <div className="flex items-start gap-4">
              <AvatarUploadControl
                variant="agent"
                value={avatarUrl}
                name={name}
                size={64}
                onUploaded={setAvatarUrl}
                onClear={() => setAvatarUrl(null)}
              />
              <div className="flex-1 min-w-0 space-y-3">
                <div>
                  <Label className="text-xs text-muted-foreground">{t(($) => $.create_dialog.name_label)}</Label>
                  <Input
                    autoFocus
                    type="text"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder={t(($) => $.create_dialog.name_placeholder)}
                    className="mt-1"
                    onKeyDown={(e) => {
                      if (isImeComposing(e)) return;
                      if (e.key === "Enter") handleSubmit();
                    }}
                  />
                </div>

                <div>
                  <Label className="text-xs text-muted-foreground">{t(($) => $.create_dialog.description_label)}</Label>
                  <Input
                    type="text"
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    placeholder={t(($) => $.create_dialog.description_placeholder)}
                    maxLength={AGENT_DESCRIPTION_MAX_LENGTH}
                    className="mt-1"
                  />
                  <div className="mt-1">
                    <CharCounter
                      length={[...description].length}
                      max={AGENT_DESCRIPTION_MAX_LENGTH}
                    />
                  </div>
                </div>
              </div>
            </div>

            {accessPickerEnabled ? (
              <AccessSection
                permissionMode={permissionMode}
                onPermissionModeChange={setPermissionMode}
                workspaceTargetOn={workspaceTargetOn}
                onWorkspaceTargetChange={setWorkspaceTargetOn}
                selectedMemberIds={selectedMemberIds}
                onSelectedMemberIdsChange={setSelectedMemberIds}
                members={members}
                currentUserId={currentUserId}
              />
            ) : (
              <div>
                <Label className="text-xs text-muted-foreground">{t(($) => $.create_dialog.visibility_label)}</Label>
                <div className="mt-1.5 flex gap-2">
                  <button
                    type="button"
                    onClick={() => setVisibility("workspace")}
                    className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors ${
                      visibility === "workspace"
                        ? "border-primary bg-primary/5"
                        : "border-border hover:bg-muted"
                    }`}
                  >
                    <Globe className="h-4 w-4 shrink-0 text-muted-foreground" />
                    <div className="text-left">
                      <div className="font-medium">{VISIBILITY_LABEL.workspace}</div>
                      <div className="text-xs text-muted-foreground">
                        {VISIBILITY_DESCRIPTION.workspace}
                      </div>
                    </div>
                  </button>
                  <button
                    type="button"
                    onClick={() => setVisibility("private")}
                    className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors ${
                      visibility === "private"
                        ? "border-primary bg-primary/5"
                        : "border-border hover:bg-muted"
                    }`}
                  >
                    <Lock className="h-4 w-4 shrink-0 text-muted-foreground" />
                    <div className="text-left">
                      <div className="font-medium">{VISIBILITY_LABEL.private}</div>
                      <div className="text-xs text-muted-foreground">
                        {VISIBILITY_DESCRIPTION.private}
                      </div>
                    </div>
                  </button>
                </div>
              </div>
            )}

            <RuntimePicker
              runtimes={runtimes}
              runtimesLoading={runtimesLoading}
              members={members}
              currentUserId={currentUserId}
              selectedRuntimeId={selectedRuntimeId}
              onSelect={setSelectedRuntimeId}
            />

            <ModelDropdown
              runtimeId={selectedRuntime?.id ?? null}
              runtimeOnline={selectedRuntime?.status === "online"}
              value={model}
              onChange={setModel}
              disabled={!selectedRuntime}
            />

            {/* --- Optional sections (instructions / skills) ---
                Collapsed by default so quick-create stays fast.
                Duplicate pre-fills everything from the source agent. */}
            <InstructionsEditor
              value={instructions}
              onChange={setInstructions}
              placeholder={
                isDuplicate
                  ? t(($) => $.create_dialog.instructions.placeholder_duplicate)
                  : t(($) => $.create_dialog.instructions.placeholder_blank)
              }
            />

            <SkillMultiSelect
              selectedIds={selectedSkillIds}
              onChange={setSelectedSkillIds}
            />
          </div>
        </div>

        {/* Inline footer instead of <DialogFooter>: the shipped
            DialogFooter applies `-mx-4 -mb-4` assuming a padded
            DialogContent (default `p-4`). Our DialogContent uses
            `p-0`, so those negative margins push the footer outside
            the dialog. A plain flex row anchored by `border-t` keeps
            the visual rhythm without the overflow bug. */}
        <div className="flex items-center justify-end gap-2 border-t bg-background px-5 py-3">
          <Button variant="ghost" onClick={onClose}>
            {t(($) => $.create_dialog.cancel)}
          </Button>
          <Button
            onClick={handleSubmit}
            disabled={
              creating || !name.trim() || !selectedRuntime || selectedRuntimeLocked
            }
            title={
              selectedRuntimeLocked
                ? t(($) => $.create_dialog.runtime_private_locked_tooltip)
                : undefined
            }
          >
            {creating ? t(($) => $.create_dialog.creating) : t(($) => $.create_dialog.create)}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}


/**
 * AccessSection — inline access editor for the create/duplicate flow, gated
 * on `COMPOSIO_MCP_APPS_FLAG`. Mirrors the semantics of
 * `AccessPicker` on the agent detail page: the underlying model is
 * `permission_mode` + `invocation_targets` (MUL-3963), not the legacy
 * `visibility`.
 *
 * The three mutually exclusive scopes mirror AccessPicker on the detail page.
 * Specific people reveals a member allow-list; an entire-workspace grant never
 * stacks redundant member targets underneath it.
 *
 * The current viewer is intentionally excluded from the member list: an
 * owner is always allowed to invoke their own agent, so listing them again
 * would be misleading.
 */
function AccessSection({
  permissionMode,
  onPermissionModeChange,
  workspaceTargetOn,
  onWorkspaceTargetChange,
  selectedMemberIds,
  onSelectedMemberIdsChange,
  members,
  currentUserId,
}: {
  permissionMode: AgentPermissionMode;
  onPermissionModeChange: (next: AgentPermissionMode) => void;
  workspaceTargetOn: boolean;
  onWorkspaceTargetChange: (next: boolean) => void;
  selectedMemberIds: Set<string>;
  onSelectedMemberIdsChange: (next: Set<string>) => void;
  members: MemberWithUser[];
  currentUserId: string | null;
}) {
  const { t } = useT("agents");
  const isPrivate = permissionMode === "private";
  const isWorkspace = !isPrivate && workspaceTargetOn;
  const isMembers = !isPrivate && !workspaceTargetOn;

  const otherMembers = members.filter((m) => m.user_id !== currentUserId);

  const toggleMember = (userId: string, checked: boolean) => {
    const next = new Set(selectedMemberIds);
    if (checked) next.add(userId);
    else next.delete(userId);
    onSelectedMemberIdsChange(next);
  };

  return (
    <div>
      <Label className="text-xs text-muted-foreground">
        {t(($) => $.create_dialog.access.label)}
      </Label>
      <fieldset className="mt-1.5 grid gap-2 sm:grid-cols-3">
        <legend className="sr-only">{t(($) => $.access.tooltip)}</legend>
        <CompactAccessChoice
          value="private"
          icon={Lock}
          title={t(($) => $.access.private_title)}
          description={t(($) => $.access.private_desc)}
          selected={isPrivate}
          onSelect={() => onPermissionModeChange("private")}
        />
        <CompactAccessChoice
          value="workspace"
          icon={Globe}
          title={t(($) => $.access.workspace_title)}
          description={t(($) => $.access.workspace_desc)}
          selected={isWorkspace}
          onSelect={() => {
            onPermissionModeChange("public_to");
            onWorkspaceTargetChange(true);
          }}
        />
        <CompactAccessChoice
          value="members"
          icon={Users}
          title={t(($) => $.access.members_title)}
          description={t(($) => $.access.members_desc)}
          selected={isMembers}
          onSelect={() => {
            onPermissionModeChange("public_to");
            onWorkspaceTargetChange(false);
          }}
        />
      </fieldset>

      {isMembers && (
        <div className="mt-2 rounded-lg border bg-muted/30 px-3 py-2">
          <div>
            {otherMembers.length === 0 ? (
              <div className="py-1 text-xs text-muted-foreground">
                {t(($) => $.create_dialog.access.public_members_empty)}
              </div>
            ) : (
              <div className="max-h-40 overflow-y-auto">
                {otherMembers.map((m) => {
                  const checked = selectedMemberIds.has(m.user_id);
                  return (
                    <label
                      key={m.user_id}
                      className="flex cursor-pointer items-center gap-2 rounded-md px-1 py-1 text-sm hover:bg-background/60"
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
                        size="sm"
                      />
                      <span className="min-w-0 flex-1 truncate">{m.name}</span>
                    </label>
                  );
                })}
              </div>
            )}
          </div>

          {selectedMemberIds.size === 0 && (
            <div className="mt-2 text-xs text-destructive" role="alert">
              {t(($) => $.access.shared_target_required)}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function CompactAccessChoice({
  value,
  icon: Icon,
  title,
  description,
  selected,
  onSelect,
}: {
  value: string;
  icon: typeof Lock;
  title: string;
  description: string;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <label
      className={`flex min-w-0 cursor-pointer items-start gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors hover:bg-muted ${
        selected ? "border-primary bg-primary/5" : "border-border"
      }`}
    >
      <input
        type="radio"
        name="create-agent-access-scope"
        value={value}
        checked={selected}
        onChange={onSelect}
        className="mt-0.5 size-4 shrink-0 accent-foreground"
      />
      <Icon
        className="mt-0.5 size-4 shrink-0 text-muted-foreground"
        aria-hidden="true"
      />
      <span className="min-w-0">
        <span className="block font-medium">{title}</span>
        <span className="mt-0.5 block text-xs leading-4 text-muted-foreground">
          {description}
        </span>
      </span>
    </label>
  );
}
