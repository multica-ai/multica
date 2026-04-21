"use client";

import { useEffect, useMemo, useState } from "react";
import { Loader2, Save } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useUpdateColumnConfig } from "@multica/core/issues";
import { ALL_STATUSES, STATUS_CONFIG } from "@multica/core/issues/config";
import type { IssueStatus, WorkspaceColumnConfig } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Label } from "@multica/ui/components/ui/label";
import { Markdown } from "../../common/markdown";

interface ColumnInstructionsModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  status: IssueStatus;
  config?: WorkspaceColumnConfig;
}

function sameTransitionSet(a: IssueStatus[], b: IssueStatus[]): boolean {
  if (a.length !== b.length) return false;
  const set = new Set(a);
  return b.every((value) => set.has(value));
}

export function ColumnInstructionsModal({
  open,
  onOpenChange,
  status,
  config,
}: ColumnInstructionsModalProps) {
  const wsId = useWorkspaceId();
  const updateColumnConfig = useUpdateColumnConfig(wsId);
  const [instructions, setInstructions] = useState(config?.instructions ?? "");
  const [allowedTransitions, setAllowedTransitions] = useState<IssueStatus[]>(
    config?.allowed_transitions ?? [],
  );

  useEffect(() => {
    if (!open) return;
    setInstructions(config?.instructions ?? "");
    setAllowedTransitions(config?.allowed_transitions ?? []);
    // Reset only when transitioning to open; avoid clobbering edits on background refetch.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const statusLabel = STATUS_CONFIG[status].label;
  const transitionOptions = useMemo(
    () => ALL_STATUSES.filter((candidate) => candidate !== status),
    [status],
  );
  const isDirty =
    instructions !== (config?.instructions ?? "") ||
    !sameTransitionSet(allowedTransitions, config?.allowed_transitions ?? []);

  const toggleTransition = (candidate: IssueStatus, checked: boolean) => {
    setAllowedTransitions((current) => {
      if (checked) return current.includes(candidate) ? current : [...current, candidate];
      return current.filter((value) => value !== candidate);
    });
  };

  const handleSave = async () => {
    try {
      await updateColumnConfig.mutateAsync({
        status,
        instructions,
        allowed_transitions: allowedTransitions,
      });
      toast.success(`${statusLabel} instructions saved`);
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to save column instructions");
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[calc(100vw-2rem)] !max-w-4xl">
        <DialogHeader>
          <DialogTitle>{statusLabel} column settings</DialogTitle>
          <DialogDescription>
            Configure the markdown guidance shown in this column and which transitions are expected from it.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
          <div className="space-y-2">
            <Label htmlFor={`column-instructions-${status}`}>Instructions</Label>
            <textarea
              id={`column-instructions-${status}`}
              value={instructions}
              onChange={(event) => setInstructions(event.target.value)}
              placeholder={`Write guidance for issues in ${statusLabel}.\n\nExample:\n- Define what “ready” means in this column\n- Include checklists or links\n- Clarify handoff expectations`}
              className="min-h-[320px] w-full resize-y rounded-md border bg-transparent px-3 py-2 text-sm font-mono placeholder:text-muted-foreground/50 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
            />
            <div className="text-xs text-muted-foreground">
              {instructions.length > 0 ? `${instructions.length} characters` : "No instructions set"}
            </div>
          </div>

          <div className="space-y-2">
            <Label>Preview</Label>
            <div className="min-h-[320px] rounded-md border bg-muted/20 p-4">
              {instructions.trim() ? (
                <Markdown mode="full">{instructions}</Markdown>
              ) : (
                <p className="text-sm text-muted-foreground">
                  Markdown preview will appear here.
                </p>
              )}
            </div>
          </div>
        </div>

        <div className="space-y-3">
          <div>
            <Label>Allowed transitions</Label>
            <p className="mt-1 text-xs text-muted-foreground">
              Select which statuses are typically valid next steps from this column.
            </p>
          </div>
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {transitionOptions.map((candidate) => (
              <label
                key={candidate}
                className="flex cursor-pointer items-center gap-2 rounded-md border px-3 py-2 text-sm"
              >
                <Checkbox
                  checked={allowedTransitions.includes(candidate)}
                  onCheckedChange={(next) => toggleTransition(candidate, next === true)}
                />
                <span>{STATUS_CONFIG[candidate].label}</span>
              </label>
            ))}
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={!isDirty || updateColumnConfig.isPending}>
            {updateColumnConfig.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Save className="size-4" />
            )}
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
