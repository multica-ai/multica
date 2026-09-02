"use client";

import { useId, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Check,
  ChevronDown,
  ChevronRight,
  Clock,
  FilePlus2,
  FolderKanban,
  Maximize2,
  Minimize2,
  Play,
  Plus,
  Rocket,
  Users,
  Webhook,
  X as XIcon,
  Zap,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
  PopoverHeader,
  PopoverTitle,
  PopoverDescription,
} from "@multica/ui/components/ui/popover";
import { Button } from "@multica/ui/components/ui/button";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import {
  useCreateAutopilot,
  useCreateAutopilotTrigger,
  useUpdateAutopilot,
  useUpdateAutopilotTrigger,
} from "@multica/core/autopilots/mutations";
import { buildAutopilotWebhookUrl } from "@multica/core/autopilots";
import { api } from "@multica/core/api";
import type {
  AutopilotAssigneeType,
  AutopilotCollaborator,
  AutopilotExecutionMode,
  AutopilotTrigger,
} from "@multica/core/types";
import { TitleEditor, ContentEditor, type TitleEditorRef } from "../../editor";
import { ActorAvatar } from "../../common/actor-avatar";
import { SegmentedToggle } from "../../common/segmented-toggle";
import { ProjectPicker } from "../../projects/components/project-picker";
import { ProjectIcon } from "../../projects/components/project-icon";
import { AgentPicker, type AssigneeSelection } from "./pickers/agent-picker";
import { SubscriberMultiSelect } from "./subscriber-multi-select";
import { AutopilotAccessManager } from "./autopilot-access-manager";
import { ScheduleEditor } from "./schedule-editor/schedule-editor";
import { getDefaultScheduleConfig, type ScheduleConfig } from "./schedule-editor/model";
import { browserTimezone } from "../../common/timezone-select";
import { parseCron, toCron } from "./schedule-editor/cron-mapping";
import { useScheduleSubmitGate } from "./schedule-editor/validate";
import { WebhookEventFilterSection } from "./webhook-event-filter-section";
import { WebhookUrlField } from "./webhook-url-field";
import { useT } from "../../i18n";
import { formatSchedulePartialFailureToast } from "./autopilot-dialog-toast";
import type { WebhookEventFilter } from "@multica/core/types";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface AutopilotInitial {
  title: string;
  description: string;
  project_id: string | null;
  assignee_type: AutopilotAssigneeType;
  assignee_id: string;
  execution_mode: AutopilotExecutionMode;
  subscriber_user_ids?: string[];
}

export type AutopilotDialogProps =
  | {
      mode: "create";
      open: boolean;
      onOpenChange: (v: boolean) => void;
      initial?: Partial<AutopilotInitial>;
      initialSchedule?: Pick<ScheduleConfig, "time" | "days">;
    }
  | {
      mode: "edit";
      open: boolean;
      onOpenChange: (v: boolean) => void;
      autopilotId: string;
      initial: AutopilotInitial;
      triggers: AutopilotTrigger[];
      collaborators: AutopilotCollaborator[];
      canManageAccess: boolean;
    };

// ---------------------------------------------------------------------------
// Static schema-level data (not user-visible)
// ---------------------------------------------------------------------------

const OUTPUT_MODE_KEYS: AutopilotExecutionMode[] = ["create_issue", "run_only"];

const OUTPUT_MODE_ICONS: Record<AutopilotExecutionMode, typeof FilePlus2> = {
  create_issue: FilePlus2,
  run_only: Play,
};

// ---------------------------------------------------------------------------
// Webhook event-filter dirty detection
// ---------------------------------------------------------------------------

// serializeEventFilters returns a stable JSON string so the edit-mode dirty
// check can compare the current filters against the snapshot taken on open
// without depending on reference equality. Normalizes empty Actions to []
// so omitted-vs-explicit-empty doesn't show as a phantom change.
function serializeEventFilters(filters: WebhookEventFilter[]): string {
  return JSON.stringify(
    filters.map((f) => ({ event: f.event, actions: f.actions ?? [] })),
  );
}

// ---------------------------------------------------------------------------
// AutopilotDialog
// ---------------------------------------------------------------------------

export function AutopilotDialog(props: AutopilotDialogProps) {
  const { t } = useT("autopilots");
  const { open, onOpenChange } = props;
  const workspaceName = useCurrentWorkspace()?.name;
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const [isExpanded, setIsExpanded] = useState(false);

  const isCreate = props.mode === "create";
  const initial: Partial<AutopilotInitial> = isCreate
    ? props.initial ?? {}
    : props.initial;

  const [title, setTitle] = useState(initial.title ?? "");
  const [description, setDescription] = useState(initial.description ?? "");
  const [projectId, setProjectId] = useState<string | null>(initial.project_id ?? null);
  const [assigneeType, setAssigneeType] = useState<AutopilotAssigneeType>(
    initial.assignee_type ?? "agent",
  );
  const [assigneeId, setAssigneeId] = useState<string>(initial.assignee_id ?? "");
  const [executionMode, setExecutionMode] = useState<AutopilotExecutionMode>(
    initial.execution_mode ?? "create_issue",
  );
  const [subscriberUserIds, setSubscriberUserIds] = useState<string[]>(
    initial.subscriber_user_ids ?? [],
  );

  // The schedule panel speaks for the autopilot's SCHEDULE trigger, not for
  // `triggers[0]` — on a webhook- or api-triggered autopilot that row is one no
  // cron may be written into.
  const existingSchedule = isCreate
    ? null
    : props.triggers.find((trig) => trig.kind === "schedule") ?? null;

  const initialCfg: ScheduleConfig = (() => {
    if (isCreate) {
      const tpl = props.initialSchedule;
      const fallback = getDefaultScheduleConfig(browserTimezone());
      return tpl ? { ...fallback, ...tpl } : fallback;
    }
    if (existingSchedule?.cron_expression) {
      return parseCron(existingSchedule.cron_expression, existingSchedule.timezone ?? "UTC");
    }
    return getDefaultScheduleConfig(browserTimezone());
  })();
  const [schedule, setSchedule] = useState<ScheduleConfig>(initialCfg);

  // Editing an autopilot that has no schedule: the panel has nothing to
  // reflect, so anything it showed would be a proposal dressed as the
  // autopilot's state — and `scheduleDirty` below, comparing that proposal
  // against itself, then dropped the save on the floor under a success toast
  // (MUL-5649). The schedule is asked for explicitly instead: until the user
  // adds one, the panel says the autopilot is manual, and once they do, Save
  // writes what it shows whether or not they touched the default.
  const [scheduleAdded, setScheduleAdded] = useState(false);

  // Trigger kind selector. Only meaningful in create mode — edit mode does
  // not support converting between kinds inline (PLAN.md calls that
  // out as "delete old, create new" rather than ambiguous in-place
  // updates), so the toggle is hidden when editing. The kind is
  // initialized from the first existing trigger so we render the right
  // panel without surprising the user.
  const initialKind: "schedule" | "webhook" = (() => {
    if (isCreate) return "schedule";
    const first = props.triggers[0];
    if (first?.kind === "webhook") return "webhook";
    return "schedule";
  })();
  const [triggerKind, setTriggerKind] = useState<"schedule" | "webhook">(initialKind);

  const initialEventFilters: WebhookEventFilter[] =
    !isCreate && props.triggers[0]?.event_filters ? props.triggers[0].event_filters : [];
  const [eventFilters, setEventFilters] = useState<WebhookEventFilter[]>(initialEventFilters);

  const initialCronRef = useRef(toCron(initialCfg));
  const initialTimezoneRef = useRef(initialCfg.timezone);
  const initialEventFiltersRef = useRef(serializeEventFilters(initialEventFilters));
  const scheduleDirty =
    toCron(schedule) !== initialCronRef.current ||
    schedule.timezone !== initialTimezoneRef.current;
  const eventFiltersDirty =
    serializeEventFilters(eventFilters) !== initialEventFiltersRef.current;

  const firstTriggerIdRef = useRef(
    !isCreate && props.triggers[0] ? props.triggers[0].id : null,
  );
  // The row the schedule write targets, snapshotted at mount like the one
  // above. Null means there is none to patch, so the write creates one.
  const scheduleTriggerIdRef = useRef(existingSchedule?.id ?? null);

  const triggerCount = isCreate ? 0 : props.triggers.length;
  const schedulePillDisabled = !isCreate && triggerCount >= 2;

  // The manual-autopilot empty state, and the only path to a first schedule
  // from this dialog. Skipped when the panel is locked (2+ triggers), which
  // keeps that case rendering exactly the disabled editor it always has.
  const showScheduleEmptyState =
    !isCreate && existingSchedule === null && !scheduleAdded && !schedulePillDisabled;

  const selectedAssignee = useMemo(() => {
    if (!assigneeId) return null;
    if (assigneeType === "squad") {
      const squad = squads.find((s) => s.id === assigneeId);
      return squad ? { name: squad.name, description: squad.description } : null;
    }
    const agent = agents.find((a) => a.id === assigneeId);
    return agent ? { name: agent.name, description: agent.description } : null;
  }, [agents, squads, assigneeId, assigneeType]);
  const selectedProject = useMemo(
    () => projects.find((project) => project.id === projectId) ?? null,
    [projects, projectId],
  );

  const handleAssigneeChange = (next: AssigneeSelection) => {
    setAssigneeType(next.type);
    setAssigneeId(next.id);
  };

  const createAutopilot = useCreateAutopilot();
  const createTrigger = useCreateAutopilotTrigger();
  const updateAutopilot = useUpdateAutopilot();
  const updateTrigger = useUpdateAutopilotTrigger();
  const [submitting, setSubmitting] = useState(false);

  // After a successful webhook-kind create, we don't close the dialog —
  // we swap to a confirmation state showing the freshly minted URL with
  // copy / done affordances. This avoids the "now go find your autopilot
  // and click into it to grab the URL" friction.
  const [createdWebhookTrigger, setCreatedWebhookTrigger] = useState<AutopilotTrigger | null>(null);

  const scheduleGate = useScheduleSubmitGate(wsId);

  // The schedule only gates submit when this save would actually write it. A
  // locked schedule (2+ triggers) or a stored one the user never touched is not
  // sent, so a preview 400 on the stored expression — an expression the server
  // accepted once and may now reject, e.g. a timezone its tzdata dropped — must
  // not veto edits to the title, prompt or assignee. A schedule the user just
  // added has no stored counterpart to differ from: adding it IS the change.
  const scheduleWillBeWritten =
    triggerKind === "schedule" &&
    !schedulePillDisabled &&
    (isCreate || (existingSchedule !== null ? scheduleDirty : scheduleAdded));

  // The FIRST empty required field in reading order — the user fills one, the
  // next surfaces. Only these two are answered here: a rejected schedule is
  // re-checked against the server below, which toasts its actual reason.
  const missingField: "title" | "assignee" | null =
    title.trim().length === 0 ? "title" : assigneeId.length === 0 ? "assignee" : null;

  // Inline errors appear only after a submit attempt: a form that opens already
  // shouting at the user for fields they have not reached yet is worse than the
  // silence this replaces. Rendered as `showErrors && <field is still empty>`,
  // so filling the field clears its error without a second submit.
  const [showErrors, setShowErrors] = useState(false);
  const titleEditorRef = useRef<TitleEditorRef>(null);
  const assigneeTriggerRef = useRef<HTMLButtonElement>(null);
  const assigneeErrorId = useId();

  const handleSubmit = async () => {
    if (submitting) return;
    if (missingField !== null) {
      // Reveal the inline errors and take the user to the field at fault;
      // focusing scrolls the config column to it on its own.
      setShowErrors(true);
      if (missingField === "title") titleEditorRef.current?.focus();
      else assigneeTriggerRef.current?.focus();
      return;
    }
    setSubmitting(true);
    try {
      if (scheduleWillBeWritten && !(await scheduleGate.ensureAccepted(schedule))) {
        setSubmitting(false);
        return;
      }
      if (isCreate) {
        const autopilot = await createAutopilot.mutateAsync({
          title: title.trim(),
          description: description.trim() || undefined,
          project_id: projectId,
          assignee_type: assigneeType,
          assignee_id: assigneeId,
          execution_mode: executionMode,
          subscribers: subscriberUserIds.map((user_id) => ({
            user_type: "member" as const,
            user_id,
          })),
        });
        let triggerOk = true;
        let triggerErrMessage: string | null = null;
        let webhookTrigger: AutopilotTrigger | null = null;
        try {
          if (triggerKind === "webhook") {
            webhookTrigger = await createTrigger.mutateAsync({
              autopilotId: autopilot.id,
              kind: "webhook",
              event_filters: eventFilters.length > 0 ? eventFilters : undefined,
            });
          } else {
            await createTrigger.mutateAsync({
              autopilotId: autopilot.id,
              kind: "schedule",
              cron_expression: toCron(schedule),
              timezone: schedule.timezone,
            });
          }
        } catch (err) {
          triggerOk = false;
          triggerErrMessage =
            err instanceof Error && err.message ? err.message : null;
        }
        if (triggerKind === "webhook" && webhookTrigger) {
          // Stay in the dialog and surface the URL inline so the user
          // can copy it without first navigating to the detail page.
          setCreatedWebhookTrigger(webhookTrigger);
          toast.success(t(($) => $.dialog.toast_created));
          return;
        }
        onOpenChange(false);
        if (triggerOk) {
          toast.success(t(($) => $.dialog.toast_created));
        } else {
          // Partial success: autopilot saved, schedule failed. Show the
          // server-provided reason so the user can act on it (cron syntax
          // error, conflict, etc.) instead of seeing a generic message.
          toast.error(formatSchedulePartialFailureToast(t, "create", triggerErrMessage));
        }
      } else {
        await updateAutopilot.mutateAsync({
          id: props.autopilotId,
          title: title.trim(),
          description: description.trim() || null,
          project_id: projectId,
          assignee_type: assigneeType,
          assignee_id: assigneeId,
          execution_mode: executionMode,
          subscribers: subscriberUserIds.map((user_id) => ({
            user_type: "member" as const,
            user_id,
          })),
        });
        let triggerOk = true;
        let triggerErrMessage: string | null = null;
        // Skip the schedule sync when the autopilot's first trigger is a
        // webhook — there's no cron to update there, and the schedule
        // panel isn't even rendered for webhook autopilots.
        if (scheduleWillBeWritten) {
          const snapshottedTriggerId = scheduleTriggerIdRef.current;
          try {
            if (snapshottedTriggerId) {
              await updateTrigger.mutateAsync({
                autopilotId: props.autopilotId,
                triggerId: snapshottedTriggerId,
                cron_expression: toCron(schedule),
                timezone: schedule.timezone,
              });
            } else {
              await createTrigger.mutateAsync({
                autopilotId: props.autopilotId,
                kind: "schedule",
                cron_expression: toCron(schedule),
                timezone: schedule.timezone,
              });
            }
          } catch (err) {
            triggerOk = false;
            triggerErrMessage =
              err instanceof Error && err.message ? err.message : null;
          }
        }
        // Webhook autopilots have no schedule, but the user can still edit
        // event_filters from the same dialog. PATCH only when the snapshot
        // taken on open differs from the live state. Sending an explicit
        // empty array clears filters server-side (tri-state semantics — see
        // UpdateAutopilotTriggerRequest in autopilot.go).
        if (
          triggerKind === "webhook" &&
          eventFiltersDirty &&
          firstTriggerIdRef.current
        ) {
          try {
            await updateTrigger.mutateAsync({
              autopilotId: props.autopilotId,
              triggerId: firstTriggerIdRef.current,
              event_filters: eventFilters,
            });
          } catch (err) {
            triggerOk = false;
            triggerErrMessage =
              err instanceof Error && err.message ? err.message : null;
          }
        }
        onOpenChange(false);
        if (triggerOk) {
          toast.success(t(($) => $.dialog.toast_updated));
        } else {
          toast.error(formatSchedulePartialFailureToast(t, "update", triggerErrMessage));
        }
      }
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : isCreate
            ? t(($) => $.dialog.toast_create_failed)
            : t(($) => $.dialog.toast_update_failed),
      );
    } finally {
      setSubmitting(false);
    }
  };

  const contentKey = isCreate ? "create" : props.autopilotId;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        className={cn(
          "p-0 gap-0 flex flex-col overflow-hidden",
          "!transition-all !duration-300 !ease-out !-translate-y-1/2",
          "!w-[calc(100vw-2rem)]",
          isExpanded
            ? "!max-w-6xl !h-[calc(100vh-4rem)]"
            : "!max-w-5xl !h-[min(720px,calc(100vh-4rem))]",
        )}
      >
        <DialogTitle className="sr-only">
          {isCreate ? t(($) => $.dialog.sr_create) : t(($) => $.dialog.sr_edit)}
        </DialogTitle>

        {/* Header */}
        <div className="flex items-center justify-between px-5 pt-3 pb-2 shrink-0 border-b">
          <div className="flex items-center gap-2 text-caption">
            <div className="flex items-center gap-1.5">
              <span className="inline-flex size-5 items-center justify-center rounded-md bg-primary/15 text-primary">
                <Rocket className="size-3" />
              </span>
              <span className="font-medium text-foreground">
                {isCreate
                  ? t(($) => $.dialog.header_create)
                  : t(($) => $.dialog.header_edit)}
              </span>
            </div>
            <span className="text-faint-foreground">·</span>
            <span className="text-muted-foreground">{t(($) => $.dialog.subtitle)}</span>
            {workspaceName && (
              <>
                <ChevronRight className="size-3 text-faint-foreground" />
                <span className="text-muted-foreground">{workspaceName}</span>
              </>
            )}
          </div>
          <div className="flex items-center gap-1">
            {!isCreate && props.canManageAccess && (
              <>
                <Popover>
                  <PopoverTrigger className="flex items-center gap-1.5 rounded-sm px-2 py-1 text-caption text-muted-foreground transition-all hover:bg-accent/60 hover:text-foreground hover:opacity-100 cursor-pointer">
                    <Users className="size-3.5" />
                    <span>{t(($) => $.access.title)}</span>
                  </PopoverTrigger>
                  <PopoverContent align="end" sideOffset={6} keepMounted className="w-80">
                    <PopoverHeader>
                      <PopoverTitle>{t(($) => $.access.title)}</PopoverTitle>
                      <PopoverDescription className="text-caption">
                        {t(($) => $.access.description)}
                      </PopoverDescription>
                    </PopoverHeader>
                    <AutopilotAccessManager
                      autopilotId={props.autopilotId}
                      collaborators={props.collaborators}
                    />
                  </PopoverContent>
                </Popover>
                <span className="mx-0.5 h-4 w-px bg-border" />
              </>
            )}
            <Tooltip>
              <TooltipTrigger
                render={
                  <button
                    type="button"
                    onClick={() => setIsExpanded((v) => !v)}
                    className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                  >
                    {isExpanded ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
                  </button>
                }
              />
              <TooltipContent side="bottom">
                {isExpanded ? t(($) => $.dialog.collapse) : t(($) => $.dialog.expand)}
              </TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger
                render={
                  <button
                    type="button"
                    onClick={() => onOpenChange(false)}
                    className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                  >
                    <XIcon className="size-4" />
                  </button>
                }
              />
              <TooltipContent side="bottom">{t(($) => $.dialog.close)}</TooltipContent>
            </Tooltip>
          </div>
        </div>

        {createdWebhookTrigger ? (
          <WebhookCreatedPanel
            trigger={createdWebhookTrigger}
            onClose={() => {
              setCreatedWebhookTrigger(null);
              onOpenChange(false);
            }}
          />
        ) : (
          <>
        {/* Body: two columns (stacks on narrow screens via flex-wrap at container level) */}
        <div
          key={contentKey}
          className="flex-1 min-h-0 flex flex-col lg:flex-row overflow-y-auto lg:overflow-hidden"
        >
          {/* Left: Runbook */}
          <div className="flex-none lg:flex-1 min-h-0 min-w-0 flex flex-col border-b lg:border-b-0 lg:border-r">
            <div className="px-6 pt-5 pb-3 shrink-0">
              <TitleEditor
                ref={titleEditorRef}
                autoFocus={isCreate}
                defaultValue={initial.title ?? ""}
                placeholder={t(($) => $.dialog.title_placeholder)}
                className="text-display-sm font-semibold tracking-tight"
                onChange={setTitle}
                onSubmit={handleSubmit}
              />
              {/* role="alert": the title is a contenteditable, so there is no
                  input to hang aria-describedby off — announcing the error is
                  the only way a screen-reader user learns why Create did
                  nothing. */}
              {showErrors && title.trim().length === 0 && (
                <p role="alert" className="mt-1.5 text-caption text-destructive">
                  {t(($) => $.dialog.error_title_required)}
                </p>
              )}
            </div>

            <div className="px-6 pb-2 shrink-0 flex items-baseline gap-2">
              <span className="text-micro font-semibold tracking-[0.08em] text-muted-foreground uppercase">
                {t(($) => $.dialog.runbook_label)}
              </span>
              <span className="text-caption text-muted-foreground">
                {t(($) => $.dialog.runbook_hint)}
              </span>
            </div>

            <div className="flex-1 min-h-0 px-6 pb-6 flex flex-col lg:h-full">
              <div className="min-h-[200px] lg:min-h-0 lg:h-full overflow-y-auto rounded-lg border border-border bg-background transition-colors focus-within:border-input px-4 py-3">
                <ContentEditor
                  defaultValue={initial.description ?? ""}
                  placeholder={t(($) => $.dialog.description_placeholder)}
                  onUpdate={setDescription}
                  debounceMs={300}
                  showBubbleMenu={false}
                />
              </div>
            </div>
          </div>

          {/* Right: Configuration */}
          <aside className="w-full lg:w-[380px] shrink-0 overflow-visible lg:overflow-y-auto px-5 py-5 space-y-5 bg-muted/30">
            <AgentSection
              ref={assigneeTriggerRef}
              selectedType={assigneeType}
              selectedId={assigneeId}
              onChange={handleAssigneeChange}
              selectedName={selectedAssignee?.name}
              selectedDescription={selectedAssignee?.description}
              invalid={showErrors && assigneeId.length === 0}
              errorId={assigneeErrorId}
            />

            <OutputModeSection mode={executionMode} onChange={setExecutionMode} />

            {/* Shown for BOTH output modes (MUL-6681). The project is not only
                issue routing: for a run_only autopilot it is the ONLY source of
                project context the daemon has (there is no issue to inherit it
                from), so it decides whether the run gets the project's
                repository / local_directory — and with it worktree isolation —
                or a bare workdir. Hiding it here left run_only autopilots with
                no way to bind one, and the save below used to send null for
                run_only, silently clearing a binding made via the CLI. */}
            <ProjectSection
              projectId={projectId}
              selectedProject={selectedProject}
              onChange={setProjectId}
            />

            {executionMode === "create_issue" && (
              <SubscribersSection
                selectedUserIds={subscriberUserIds}
                onChange={setSubscriberUserIds}
              />
            )}

            {isCreate && (
              <TriggerKindSection kind={triggerKind} onChange={setTriggerKind} />
            )}

            {triggerKind === "schedule" ? (
              <div>
                <SectionLabel>{t(($) => $.dialog.section_schedule)}</SectionLabel>
                {showScheduleEmptyState ? (
                  <ScheduleEmptyState onAdd={() => setScheduleAdded(true)} />
                ) : (
                  /* No `onValidityChange` / `clearRejection` here, unlike the
                     detail page's add-trigger dialog: nothing in this footer is
                     gated on the schedule's validity any more, so the gate's
                     `scheduleValid` would have no reader. The editor still shows
                     its own inline rejection, and `ensureAccepted` re-asks the
                     server on submit and toasts what it says. */
                  <ScheduleEditor
                    value={schedule}
                    onChange={setSchedule}
                    wsId={wsId}
                    // Locked while the save is in flight: the submit path validates
                    // over the network and then writes the schedule it read before
                    // that round trip, so an edit made in between would be dropped
                    // on the floor with a success toast over it.
                    disabled={schedulePillDisabled || submitting}
                    disabledReason={
                      schedulePillDisabled
                        ? t(($) => $.dialog.schedule_disabled_reason)
                        : undefined
                    }
                  />
                )}
              </div>
            ) : (
              <WebhookSection
                isCreate={isCreate}
                eventFilters={eventFilters}
                onEventFiltersChange={setEventFilters}
              />
            )}
          </aside>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between gap-3 px-5 py-3 border-t shrink-0 bg-background">
          {/* The hint drops out while the schedule section states the autopilot
              is manual — a footer promising automatic runs directly under that
              is the same false promise the empty state exists to retire. The
              slot itself stays, or `justify-between` would walk the buttons
              over to the left edge. */}
          <div className="flex items-center gap-1.5 text-caption text-muted-foreground min-w-0">
            {!showScheduleEmptyState && (
              <>
                <Zap className="size-3.5 text-amber-500 shrink-0" />
                <span className="truncate">{t(($) => $.dialog.auto_run_hint)}</span>
              </>
            )}
          </div>
          <div className="flex items-center gap-2 shrink-0">
            <Button size="sm" variant="outline" onClick={() => onOpenChange(false)}>
              {t(($) => $.dialog.cancel)}
            </Button>
            {/* Live whenever a save isn't already in flight — an unmet
                requirement never dims it. A greyed-out button is a dead end
                with no room for a reason (#6231); a live one answers the click
                with an inline error on the field at fault, which says more than
                any disabled state could. `handleSubmit` is the gate. */}
            <Button
              size="sm"
              onClick={handleSubmit}
              disabled={submitting}
              aria-busy={submitting || undefined}
            >
              {submitting
                ? isCreate
                  ? t(($) => $.dialog.creating)
                  : t(($) => $.dialog.saving)
                : isCreate
                  ? t(($) => $.dialog.create)
                  : t(($) => $.dialog.save)}
            </Button>
          </div>
        </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Right column sections
// ---------------------------------------------------------------------------

function SectionLabel({
  children,
  required,
}: {
  children: React.ReactNode;
  // Purely the sighted user's advance warning. `aria-required` is not supported
  // on `role="button"`, so the picker cannot carry it; what a screen reader
  // gets instead is the blocked submit's error, wired to the trigger through
  // aria-describedby and announced by its own role="alert".
  required?: boolean;
}) {
  return (
    <div className="text-micro font-semibold tracking-[0.08em] text-muted-foreground uppercase mb-2">
      {children}
      {required === true && (
        <span aria-hidden className="ml-0.5 text-destructive">
          *
        </span>
      )}
    </div>
  );
}

function AgentSection({
  ref,
  selectedType,
  selectedId,
  onChange,
  selectedName,
  selectedDescription,
  invalid,
  errorId,
}: {
  ref: React.Ref<HTMLButtonElement>;
  selectedType: AutopilotAssigneeType;
  selectedId: string;
  onChange: (next: AssigneeSelection) => void;
  selectedName?: string;
  selectedDescription?: string;
  /** A submit was attempted with no assignee picked. */
  invalid: boolean;
  errorId: string;
}) {
  const { t } = useT("autopilots");
  const hasSelection = selectedId.length > 0;
  return (
    <div>
      {/* Marked required, unlike the Project and Subscribers pickers below it:
          the three look identical, and nothing else told the user that only
          this one blocks Create (#6231). */}
      <SectionLabel required>{t(($) => $.dialog.section_assignee)}</SectionLabel>
      <AgentPicker
        assignee={hasSelection ? { type: selectedType, id: selectedId } : null}
        onChange={onChange}
        align="start"
        triggerRender={
          <button
            ref={ref}
            type="button"
            aria-invalid={invalid || undefined}
            aria-describedby={invalid ? errorId : undefined}
            className={cn(
              "w-full flex items-center gap-2.5 rounded-md border bg-background px-3 py-2 text-left",
              "hover:bg-accent/40 transition-colors cursor-pointer",
              invalid && "border-destructive",
            )}
          >
            {hasSelection ? (
              <ActorAvatar
                actorType={selectedType}
                actorId={selectedId}
                size="md"
                showStatusDot={selectedType === "agent"}
              />
            ) : (
              <span className="inline-flex size-7 items-center justify-center rounded-full bg-muted text-muted-foreground">
                <Rocket className="size-3.5" />
              </span>
            )}
            <span className="flex-1 min-w-0">
              <span className="block text-body font-medium truncate">
                {selectedName ?? t(($) => $.dialog.select_assignee)}
              </span>
              {selectedDescription && (
                <span className="block text-caption text-muted-foreground truncate">
                  {selectedDescription}
                </span>
              )}
            </span>
            <ChevronDown className="size-3.5 text-muted-foreground shrink-0" />
          </button>
        }
      />
      {invalid && (
        <p id={errorId} role="alert" className="mt-1.5 text-caption text-destructive">
          {t(($) => $.dialog.error_assignee_required)}
        </p>
      )}
    </div>
  );
}

function OutputModeSection({
  mode,
  onChange,
}: {
  mode: AutopilotExecutionMode;
  onChange: (mode: AutopilotExecutionMode) => void;
}) {
  const { t } = useT("autopilots");
  return (
    <div>
      <SectionLabel>{t(($) => $.dialog.section_output_mode)}</SectionLabel>
      <div className="space-y-1.5">
        {OUTPUT_MODE_KEYS.map((key) => {
          const selected = key === mode;
          const Icon = OUTPUT_MODE_ICONS[key];
          return (
            <button
              key={key}
              type="button"
              onClick={() => onChange(key)}
              className={cn(
                "w-full flex items-start gap-2.5 rounded-md border px-3 py-2 text-left cursor-pointer transition-colors",
                selected
                  ? "border-primary bg-primary/5"
                  : "bg-background hover:bg-accent/40",
              )}
            >
              <span
                className={cn(
                  "mt-0.5 inline-flex size-4 shrink-0 items-center justify-center rounded-full border",
                  selected
                    ? "border-primary bg-primary text-primary-foreground"
                    : "border-muted-foreground/40 bg-background",
                )}
              >
                {selected ? (
                  <Check className="size-2.5" strokeWidth={3} />
                ) : (
                  <Icon className="size-2.5 opacity-0" />
                )}
              </span>
              <span className="flex-1 min-w-0">
                <span className="block text-body font-medium">
                  {t(($) => $.dialog.output_modes[key].label)}
                </span>
                <span className="block text-caption text-muted-foreground">
                  {t(($) => $.dialog.output_modes[key].description)}
                </span>
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}

function ProjectSection({
  projectId,
  selectedProject,
  onChange,
}: {
  projectId: string | null;
  selectedProject: { title: string; icon: string | null } | null;
  onChange: (projectId: string | null) => void;
}) {
  const { t } = useT("autopilots");
  return (
    <div>
      <SectionLabel>{t(($) => $.dialog.section_project)}</SectionLabel>
      <p className="mb-2 text-micro text-muted-foreground">
        {t(($) => $.dialog.project_hint)}
      </p>
      <ProjectPicker
        projectId={projectId}
        onUpdate={(updates) => onChange(updates.project_id ?? null)}
        align="start"
        triggerRender={
          <button
            type="button"
            className={cn(
              "w-full flex items-center gap-2.5 rounded-md border bg-background px-3 py-2 text-left",
              "hover:bg-accent/40 transition-colors cursor-pointer",
            )}
          >
            {selectedProject ? (
              <ProjectIcon project={selectedProject} size="md" />
            ) : (
              <span className="inline-flex size-5 items-center justify-center rounded-md bg-muted text-muted-foreground">
                <FolderKanban className="size-3.5" />
              </span>
            )}
            <span className="flex-1 min-w-0 truncate text-body font-medium">
              {selectedProject?.title ?? t(($) => $.dialog.no_project)}
            </span>
            <ChevronDown className="size-3.5 text-muted-foreground shrink-0" />
          </button>
        }
      />
    </div>
  );
}

function SubscribersSection({
  selectedUserIds,
  onChange,
}: {
  selectedUserIds: string[];
  onChange: (next: string[]) => void;
}) {
  const { t } = useT("autopilots");
  return (
    <div>
      <SectionLabel>{t(($) => $.dialog.section_subscribers)}</SectionLabel>
      <p className="mb-2 text-micro text-muted-foreground">
        {t(($) => $.dialog.subscribers_hint)}
      </p>
      <SubscriberMultiSelect
        selectedIds={selectedUserIds}
        onChange={onChange}
      />
    </div>
  );
}


// The schedule section of an autopilot that has none. Mirrors the detail
// page's trigger empty state — a dashed card that states the autopilot is
// manual — so the two surfaces agree on what "no schedule" looks like instead
// of one of them showing a filled-in editor and a next-run preview for a
// schedule that does not exist.
function ScheduleEmptyState({ onAdd }: { onAdd: () => void }) {
  const { t } = useT("autopilots");
  return (
    <div className="rounded-md border border-dashed p-3 text-center">
      <p className="text-caption text-muted-foreground">
        {t(($) => $.dialog.schedule_empty)}
      </p>
      <Button size="sm" variant="outline" className="mt-2.5" onClick={onAdd}>
        <Plus className="size-3.5 mr-1" />
        {t(($) => $.dialog.schedule_add)}
      </Button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Trigger kind segmented control + webhook help section
// ---------------------------------------------------------------------------

function TriggerKindSection({
  kind,
  onChange,
}: {
  kind: "schedule" | "webhook";
  onChange: (kind: "schedule" | "webhook") => void;
}) {
  const { t } = useT("autopilots");
  return (
    <div>
      <SectionLabel>{t(($) => $.dialog.section_trigger_kind)}</SectionLabel>
      <SegmentedToggle
        value={kind}
        onChange={onChange}
        buttonClassName="px-3 py-1.5 text-body"
        options={[
          [
            "schedule",
            <span key="schedule" className="flex items-center justify-center gap-1.5">
              <Clock className="h-3.5 w-3.5" />
              {t(($) => $.dialog.trigger_kind_schedule)}
            </span>,
          ],
          [
            "webhook",
            <span key="webhook" className="flex items-center justify-center gap-1.5">
              <Webhook className="h-3.5 w-3.5" />
              {t(($) => $.dialog.trigger_kind_webhook)}
            </span>,
          ],
        ]}
      />
    </div>
  );
}

function WebhookSection({
  isCreate,
  eventFilters,
  onEventFiltersChange,
}: {
  isCreate: boolean;
  eventFilters: WebhookEventFilter[];
  onEventFiltersChange: (filters: WebhookEventFilter[]) => void;
}) {
  const { t } = useT("autopilots");
  return (
    <div className="space-y-3">
      <div>
        <SectionLabel>{t(($) => $.dialog.section_webhook)}</SectionLabel>
        <p className="rounded-md border bg-background px-3 py-2 text-caption text-muted-foreground leading-relaxed">
          {isCreate
            ? t(($) => $.dialog.webhook_help_create)
            : t(($) => $.dialog.webhook_help_edit)}
        </p>
      </div>
      <WebhookEventFilterSection
        filters={eventFilters}
        onChange={onEventFiltersChange}
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Post-create state for webhook autopilots: shows the freshly minted URL
// inline so the user can copy it without leaving the dialog.
// ---------------------------------------------------------------------------

function WebhookCreatedPanel({
  trigger,
  onClose,
}: {
  trigger: AutopilotTrigger;
  onClose: () => void;
}) {
  const { t } = useT("autopilots");

  // Same URL composition the trigger row uses: prefer the server-provided
  // webhook_url, fall back to apiBaseUrl + webhook_path, then origin + path.
  const url =
    buildAutopilotWebhookUrl({
      trigger,
      apiBaseUrl: api.getBaseUrl(),
      currentOrigin: typeof window !== "undefined" ? window.location.origin : undefined,
    }) ?? "";

  return (
    <>
      <div className="flex-1 min-h-0 overflow-y-auto px-8 py-10">
        <div className="mx-auto max-w-xl space-y-5">
          <div className="flex items-center gap-3">
            <span className="inline-flex size-9 items-center justify-center rounded-full bg-primary/15 text-primary">
              <Webhook className="size-4" />
            </span>
            <h2 className="text-title font-semibold tracking-tight">
              {t(($) => $.dialog.webhook_created_title)}
            </h2>
          </div>
          <p className="text-body text-muted-foreground leading-relaxed">
            {t(($) => $.dialog.webhook_created_description)}
          </p>

          <div>
            <div className="text-micro font-semibold tracking-[0.08em] text-muted-foreground uppercase mb-2">
              {t(($) => $.trigger_row.webhook_url_label)}
            </div>
            <WebhookUrlField url={url} size="md" />
          </div>

          <div className="rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-caption text-amber-700 dark:text-amber-400 leading-relaxed">
            {t(($) => $.dialog.webhook_created_warning)}
          </div>
        </div>
      </div>

      <div className="flex items-center justify-end gap-3 px-5 py-3 border-t shrink-0 bg-background">
        <Button size="sm" onClick={onClose}>
          {t(($) => $.dialog.webhook_created_done)}
        </Button>
      </div>
    </>
  );
}
