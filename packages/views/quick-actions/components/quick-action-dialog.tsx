"use client";

import { useEffect, useMemo, useState } from "react";
import { Globe, Info, Lock } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { useCreateQuickAction } from "@multica/core/quick-actions";
import type { QuickAction, QuickActionAssigneeType, QuickActionVisibility } from "@multica/core/types";
import { findQuickActionTemplateToken } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label as FieldLabel } from "@multica/ui/components/ui/label";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { AgentPicker } from "../../autopilots/components/pickers/agent-picker";
import { useT } from "../../i18n";

interface FormState {
  name: string;
  description: string;
  assigneeType: Exclude<QuickActionAssigneeType, "">;
  assigneeId: string;
  prompt: string;
  visibility: QuickActionVisibility;
}

const EMPTY_FORM: FormState = {
  name: "",
  description: "",
  assigneeType: "agent",
  assigneeId: "",
  prompt: "",
  visibility: "private",
};

function toFormState(action: QuickAction): FormState {
  return {
    name: action.name,
    description: action.description,
    assigneeType: action.assignee_type === "squad" ? "squad" : "agent",
    assigneeId: action.assignee_id,
    prompt: action.prompt,
    visibility: action.visibility === "private" ? "private" : "public",
  };
}

export function QuickActionDialog({
  open,
  action,
  onOpenChange,
  onSubmit,
}: {
  open: boolean;
  action: QuickAction | null;
  onOpenChange: (open: boolean) => void;
  onSubmit: (form: FormState) => Promise<void>;
}) {
  const { t } = useT("settings");
  const { role } = useCurrentMember(useWorkspaceId());
  const canPublish = role === "owner" || role === "admin";
  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    setForm(action ? toFormState(action) : EMPTY_FORM);
  }, [open, action]);

  // Mirror the server's rejection inline so the author sees it while typing
  // instead of on submit. The server stays the authority; this is an
  // affordance.
  const templateToken = useMemo(() => findQuickActionTemplateToken(form.prompt), [form.prompt]);

  const canSave =
    form.name.trim().length > 0 &&
    form.prompt.trim().length > 0 &&
    templateToken === null;

  const handleSubmit = async () => {
    if (!canSave) return;
    setSaving(true);
    try {
      await onSubmit(form);
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {action ? t(($) => $.quick_actions.edit_title) : t(($) => $.quick_actions.create_title)}
          </DialogTitle>
          <DialogDescription>{t(($) => $.quick_actions.dialog_description)}</DialogDescription>
        </DialogHeader>

        {/* space-y-5 against space-y-1.5 inside each group: the gap BETWEEN
            fields has to clearly beat the gap between a label and its own
            control, or the four groups read as one continuous block of text. */}
        <div className="space-y-5">
          <div className="space-y-1.5">
            <FieldLabel htmlFor="qa-name">{t(($) => $.quick_actions.field_name)}</FieldLabel>
            <Input
              id="qa-name"
              value={form.name}
              maxLength={32}
              placeholder={t(($) => $.quick_actions.field_name_placeholder)}
              onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
            />
          </div>

          {/* Visibility first: it is the one choice that constrains the rest
              (a public action may only bind a publicly-invocable target), so
              asking it up front avoids a rejected Save. */}
          <div className="space-y-1.5">
            <FieldLabel>{t(($) => $.quick_actions.field_visibility)}</FieldLabel>
            <div className={cn("grid gap-2", action || canPublish ? "grid-cols-2" : "grid-cols-1")}>
              {(action || canPublish ? ["public", "private"] as const : ["private"] as const).map((option) => (
                <button
                  key={option}
                  type="button"
                  aria-pressed={form.visibility === option}
                  onClick={() => setForm((f) => ({ ...f, visibility: option }))}
                  className={cn(
                    "flex items-start gap-2 rounded-lg border px-3 py-2 text-left transition-colors",
                    form.visibility === option
                      ? "border-primary bg-accent/50"
                      : "border-surface-border hover:bg-accent/30",
                  )}
                >
                  {option === "public" ? (
                    <Globe className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                  ) : (
                    <Lock className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                  )}
                  <span className="min-w-0">
                    <span className="block text-body font-medium">
                      {option === "public"
                        ? t(($) => $.quick_actions.visibility_public)
                        : t(($) => $.quick_actions.visibility_private)}
                    </span>
                    <span className="mt-0.5 block text-caption text-muted-foreground">
                      {option === "public"
                        ? t(($) => $.quick_actions.visibility_public_hint)
                        : t(($) => $.quick_actions.visibility_private_hint)}
                    </span>
                  </span>
                </button>
              ))}
            </div>
          </div>

          <div className="space-y-1.5">
            <FieldLabel id="qa-target-label" htmlFor="qa-target">
              {t(($) => $.quick_actions.field_target)}
            </FieldLabel>
            <AgentPicker
              assignee={form.assigneeId ? { type: form.assigneeType, id: form.assigneeId } : null}
              onChange={(next) =>
                setForm((f) => ({ ...f, assigneeType: next.type, assigneeId: next.id }))
              }
              emptyOption={{
                label: t(($) => $.quick_actions.no_target),
                onSelect: () => setForm((f) => ({ ...f, assigneeId: "" })),
              }}
              triggerRender={
                <Button
                  id="qa-target"
                  type="button"
                  variant="outline"
                  aria-labelledby="qa-target-label qa-target"
                />
              }
            />
            {/* Public promises "everyone can run this", so the server refuses a
                target the team cannot invoke. Saying so here turns a 400 into
                an expectation set before the user hits Save. */}
            {!form.assigneeId ? (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.quick_actions.no_target_hint)}
              </p>
            ) : form.visibility === "public" ? (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.quick_actions.public_target_hint)}
              </p>
            ) : null}
          </div>

          <div className="space-y-1.5">
            <FieldLabel htmlFor="qa-prompt">{t(($) => $.quick_actions.field_prompt)}</FieldLabel>
            <Textarea
              id="qa-prompt"
              value={form.prompt}
              rows={5}
              placeholder={t(($) => $.quick_actions.field_prompt_placeholder)}
              onChange={(e) => setForm((f) => ({ ...f, prompt: e.target.value }))}
            />
            {templateToken !== null ? (
              <p className="text-caption text-destructive">
                {t(($) => $.quick_actions.template_not_supported, { token: templateToken })}
              </p>
            ) : (
              <p className="inline-flex items-center gap-1 text-caption text-muted-foreground">
                <Info className="size-3" />
                {t(($) => $.quick_actions.prompt_hint)}
              </p>
            )}
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t(($) => $.quick_actions.cancel)}
          </Button>
          <Button onClick={handleSubmit} disabled={!canSave || saving}>
            {t(($) => $.quick_actions.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function CreateQuickActionDialog({ onClose }: { onClose: () => void }) {
  const create = useCreateQuickAction();
  return <QuickActionDialog open action={null} onOpenChange={(open) => { if (!open) onClose(); }} onSubmit={async (form) => {
    await create.mutateAsync({ name: form.name, description: form.description, assignee_type: form.assigneeId ? form.assigneeType : "", assignee_id: form.assigneeId, prompt: form.prompt, visibility: form.visibility });
  }} />;
}
