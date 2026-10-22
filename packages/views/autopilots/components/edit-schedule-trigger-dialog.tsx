"use client";

import { useRef, useState } from "react";
import { useUpdateAutopilotTrigger } from "@multica/core/autopilots/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { toast } from "sonner";
import type { AutopilotTrigger } from "@multica/core/types";
import { ScheduleEditor } from "./schedule-editor/schedule-editor";
import { parseCron, toCron } from "./schedule-editor/cron-mapping";
import { useScheduleSubmitGate } from "./schedule-editor/validate";
import type { ScheduleConfig } from "./schedule-editor/model";
import { useT } from "../../i18n";

// The only place in the UI where an existing schedule can be changed. The
// autopilot dialog's panel speaks for the autopilot's one schedule; a trigger
// row speaks for itself, which is what an autopilot carrying several of them
// needs (MUL-7478). Mounted per open so the editor always hydrates from the
// row as it stands now — a stale snapshot here would write back a cron the
// user never saw.
export function EditScheduleTriggerDialog({
  open,
  onOpenChange,
  autopilotId,
  trigger,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  autopilotId: string;
  trigger: AutopilotTrigger;
}) {
  if (!open) return null;
  return (
    <EditScheduleTriggerDialogBody
      onOpenChange={onOpenChange}
      autopilotId={autopilotId}
      trigger={trigger}
    />
  );
}

function EditScheduleTriggerDialogBody({
  onOpenChange,
  autopilotId,
  trigger,
}: {
  onOpenChange: (open: boolean) => void;
  autopilotId: string;
  trigger: AutopilotTrigger;
}) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const updateTrigger = useUpdateAutopilotTrigger();
  // `parseCron` round-trips anything the server stored: an expression outside
  // the structured model comes back as an advanced config holding the raw
  // fields, which the editor renders in its expression row. So every schedule
  // row is editable here, not only the ones the pickers can describe.
  const initialCfg = parseCron(trigger.cron_expression ?? "", trigger.timezone ?? "UTC");
  const [config, setConfig] = useState<ScheduleConfig>(initialCfg);
  const [label, setLabel] = useState(trigger.label ?? "");
  const [enabled, setEnabled] = useState(trigger.enabled);
  const [submitting, setSubmitting] = useState(false);
  const scheduleGate = useScheduleSubmitGate(wsId);

  // What "changed" is measured against, snapshotted at mount — never read live
  // off `trigger`. The detail query refreshes under an open dialog (a teammate
  // saving this same row), and a prop moving under an untouched control would
  // read as this user's edit: Save would then send the value they never set,
  // back over the one that had just landed.
  //
  // The cron baseline is the editor's own rendering of the stored expression,
  // not the stored text: `parseCron` → `toCron` normalizes (a bare cron on a
  // zoned row comes back carrying its `TZ=` prefix), so comparing against the
  // stored string would call an untouched schedule edited. Server-side that
  // reads as a substantive change — republishing the rule version and moving
  // this trigger's accountability to whoever opened the dialog, which MUL-4302
  // settled must not happen on a label-only or no-op save.
  const baseline = useRef({
    cron: toCron(initialCfg),
    timezone: initialCfg.timezone,
    // Trimmed like the value submit sends, so a stored label carrying stray
    // whitespace is not already an edit the moment the dialog opens.
    label: (trigger.label ?? "").trim(),
    enabled: trigger.enabled,
  });
  const scheduleDirty =
    toCron(config) !== baseline.current.cron ||
    config.timezone !== baseline.current.timezone;
  const labelDirty = label.trim() !== baseline.current.label;
  const enabledDirty = enabled !== baseline.current.enabled;
  const dirty = scheduleDirty || labelDirty || enabledDirty;
  // The cron gate only stands between the user and a write that carries a cron.
  // A row whose stored expression the server can no longer preview is exactly
  // the one a user reaches for this dialog to switch OFF, and a rejection of an
  // expression they are not sending must not be what stops them.
  const canSubmit = !submitting && dirty && (!scheduleDirty || scheduleGate.scheduleValid);

  const handleSubmit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      let cronExpr: string | null = null;
      if (scheduleDirty) {
        if (!(await scheduleGate.ensureAccepted(config))) {
          setSubmitting(false);
          return;
        }
        cronExpr = toCron(config);
        if (!cronExpr.trim()) {
          setSubmitting(false);
          return;
        }
      }
      // Only the fields that moved. The PATCH preserves everything it is not
      // sent, so a field left out here keeps whatever the row has now — which
      // is also what makes this dialog safe to have open while someone else
      // edits the same trigger: it can only overwrite what its user touched.
      await updateTrigger.mutateAsync({
        autopilotId,
        triggerId: trigger.id,
        ...(cronExpr !== null
          ? { cron_expression: cronExpr, timezone: config.timezone || undefined }
          : {}),
        ...(labelDirty ? { label: label.trim() } : {}),
        ...(enabledDirty ? { enabled } : {}),
      });
      toast.success(t(($) => $.edit_trigger_dialog.toast_updated));
      onOpenChange(false);
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.edit_trigger_dialog.toast_update_failed),
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogTitle>{t(($) => $.edit_trigger_dialog.title)}</DialogTitle>
        {/* Same min-w-0 as the add dialog: the cron readback is one unbreakable
            line that would otherwise push the grid track past the dialog. */}
        <div className="min-w-0 space-y-4 pt-2">
          <ScheduleEditor
            value={config}
            onChange={(next) => {
              scheduleGate.clearRejection();
              setConfig(next);
            }}
            wsId={wsId}
            onValidityChange={scheduleGate.onValidityChange}
            // Same reason as the other two schedule dialogs: submit validates
            // over the network and then writes what it read going in, so an
            // edit landing inside that window would be discarded silently.
            disabled={submitting}
          />

          <div>
            <label className="text-caption font-medium text-muted-foreground">
              {t(($) => $.edit_trigger_dialog.label_field)}
            </label>
            <input
              type="text"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder={t(($) => $.edit_trigger_dialog.label_placeholder)}
              // Same lock as the editor above, for the same reason: submit reads
              // the label going in and validates over the network before
              // writing, so a label typed inside that window would be dropped —
              // silently, under the success toast for the write that shipped
              // without it.
              disabled={submitting}
              className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-body outline-none focus:ring-1 focus:ring-ring disabled:opacity-50"
            />
          </div>

          <div className="flex items-center justify-between gap-3">
            <span className="text-caption font-medium text-muted-foreground">
              {t(($) => $.edit_trigger_dialog.enabled_label)}
            </span>
            <Switch
              size="sm"
              checked={enabled}
              onCheckedChange={setEnabled}
              disabled={submitting}
              aria-label={t(($) => $.edit_trigger_dialog.enabled_label)}
            />
          </div>

          <div className="flex justify-end pt-1">
            <Button size="sm" onClick={handleSubmit} disabled={!canSubmit}>
              {submitting
                ? t(($) => $.edit_trigger_dialog.submitting)
                : t(($) => $.edit_trigger_dialog.submit)}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
