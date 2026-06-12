"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Plus, Settings2, Trash2, Pencil, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  quickActionListOptions,
  useCreateQuickAction,
  useUpdateQuickAction,
  useDeleteQuickAction,
} from "@multica/core/quick-actions";
import type { QuickAction } from "@multica/core/types";
import { useT } from "../../i18n";

type QuickActionsSectionProps = {
  // Post a quick action's body as a comment on the current issue.
  onRun: (content: string) => Promise<void> | void;
};

// Workspace-shared quick actions: macro buttons in the issue sidebar. Each
// posts a preset comment on the issue, which can kick off agent work. The set
// is shared across the whole workspace and managed via the dialog below.
export function QuickActionsSection({ onRun }: QuickActionsSectionProps) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: actions = [] } = useQuery(quickActionListOptions(wsId));
  const [open, setOpen] = useState(true);
  const [runningId, setRunningId] = useState<string | null>(null);
  const [managerOpen, setManagerOpen] = useState(false);

  async function runAction(action: QuickAction) {
    if (runningId) return;
    setRunningId(action.id);
    try {
      // onRun (submitComment) handles its own error feedback and never
      // rethrows; the posted comment lands optimistically in the timeline,
      // which is the user-visible confirmation.
      await onRun(action.body);
    } finally {
      setRunningId(null);
    }
  }

  return (
    <div>
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.detail.section_quick_actions)}
        <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
      </button>
      {open && (
        <div className="flex flex-col gap-1.5 pl-2">
          {actions.length === 0 && (
            <p className="px-1 text-xs text-muted-foreground">
              {t(($) => $.detail.quick_action_empty)}
            </p>
          )}
          {actions.map((action) => (
            <Button
              key={action.id}
              type="button"
              variant="outline"
              size="sm"
              className="h-auto w-full justify-start whitespace-normal px-2 py-1.5 text-left text-xs font-normal"
              disabled={runningId !== null}
              onClick={() => runAction(action)}
              title={action.body}
            >
              {runningId === action.id ? (
                <Loader2 className="!size-3 shrink-0 animate-spin" />
              ) : null}
              <span className="truncate">{action.label}</span>
            </Button>
          ))}
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-auto w-full justify-start gap-1.5 px-2 py-1 text-xs font-normal text-muted-foreground hover:text-foreground"
            onClick={() => setManagerOpen(true)}
          >
            <Settings2 className="!size-3 shrink-0" />
            {t(($) => $.detail.quick_action_manage)}
          </Button>
        </div>
      )}
      <QuickActionManagerDialog open={managerOpen} onOpenChange={setManagerOpen} actions={actions} />
    </div>
  );
}

type QuickActionManagerDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  actions: QuickAction[];
};

function QuickActionManagerDialog({ open, onOpenChange, actions }: QuickActionManagerDialogProps) {
  const { t } = useT("issues");
  const create = useCreateQuickAction();
  const update = useUpdateQuickAction();
  const del = useDeleteQuickAction();

  const [editingId, setEditingId] = useState<string | null>(null);
  const [editLabel, setEditLabel] = useState("");
  const [editBody, setEditBody] = useState("");
  const [newLabel, setNewLabel] = useState("");
  const [newBody, setNewBody] = useState("");

  function startEdit(action: QuickAction) {
    setEditingId(action.id);
    setEditLabel(action.label);
    setEditBody(action.body);
  }

  function saveEdit() {
    if (!editingId) return;
    const label = editLabel.trim();
    const body = editBody.trim();
    if (!label || !body) return;
    update.mutate(
      { id: editingId, label, body },
      {
        onSuccess: () => setEditingId(null),
        onError: () => toast.error(t(($) => $.detail.quick_action_save_failed)),
      },
    );
  }

  function addNew() {
    const label = newLabel.trim();
    const body = newBody.trim();
    if (!label || !body) return;
    create.mutate(
      { label, body },
      {
        onSuccess: () => {
          setNewLabel("");
          setNewBody("");
        },
        onError: () => toast.error(t(($) => $.detail.quick_action_save_failed)),
      },
    );
  }

  function remove(id: string) {
    del.mutate(id, {
      onError: () => toast.error(t(($) => $.detail.quick_action_save_failed)),
    });
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.detail.quick_action_manage_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.detail.quick_action_manage_desc)}
          </DialogDescription>
        </DialogHeader>
        <div className="flex max-h-[55vh] flex-col gap-3 overflow-auto py-1">
          {actions.length === 0 && (
            <p className="text-sm text-muted-foreground">
              {t(($) => $.detail.quick_action_empty)}
            </p>
          )}
          {actions.map((action) =>
            editingId === action.id ? (
              <div key={action.id} className="space-y-2 rounded-md border border-border p-3">
                <div className="space-y-1">
                  <Label className="text-xs text-muted-foreground">
                    {t(($) => $.detail.quick_action_label_field)}
                  </Label>
                  <Input value={editLabel} onChange={(e) => setEditLabel(e.target.value)} />
                </div>
                <div className="space-y-1">
                  <Label className="text-xs text-muted-foreground">
                    {t(($) => $.detail.quick_action_body_field)}
                  </Label>
                  <Textarea value={editBody} onChange={(e) => setEditBody(e.target.value)} rows={3} />
                </div>
                <div className="flex justify-end gap-2">
                  <Button type="button" variant="ghost" size="sm" onClick={() => setEditingId(null)}>
                    {t(($) => $.detail.quick_action_cancel)}
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    disabled={!editLabel.trim() || !editBody.trim() || update.isPending}
                    onClick={saveEdit}
                  >
                    {t(($) => $.detail.quick_action_save)}
                  </Button>
                </div>
              </div>
            ) : (
              <div key={action.id} className="flex items-start gap-2 rounded-md border border-border p-3">
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">{action.label}</p>
                  <p className="line-clamp-2 text-xs text-muted-foreground">{action.body}</p>
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  className="shrink-0 text-muted-foreground hover:text-foreground"
                  onClick={() => startEdit(action)}
                  aria-label={t(($) => $.detail.quick_action_edit)}
                >
                  <Pencil className="size-4" />
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  className="shrink-0 text-muted-foreground hover:text-destructive"
                  disabled={del.isPending}
                  onClick={() => remove(action.id)}
                  aria-label={t(($) => $.detail.quick_action_remove)}
                >
                  <Trash2 className="size-4" />
                </Button>
              </div>
            ),
          )}
          {/* New action */}
          <div className="space-y-2 rounded-md border border-dashed border-border p-3">
            <div className="space-y-1">
              <Label className="text-xs text-muted-foreground">
                {t(($) => $.detail.quick_action_label_field)}
              </Label>
              <Input
                value={newLabel}
                onChange={(e) => setNewLabel(e.target.value)}
                placeholder={t(($) => $.detail.quick_action_label_placeholder)}
              />
            </div>
            <div className="space-y-1">
              <Label className="text-xs text-muted-foreground">
                {t(($) => $.detail.quick_action_body_field)}
              </Label>
              <Textarea
                value={newBody}
                onChange={(e) => setNewBody(e.target.value)}
                placeholder={t(($) => $.detail.quick_action_body_placeholder)}
                rows={3}
              />
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="w-full gap-1.5"
              disabled={!newLabel.trim() || !newBody.trim() || create.isPending}
              onClick={addNew}
            >
              <Plus className="size-4" />
              {t(($) => $.detail.quick_action_add)}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
