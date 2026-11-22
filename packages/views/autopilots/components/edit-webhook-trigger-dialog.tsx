"use client";

import { useRef, useState } from "react";
import { useUpdateAutopilotTrigger } from "@multica/core/autopilots/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { toast } from "sonner";
import type { AutopilotTrigger } from "@multica/core/types";
import { WebhookEventFilterSection } from "./webhook-event-filter-section";
import { serializeEventFilters } from "./webhook-payload-condition";
import {
  draftsFromEventFilters,
  resolveEventFilterDrafts,
  type EventFilterDraft,
} from "./webhook-event-filter-draft";
import { useT } from "../../i18n";

// The webhook counterpart of EditScheduleTriggerDialog: every webhook row opens
// its own editor for event filters, label and enabled. The autopilot dialog's
// webhook panel only reaches an autopilot whose first trigger is its sole
// webhook, so a webhook added beside a schedule, or a second webhook, had no
// editor at all. URL copy / rotate stays on the row. Mounted per open so the
// editor hydrates from the row as it stands now.
export function EditWebhookTriggerDialog({
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
    <EditWebhookTriggerDialogBody
      onOpenChange={onOpenChange}
      autopilotId={autopilotId}
      trigger={trigger}
    />
  );
}

function EditWebhookTriggerDialogBody({
  onOpenChange,
  autopilotId,
  trigger,
}: {
  onOpenChange: (open: boolean) => void;
  autopilotId: string;
  trigger: AutopilotTrigger;
}) {
  const { t } = useT("autopilots");
  const updateTrigger = useUpdateAutopilotTrigger();
  const [drafts, setDrafts] = useState<EventFilterDraft[]>(() =>
    draftsFromEventFilters(trigger.event_filters ?? []),
  );
  const [showErrors, setShowErrors] = useState(false);
  const [label, setLabel] = useState(trigger.label ?? "");
  const [enabled, setEnabled] = useState(trigger.enabled);
  const [submitting, setSubmitting] = useState(false);

  // Snapshotted at mount, never read live off `trigger`, for the same reason
  // as the schedule dialog: a teammate's save refreshing the detail query under
  // an open dialog must not read as this user's edit and be written back.
  const baseline = useRef({
    filters: serializeEventFilters(trigger.event_filters ?? []),
    label: (trigger.label ?? "").trim(),
    enabled: trigger.enabled,
  });
  const resolved = resolveEventFilterDrafts(drafts);
  // An incomplete row counts as a change: Save stays available so pressing it
  // can point at the row, rather than going quiet over an edit it won't send.
  const filtersDirty =
    !resolved.valid || serializeEventFilters(resolved.filters) !== baseline.current.filters;
  const labelDirty = label.trim() !== baseline.current.label;
  const enabledDirty = enabled !== baseline.current.enabled;
  const canSubmit = !submitting && (filtersDirty || labelDirty || enabledDirty);

  const handleSubmit = async () => {
    if (!canSubmit) return;
    if (!resolved.valid) {
      setShowErrors(true);
      return;
    }
    setSubmitting(true);
    try {
      // Only the fields that moved. A changed filter list or enabled flag is a
      // substantive edit server-side (rule version + accountability move to the
      // saver, MUL-4302), so a rename must not carry them along. The list is
      // sent whole: an empty array clears the filters and accepts every event.
      await updateTrigger.mutateAsync({
        autopilotId,
        triggerId: trigger.id,
        ...(filtersDirty ? { event_filters: resolved.filters } : {}),
        ...(labelDirty ? { label: label.trim() } : {}),
        ...(enabledDirty ? { enabled } : {}),
      });
      toast.success(t(($) => $.edit_webhook_dialog.toast_updated));
      onOpenChange(false);
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.edit_webhook_dialog.toast_update_failed),
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open onOpenChange={onOpenChange}>
      {/* Wider than the schedule dialog: rules and conditions are edited in
          place, and a condition's operator, value and ignore-case share a line. */}
      <DialogContent className="sm:max-w-lg">
        <DialogTitle>{t(($) => $.edit_webhook_dialog.title)}</DialogTitle>
        <div className="min-w-0 space-y-4 pt-2">
          <WebhookEventFilterSection drafts={drafts} onChange={setDrafts} showErrors={showErrors} />

          <div>
            <label className="text-caption font-medium text-muted-foreground">
              {t(($) => $.edit_trigger_dialog.label_field)}
            </label>
            <input
              type="text"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder={t(($) => $.edit_webhook_dialog.label_placeholder)}
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
