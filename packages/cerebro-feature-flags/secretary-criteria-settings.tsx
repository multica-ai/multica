// TECH-3557 follow-up — inline settings for the inbox Secretary feature, shown
// under its toggle in the Cerebro features tab (registered in FLAG_SETTINGS).
// Lets the user decide what the Secretary block is allowed to pick from.
"use client";

import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { toast } from "sonner";
import {
  useSecretaryCriteria,
  useSetSecretaryCriteria,
  type SecretaryCriteria,
} from "./secretary-criteria";

const ROWS: { key: keyof SecretaryCriteria; label: string; hint: string }[] = [
  { key: "unreadOnly", label: "Only unread", hint: "Pick only rows you haven't read yet." },
  {
    key: "includeIssues",
    label: "Issue messages",
    hint: "Mentions, comments, reminders and other issue activity.",
  },
  { key: "includeChannels", label: "Channels & DMs", hint: "Channel and direct-message rows." },
  { key: "includeChats", label: "Agent chats", hint: "1:1 agent chat threads." },
];

export function SecretaryCriteriaSettings() {
  const criteria = useSecretaryCriteria();
  const setCriteria = useSetSecretaryCriteria();

  const update = (key: keyof SecretaryCriteria, checked: boolean) => {
    void setCriteria({ [key]: checked }).catch((err) => {
      toast.error(err instanceof Error ? err.message : "Failed to update Secretary criteria");
    });
  };

  return (
    <div className="space-y-3 pt-1">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <Label className="text-sm font-medium">Start collapsed</Label>
          <p className="text-xs text-muted-foreground">
            Show only a small Secretary bar; unfold it to start a batch.
          </p>
        </div>
        <Switch
          checked={criteria.startFolded}
          onCheckedChange={(checked) => update("startFolded", checked)}
        />
      </div>
      <p className="border-t border-border/60 pt-3 text-xs text-muted-foreground">
        Choose what the Secretary block may pick from. Muted rows are always skipped.
      </p>
      {ROWS.map((row) => (
        <div key={row.key} className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <Label className="text-sm font-medium">{row.label}</Label>
            <p className="text-xs text-muted-foreground">{row.hint}</p>
          </div>
          <Switch
            checked={criteria[row.key]}
            onCheckedChange={(checked) => update(row.key, checked)}
          />
        </div>
      ))}
    </div>
  );
}
