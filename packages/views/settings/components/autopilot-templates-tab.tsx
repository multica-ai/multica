"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { autopilotTemplateListOptions } from "@multica/core/autopilots/queries";
import {
  useCreateAutopilotTemplate,
  useDeleteAutopilotTemplate,
  useUpdateAutopilotTemplate,
} from "@multica/core/autopilots/mutations";
import type {
  AutopilotExecutionMode,
  AutopilotTemplate,
  SaveAutopilotTemplateRequest,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";

type Draft = SaveAutopilotTemplateRequest;

function blankDraft(): Draft {
  return {
    name: "",
    description: "",
    execution_mode: "create_issue",
    trigger_kind: "schedule",
    cron_expression: "0 9 * * 1-5",
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
  };
}

function draftFromTemplate(template: AutopilotTemplate): Draft {
  return {
    name: template.name,
    description: template.description,
    execution_mode: template.execution_mode,
    issue_title_template: template.issue_title_template,
    trigger_kind: template.trigger_kind,
    cron_expression: template.cron_expression,
    timezone: template.timezone,
  };
}

export function AutopilotTemplatesTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const userId = useAuthStore((state) => state.user?.id ?? null);
  const { role } = useCurrentMember(wsId);
  const isAdmin = role === "owner" || role === "admin";
  const { data: templates = [], isLoading } = useQuery(
    autopilotTemplateListOptions(wsId),
  );
  const createTemplate = useCreateAutopilotTemplate();
  const updateTemplate = useUpdateAutopilotTemplate();
  const deleteTemplate = useDeleteAutopilotTemplate();
  const [editingId, setEditingId] = useState<string | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [deletingTemplate, setDeletingTemplate] = useState<AutopilotTemplate | null>(null);

  const beginCreate = () => {
    setEditingId(null);
    setDraft(blankDraft());
  };
  const beginEdit = (template: AutopilotTemplate) => {
    setEditingId(template.id);
    setDraft(draftFromTemplate(template));
  };

  const save = async () => {
    if (!draft || !draft.name.trim()) return;
    const payload: Draft = {
      ...draft,
      name: draft.name.trim(),
      description: draft.description.trim(),
      issue_title_template:
        draft.execution_mode === "create_issue"
          ? draft.issue_title_template?.trim() || null
          : null,
      cron_expression:
        draft.trigger_kind === "schedule" ? draft.cron_expression : null,
      timezone: draft.trigger_kind === "schedule" ? draft.timezone : null,
    };
    try {
      if (editingId) {
        await updateTemplate.mutateAsync({ id: editingId, ...payload });
      } else {
        await createTemplate.mutateAsync(payload);
      }
      toast.success(t(($) => $.autopilot_templates.toast_saved));
      setDraft(null);
      setEditingId(null);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.autopilot_templates.toast_save_failed),
      );
    }
  };

  const remove = async (template: AutopilotTemplate) => {
    try {
      await deleteTemplate.mutateAsync(template.id);
      toast.success(t(($) => $.autopilot_templates.toast_deleted));
      setDeletingTemplate(null);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.autopilot_templates.toast_delete_failed),
      );
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-base font-semibold">
            {t(($) => $.autopilot_templates.title)}
          </h2>
          <p className="mt-1 text-sm text-muted-foreground">
            {t(($) => $.autopilot_templates.description)}
          </p>
        </div>
        <Button size="sm" onClick={beginCreate}>
          <Plus className="size-4" aria-hidden />
          {t(($) => $.autopilot_templates.new)}
        </Button>
      </div>

      {draft && (
        <div className="space-y-4 rounded-lg border p-4">
          <div className="space-y-1.5">
            <Label htmlFor="autopilot-template-name">
              {t(($) => $.autopilot_templates.name)}
            </Label>
            <Input
              id="autopilot-template-name"
              name="autopilot_template_name"
              autoComplete="off"
              value={draft.name}
              onChange={(event) => setDraft({ ...draft, name: event.target.value })}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="autopilot-template-runbook">
              {t(($) => $.autopilot_templates.runbook)}
            </Label>
            <Textarea
              id="autopilot-template-runbook"
              name="autopilot_template_runbook"
              autoComplete="off"
              rows={7}
              value={draft.description}
              onChange={(event) =>
                setDraft({ ...draft, description: event.target.value })
              }
            />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label>{t(($) => $.autopilot_templates.execution_mode)}</Label>
              <Select
                items={[
                  {
                    value: "create_issue",
                    label: t(($) => $.autopilot_templates.execution_mode_create_issue),
                  },
                  {
                    value: "run_only",
                    label: t(($) => $.autopilot_templates.execution_mode_run_only),
                  },
                ]}
                value={draft.execution_mode}
                onValueChange={(value) =>
                  value &&
                  setDraft({
                    ...draft,
                    execution_mode: value as AutopilotExecutionMode,
                  })
                }
              >
                <SelectTrigger aria-label={t(($) => $.autopilot_templates.execution_mode)}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="create_issue">
                    {t(($) => $.autopilot_templates.execution_mode_create_issue)}
                  </SelectItem>
                  <SelectItem value="run_only">
                    {t(($) => $.autopilot_templates.execution_mode_run_only)}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label>{t(($) => $.autopilot_templates.trigger)}</Label>
              <Select
                items={[
                  {
                    value: "schedule",
                    label: t(($) => $.autopilot_templates.trigger_schedule),
                  },
                  {
                    value: "webhook",
                    label: t(($) => $.autopilot_templates.trigger_webhook),
                  },
                ]}
                value={draft.trigger_kind}
                onValueChange={(value) =>
                  value &&
                  setDraft({
                    ...draft,
                    trigger_kind: value as "schedule" | "webhook",
                  })
                }
              >
                <SelectTrigger aria-label={t(($) => $.autopilot_templates.trigger)}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="schedule">
                    {t(($) => $.autopilot_templates.trigger_schedule)}
                  </SelectItem>
                  <SelectItem value="webhook">
                    {t(($) => $.autopilot_templates.trigger_webhook)}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
          {draft.execution_mode === "create_issue" && (
            <div className="space-y-1.5">
              <Label htmlFor="autopilot-template-issue-title">
                {t(($) => $.autopilot_templates.issue_title_template)}
              </Label>
              <Input
                id="autopilot-template-issue-title"
                name="autopilot_template_issue_title"
                autoComplete="off"
                value={draft.issue_title_template ?? ""}
                placeholder={t(
                  ($) => $.autopilot_templates.issue_title_template_placeholder,
                  { date: "{{date}}" },
                )}
                onChange={(event) =>
                  setDraft({
                    ...draft,
                    issue_title_template: event.target.value,
                  })
                }
              />
            </div>
          )}
          {draft.trigger_kind === "schedule" && (
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="autopilot-template-cron">
                  {t(($) => $.autopilot_templates.cron)}
                </Label>
                <Input
                  id="autopilot-template-cron"
                  name="autopilot_template_cron"
                  autoComplete="off"
                  value={draft.cron_expression ?? ""}
                  onChange={(event) =>
                    setDraft({ ...draft, cron_expression: event.target.value })
                  }
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="autopilot-template-timezone">
                  {t(($) => $.autopilot_templates.timezone)}
                </Label>
                <Input
                  id="autopilot-template-timezone"
                  name="autopilot_template_timezone"
                  autoComplete="off"
                  value={draft.timezone ?? ""}
                  onChange={(event) =>
                    setDraft({ ...draft, timezone: event.target.value })
                  }
                />
              </div>
            </div>
          )}
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setDraft(null)}>
              {t(($) => $.autopilot_templates.cancel)}
            </Button>
            <Button
              onClick={save}
              disabled={
                !draft.name.trim() || createTemplate.isPending || updateTemplate.isPending
              }
            >
              {t(($) => $.autopilot_templates.save)}
            </Button>
          </div>
        </div>
      )}

      <div className="divide-y rounded-lg border">
        {isLoading ? (
          <p className="p-4 text-sm text-muted-foreground">
            {t(($) => $.autopilot_templates.loading)}
          </p>
        ) : templates.length === 0 ? (
          <p className="p-4 text-sm text-muted-foreground">
            {t(($) => $.autopilot_templates.empty)}
          </p>
        ) : (
          templates.map((template) => {
            const canManage = isAdmin || template.created_by === userId;
            return (
              <div key={template.id} className="flex items-start gap-3 p-4">
                <div className="min-w-0 flex-1">
                  <p className="break-words text-sm font-medium">{template.name}</p>
                  <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">
                    {template.description || t(($) => $.autopilot_templates.no_runbook)}
                  </p>
                  <p className="mt-2 text-xs text-muted-foreground">
                    {template.trigger_kind === "schedule"
                      ? `${template.cron_expression} · ${template.timezone}`
                      : t(($) => $.autopilot_templates.trigger_webhook)}
                  </p>
                </div>
                {canManage && (
                  <div className="flex gap-1">
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      aria-label={t(($) => $.autopilot_templates.edit, { name: template.name })}
                      onClick={() => beginEdit(template)}
                    >
                      <Pencil className="size-4" aria-hidden />
                    </Button>
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      aria-label={t(($) => $.autopilot_templates.delete, { name: template.name })}
                      onClick={() => setDeletingTemplate(template)}
                    >
                      <Trash2 className="size-4" aria-hidden />
                    </Button>
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>

      <Dialog
        open={deletingTemplate !== null}
        onOpenChange={(open) => {
          if (!open && !deleteTemplate.isPending) setDeletingTemplate(null);
        }}
      >
        <DialogContent showCloseButton={false} className="sm:max-w-sm">
          <DialogTitle>{t(($) => $.autopilot_templates.delete_confirm_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.autopilot_templates.delete_confirm_body, {
              name: deletingTemplate?.name ?? "",
            })}
          </DialogDescription>
          <DialogFooter>
            <Button
              variant="outline"
              disabled={deleteTemplate.isPending}
              onClick={() => setDeletingTemplate(null)}
            >
              {t(($) => $.autopilot_templates.cancel)}
            </Button>
            <Button
              variant="destructive"
              disabled={!deletingTemplate || deleteTemplate.isPending}
              onClick={() => deletingTemplate && void remove(deletingTemplate)}
            >
              {t(($) => $.autopilot_templates.delete_action)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
