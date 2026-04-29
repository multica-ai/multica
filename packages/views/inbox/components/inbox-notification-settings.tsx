"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { notificationPreferencesOptions } from "@multica/core/inbox/queries";
import { useUpdateNotificationPreferences } from "@multica/core/inbox/mutations";
import { toast } from "sonner";
import {
  MessageSquare,
  UserCheck,
  AtSign,
  RefreshCw,
  ArrowUpDown,
  CalendarDays,
  Heart,
  AlertTriangle,
  Sparkles,
} from "lucide-react";
import type { InboxItemType, NotificationPreference } from "@multica/core/types";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import type { ComponentType } from "react";

interface NotificationCategory {
  label: string;
  description: string;
  types: InboxItemType[];
  icon: ComponentType<{ className?: string }>;
}

const categories: NotificationCategory[] = [
  {
    label: "Status changes",
    description: "Issue status transitions (e.g. Todo → In Progress)",
    types: ["status_changed"],
    icon: RefreshCw,
  },
  {
    label: "Comments",
    description: "New comments on subscribed issues",
    types: ["new_comment"],
    icon: MessageSquare,
  },
  {
    label: "Assignments",
    description: "Issue assignment and unassignment",
    types: ["issue_assigned", "unassigned", "assignee_changed"],
    icon: UserCheck,
  },
  {
    label: "Mentions",
    description: "When you are @mentioned",
    types: ["mentioned"],
    icon: AtSign,
  },
  {
    label: "Priority changes",
    description: "Issue priority updates",
    types: ["priority_changed"],
    icon: ArrowUpDown,
  },
  {
    label: "Due date changes",
    description: "Issue due date updates",
    types: ["due_date_changed"],
    icon: CalendarDays,
  },
  {
    label: "Reactions",
    description: "Reactions to your issues or comments",
    types: ["reaction_added"],
    icon: Heart,
  },
  {
    label: "Agent events",
    description: "Agent task failures and blocks",
    types: ["task_failed", "agent_blocked", "task_completed", "agent_completed"],
    icon: AlertTriangle,
  },
  {
    label: "Quick create",
    description: "Quick create success and failure results",
    types: ["quick_create_done", "quick_create_failed"],
    icon: Sparkles,
  },
];

function isCategoryEnabled(
  prefs: NotificationPreference[],
  types: InboxItemType[],
): boolean {
  const prefMap = new Map(prefs.map((p) => [p.notification_type, p.enabled]));
  return types.every((t) => prefMap.get(t) !== false);
}

export function InboxNotificationSettings({
  children,
}: {
  children: React.ReactNode;
}) {
  const wsId = useWorkspaceId();
  const { data: prefs = [] } = useQuery(notificationPreferencesOptions(wsId));
  const updateMutation = useUpdateNotificationPreferences();

  const handleToggle = (category: NotificationCategory, enabled: boolean) => {
    const updates: NotificationPreference[] = category.types.map((t) => ({
      notification_type: t,
      enabled,
    }));
    updateMutation.mutate(updates, {
      onError: () => toast.error("Failed to update notification settings"),
    });
  };

  return (
    <Dialog>
      <DialogTrigger render={<>{children}</>} />
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Notification settings</DialogTitle>
          <DialogDescription>
            Choose which notification types appear in your Inbox.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-1 -mx-4 max-h-[60vh] overflow-y-auto px-4">
          {categories.map((category) => {
            const enabled = isCategoryEnabled(prefs, category.types);
            return (
              <label
                key={category.label}
                className="flex items-center justify-between gap-3 rounded-md px-2 py-2.5 hover:bg-muted/50 cursor-pointer"
              >
                <div className="flex items-start gap-3">
                  <category.icon className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
                  <div className="space-y-0.5">
                    <div className="text-sm font-medium leading-none">
                      {category.label}
                    </div>
                    <div className="text-xs text-muted-foreground leading-snug">
                      {category.description}
                    </div>
                  </div>
                </div>
                <Switch
                  size="sm"
                  checked={enabled}
                  onCheckedChange={(checked) =>
                    handleToggle(category, checked)
                  }
                />
              </label>
            );
          })}
        </div>
      </DialogContent>
    </Dialog>
  );
}
