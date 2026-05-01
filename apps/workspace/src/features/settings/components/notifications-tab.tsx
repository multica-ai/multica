"use client";

import { useEffect, useState } from "react";
import { Bell, BellOff, CheckCircle2, Loader2, Send } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { toast } from "sonner";
import type { InboxItemType } from "@/shared/types";
import { api } from "@/shared/api";
import { useNotificationPreferencesQuery } from "@/features/settings/queries";
import { useNotificationPreferenceMutations } from "@/features/settings/mutations";

type TypeGroup = { label: string; types: { key: InboxItemType; label: string }[] };

const TYPE_GROUPS: TypeGroup[] = [
  {
    label: "Assignments",
    types: [
      { key: "issue_assigned", label: "Issue assigned to you" },
      { key: "unassigned", label: "Issue unassigned from you" },
      { key: "assignee_changed", label: "Assignee changed" },
    ],
  },
  {
    label: "Status & Priority",
    types: [
      { key: "status_changed", label: "Status changed" },
      { key: "priority_changed", label: "Priority changed" },
      { key: "review_requested", label: "Review requested" },
    ],
  },
  {
    label: "Dates",
    types: [
      { key: "due_date_changed", label: "Due date changed" },
      { key: "start_date_changed", label: "Start date changed" },
      { key: "end_date_changed", label: "End date changed" },
    ],
  },
  {
    label: "Comments & Reactions",
    types: [
      { key: "new_comment", label: "New comment" },
      { key: "mentioned", label: "Mentioned" },
      { key: "reaction_added", label: "Reaction added" },
    ],
  },
  {
    label: "Agent Tasks",
    types: [
      { key: "task_completed", label: "Task completed" },
      { key: "task_failed", label: "Task failed" },
      { key: "agent_blocked", label: "Agent blocked" },
      { key: "agent_completed", label: "Agent completed" },
    ],
  },
];

export function NotificationsTab() {
  const query = useNotificationPreferencesQuery();
  const { updatePreferences, updating } = useNotificationPreferenceMutations();

  const [ntfyUrl, setNtfyUrl] = useState("");
  const [ntfyToken, setNtfyToken] = useState("");
  const [disabledTypes, setDisabledTypes] = useState<Set<InboxItemType>>(new Set());
  const [testing, setTesting] = useState(false);

  useEffect(() => {
    if (query.data) {
      setNtfyUrl(query.data.ntfy_url ?? "");
      setNtfyToken(query.data.ntfy_token ?? "");
      setDisabledTypes(new Set(query.data.disabled_types ?? []));
    }
  }, [query.data]);

  const toggleType = (type: InboxItemType) => {
    setDisabledTypes((prev) => {
      const next = new Set(prev);
      if (next.has(type)) {
        next.delete(type);
      } else {
        next.add(type);
      }
      return next;
    });
  };

  const handleSave = async () => {
    try {
      await updatePreferences({
        ntfy_url: ntfyUrl,
        ntfy_token: ntfyToken,
        disabled_types: Array.from(disabledTypes),
      });
      toast.success("Notification preferences saved");
    } catch {
      toast.error("Failed to save preferences");
    }
  };

  const handleTest = async () => {
    if (!ntfyUrl) {
      toast.error("Enter an ntfy URL first");
      return;
    }
    setTesting(true);
    try {
      await api.testNotificationPreference({ ntfy_url: ntfyUrl, ntfy_token: ntfyToken });
      toast.success("Test notification sent");
    } catch {
      toast.error("Failed to send test notification");
    } finally {
      setTesting(false);
    }
  };

  const isActive = !!ntfyUrl;

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-base font-semibold">Push Notifications</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          Receive push notifications via{" "}
          <a
            href="https://ntfy.sh"
            target="_blank"
            rel="noopener noreferrer"
            className="underline underline-offset-4"
          >
            ntfy
          </a>
          . Install the ntfy app and subscribe to a topic URL to get started.
        </p>
      </div>

      {isActive ? (
        <div className="flex items-center gap-2 rounded-md border border-border bg-accent/50 px-3 py-2 text-sm">
          <CheckCircle2 className="h-4 w-4 shrink-0 text-green-500" />
          <span>ntfy push notifications active</span>
        </div>
      ) : (
        <div className="flex items-center gap-2 rounded-md border border-border bg-muted/40 px-3 py-2 text-sm text-muted-foreground">
          <BellOff className="h-4 w-4 shrink-0" />
          <span>Configure an ntfy URL below to enable push notifications</span>
        </div>
      )}

      <div className="space-y-4">
        <div className="space-y-1.5">
          <Label htmlFor="ntfy-url">ntfy Topic URL</Label>
          <div className="flex gap-2">
            <Input
              id="ntfy-url"
              placeholder="https://ntfy.sh/your-topic"
              value={ntfyUrl}
              onChange={(e) => setNtfyUrl(e.target.value)}
              className="flex-1"
            />
            <Button
              variant="outline"
              size="sm"
              onClick={handleTest}
              disabled={testing || !ntfyUrl}
            >
              {testing ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Send className="h-4 w-4" />
              )}
              <span className="ml-1.5">Test</span>
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">
            Your ntfy topic URL, e.g.{" "}
            <code className="rounded bg-muted px-1 py-0.5 text-xs">
              https://ntfy.sh/my-unique-topic
            </code>
          </p>
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="ntfy-token">Access Token (optional)</Label>
          <Input
            id="ntfy-token"
            type="password"
            placeholder="tk_..."
            value={ntfyToken}
            onChange={(e) => setNtfyToken(e.target.value)}
          />
          <p className="text-xs text-muted-foreground">
            Required only for protected ntfy topics.
          </p>
        </div>
      </div>

      <div className="space-y-4">
        <div>
          <h3 className="text-sm font-medium">Notification Types</h3>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Choose which events trigger a push notification.
          </p>
        </div>

        {TYPE_GROUPS.map((group) => (
          <div key={group.label} className="space-y-2">
            <p className="text-xs font-medium text-muted-foreground">{group.label}</p>
            <div className="space-y-2 rounded-md border border-border p-3">
              {group.types.map(({ key, label }) => (
                <div key={key} className="flex items-center justify-between">
                  <Label htmlFor={`toggle-${key}`} className="text-sm font-normal cursor-pointer">
                    {label}
                  </Label>
                  <Switch
                    id={`toggle-${key}`}
                    checked={!disabledTypes.has(key)}
                    onCheckedChange={() => toggleType(key)}
                  />
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>

      <div className="flex justify-end">
        <Button onClick={handleSave} disabled={updating}>
          {updating && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          Save preferences
        </Button>
      </div>
    </div>
  );
}
