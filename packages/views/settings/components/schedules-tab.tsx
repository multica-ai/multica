"use client";

import { useEffect, useState, useCallback } from "react";
import { Clock, Plus, Trash2, Play, Power, PowerOff } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type { Schedule, Agent } from "@multica/core/types";

// Common cron presets for the dropdown helper.
const CRON_PRESETS = [
  { label: "Every minute", value: "* * * * *" },
  { label: "Every 5 minutes", value: "*/5 * * * *" },
  { label: "Every 15 minutes", value: "*/15 * * * *" },
  { label: "Every hour", value: "0 * * * *" },
  { label: "Every day at 9am", value: "0 9 * * *" },
  { label: "Every Monday at 9am", value: "0 9 * * 1" },
  { label: "Custom", value: "" },
];

const TIMEZONES = [
  "UTC",
  "America/New_York",
  "America/Chicago",
  "America/Denver",
  "America/Los_Angeles",
  "Europe/London",
  "Europe/Berlin",
  "Asia/Tokyo",
  "Asia/Kolkata",
  "Australia/Sydney",
];

function formatDate(iso: string | null): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function SchedulesTab() {
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [deleteId, setDeleteId] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  // Form state
  const [formName, setFormName] = useState("");
  const [formCron, setFormCron] = useState("0 9 * * *");
  const [formCronPreset, setFormCronPreset] = useState("0 9 * * *");
  const [formTimezone, setFormTimezone] = useState("UTC");
  const [formTitle, setFormTitle] = useState("");
  const [formDescription, setFormDescription] = useState("");
  const [formAssigneeId, setFormAssigneeId] = useState("");
  const [formPriority, setFormPriority] = useState("none");

  const loadData = useCallback(async () => {
    try {
      const [s, a] = await Promise.all([
        api.listSchedules(),
        api.listAgents(),
      ]);
      setSchedules(s);
      setAgents(a);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to load schedules");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadData();
  }, [loadData]);

  const resetForm = () => {
    setFormName("");
    setFormCron("0 9 * * *");
    setFormCronPreset("0 9 * * *");
    setFormTimezone("UTC");
    setFormTitle("");
    setFormDescription("");
    setFormAssigneeId("");
    setFormPriority("none");
  };

  const handleCreate = async () => {
    if (!formName || !formCron || !formTitle || !formAssigneeId) {
      toast.error("Name, cron, title, and assignee are required");
      return;
    }
    setSaving(true);
    try {
      await api.createSchedule({
        name: formName,
        cron_expression: formCron,
        timezone: formTimezone,
        title_template: formTitle,
        description: formDescription,
        assignee_type: "agent",
        assignee_id: formAssigneeId,
        priority: formPriority,
        enabled: true,
      });
      toast.success("Schedule created");
      setDialogOpen(false);
      resetForm();
      await loadData();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to create schedule");
    } finally {
      setSaving(false);
    }
  };

  const handleToggle = async (id: string, enabled: boolean) => {
    try {
      await api.updateSchedule(id, { enabled });
      setSchedules((prev) =>
        prev.map((s) => (s.id === id ? { ...s, enabled } : s))
      );
      toast.success(enabled ? "Schedule enabled" : "Schedule disabled");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to toggle");
    }
  };

  const handleDelete = async () => {
    if (!deleteId) return;
    try {
      await api.deleteSchedule(deleteId);
      setSchedules((prev) => prev.filter((s) => s.id !== deleteId));
      toast.success("Schedule deleted");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to delete");
    } finally {
      setDeleteId(null);
    }
  };

  const handleRunNow = async (id: string) => {
    try {
      const result = await api.runScheduleNow(id);
      toast.success(`Fired! Issue: ${result.issue_id.slice(0, 8)}`);
      await loadData();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to fire");
    }
  };

  const agentName = (id: string) =>
    agents.find((a) => a.id === id)?.name ?? id.slice(0, 8);

  if (loading) {
    return (
      <div className="space-y-3">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-20 w-full" />
        <Skeleton className="h-20 w-full" />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold">Scheduled Tasks</h2>
          <p className="text-sm text-muted-foreground">
            Automatically create issues on a cron schedule and assign them to agents.
          </p>
        </div>
        <Button size="sm" onClick={() => setDialogOpen(true)}>
          <Plus className="h-4 w-4 mr-1" />
          New Schedule
        </Button>
      </div>

      {schedules.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-12 text-center">
          <Clock className="h-10 w-10 text-muted-foreground/50 mb-3" />
          <p className="text-sm text-muted-foreground">No schedules yet</p>
          <p className="text-xs text-muted-foreground mt-1">
            Create one to start automating recurring tasks.
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {schedules.map((s) => (
            <div
              key={s.id}
              className="flex items-center justify-between border rounded-lg px-4 py-3"
            >
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="font-medium truncate">{s.name}</span>
                  {!s.enabled && (
                    <span className="text-xs text-muted-foreground bg-muted px-1.5 py-0.5 rounded">
                      disabled
                    </span>
                  )}
                  {s.last_run_error && (
                    <Tooltip>
                      <TooltipTrigger>
                        <span className="text-xs text-destructive bg-destructive/10 px-1.5 py-0.5 rounded">
                          error
                        </span>
                      </TooltipTrigger>
                      <TooltipContent side="bottom" className="max-w-xs">
                        {s.last_run_error}
                      </TooltipContent>
                    </Tooltip>
                  )}
                </div>
                <div className="flex items-center gap-3 text-xs text-muted-foreground mt-1">
                  <span className="font-mono">{s.cron_expression}</span>
                  <span>{s.timezone}</span>
                  <span>→ {agentName(s.assignee_id)}</span>
                  <span>Next: {formatDate(s.next_run_at)}</span>
                  <span>Last: {formatDate(s.last_run_at)}</span>
                </div>
              </div>
              <div className="flex items-center gap-1 ml-4 shrink-0">
                <Tooltip>
                  <TooltipTrigger
                    render={<Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => handleRunNow(s.id)} />}
                  >
                    <Play className="h-3.5 w-3.5" />
                  </TooltipTrigger>
                  <TooltipContent>Run now</TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger
                    render={<Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => handleToggle(s.id, !s.enabled)} />}
                  >
                    {s.enabled ? (
                      <Power className="h-3.5 w-3.5" />
                    ) : (
                      <PowerOff className="h-3.5 w-3.5 text-muted-foreground" />
                    )}
                  </TooltipTrigger>
                  <TooltipContent>{s.enabled ? "Disable" : "Enable"}</TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger
                    render={<Button variant="ghost" size="icon" className="h-8 w-8 text-muted-foreground hover:text-destructive" onClick={() => setDeleteId(s.id)} />}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </TooltipTrigger>
                  <TooltipContent>Delete</TooltipContent>
                </Tooltip>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Create dialog */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>New Scheduled Task</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1.5">
              <Label>Name</Label>
              <Input
                placeholder="Daily standup summary"
                value={formName}
                onChange={(e) => setFormName(e.target.value)}
              />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label>Cron Schedule</Label>
                <Select
                  value={formCronPreset}
                  onValueChange={(v) => {
                    if (v) {
                      setFormCronPreset(v);
                      setFormCron(v);
                    }
                  }}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {CRON_PRESETS.map((p) => (
                      <SelectItem key={p.label} value={p.value || "custom"}>
                        {p.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {(formCronPreset === "custom" || formCronPreset === "") && (
                  <Input
                    placeholder="*/5 * * * *"
                    value={formCron}
                    onChange={(e) => setFormCron(e.target.value)}
                    className="mt-1.5 font-mono text-sm"
                  />
                )}
              </div>
              <div className="space-y-1.5">
                <Label>Timezone</Label>
                <Select value={formTimezone} onValueChange={(v) => { if (v) setFormTimezone(v); }}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {TIMEZONES.map((tz) => (
                      <SelectItem key={tz} value={tz}>
                        {tz}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className="space-y-1.5">
              <Label>Issue Title Template</Label>
              <Input
                placeholder="Daily standup {{date}}"
                value={formTitle}
                onChange={(e) => setFormTitle(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                Variables: {"{{date}}"}, {"{{datetime}}"}, {"{{schedule_name}}"}
              </p>
            </div>
            <div className="space-y-1.5">
              <Label>Description</Label>
              <textarea
                className="flex min-h-[80px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                placeholder="What should the agent do?"
                value={formDescription}
                onChange={(e) => setFormDescription(e.target.value)}
              />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label>Assign to Agent</Label>
                <Select value={formAssigneeId} onValueChange={(v) => { if (v) setFormAssigneeId(v); }}>
                  <SelectTrigger>
                    <SelectValue placeholder="Select agent..." />
                  </SelectTrigger>
                  <SelectContent>
                    {agents.map((a) => (
                      <SelectItem key={a.id} value={a.id}>
                        {a.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label>Priority</Label>
                <Select value={formPriority} onValueChange={(v) => { if (v) setFormPriority(v); }}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {["none", "low", "medium", "high", "urgent"].map((p) => (
                      <SelectItem key={p} value={p}>
                        {p}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handleCreate} disabled={saving}>
              {saving ? "Creating..." : "Create Schedule"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete confirmation */}
      <AlertDialog open={!!deleteId} onOpenChange={() => setDeleteId(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete schedule?</AlertDialogTitle>
            <AlertDialogDescription>
              This will permanently remove the schedule. Existing issues created
              by past runs are not affected.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete}>Delete</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
