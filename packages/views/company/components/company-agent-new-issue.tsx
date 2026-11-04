"use client";

import { useState } from "react";
import { toast } from "sonner";
import { PRIORITY_DISPLAY_ORDER } from "@multica/core/issues/config";
import { useCreateIssue } from "@multica/core/issues/mutations";
import type { Agent, IssuePriority } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../i18n";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { priorityLabel } from "../../issues/utils/priority-label";

/**
 * A quick issue form, handed to the agent by default. Handing it over starts the
 * issue as `todo` so the agent picks it up; without that it waits in the backlog.
 */
export function CompanyAgentNewIssue({
  agent,
  projectId,
  onCreated,
  onCancel,
}: {
  agent: Agent;
  projectId?: string;
  /** Called with the new issue's id once it exists. */
  onCreated: (issueId: string) => void;
  onCancel: () => void;
}) {
  const { t } = useT("issues");
  const create = useCreateIssue();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [priority, setPriority] = useState<IssuePriority>("none");
  const [assign, setAssign] = useState(true);

  const submit = () => {
    const trimmed = title.trim();
    if (!trimmed || create.isPending) return;
    create.mutate(
      {
        title: trimmed,
        ...(description.trim() ? { description: description.trim() } : {}),
        priority,
        status: assign ? "todo" : "backlog",
        ...(assign ? { assignee_type: "agent" as const, assignee_id: agent.id } : {}),
        ...(projectId ? { project_id: projectId } : {}),
      },
      {
        onSuccess: (issue) => {
          toast.success(t(($) => $.company.new_created, { identifier: issue.identifier }));
          onCreated(issue.id);
        },
        onError: () => toast.error(t(($) => $.company.new_failed)),
      },
    );
  };

  return (
    <form
      className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4"
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
    >
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="company-new-issue-title">{t(($) => $.company.new_title)}</Label>
        <Input id="company-new-issue-title" autoFocus value={title} onChange={(event) => setTitle(event.target.value)} />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="company-new-issue-description">{t(($) => $.company.new_description)}</Label>
        <Textarea
          id="company-new-issue-description"
          value={description}
          onChange={(event) => setDescription(event.target.value)}
          className="min-h-28"
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <span className="text-caption font-medium">{t(($) => $.company.new_priority)}</span>
        <div className="flex flex-wrap gap-1.5">
          {PRIORITY_DISPLAY_ORDER.map((value) => (
            <Button
              key={value}
              type="button"
              size="xs"
              variant={priority === value ? "secondary" : "outline"}
              aria-pressed={priority === value}
              onClick={() => setPriority(value)}
            >
              <PriorityIcon priority={value} />
              {priorityLabel(value, t)}
            </Button>
          ))}
        </div>
      </div>
      <div className="flex items-center gap-2">
        <Switch id="company-new-issue-assign" checked={assign} onCheckedChange={setAssign} />
        <Label htmlFor="company-new-issue-assign">{t(($) => $.company.new_assign, { name: agent.name })}</Label>
      </div>
      <div className="mt-auto flex justify-end gap-2">
        <Button type="button" variant="ghost" onClick={onCancel}>
          {t(($) => $.company.new_cancel)}
        </Button>
        <Button type="submit" disabled={!title.trim() || create.isPending}>
          {t(($) => $.company.new_create)}
        </Button>
      </div>
    </form>
  );
}
