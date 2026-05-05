"use client";

import { useState } from "react";
import { Bell, User, Inbox, Eye, UserPlus } from "lucide-react";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import {
  DEFAULT_ROUTES,
  getRouteChoice,
  type RoutingKey,
  type RouteChoice,
  getAutoSubscribe,
  type AutoSubscribeReason,
} from "@multica/core/notifications";
import { PushNotificationsSection } from "./push-notifications-section";

interface RowSpec {
  routingKey: RoutingKey;
  label: string;
  hint: string;
}

interface AutoSubscribeRowSpec {
  reason: AutoSubscribeReason;
  label: string;
  hint: string;
}

const autoSubscribeRows: AutoSubscribeRowSpec[] = [
  {
    reason: "creator",
    label: "Issues you create",
    hint: "Stay in the loop on issues you opened.",
  },
  {
    reason: "assignee",
    label: "Issues assigned to you",
    hint: "Get updates on issues that are your responsibility.",
  },
  {
    reason: "mentioned",
    label: "Issues where you're @-mentioned",
    hint: "Off by default — opt in if every mention should subscribe you.",
  },
  {
    reason: "commenter",
    label: "Issues you comment on",
    hint: "Off by default — opt in to follow every issue you reply to.",
  },
];

const personalRows: RowSpec[] = [
  {
    routingKey: "issue_assigned",
    label: "Issue assigned to you",
    hint: "You were set as the assignee on an issue.",
  },
  {
    routingKey: "mentioned",
    label: "@-mention",
    hint: "Someone @-tagged you in a comment or description.",
  },
  {
    routingKey: "task_failed",
    label: "Agent task failed",
    hint: "An agent task you started failed.",
  },
  {
    routingKey: "unassigned",
    label: "Removed as assignee",
    hint: "You're no longer the assignee on an issue.",
  },
  {
    routingKey: "reaction_added",
    label: "Reaction on your content",
    hint: "Someone reacted to a comment or issue you own.",
  },
];

const mineRows: RowSpec[] = [
  {
    routingKey: "due_date_changed.assignee",
    label: "Due date changed",
    hint: "A deadline on an issue you're assigned to moved.",
  },
  {
    routingKey: "priority_changed.assignee",
    label: "Priority changed",
    hint: "An issue you're assigned to was re-prioritized.",
  },
];

const followingRows: RowSpec[] = [
  {
    routingKey: "new_comment",
    label: "New comment",
    hint: "Bumped to Inbox automatically if you're @-tagged.",
  },
  {
    routingKey: "assignee_changed",
    label: "Assignee changed",
    hint: "Someone else gained or lost ownership.",
  },
  {
    routingKey: "status_changed",
    label: "Status changed",
    hint: "Issue moved to a new status.",
  },
  {
    routingKey: "priority_changed.follower",
    label: "Priority changed",
    hint: "An issue you follow was re-prioritized.",
  },
  {
    routingKey: "due_date_changed.follower",
    label: "Due date changed",
    hint: "Deadline moved on an issue you follow.",
  },
];

const ROUTE_OPTIONS: { value: RouteChoice; label: string }[] = [
  { value: "inbox", label: "Inbox" },
  { value: "notifications", label: "Notifications" },
  { value: "off", label: "Off" },
];

export function NotificationsTab() {
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);
  const [savingKey, setSavingKey] = useState<RoutingKey | null>(null);
  const [savingAutoSub, setSavingAutoSub] = useState<AutoSubscribeReason | null>(null);

  const handleChange = async (key: RoutingKey, next: RouteChoice) => {
    if (!user) return;
    setSavingKey(key);
    try {
      const existingNotifBlock =
        (user.preferences?.notifications as Record<string, RouteChoice>) ?? {};
      const updated = await api.updateMyPreferences({
        notifications: { ...existingNotifBlock, [key]: next },
      });
      setUser(updated);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : "Failed to update preference",
      );
    } finally {
      setSavingKey(null);
    }
  };

  const handleAutoSubscribeChange = async (
    reason: AutoSubscribeReason,
    next: boolean,
  ) => {
    if (!user) return;
    setSavingAutoSub(reason);
    try {
      const existing =
        (user.preferences?.auto_subscribe as Record<string, boolean>) ?? {};
      const updated = await api.updateMyPreferences({
        auto_subscribe: { ...existing, [reason]: next },
      });
      setUser(updated);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : "Failed to update preference",
      );
    } finally {
      setSavingAutoSub(null);
    }
  };

  return (
    <div className="space-y-8">
      <section className="space-y-3">
        <div>
          <h2 className="text-sm font-semibold flex items-center gap-2">
            <Bell className="h-4 w-4 text-muted-foreground" />
            Notifications
          </h2>
          <p className="text-xs text-muted-foreground mt-1 max-w-xl">
            Choose where each kind of notification lands.{" "}
            <strong>Inbox</strong> keeps it in your persistent queue.{" "}
            <strong>Notifications</strong> shows it in the notifications page in
            the bottom of the sidebar. <strong>Off</strong> skips it entirely.
          </p>
        </div>
      </section>

      <Section
        icon={<User className="h-4 w-4 text-muted-foreground" />}
        title="Personal"
        subtitle="Sent directly to you"
        rows={personalRows}
        user={user}
        savingKey={savingKey}
        onChange={handleChange}
      />

      <Section
        icon={<Inbox className="h-4 w-4 text-muted-foreground" />}
        title="On issues you're assigned to"
        subtitle="Affects your work"
        rows={mineRows}
        user={user}
        savingKey={savingKey}
        onChange={handleChange}
      />

      <Section
        icon={<Eye className="h-4 w-4 text-muted-foreground" />}
        title="On issues you follow"
        subtitle="You're a subscriber but not the assignee"
        rows={followingRows}
        user={user}
        savingKey={savingKey}
        onChange={handleChange}
      />

      <AutoSubscribeSection
        rows={autoSubscribeRows}
        user={user}
        savingReason={savingAutoSub}
        onChange={handleAutoSubscribeChange}
      />

      <PushNotificationsSection />
    </div>
  );
}

interface AutoSubscribeSectionProps {
  rows: AutoSubscribeRowSpec[];
  user: ReturnType<typeof useAuthStore.getState>["user"];
  savingReason: AutoSubscribeReason | null;
  onChange: (reason: AutoSubscribeReason, next: boolean) => void | Promise<void>;
}

function AutoSubscribeSection({
  rows,
  user,
  savingReason,
  onChange,
}: AutoSubscribeSectionProps) {
  return (
    <section className="space-y-3">
      <div className="flex items-center gap-2">
        <UserPlus className="h-4 w-4 text-muted-foreground" />
        <h3 className="text-sm font-semibold">Auto-subscribe</h3>
        <span className="text-xs text-muted-foreground">
          — Decide when you become a follower of an issue
        </span>
      </div>
      <Card>
        <CardContent className="p-0">
          {rows.map((row, idx) => {
            const value = getAutoSubscribe(
              user?.preferences as Record<string, unknown> | undefined,
              row.reason,
            );
            return (
              <div
                key={row.reason}
                className={cn(
                  "flex items-start justify-between gap-4 px-4 py-3",
                  idx > 0 && "border-t",
                )}
              >
                <div>
                  <Label className="text-sm font-medium">{row.label}</Label>
                  <p className="text-xs text-muted-foreground mt-0.5">
                    {row.hint}
                  </p>
                </div>
                <Switch
                  checked={value}
                  disabled={savingReason === row.reason || !user}
                  onCheckedChange={(next) => void onChange(row.reason, next)}
                />
              </div>
            );
          })}
        </CardContent>
      </Card>
    </section>
  );
}

interface SectionProps {
  icon: React.ReactNode;
  title: string;
  subtitle: string;
  rows: RowSpec[];
  user: ReturnType<typeof useAuthStore.getState>["user"];
  savingKey: RoutingKey | null;
  onChange: (key: RoutingKey, next: RouteChoice) => void | Promise<void>;
}

function Section({
  icon,
  title,
  subtitle,
  rows,
  user,
  savingKey,
  onChange,
}: SectionProps) {
  return (
    <section className="space-y-3">
      <div className="flex items-center gap-2">
        {icon}
        <h3 className="text-sm font-semibold">{title}</h3>
        <span className="text-xs text-muted-foreground">— {subtitle}</span>
      </div>
      <Card>
        <CardContent className="p-0">
          {rows.map((row, idx) => {
            const value =
              user?.preferences != null
                ? getRouteChoice(
                    user.preferences as Record<string, unknown>,
                    row.routingKey,
                  )
                : DEFAULT_ROUTES[row.routingKey];
            return (
              <div
                key={row.routingKey}
                className={cn(
                  "flex items-start justify-between gap-4 px-4 py-3",
                  idx > 0 && "border-t",
                )}
              >
                <div>
                  <Label className="text-sm font-medium">{row.label}</Label>
                  <p className="text-xs text-muted-foreground mt-0.5">
                    {row.hint}
                  </p>
                </div>
                <Segmented
                  value={value}
                  disabled={savingKey === row.routingKey || !user}
                  onChange={(next) => void onChange(row.routingKey, next)}
                />
              </div>
            );
          })}
        </CardContent>
      </Card>
    </section>
  );
}

interface SegmentedProps {
  value: RouteChoice;
  disabled: boolean;
  onChange: (next: RouteChoice) => void;
}

function Segmented({ value, disabled, onChange }: SegmentedProps) {
  return (
    <div className="inline-flex shrink-0 rounded-md border bg-muted p-0.5">
      {ROUTE_OPTIONS.map((opt) => (
        <button
          key={opt.value}
          type="button"
          disabled={disabled}
          onClick={() => onChange(opt.value)}
          className={cn(
            "px-3 py-1 text-xs font-medium rounded transition-colors",
            value === opt.value
              ? "bg-background text-foreground shadow-sm"
              : "text-muted-foreground hover:text-foreground",
            disabled && "opacity-50 cursor-not-allowed",
          )}
        >
          {opt.label}
        </button>
      ))}
    </div>
  );
}
