// FIR-394 (Jesper) — inline settings for the unified Reminders feature, shown
// under the "Reminders" toggle in the Cerebro features tab (registered in
// FLAG_SETTINGS). Keeps the standalone-page and hide-from-"All messages" options
// inside the Reminders box instead of as separate top-level feature flags.
"use client";

import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { toast } from "sonner";
import type { CerebroFlagKey } from "./registry";
import { useFeatureFlag, useSetFeatureFlagMutation } from "./api";
import { useFlagLocked } from "./store";

const ROWS: { key: CerebroFlagKey; label: string; hint: string }[] = [
  {
    key: "cerebro_reminders_page",
    label: "Reminders page",
    hint: 'Show the standalone "Reminders" entry in the sidebar and the /reminders overview that lists every reminder on its own. Off hides the page; the "Remind me" actions keep working regardless.',
  },
  {
    key: "cerebro_inbox_hide_reminders",
    label: "Hide reminders from All messages",
    hint: 'Keep fired reminders out of the "All messages" box so they only live in their own Reminders box, never in two places. Add a Reminders box from the "Add section" menu to see them on their own.',
  },
];

function ReminderToggleRow({ flagKey, label, hint }: { flagKey: CerebroFlagKey; label: string; hint: string }) {
  const enabled = useFeatureFlag(flagKey);
  const locked = useFlagLocked(flagKey);
  const mutation = useSetFeatureFlagMutation();

  return (
    <div className="flex items-start justify-between gap-3">
      <div className="min-w-0">
        <Label className="text-sm font-medium">{label}</Label>
        <p className="text-xs text-muted-foreground">{hint}</p>
      </div>
      <Switch
        checked={enabled}
        disabled={mutation.isPending || locked}
        aria-label={label}
        onCheckedChange={(next) =>
          mutation.mutate(
            { key: flagKey, enabled: next },
            {
              onError: (err) =>
                toast.error(err instanceof Error ? err.message : `Failed to update ${label}`),
            },
          )
        }
      />
    </div>
  );
}

export function RemindersSettings() {
  return (
    <div className="space-y-3 pt-1">
      <p className="text-xs text-muted-foreground">How reminders show up.</p>
      {ROWS.map((row) => (
        <ReminderToggleRow key={row.key} flagKey={row.key} label={row.label} hint={row.hint} />
      ))}
    </div>
  );
}
