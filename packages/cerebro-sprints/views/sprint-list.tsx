"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { CalendarRange } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspacePaths } from "@multica/core/paths";
// Imported as a leaf primitive WITHOUT declaring `@multica/views` in this
// package's package.json — `views` already depends on `@multica/cerebro-sprints`,
// so declaring the reverse edge creates a turbo build cycle (broke all frontend
// CI on main, #1352). Same phantom-import pattern as cerebro-groups. Do not add
// `@multica/views` to dependencies here.
import { AppLink } from "@multica/views/navigation";

import {
  projectSprintsOptions,
  sprintSettingsOptions,
  useCreateSprint,
  useDeleteSprint,
} from "../core/queries";
import type { Sprint, SprintStatus } from "../core/types";
import {
  applyNameTemplate,
  computeEnd,
  computeNextStart,
  formatDateOnly,
  formatSprintDateRange,
  parseDateOnly,
} from "../core/template";

interface Props {
  workspaceId: string;
  projectId: string;
}

// Sprint status → badge styling. Sprints carry their own lifecycle
// (planned/active/done/cancelled), distinct from project status.
const SPRINT_STATUS_CONFIG: Record<SprintStatus, { label: string; className: string }> = {
  planned: { label: "Planned", className: "bg-muted text-muted-foreground" },
  active: { label: "Active", className: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400" },
  done: { label: "Done", className: "bg-blue-500/15 text-blue-600 dark:text-blue-400" },
  cancelled: { label: "Cancelled", className: "bg-destructive/15 text-destructive" },
};

function openDatePicker(e: { currentTarget: HTMLInputElement }) {
  try {
    e.currentTarget.showPicker?.();
  } catch {
    // Native date inputs still focus normally when showPicker is unavailable.
  }
}

interface SprintFormDefaults {
  name: string;
  start_date: string;
  end_date: string;
  sequence_no: number;
}

// Cadence-aware defaults for the "New sprint" dialog so a hand-created sprint
// follows the same Name template + duration as an auto-created one (FIR-1581).
// Mirrors the server's nextSequenceFor + ComputeNextStart/ComputeEnd: sequence
// continues from the latest sprint, dates roll forward by the configured
// cadence (falling back to today when there is no prior sprint), and the name
// is rendered from the template. `settings` may be undefined while the query
// loads — defaults degrade to a plain sequence with empty dates.
function buildSprintDefaults(
  sprints: Sprint[],
  settings:
    | {
        name_template: string;
        duration_unit: "day" | "week" | "month";
        duration_count: number;
        start_weekday: number;
      }
    | undefined,
  today: Date,
): SprintFormDefaults {
  const maxSeq = sprints.reduce((max, s) => (s.sequence_no > max ? s.sequence_no : max), 0);
  const sequenceNo = maxSeq + 1;

  const template = settings?.name_template ?? "";
  const unit = settings?.duration_unit ?? "week";
  const count = settings?.duration_count ?? 1;
  const weekday = settings?.start_weekday ?? 1;

  // Roll forward from the latest sprint that has an end date; otherwise seed
  // from today so the picker is pre-filled rather than blank.
  const latest = sprints.reduce<Sprint | null>(
    (acc, s) => (acc === null || s.sequence_no > acc.sequence_no ? s : acc),
    null,
  );
  const previousEnd = latest ? parseDateOnly(latest.end_date) : null;

  let start: Date;
  let end: Date;
  if (previousEnd) {
    start = computeNextStart(previousEnd, unit, weekday);
    end = computeEnd(start, unit, count);
  } else if (settings) {
    start = today;
    end = computeEnd(start, unit, count);
  } else {
    // No settings yet — leave dates blank so the user picks them manually.
    return {
      name: applyNameTemplate(template, sequenceNo),
      start_date: "",
      end_date: "",
      sequence_no: sequenceNo,
    };
  }

  const start_date = formatDateOnly(start);
  const end_date = formatDateOnly(end);
  return {
    name: applyNameTemplate(template, sequenceNo, start_date, end_date),
    start_date,
    end_date,
    sequence_no: sequenceNo,
  };
}

export function SprintList({ workspaceId, projectId }: Props) {
  const sprintsQuery = useQuery(projectSprintsOptions(workspaceId, projectId));
  const settingsQuery = useQuery(sprintSettingsOptions(workspaceId, projectId));
  const createSprint = useCreateSprint(workspaceId, projectId);
  const deleteSprint = useDeleteSprint(workspaceId, projectId);
  const paths = useWorkspacePaths();

  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({
    name: "Sprint 1",
    start_date: "",
    end_date: "",
    goal: "",
  });
  // Once the user types in the Name field we stop re-deriving it from the
  // template on date changes, so a manual override is never clobbered.
  const [nameEdited, setNameEdited] = useState(false);
  // Sequence the prefill computed, reused to keep {n} stable while the user
  // tweaks the dates.
  const [prefillSeq, setPrefillSeq] = useState(1);

  const sprints = sprintsQuery.data?.sprints ?? [];
  const settings = settingsQuery.data ?? undefined;

  // When the dialog opens, prefill name + dates from the Name template and
  // cadence so a hand-created sprint matches an auto-created one (FIR-1581).
  useEffect(() => {
    if (!open) return;
    const defaults = buildSprintDefaults(sprints, settings, new Date());
    setForm({
      name: defaults.name,
      start_date: defaults.start_date,
      end_date: defaults.end_date,
      goal: "",
    });
    setPrefillSeq(defaults.sequence_no);
    setNameEdited(false);
    // Only re-run when the dialog is (re)opened — not on every keystroke.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // Re-render the name from the template when dates change, unless the user has
  // taken over the Name field. Keeps {start}/{end} in sync with the pickers.
  function setDate(field: "start_date" | "end_date", value: string) {
    setForm((prev) => {
      const next = { ...prev, [field]: value };
      if (!nameEdited && settings) {
        next.name = applyNameTemplate(
          settings.name_template,
          prefillSeq,
          next.start_date,
          next.end_date,
        );
      }
      return next;
    });
  }

  function submit() {
    if (!form.name.trim()) {
      toast.error("Name is required");
      return;
    }
    if (!form.start_date || !form.end_date) {
      toast.error("Start and end dates are required");
      return;
    }
    if (form.end_date < form.start_date) {
      toast.error("End date must be on or after start date");
      return;
    }
    createSprint.mutate(
      {
        name: form.name.trim(),
        start_date: form.start_date,
        end_date: form.end_date,
        goal: form.goal.trim() || undefined,
      },
      {
        onSuccess: () => {
          setOpen(false);
          toast.success("Sprint created");
        },
        onError: () => toast.error("Failed to create sprint"),
      },
    );
  }

  return (
    <div className="flex flex-col gap-3 p-6">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-medium">Sprints</h3>
          <p className="text-sm text-muted-foreground">
            Sprints belong to this project. Assign an issue to a sprint from the issue&apos;s
            Sprint picker — the issue stays in its project and also shows on the sprint board.
          </p>
        </div>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger render={<Button size="sm">New sprint</Button>} />
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Create sprint</DialogTitle>
            </DialogHeader>
            <div className="grid gap-3">
              <div className="grid gap-1.5">
                <Label htmlFor="sprint-name">Name</Label>
                <Input
                  id="sprint-name"
                  value={form.name}
                  onChange={(e) => {
                    setNameEdited(true);
                    setForm({ ...form, name: e.target.value });
                  }}
                />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="grid gap-1.5">
                  <Label htmlFor="sprint-start">Start</Label>
                  <Input
                    id="sprint-start"
                    type="date"
                    value={form.start_date}
                    onChange={(e) => setDate("start_date", e.target.value)}
                    onClick={openDatePicker}
                    onFocus={openDatePicker}
                  />
                </div>
                <div className="grid gap-1.5">
                  <Label htmlFor="sprint-end">End</Label>
                  <Input
                    id="sprint-end"
                    type="date"
                    value={form.end_date}
                    onChange={(e) => setDate("end_date", e.target.value)}
                    onClick={openDatePicker}
                    onFocus={openDatePicker}
                  />
                </div>
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="sprint-goal">Goal</Label>
                <Input
                  id="sprint-goal"
                  value={form.goal}
                  onChange={(e) => setForm({ ...form, goal: e.target.value })}
                />
              </div>
            </div>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setOpen(false)}>
                Cancel
              </Button>
              <Button onClick={submit} disabled={createSprint.isPending}>
                Create
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {sprintsQuery.isLoading && (
        <div className="text-sm text-muted-foreground">Loading sprints...</div>
      )}

      {sprints.length === 0 && !sprintsQuery.isLoading && (
        <div className="rounded-md border p-4 text-sm text-muted-foreground">
          No sprints yet. Create one here, then assign issues to it from the issue sidebar.
        </div>
      )}

      <ul className="flex flex-col divide-y rounded-md border">
        {sprints.map((sprint) => {
          const statusConfig = SPRINT_STATUS_CONFIG[sprint.status] ?? SPRINT_STATUS_CONFIG.planned;
          return (
            <li key={sprint.id} className="flex items-center justify-between gap-3 p-3">
              <div className="flex min-w-0 items-center gap-3">
                <CalendarRange className="h-4 w-4 shrink-0 text-muted-foreground" />
                <div className="min-w-0">
                  <div className="flex min-w-0 items-center gap-2">
                    {/* Clicking the sprint opens it as its own board view (create + drag-and-drop). */}
                    <AppLink
                      href={paths.sprintDetail(sprint.id)}
                      className="truncate font-medium hover:underline"
                    >
                      {sprint.name}
                    </AppLink>
                    <Badge variant="secondary" className={cn(statusConfig.className)}>
                      {statusConfig.label}
                    </Badge>
                  </div>
                  <span className="text-xs text-muted-foreground">
                    {formatSprintDateRange(sprint.start_date, sprint.end_date)}
                    {sprint.goal ? ` · ${sprint.goal}` : ""}
                  </span>
                </div>
              </div>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  if (!window.confirm(`Delete sprint "${sprint.name}"?`)) return;
                  deleteSprint.mutate(sprint.id, {
                    onSuccess: () => toast.success("Sprint deleted"),
                    onError: () => toast.error("Failed to delete sprint"),
                  });
                }}
              >
                Delete
              </Button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
