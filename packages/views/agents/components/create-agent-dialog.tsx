"use client";

import { useState, useEffect, useMemo } from "react";
import {
  ArrowLeft,
  ChevronDown,
  Cloud,
  FileText,
  Globe,
  Loader2,
  Lock,
  PenLine,
} from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { ActorAvatar } from "../../common/actor-avatar";
import { ModelDropdown } from "./model-dropdown";
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
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
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

type RuntimeFilter = "mine" | "all";

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
  const [runtimeOpen, setRuntimeOpen] = useState(false);
  const [runtimeFilter, setRuntimeFilter] = useState<RuntimeFilter>("mine");

  const getOwnerMember = (ownerId: string | null) => {
    if (!ownerId) return null;
    return members.find((m) => m.user_id === ownerId) ?? null;
  };

  const hasOtherRuntimes = runtimes.some((r) => r.owner_id !== currentUserId);

  // A runtime is disabled for the caller when it's owned by someone else
  // AND its visibility is not "public". Older backends that haven't shipped
  // MUL-2062 leave visibility undefined; we treat anything other than the
  // literal string "public" as private so the strict default holds (the
  // backend will reject the create anyway).
  const isRuntimeDisabledForUser = (r: RuntimeDevice): boolean => {
    if (!currentUserId) return false;
    if (r.owner_id === currentUserId) return false;
    return r.visibility !== "public";
  };

  const filteredRuntimes = useMemo(() => {
    const filtered = runtimeFilter === "mine" && currentUserId
      ? runtimes.filter((r) => r.owner_id === currentUserId)
      : runtimes;
    return [...filtered].sort((a, b) => {
      // Caller's own runtimes first; among the rest, usable (public) ones
      // come before unusable (private) ones so the picker doesn't lead
      // with greyed-out rows.
      const aMine = a.owner_id === currentUserId;
      const bMine = b.owner_id === currentUserId;
      if (aMine && !bMine) return -1;
      if (!aMine && bMine) return 1;
      const aDisabled = isRuntimeDisabledForUser(a);
      const bDisabled = isRuntimeDisabledForUser(b);
      if (!aDisabled && bDisabled) return -1;
      if (aDisabled && !bDisabled) return 1;
      return 0;
    });
    // currentUserId is the only external dep of isRuntimeDisabledForUser;
    // listing it in the deps array is enough.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runtimes, runtimeFilter, currentUserId]);

  // When duplicating, default to the template's runtime so the clone
  // lands on the same machine — caller can still switch in the picker.
  // But never seed with a runtime the caller can't actually use (locked
  // by visibility); otherwise the dialog opens with a selected row the
  // user can't submit, and Create falls through to a backend 403. Falling
  // back to the first usable runtime is friendlier than the locked
  // pre-fill. Computed inside the initializer so the find-walk only runs
  // on first mount, not on every render.
  const [selectedRuntimeId, setSelectedRuntimeId] = useState(() => {
    const templateRuntime = template?.runtime_id
      ? runtimes.find((r) => r.id === template.runtime_id)
      : undefined;
    if (templateRuntime && !isRuntimeDisabledForUser(templateRuntime)) {
      return templateRuntime.id;
    }
    return filteredRuntimes.find((r) => !isRuntimeDisabledForUser(r))?.id ?? "";
  });

  useEffect(() => {
    if (!selectedRuntimeId) {
      const firstUsable = filteredRuntimes.find(
        (r) => !isRuntimeDisabledForUser(r),
      );
      if (firstUsable) setSelectedRuntimeId(firstUsable.id);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filteredRuntimes, selectedRuntimeId]);

  const selectedRuntime = runtimes.find((d) => d.id === selectedRuntimeId) ?? null;
  // Defense-in-depth: even if a locked runtime somehow ends up selected
  // (e.g. duplicate of an agent whose template runtime is now locked, and
  // the workspace has no usable fallback), gate Create on it so we don't
  // submit a request the backend will reject with 403.
  const selectedRuntimeLocked =
    selectedRuntime != null && isRuntimeDisabledForUser(selectedRuntime);

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
      onClose();
      // Land on the new agent's detail page so the user can verify or
      // customise instructions / skills / avatar — matches the navigation
      // behaviour of the manual create path in agents-page.tsx. When the
      // response failed schema parsing (`agent.id === ""`, see schema
      // fallback) we skip the navigation: the agent was created server-
      // side, the list-invalidation above will surface it, and a push
      // to `/agents/` would land on a broken detail page.
      if (resp.agent.id) {
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

                <div className="min-w-0">
                  <div className="flex items-center justify-between">
                    <Label className="text-xs text-muted-foreground">{t(($) => $.create_dialog.runtime_label)}</Label>
                    {hasOtherRuntimes && (
                      <div className="flex items-center gap-0.5 rounded-md bg-muted p-0.5">
                        <button
                          type="button"
                          onClick={() => { setRuntimeFilter("mine"); setSelectedRuntimeId(""); }}
                          className={`rounded px-2 py-0.5 text-xs font-medium transition-colors ${
                            runtimeFilter === "mine"
                              ? "bg-background text-foreground shadow-sm"
                              : "text-muted-foreground hover:text-foreground"
                          }`}
                        >
                          {t(($) => $.create_dialog.runtime_filter_mine)}
                        </button>
                        <button
                          type="button"
                          onClick={() => { setRuntimeFilter("all"); setSelectedRuntimeId(""); }}
                          className={`rounded px-2 py-0.5 text-xs font-medium transition-colors ${
                            runtimeFilter === "all"
                              ? "bg-background text-foreground shadow-sm"
                              : "text-muted-foreground hover:text-foreground"
                          }`}
                        >
                          {t(($) => $.create_dialog.runtime_filter_all)}
                        </button>
                      </div>
                    )}
                  </div>
                  <Popover open={runtimeOpen} onOpenChange={setRuntimeOpen}>
                    <PopoverTrigger
                      disabled={runtimes.length === 0 && !runtimesLoading}
                      className="flex w-full min-w-0 items-center gap-3 rounded-lg border border-border bg-background px-3 py-2.5 mt-1.5 text-left text-sm transition-colors hover:bg-muted disabled:pointer-events-none disabled:opacity-50"
                    >
                      {runtimesLoading ? (
                        <Loader2 className="h-4 w-4 shrink-0 animate-spin text-muted-foreground" />
                      ) : selectedRuntime ? (
                        <ProviderLogo provider={selectedRuntime.provider} className="h-4 w-4 shrink-0" />
                      ) : (
                        <Cloud className="h-4 w-4 shrink-0 text-muted-foreground" />
                      )}
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <span className="truncate font-medium">
                            {runtimesLoading ? t(($) => $.create_dialog.runtime_loading) : (selectedRuntime?.name ?? t(($) => $.create_dialog.runtime_none))}
                          </span>
                          {selectedRuntime?.runtime_mode === "cloud" && (
                            <span className="shrink-0 rounded bg-info/10 px-1.5 py-0.5 text-xs font-medium text-info">
                              {t(($) => $.create_dialog.runtime_cloud_badge)}
                            </span>
                          )}
                        </div>
                        <div className="truncate text-xs text-muted-foreground">
                          {selectedRuntime
                            ? (getOwnerMember(selectedRuntime.owner_id)?.name ?? selectedRuntime.device_info)
                            : t(($) => $.create_dialog.runtime_register_first)}
                        </div>
                      </div>
                      <ChevronDown className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${runtimeOpen ? "rotate-180" : ""}`} />
                    </PopoverTrigger>
                    <PopoverContent align="start" className="w-[var(--anchor-width)] p-1 max-h-60 overflow-y-auto">
                      {filteredRuntimes.map((device) => {
                        const ownerMember = getOwnerMember(device.owner_id);
                        const disabled = isRuntimeDisabledForUser(device);
                        const disabledTitle = disabled
                          ? t(($) => $.create_dialog.runtime_private_locked_tooltip)
                          : undefined;
                        return (
                          <button
                            key={device.id}
                            type="button"
                            disabled={disabled}
                            title={disabledTitle}
                            onClick={() => {
                              if (disabled) return;
                              setSelectedRuntimeId(device.id);
                              setRuntimeOpen(false);
                            }}
                            className={`flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors ${
                              disabled
                                ? "cursor-not-allowed opacity-50"
                                : device.id === selectedRuntimeId
                                  ? "bg-accent"
                                  : "hover:bg-accent/50"
                            }`}
                          >
                            <ProviderLogo provider={device.provider} className="h-4 w-4 shrink-0" />
                            <div className="min-w-0 flex-1">
                              <div className="flex items-center gap-2">
                                <span className="truncate font-medium">{device.name}</span>
                                {device.runtime_mode === "cloud" && (
                                  <span className="shrink-0 rounded bg-info/10 px-1.5 py-0.5 text-xs font-medium text-info">
                                    {t(($) => $.create_dialog.runtime_cloud_badge)}
                                  </span>
                                )}
                                {disabled && (
                                  <span className="shrink-0 inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                                    <Lock className="h-3 w-3" />
                                    {t(($) => $.create_dialog.runtime_private_badge)}
                                  </span>
                                )}
                              </div>
                              <div className="mt-0.5 flex items-center gap-1 text-xs text-muted-foreground">
                                {ownerMember ? (
                                  <>
                                    <ActorAvatar actorType="member" actorId={ownerMember.user_id} size={14} />
                                    <span className="truncate">{ownerMember.name}</span>
                                  </>
                                ) : (
                                  <span className="truncate">{device.device_info}</span>
                                )}
                              </div>
                            </div>
                            <span
                              className={`h-2 w-2 shrink-0 rounded-full ${
                                device.status === "online" ? "bg-success" : "bg-muted-foreground/40"
                              }`}
                            />
                          </button>
                        );
                      })}
                    </PopoverContent>
                  </Popover>
                </div>

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
