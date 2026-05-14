"use client";

import { useState } from "react";
import {
  ArrowLeft,
  FileText,
  Globe,
  Lock,
  PenLine,
} from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { ModelDropdown } from "./model-dropdown";
import { RuntimePicker, isRuntimeUsableForUser } from "./runtime-picker";
import { TemplatePicker } from "./template-picker";
import { TemplateDetail } from "./template-detail";
import { InstructionsEditor } from "./instructions-editor";
import { SkillMultiSelect } from "./skill-multi-select";
import { AvatarPicker } from "./avatar-picker";
import { useNavigation } from "../../navigation";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type {
  Agent,
  AgentTemplateSummary,
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
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import {
  AGENT_DESCRIPTION_MAX_LENGTH,
  VISIBILITY_DESCRIPTION,
  VISIBILITY_LABEL,
} from "@multica/core/agents";
import { CharCounter } from "./char-counter";
import { useT } from "../../i18n";

// State machine encoded as a discriminated union.
//
//   chooser          → "Start blank" or "From template" cards
//   blank-form       → standard manual-create form (post-chooser)
//   duplicate-form   → form pre-filled from a duplicated agent (entry only)
//   template-picker  → grid of templates — clicking a card immediately
//                      creates the agent (no intermediate form step);
//                      the user refines on the detail page if needed.
type DialogStep =
  | { kind: "chooser" }
  | { kind: "blank-form" }
  | { kind: "duplicate-form" }
  | { kind: "template-picker" }
  | { kind: "template-detail"; template: AgentTemplateSummary };

/** Helper: which step kinds render the form view. Template path is now
 *  a one-click flow (picker → API call → close), so it never lands on
 *  the form — defaults are auto-populated, the user customises later
 *  on the detail page. */
function isFormStep(step: DialogStep): step is { kind: "blank-form" } | { kind: "duplicate-form" } {
  return step.kind === "blank-form" || step.kind === "duplicate-form";
}

/**
 * Per-step dialog sizing.
 *
 * Width is wider on info-dense steps (picker, detail, form) — `max-w-5xl`
 * gives instructions and skill descriptions room to breathe without
 * wrapping every line. Chooser stays at `max-w-4xl`: only two cards, more
 * width just creates empty side-margin.
 *
 * Heights are **fixed pixel-ish values** because CSS `transition` cannot
 * interpolate to/from `height: auto` (the keyword isn't a number). Both
 * `h-[420px]` (chooser) and `h-[85vh]` (others) are numeric and animate
 * cleanly.
 *
 * `transition-[height,max-width]` runs whenever the step changes, giving
 * the modal a smooth grow/shrink instead of a hard size pop. !important
 * is required because the Dialog primitive sets its own `duration-100`.
 */
function agentDialogContentClass(step: DialogStep): string {
  return cn(
    "p-0 gap-0 flex flex-col overflow-hidden",
    "!top-1/2 !left-1/2 !-translate-x-1/2 !-translate-y-1/2",
    "!w-full",
    "!transition-[height,max-width] !duration-300 !ease-out",
    step.kind === "chooser" ? "!max-w-4xl" : "!max-w-5xl",
    step.kind === "chooser" ? "!h-[420px]" : "!h-[85vh]",
  );
}

export function CreateAgentDialog({
  runtimes,
  runtimesLoading,
  members,
  currentUserId,
  template,
  existingAgentNames,
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
  // Names of agents already in the workspace, used to auto-dedupe the
  // default name when picking a template (so the form lands on a unique
  // name instead of immediately hitting the backend's 409). Optional —
  // when absent, default names are used verbatim and 409 stays the
  // safety net.
  existingAgentNames?: readonly string[];
  // When set, every successful create (manual, duplicate, or template)
  // is followed by addSquadMember(squadId, agent) so the new agent
  // joins this squad. The template path also skips its usual
  // navigation to the agent detail page so the user stays on the
  // squad. If the squad-join call fails the agent still exists and
  // the dialog surfaces a warning toast — the user can add it
  // manually from the Members tab.
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
  const navigation = useNavigation();
  const paths = useWorkspacePaths();

  // Duplicate path comes in via the row action with a concrete agent —
  // chooser would be redundant, jump straight to the form.
  const [step, setStep] = useState<DialogStep>(
    isDuplicate ? { kind: "duplicate-form" } : { kind: "chooser" },
  );

  // Name defaults: duplicate uses "<original> copy", template uses the
  // template's name verbatim. Manual-create starts blank.
  const [name, setName] = useState(
    template ? `${template.name}${t(($) => $.create_dialog.duplicate_copy_suffix)}` : "",
  );
  const [description, setDescription] = useState(template?.description ?? "");
  const [visibility, setVisibility] = useState<AgentVisibility>(
    template?.visibility ?? "workspace",
  );
  const [model, setModel] = useState(template?.model ?? "");
  // Optional fields exposed through the collapsible sections at the bottom
  // of the form. Each initialises from the duplicate template (if any);
  // template-flow defaults are populated when the user picks one (see
  // goToTemplateForm). Selected skills are stored as IDs only — the
  // workspace skill list is fetched on demand by SkillMultiSelect.
  const [instructions, setInstructions] = useState(template?.instructions ?? "");
  const [avatarUrl, setAvatarUrl] = useState<string | null>(template?.avatar_url ?? null);
  const [selectedSkillIds, setSelectedSkillIds] = useState<Set<string>>(
    () => new Set(template?.skills.map((s) => s.id) ?? []),
  );
  const [creating, setCreating] = useState(false);
  const [failedURLs, setFailedURLs] = useState<string[] | null>(null);

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

  // Transition helpers. Each centralises the form-field initialisation
  // for its target step, so transitions can't leave stale state behind.

  const goToBlankForm = () => {
    setName("");
    setDescription("");
    setInstructions("");
    setAvatarUrl(null);
    setSelectedSkillIds(new Set());
    setStep({ kind: "blank-form" });
  };

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

  // Template path is one-click — picker card click goes straight to the
  // API. Defaults: name auto-deduped, runtime = first usable one,
  // visibility = workspace. User refines on the detail page if needed.
  // On 422 with failed_urls the user stays on the template-detail step
  // and the banner there reports the bad URLs; on any other error we
  // surface a toast and reset the spinner so they can retry.
  const quickCreateFromTemplate = async (tmpl: AgentTemplateSummary) => {
    if (!selectedRuntime || selectedRuntimeLocked) {
      toast.error(t(($) => $.create_dialog.no_runtime_toast));
      return;
    }
    const taken = new Set(existingAgentNames ?? []);
    let candidate = tmpl.name;
    let n = 2;
    while (taken.has(candidate)) {
      candidate = `${tmpl.name} ${n}`;
      n++;
    }

    setFailedURLs(null);
    setCreating(true);
    try {
      const resp = await api.createAgentFromTemplate({
        template_slug: tmpl.slug,
        name: candidate,
        runtime_id: selectedRuntime.id,
        model: model.trim() || undefined,
        visibility: "workspace",
      });
      if (wsId) {
        queryClient.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
        queryClient.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
      }
      if (resp.reused_skill_ids.length > 0) {
        toast.success(
          t(($) => $.create_dialog.template_created_with_reuse_toast, {
            name: candidate,
            count: resp.reused_skill_ids.length,
          }),
        );
      } else {
        toast.success(
          t(($) => $.create_dialog.template_created_toast, { name: candidate }),
        );
      }
      // Squad context: attach the freshly created agent to the squad
      // before closing — and skip the agent-detail navigation so the
      // user lands back on the squad page where they triggered the
      // flow. Failures here are non-fatal; the agent exists and can be
      // added manually if the join call 4xxs.
      if (squadId && resp.agent.id) {
        await attachToSquad(resp.agent.id, candidate);
      }
      onClose();
      // Land on the new agent's detail page so the user can verify or
      // customise instructions / skills / avatar — matches the navigation
      // behaviour of the manual create path in agents-page.tsx. When the
      // response failed schema parsing (`agent.id === ""`, see schema
      // fallback) we skip the navigation: the agent was created server-
      // side, the list-invalidation above will surface it, and a push
      // to `/agents/` would land on a broken detail page. Squad-context
      // entry also skips the push — the user wants to stay on Members.
      if (resp.agent.id && !squadId) {
        navigation.push(paths.agentDetail(resp.agent.id));
      }
    } catch (err) {
      const body = extractFailureBody(err);
      if (body?.failed_urls?.length) {
        setFailedURLs(body.failed_urls);
      } else {
        toast.error(
          err instanceof Error ? err.message : t(($) => $.create_dialog.create_failed_toast),
        );
      }
      setCreating(false);
    }
  };

  const handleSubmit = async () => {
    if (!name.trim() || !selectedRuntime || selectedRuntimeLocked) return;
    setFailedURLs(null);
    setCreating(true);

    // Template path goes through quickCreateFromTemplate directly from
    // the picker — no form step. So handleSubmit only fires for blank /
    // duplicate flows below.

    // Scratch / duplicate path: hits the manual POST /api/agents through
    // the caller's onCreate callback, then we run a follow-up
    // setAgentSkills here for any skills the user selected in the form.
    // Duplicate path already had this two-phase behaviour in
    // agents-page.tsx; we extend it by sending whichever skill IDs the
    // user explicitly chose in the dialog (overriding duplicate's
    // copy-source skills if both apply).
    try {
      const trimmedInstructions = instructions.trim();
      // Manual fields take precedence over duplicate source for
      // instructions / avatar / skills — duplicate fills the defaults at
      // mount time, then anything the user edits overrides those defaults
      // naturally because we read from state.
      const data: CreateAgentRequest = {
        name: name.trim(),
        description: description.trim(),
        runtime_id: selectedRuntime.id,
        visibility,
        model: model.trim() || undefined,
        instructions: trimmedInstructions || undefined,
        avatar_url: avatarUrl ?? undefined,
      };
      if (template) {
        // Duplicate path: forward the hidden config fields the source
        // agent had so the clone is functional out of the box (env / args
        // / concurrency). Skills now flow through the dialog form, so we
        // don't blindly carry template.skills here anymore — the form's
        // selectedSkillIds is the source of truth.
        if (template.custom_args.length) data.custom_args = template.custom_args;
        if (
          !template.custom_env_redacted &&
          Object.keys(template.custom_env).length > 0
        ) {
          data.custom_env = template.custom_env;
        }
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

  // Header title — narrowed by step.kind, so reads on `step.template` are
  // type-safe and there's no fallback string compilation needed.
  const headerTitle = (() => {
    switch (step.kind) {
      case "chooser":
        return t(($) => $.create_dialog.title_create);
      case "blank-form":
        return t(($) => $.create_dialog.title_create);
      case "duplicate-form":
        return t(($) => $.create_dialog.title_duplicate);
      case "template-picker":
        return t(($) => $.create_dialog.template_picker.title);
      case "template-detail":
        return step.template.name;
    }
  })();

  // Back navigation — each kind maps to one previous kind. Duplicate-form
  // and chooser have no back (duplicate enters from a row action, chooser
  // is the root).
  const handleBack = () => {
    switch (step.kind) {
      case "template-picker":
        setStep({ kind: "chooser" });
        return;
      case "template-detail":
        setStep({ kind: "template-picker" });
        return;
      case "blank-form":
        setStep({ kind: "chooser" });
        return;
      case "chooser":
      case "duplicate-form":
        return;
    }
  };

  const showBackButton =
    step.kind === "template-picker" ||
    step.kind === "template-detail" ||
    step.kind === "blank-form";

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className={agentDialogContentClass(step)}>
        <DialogHeader className="border-b px-5 py-3 space-y-0">
          <div className="flex items-center gap-2">
            {showBackButton && (
              <button
                type="button"
                onClick={handleBack}
                className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                aria-label={t(($) => $.create_dialog.back_aria)}
              >
                <ArrowLeft className="h-4 w-4" />
              </button>
            )}
            <DialogTitle className="text-base font-semibold">{headerTitle}</DialogTitle>
          </div>
          {step.kind === "chooser" && (
            <DialogDescription className="mt-1 text-xs">
              {t(($) => $.create_dialog.description_create)}
            </DialogDescription>
          )}
          {step.kind === "duplicate-form" && template && (
            <DialogDescription className="mt-1 text-xs">
              {t(($) => $.create_dialog.description_duplicate, { name: template.name })}
            </DialogDescription>
          )}
        </DialogHeader>

        {/* ---------- Step 1: Chooser (two large cards, vertically centred) ---------- */}
        {step.kind === "chooser" && (
          <div className="flex flex-1 items-center justify-center p-8">
            <div className="grid w-full max-w-3xl grid-cols-2 gap-4">
              <button
                type="button"
                onClick={goToBlankForm}
                className="group flex flex-col items-center justify-center gap-3 rounded-xl border bg-card p-8 text-center transition-colors hover:border-primary/40 hover:bg-accent/40"
              >
                <div className="flex h-14 w-14 items-center justify-center rounded-xl bg-muted text-muted-foreground group-hover:bg-accent group-hover:text-foreground">
                  <PenLine className="h-7 w-7" />
                </div>
                <div>
                  <div className="text-base font-semibold">
                    {t(($) => $.create_dialog.chooser.blank_title)}
                  </div>
                  <div className="mt-1 text-sm text-muted-foreground">
                    {t(($) => $.create_dialog.chooser.blank_desc)}
                  </div>
                </div>
              </button>

              <button
                type="button"
                onClick={() => setStep({ kind: "template-picker" })}
                className="group flex flex-col items-center justify-center gap-3 rounded-xl border bg-card p-8 text-center transition-colors hover:border-primary/40 hover:bg-accent/40"
              >
                <div className="flex h-14 w-14 items-center justify-center rounded-xl bg-primary/10 text-primary">
                  <FileText className="h-7 w-7" />
                </div>
                <div>
                  <div className="text-base font-semibold">
                    {t(($) => $.create_dialog.chooser.template_title)}
                  </div>
                  <div className="mt-1 text-sm text-muted-foreground">
                    {t(($) => $.create_dialog.chooser.template_desc)}
                  </div>
                </div>
              </button>
            </div>
          </div>
        )}

        {/* ---------- Step 2: Template Picker — click a card opens the
            detail preview so the user can review instructions / skills
            before committing. ---------- */}
        {step.kind === "template-picker" && (
          <TemplatePicker
            onSelect={(tmpl) => setStep({ kind: "template-detail", template: tmpl })}
          />
        )}

        {/* ---------- Step 3: Template Detail — read-only preview with a
            "Use this template" button. Clicking Use fires the one-shot
            create with default settings (name auto-deduped, first usable
            runtime, private visibility); the user customises further on
            the agent detail page if needed. ---------- */}
        {step.kind === "template-detail" && (
          <TemplateDetail
            template={step.template}
            onUse={quickCreateFromTemplate}
            creating={creating}
            failedURLs={failedURLs}
            runtimes={runtimes}
            runtimesLoading={runtimesLoading}
            members={members}
            currentUserId={currentUserId}
            selectedRuntimeId={selectedRuntimeId}
            onRuntimeSelect={setSelectedRuntimeId}
            selectedRuntime={selectedRuntime}
            model={model}
            onModelChange={setModel}
            useDisabled={!selectedRuntime || selectedRuntimeLocked}
          />
        )}

        {/* ---------- Form step (blank / duplicate / template) ---------- */}
        {isFormStep(step) && (
          <>
            <div className="flex-1 overflow-y-auto p-5">
              <div className="space-y-4 min-w-0">
                {/* failedURLs banner lives in TemplateDetail — it's the
                    only step that can trigger a 422, and rendering the
                    error there keeps the user where the action was. */}

                {/* Identity row: avatar (left) + name & description stack
                    (right). The avatar visually anchors the identity of
                    what the user is creating; pairing it with the Name
                    field reads as "this is the agent's face + name",
                    same shape as detail-page header so the affordance is
                    instantly familiar. */}
                <div className="flex items-start gap-4">
                  <AvatarPicker value={avatarUrl} onChange={setAvatarUrl} size={64} />
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
                    step.kind === "duplicate-form"
                      ? t(($) => $.create_dialog.instructions.placeholder_duplicate)
                      : t(($) => $.create_dialog.instructions.placeholder_blank)
                  }
                />

                <SkillMultiSelect
                  selectedIds={selectedSkillIds}
                  onChange={setSelectedSkillIds}
                  // We deliberately don't filter by `template.skills` here:
                  // the template's skill IDs live in skills.sh / GitHub
                  // space (source_url), while the picker shows workspace
                  // skill UUIDs — they're incomparable. If the user does
                  // tick a workspace skill that happens to match a
                  // template skill by name, the server's AddAgentSkill
                  // uses ON CONFLICT DO NOTHING so the duplicate is a
                  // silent no-op.
                />

                {/* Avatar moved up next to Name — see the identity row at
                    the top of the form. */}
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
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

// extractFailureBody recovers the structured 422 payload from the fetch
// error our APIClient throws. The client wraps non-2xx responses as a thrown
// Error whose message is the JSON body; this helper parses it so we can
// surface specific failed URLs instead of just "request failed".
function extractFailureBody(err: unknown): { error?: string; failed_urls?: string[] } | null {
  if (!(err instanceof Error)) return null;
  // The fetch helper attaches the body as a stringified JSON on the error
  // message (or as `body` on the error object). Try both shapes.
  const maybeBody = (err as Error & { body?: unknown }).body;
  if (maybeBody && typeof maybeBody === "object") {
    return maybeBody as { error?: string; failed_urls?: string[] };
  }
  try {
    const parsed = JSON.parse(err.message);
    if (parsed && typeof parsed === "object") {
      return parsed as { error?: string; failed_urls?: string[] };
    }
  } catch {
    // not JSON — fall through
  }
  return null;
}
