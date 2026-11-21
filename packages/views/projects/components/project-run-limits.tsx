"use client";

import { useState, type FormEvent } from "react";
import { useUpdateProject } from "@multica/core/projects/mutations";
import type { Project } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { toast } from "sonner";
import { useT } from "../../i18n";

export function ProjectRunLimits({ project }: { project: Project }) {
  const { t } = useT("projects");
  const { t: tc } = useT("common");
  const [open, setOpen] = useState(false);
  const update = useUpdateProject();

  function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (update.isPending) return;
    const form = new FormData(event.currentTarget);
    const issueLimit = form.get("issueLimit");
    const agentLimit = form.get("agentLimit");
    void update.mutateAsync({
      id: project.id,
      max_runs_per_issue: issueLimit ? Number(issueLimit) : null,
      max_runs_per_agent_per_issue: agentLimit ? Number(agentLimit) : null,
    }).then(() => setOpen(false), () => toast.error(t(($) => $.run_limits.save_failed)));
  }

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!update.isPending) setOpen(next); }}>
      <DialogTrigger render={<Button variant="ghost" size="sm" />}>
        {t(($) => $.run_limits.title)}
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.run_limits.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.run_limits.description)}</DialogDescription>
        </DialogHeader>
        <form onSubmit={save} className="space-y-4">
          <label className="grid gap-2 text-body">
            {t(($) => $.run_limits.issue)}
            <Input name="issueLimit" type="number" min={1} max={2147483647} step={1} defaultValue={project.max_runs_per_issue ?? ""} />
          </label>
          <label className="grid gap-2 text-body">
            {t(($) => $.run_limits.agent)}
            <Input name="agentLimit" type="number" min={1} max={2147483647} step={1} defaultValue={project.max_runs_per_agent_per_issue ?? ""} />
          </label>
          <DialogFooter>
            <Button type="button" variant="outline" disabled={update.isPending} onClick={() => setOpen(false)}>
              {tc(($) => $.cancel)}
            </Button>
            <Button type="submit" disabled={update.isPending} aria-busy={update.isPending}>{tc(($) => $.save)}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
