"use client";

import { useState } from "react";
import { useWorkspacePaths } from "@multica/core/paths";
import { useCreateSprint } from "@multica/core/sprints";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";

export function CreateSprintModal({ onClose }: { onClose: () => void }) {
  const { t } = useT("sprints");
  const router = useNavigation();
  const wsPaths = useWorkspacePaths();
  const createSprint = useCreateSprint();

  const [name, setName] = useState("");
  const [goal, setGoal] = useState("");
  const [startDate, setStartDate] = useState("");
  const [endDate, setEndDate] = useState("");

  const canSubmit =
    !!name.trim() &&
    !!startDate &&
    !!endDate &&
    new Date(endDate) > new Date(startDate) &&
    !createSprint.isPending;

  const handleSubmit = () => {
    if (!canSubmit) return;
    createSprint.mutate(
      {
        name: name.trim(),
        goal: goal.trim() || undefined,
        start_date: startDate,
        end_date: endDate,
      },
      {
        onSuccess: (sprint) => {
          onClose();
          toast.success("Sprint created");
          router.push(wsPaths.sprintDetail(sprint.id));
        },
        onError: (err) => {
          toast.error(err instanceof Error ? err.message : "Failed to create sprint");
        },
      },
    );
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="p-0 gap-0 flex flex-col overflow-hidden !top-1/2 !left-1/2 !-translate-x-1/2 !-translate-y-1/2 !w-full !max-w-lg">
        <DialogHeader className="border-b px-5 py-3 space-y-0">
          <DialogTitle className="text-base font-semibold">
            {t(($) => $.create.title)}
          </DialogTitle>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto p-5 space-y-4">
          <div>
            <Label className="text-xs text-muted-foreground">{t(($) => $.create.label_name)}</Label>
            <Input
              autoFocus
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t(($) => $.create.name_placeholder)}
              className="mt-1"
              onKeyDown={(e) => {
                if (e.key === "Enter") handleSubmit();
              }}
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">{t(($) => $.create.label_goal)}</Label>
            <Input
              type="text"
              value={goal}
              onChange={(e) => setGoal(e.target.value)}
              placeholder={t(($) => $.create.goal_placeholder)}
              className="mt-1"
            />
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label className="text-xs text-muted-foreground">
                {t(($) => $.create.start_date)}
              </Label>
              <Input
                type="date"
                value={startDate}
                onChange={(e) => setStartDate(e.target.value)}
                className="mt-1"
              />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">
                {t(($) => $.create.end_date)}
              </Label>
              <Input
                type="date"
                value={endDate}
                onChange={(e) => setEndDate(e.target.value)}
                className="mt-1"
              />
            </div>
          </div>
        </div>

        <div className="flex items-center justify-end gap-2 border-t bg-background px-5 py-3">
          <Button variant="ghost" onClick={onClose}>
            {t(($) => $.create.cancel)}
          </Button>
          <Button onClick={handleSubmit} disabled={!canSubmit}>
            {createSprint.isPending ? "..." : t(($) => $.create.submit)}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
