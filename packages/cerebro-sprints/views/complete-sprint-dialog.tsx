"use client";

// FIR-2828: there was previously no way to end a sprint at all — this dialog
// is the one place that does it, reused by the sprint list row menu and the
// sprint detail sidebar so the flow (and the incomplete-issues choice) stays
// identical everywhere a sprint can be completed.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Label } from "@multica/ui/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { RadioGroup, RadioGroupItem } from "@multica/ui/components/ui/radio-group";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";

import { projectSprintsOptions, useCompleteSprint } from "../core/queries";
import type { IncompleteIssuesAction, Sprint } from "../core/types";

interface Props {
  workspaceId: string;
  projectId: string;
  sprint: Sprint;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCompleted?: () => void;
}

export function CompleteSprintDialog({
  workspaceId,
  projectId,
  sprint,
  open,
  onOpenChange,
  onCompleted,
}: Props) {
  const sprintsQuery = useQuery({
    ...projectSprintsOptions(workspaceId, projectId),
    enabled: open,
  });
  const completeSprint = useCompleteSprint(workspaceId, projectId);

  const [action, setAction] = useState<IncompleteIssuesAction>("leave");
  const [targetSprintId, setTargetSprintId] = useState("");

  // Other open sprints in this project the remaining issues could move into.
  const openTargets = useMemo(
    () =>
      (sprintsQuery.data?.sprints ?? []).filter(
        (s) => s.id !== sprint.id && s.status !== "done" && s.status !== "cancelled",
      ),
    [sprintsQuery.data, sprint.id],
  );

  function submit() {
    if (action === "move_to_sprint" && !targetSprintId) {
      toast.error("Choose a sprint to move the remaining issues into");
      return;
    }
    completeSprint.mutate(
      {
        id: sprint.id,
        payload: {
          incomplete_issues_action: action,
          target_sprint_id: action === "move_to_sprint" ? targetSprintId : undefined,
        },
      },
      {
        onSuccess: (result) => {
          const moved = result?.issues_moved ?? 0;
          toast.success(
            moved > 0 ? `Sprint completed — ${moved} issue(s) moved` : "Sprint completed",
          );
          onOpenChange(false);
          onCompleted?.();
        },
        onError: () => toast.error("Failed to complete sprint"),
      },
    );
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Complete &ldquo;{sprint.name}&rdquo;</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label>Issues still in this sprint</Label>
            <RadioGroup
              value={action}
              onValueChange={(v) => setAction(v as IncompleteIssuesAction)}
              className="gap-3"
            >
              <label className="flex cursor-pointer items-start gap-3">
                <RadioGroupItem value="leave" className="mt-0.5" />
                <span>
                  <span className="text-sm font-medium">Leave as-is</span>
                  <span className="block text-sm text-muted-foreground">
                    Unfinished issues stay assigned to this sprint.
                  </span>
                </span>
              </label>
              <label className="flex cursor-pointer items-start gap-3">
                <RadioGroupItem value="backlog" className="mt-0.5" />
                <span>
                  <span className="text-sm font-medium">Move to backlog</span>
                  <span className="block text-sm text-muted-foreground">
                    Unfinished issues leave the sprint and their status is set to Backlog.
                  </span>
                </span>
              </label>
              <label className="flex cursor-pointer items-start gap-3">
                <RadioGroupItem
                  value="move_to_sprint"
                  className="mt-0.5"
                  disabled={openTargets.length === 0}
                />
                <span>
                  <span className="text-sm font-medium">Move to another sprint</span>
                  <span className="block text-sm text-muted-foreground">
                    {openTargets.length === 0
                      ? "No other open sprint exists in this project yet."
                      : "Unfinished issues are reassigned to the sprint you pick below."}
                  </span>
                </span>
              </label>
            </RadioGroup>
          </div>
          {action === "move_to_sprint" && openTargets.length > 0 && (
            <div className="flex flex-col gap-1.5 pl-7">
              <Label htmlFor="target-sprint">Target sprint</Label>
              <Select value={targetSprintId} onValueChange={(v) => setTargetSprintId(v ?? "")}>
                <SelectTrigger id="target-sprint">
                  <SelectValue placeholder="Choose a sprint" />
                </SelectTrigger>
                <SelectContent>
                  {openTargets.map((s) => (
                    <SelectItem key={s.id} value={s.id}>
                      {s.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={completeSprint.isPending}>
            Complete sprint
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
