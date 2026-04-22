"use client";

import { useRef, useState } from "react";
import { toast } from "sonner";
import { Calendar, ChevronRight, Maximize2, Minimize2, Rocket, X as XIcon } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { Button } from "@multica/ui/components/ui/button";
import { useCurrentWorkspace } from "@multica/core/paths";
import {
  useCreateAutopilot,
  useCreateAutopilotTrigger,
  useUpdateAutopilot,
  useUpdateAutopilotTrigger,
} from "@multica/core/autopilots/mutations";
import type {
  AutopilotExecutionMode,
  AutopilotTrigger,
  IssuePriority,
} from "@multica/core/types";
import { TitleEditor, ContentEditor } from "../../editor";
import { PillButton } from "../../common/pill-button";
import { PriorityPicker } from "../../issues/components/pickers";
import {
  getDefaultTriggerConfig,
  parseCronExpression,
  summarizeTrigger,
  toCronExpression,
  type TriggerConfig,
} from "./trigger-config";
import { AgentPicker } from "./pickers/agent-picker";
import { ExecutionModePicker } from "./pickers/execution-mode-picker";
import { SchedulePicker } from "./pickers/schedule-picker";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface AutopilotInitial {
  title: string;
  description: string;
  assignee_id: string;
  priority: string;
  execution_mode: AutopilotExecutionMode;
}

export type AutopilotDialogProps =
  | {
      mode: "create";
      open: boolean;
      onOpenChange: (v: boolean) => void;
      initial?: Partial<AutopilotInitial>;
      initialTriggerConfig?: Partial<TriggerConfig>;
    }
  | {
      mode: "edit";
      open: boolean;
      onOpenChange: (v: boolean) => void;
      autopilotId: string;
      initial: AutopilotInitial;
      triggers: AutopilotTrigger[];
    };

// ---------------------------------------------------------------------------
// AutopilotDialog — shared Create/Edit dialog for autopilots
// ---------------------------------------------------------------------------

export function AutopilotDialog(props: AutopilotDialogProps) {
  const { open, onOpenChange } = props;
  const workspaceName = useCurrentWorkspace()?.name;
  const [isExpanded, setIsExpanded] = useState(false);

  const isCreate = props.mode === "create";
  const initial: Partial<AutopilotInitial> = isCreate ? props.initial ?? {} : props.initial;
  const [title, setTitle] = useState(initial.title ?? "");
  const [description, setDescription] = useState(initial.description ?? "");
  const [assigneeId, setAssigneeId] = useState<string>(initial.assignee_id ?? "");
  const [priority, setPriority] = useState<string>(initial.priority ?? "medium");
  const [executionMode, setExecutionMode] = useState<AutopilotExecutionMode>(
    initial.execution_mode ?? "create_issue",
  );

  const initialCfg: TriggerConfig = (() => {
    if (isCreate) {
      const tpl = props.initialTriggerConfig;
      return tpl ? { ...getDefaultTriggerConfig(), ...tpl } : getDefaultTriggerConfig();
    }
    const first = props.triggers[0];
    if (first?.cron_expression) {
      return parseCronExpression(first.cron_expression, first.timezone ?? "UTC");
    }
    return getDefaultTriggerConfig();
  })();
  const [triggerConfig, setTriggerConfig] = useState<TriggerConfig>(initialCfg);
  // Snapshot initial cron payload at mount so `scheduleDirty` only flips when
  // the payload actually changes (prevents phantom trigger creates when the
  // user opens the popover, clicks the already-active option, then Saves).
  const initialCronRef = useRef(toCronExpression(initialCfg));
  const initialTimezoneRef = useRef(initialCfg.timezone);
  const scheduleDirty =
    toCronExpression(triggerConfig) !== initialCronRef.current ||
    triggerConfig.timezone !== initialTimezoneRef.current;

  // Snapshot the first-trigger id at mount. Parent `triggers` prop can update
  // mid-dialog via WS refetch — we want Save to act on the trigger we showed.
  const firstTriggerIdRef = useRef(
    !isCreate && props.triggers[0] ? props.triggers[0].id : null,
  );

  const triggerCount = isCreate ? 0 : props.triggers.length;
  const schedulePillDisabled = !isCreate && triggerCount >= 2;

  const schedulePillLabel = (() => {
    if (isCreate) return summarizeTrigger(triggerConfig);
    if (triggerCount === 0) return "Add schedule";
    if (triggerCount === 1) return summarizeTrigger(triggerConfig);
    return `${triggerCount} schedules`;
  })();

  const createAutopilot = useCreateAutopilot();
  const createTrigger = useCreateAutopilotTrigger();
  const updateAutopilot = useUpdateAutopilot();
  const updateTrigger = useUpdateAutopilotTrigger();
  const [submitting, setSubmitting] = useState(false);

  const canSubmit = title.trim().length > 0 && assigneeId.length > 0 && !submitting;

  const handleSubmit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      if (isCreate) {
        const autopilot = await createAutopilot.mutateAsync({
          title: title.trim(),
          description: description.trim() || undefined,
          assignee_id: assigneeId,
          priority,
          execution_mode: executionMode,
        });
        let scheduleOk = true;
        try {
          await createTrigger.mutateAsync({
            autopilotId: autopilot.id,
            kind: "schedule",
            cron_expression: toCronExpression(triggerConfig),
            timezone: triggerConfig.timezone,
          });
        } catch {
          scheduleOk = false;
        }
        onOpenChange(false);
        if (scheduleOk) toast.success("Autopilot created");
        else toast.error("Autopilot created, but schedule failed to save");
      } else {
        await updateAutopilot.mutateAsync({
          id: props.autopilotId,
          title: title.trim(),
          description: description.trim() || null,
          assignee_id: assigneeId,
          priority,
          execution_mode: executionMode,
        });
        // Schedule: patch the trigger we snapshotted at mount, else create one.
        let scheduleOk = true;
        if (scheduleDirty && !schedulePillDisabled) {
          const snapshottedTriggerId = firstTriggerIdRef.current;
          try {
            if (snapshottedTriggerId) {
              await updateTrigger.mutateAsync({
                autopilotId: props.autopilotId,
                triggerId: snapshottedTriggerId,
                cron_expression: toCronExpression(triggerConfig),
                timezone: triggerConfig.timezone,
              });
            } else {
              await createTrigger.mutateAsync({
                autopilotId: props.autopilotId,
                kind: "schedule",
                cron_expression: toCronExpression(triggerConfig),
                timezone: triggerConfig.timezone,
              });
            }
          } catch {
            scheduleOk = false;
          }
        }
        onOpenChange(false);
        if (scheduleOk) toast.success("Autopilot updated");
        else toast.error("Autopilot updated, but schedule failed to save");
      }
    } catch {
      toast.error(isCreate ? "Failed to create autopilot" : "Failed to update autopilot");
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
          isExpanded ? "!max-w-4xl !h-5/6" : "!max-w-2xl !h-96",
        )}
      >
        <DialogTitle className="sr-only">
          {isCreate ? "New Autopilot" : "Edit Autopilot"}
        </DialogTitle>

        {/* Header */}
        <div className="flex items-center justify-between px-5 pt-3 pb-2 shrink-0">
          <div className="flex items-center gap-1.5 text-xs">
            <span className="text-muted-foreground">{workspaceName}</span>
            <ChevronRight className="size-3 text-muted-foreground/50" />
            <Rocket className="size-3 text-muted-foreground" />
            <span className="font-medium">
              {isCreate ? "New autopilot" : "Edit autopilot"}
            </span>
          </div>
          <div className="flex items-center gap-1">
            <Tooltip>
              <TooltipTrigger
                render={
                  <button
                    onClick={() => setIsExpanded((v) => !v)}
                    className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                  >
                    {isExpanded ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
                  </button>
                }
              />
              <TooltipContent side="bottom">{isExpanded ? "Collapse" : "Expand"}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger
                render={
                  <button
                    onClick={() => onOpenChange(false)}
                    className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                  >
                    <XIcon className="size-4" />
                  </button>
                }
              />
              <TooltipContent side="bottom">Close</TooltipContent>
            </Tooltip>
          </div>
        </div>

        {/* Body re-mounts when switching between autopilots (or create/edit) */}
        <div key={contentKey} className="flex-1 flex flex-col min-h-0">
          {/* Name */}
          <div className="px-5 pb-2 shrink-0">
            <TitleEditor
              autoFocus={isCreate}
              defaultValue={initial.title ?? ""}
              placeholder="Autopilot name"
              className="text-lg font-semibold"
              onChange={setTitle}
              onSubmit={handleSubmit}
            />
          </div>

          {/* Prompt — takes remaining space */}
          <div className="relative flex-1 min-h-0 overflow-y-auto px-5">
            <ContentEditor
              defaultValue={initial.description ?? ""}
              placeholder="Step-by-step instructions for the agent..."
              onUpdate={setDescription}
              debounceMs={300}
              showBubbleMenu={false}
            />
          </div>

          {/* Pill toolbar */}
          <div className="flex items-center gap-1.5 px-4 py-2 shrink-0 flex-wrap">
            <AgentPicker
              agentId={assigneeId || null}
              onChange={setAssigneeId}
              triggerRender={<PillButton />}
              align="start"
            />
            <PriorityPicker
              priority={priority as IssuePriority}
              onUpdate={(u) => { if (u.priority) setPriority(u.priority); }}
              triggerRender={<PillButton />}
              align="start"
            />
            <ExecutionModePicker
              mode={executionMode}
              onChange={setExecutionMode}
              triggerRender={<PillButton />}
              align="start"
            />
            {schedulePillDisabled ? (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <PillButton
                      disabled
                      className="opacity-60 cursor-not-allowed"
                    >
                      <Calendar className="size-3" />
                      <span className="truncate">{schedulePillLabel}</span>
                    </PillButton>
                  }
                />
                <TooltipContent side="top">Edit schedules in detail page</TooltipContent>
              </Tooltip>
            ) : (
              <SchedulePicker
                config={triggerConfig}
                onChange={setTriggerConfig}
                triggerRender={
                  <PillButton>
                    <Calendar className="size-3" />
                    <span className="truncate">{schedulePillLabel}</span>
                  </PillButton>
                }
              />
            )}
          </div>

          {/* Footer */}
          <div className="flex items-center justify-end gap-2 px-4 py-3 border-t shrink-0">
            <Button size="sm" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button size="sm" onClick={handleSubmit} disabled={!canSubmit}>
              {submitting
                ? isCreate ? "Creating..." : "Saving..."
                : isCreate ? "Create autopilot" : "Save"}
            </Button>
          </div>
        </div>

      </DialogContent>
    </Dialog>
  );
}
