"use client";

import { useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Check,
  ChevronDown,
  ChevronRight,
  Clock,
  Copy,
  FilePlus2,
  FolderKanban,
  Maximize2,
  Minimize2,
  Play,
  Rocket,
  Users,
  Webhook,
  X as XIcon,
  Zap,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
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
import { TitleEditor, ContentEditor } from "../../editor";
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

  const initialCfg: ScheduleConfig = (() => {
    if (isCreate) {
      const tpl = props.initialSchedule;
      const fallback = getDefaultScheduleConfig(browserTimezone());
      return tpl ? { ...fallback, ...tpl } : fallback;
    }
    const first = props.triggers[0];
    if (first?.cron_expression) {
      return parseCron(first.cron_expression, first.timezone ?? "UTC");
    }
    return getDefaultScheduleConfig(browserTimezone());
  })();
  const [schedule, setSchedule] = useState<ScheduleConfig>(initialCfg);

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

  const triggerCount = isCreate ? 0 : props.triggers.length;
  const schedulePillDisabled = !isCreate && triggerCount >= 2;

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
  // locked schedule (2+ triggers) or one the user never touched is not sent, so
  // a preview 400 on the stored expression — an expression the server accepted
  // once and may now reject, e.g. a timezone its tzdata dropped — must not veto
  // edits to the title, prompt or assignee.
  const scheduleWillBeWritten =
    triggerKind === "schedule" && !schedulePillDisabled && (isCreate || scheduleDirty);
  const canSubmit =
    title.trim().length > 0 &&
    assigneeId.length > 0 &&
    !submitting &&
    (!scheduleWillBeWritten || scheduleGate.scheduleValid);

  const handleSubmit = async () => {
    if (!canSubmit) return;
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
          project_id: executionMode === "create_issue" ? projectId : null,
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
          project_id: executionMode === "create_issue" ? projectId : null,
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
          const snapshottedTriggerId = firstTriggerIdRef.current;
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
          <div className="flex items-center gap-2 text-xs">
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
            <span className="text-muted-foreground/60">·</span>
            <span className="text-muted-foreground">{t(($) => $.dialog.subtitle)}</span>
            {workspaceName && (
              <>
                <ChevronRight className="size-3 text-muted-foreground/40" />
                <span className="text-muted-foreground">{workspaceName}</span>
              </>
            )}
          </div>
          <div className="flex items-center gap-1">
            {!isCreate && props.canManageAccess && (
              <>
                <Popover>
                  <PopoverTrigger className="flex items-center gap-1.5 rounded-sm px-2 py-1 text-xs text-muted-foreground opacity-90 transition-all hover:bg-accent/60 hover:text-foreground hover:opacity-100 cursor-pointer">
                    <Users className="size-3.5" />
                    <span>{t(($) => $.access.title)}</span>
                  </PopoverTrigger>
                  <PopoverContent align="end" sideOffset={6} keepMounted className="w-80">
                    <PopoverHeader>
                      <PopoverTitle>{t(($) => $.access.title)}</PopoverTitle>
                      <PopoverDescription className="text-xs">
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
          <div className="flex-none lg:flex-1 min-h-0 flex flex-col border-b lg:border-b-0 lg:border-r">
            <div className="px-6 pt-5 pb-3 shrink-0">
              <TitleEditor
                autoFocus={isCreate}
                defaultValue={initial.title ?? ""}
                placeholder={t(($) => $.dialog.title_placeholder)}
                className="text-2xl font-semibold tracking-tight"
                onChange={setTitle}
                onSubmit={handleSubmit}
              />
            </div>

            <div className="px-6 pb-2 shrink-0 flex items-baseline gap-2">
              <span className="text-[11px] font-semibold tracking-[0.08em] text-muted-foreground uppercase">
                {t(($) => $.dialog.runbook_label)}
              </span>
              <span className="text-xs text-muted-foreground/80">
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
              selectedType={assigneeType}
              selectedId={assigneeId}
              onChange={handleAssigneeChange}
              selectedName={selectedAssignee?.name}
              selectedDescription={selectedAssignee?.description}
            />

            <OutputModeSection mode={executionMode} onChange={setExecutionMode} />

            {executionMode === "create_issue" && (
              <ProjectSection
                projectId={projectId}
                selectedProject={selectedProject}
                onChange={setProjectId}
              />
            )}

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
                <ScheduleEditor
                  value={schedule}
                  onChange={(next) => {
                    scheduleGate.clearRejection();
                    setSchedule(next);
                  }}
                  wsId={wsId}
                  onValidityChange={scheduleGate.onValidityChange}
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
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground min-w-0">
            <Zap className="size-3.5 text-amber-500 shrink-0" />
            <span className="truncate">{t(($) => $.dialog.auto_run_hint)}</span>
          </div>
          <div className="flex items-center gap-2 shrink-0">
            <Button size="sm" variant="outline" onClick={() => onOpenChange(false)}>
              {t(($) => $.dialog.cancel)}
            </Button>
            <Button size="sm" onClick={handleSubmit} disabled={!canSubmit}>
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

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-[11px] font-semibold tracking-[0.08em] text-muted-foreground uppercase mb-2">
      {children}
    </div>
  );
}

function AgentSection({
  selectedType,
  selectedId,
  onChange,
  selectedName,
  selectedDescription,
}: {
  selectedType: AutopilotAssigneeType;
  selectedId: string;
  onChange: (next: AssigneeSelection) => void;
  selectedName?: string;
  selectedDescription?: string;
}) {
  const { t } = useT("autopilots");
  const hasSelection = selectedId.length > 0;
  return (
    <div>
      <SectionLabel>{t(($) => $.dialog.section_assignee)}</SectionLabel>
      <AgentPicker
        assignee={hasSelection ? { type: selectedType, id: selectedId } : null}
        onChange={onChange}
        align="start"
        triggerRender={
          <button
            type="button"
            className={cn(
              "w-full flex items-center gap-2.5 rounded-md border bg-background px-3 py-2 text-left",
              "hover:bg-accent/40 transition-colors cursor-pointer",
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
              <span className="block text-sm font-medium truncate">
                {selectedName ?? t(($) => $.dialog.select_assignee)}
              </span>
              {selectedDescription && (
                <span className="block text-xs text-muted-foreground truncate">
                  {selectedDescription}
                </span>
              )}
            </span>
            <ChevronDown className="size-3.5 text-muted-foreground shrink-0" />
          </button>
        }
      />
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
                <span className="block text-sm font-medium">
                  {t(($) => $.dialog.output_modes[key].label)}
                </span>
                <span className="block text-xs text-muted-foreground">
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
            <span className="flex-1 min-w-0 truncate text-sm font-medium">
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
      <p className="mb-2 text-[11px] text-muted-foreground">
        {t(($) => $.dialog.subscribers_hint)}
      </p>
      <SubscriberMultiSelect
        selectedIds={selectedUserIds}
        onChange={onChange}
      />
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
        buttonClassName="px-3 py-1.5 text-sm"
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
        <p className="rounded-md border bg-background px-3 py-2 text-xs text-muted-foreground leading-relaxed">
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
  const [copied, setCopied] = useState(false);

  // Same URL composition the trigger row uses: prefer the server-provided
  // webhook_url, fall back to apiBaseUrl + webhook_path, then origin + path.
  const url =
    buildAutopilotWebhookUrl({
      trigger,
      apiBaseUrl: api.getBaseUrl(),
      currentOrigin: typeof window !== "undefined" ? window.location.origin : undefined,
    }) ?? "";

  const handleCopy = async () => {
    if (!url) return;
    if (await copyText(url)) {
      setCopied(true);
      toast.success(t(($) => $.trigger_row.url_copied));
      setTimeout(() => setCopied(false), 1500);
    } else {
      toast.error(t(($) => $.trigger_row.url_copy_failed));
    }
  };

  return (
    <>
      <div className="flex-1 min-h-0 overflow-y-auto px-8 py-10">
        <div className="mx-auto max-w-xl space-y-5">
          <div className="flex items-center gap-3">
            <span className="inline-flex size-9 items-center justify-center rounded-full bg-primary/15 text-primary">
              <Webhook className="size-4" />
            </span>
            <h2 className="text-lg font-semibold tracking-tight">
              {t(($) => $.dialog.webhook_created_title)}
            </h2>
          </div>
          <p className="text-sm text-muted-foreground leading-relaxed">
            {t(($) => $.dialog.webhook_created_description)}
          </p>

          <div>
            <div className="text-[11px] font-semibold tracking-[0.08em] text-muted-foreground uppercase mb-2">
              {t(($) => $.trigger_row.webhook_url_label)}
            </div>
            <div className="flex items-stretch gap-1.5">
              <code className="flex-1 min-w-0 truncate rounded-md border bg-muted px-3 py-2 text-xs font-mono text-foreground">
                {url}
              </code>
              <Button
                size="icon"
                variant="outline"
                className="h-9 w-9 shrink-0"
                onClick={handleCopy}
                title={t(($) => $.trigger_row.copy_url)}
              >
                {copied ? (
                  <Check className="size-4 text-emerald-500" />
                ) : (
                  <Copy className="size-4 text-muted-foreground" />
                )}
              </Button>
            </div>
          </div>

          <div className="rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-400 leading-relaxed">
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
