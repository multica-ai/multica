"use client";

import { useState } from "react";
import {
  Bell,
  Inbox,
  Mail,
  Monitor,
  Smartphone,
  UserPlus,
} from "lucide-react";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import {
  type Channel,
  type ChannelChoice,
  type ChannelTransport,
  getAutoSubscribe,
  getChannelChoice,
  getChannelTransport,
  type RoutingKey,
  type AutoSubscribeReason,
} from "@multica/core/notifications";
import { PushNotificationsSection } from "./push-notifications-section";

// Notification rows grouped the way the channel-first mockup shows them.
// Each row has a stable `prefId` so the inbox "turn this off" shortcut
// (P6 / JEH-617) can scroll-and-highlight the toggle for a given (channel, key).

interface RowSpec {
  routingKey: RoutingKey;
  label: string;
  hint: string;
}

interface RowGroup {
  title: string;
  rows: RowSpec[];
}

const ROW_GROUPS: RowGroup[] = [
  {
    title: "Personal — sent directly to you",
    rows: [
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
    ],
  },
  {
    title: "On issues you're assigned to",
    rows: [
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
    ],
  },
  {
    title: "On issues you follow",
    rows: [
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
    ],
  },
];

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

interface ChannelMeta {
  channel: Channel;
  title: string;
  subtitle: string;
  icon: React.ReactNode;
  comingSoon?: boolean;
  // Channel-level "Visning" toggles (badge, sound, banner). Only the fields
  // listed here render in the channel header.
  transportFields?: Array<keyof ChannelTransport>;
}

const CHANNEL_META: ChannelMeta[] = [
  {
    channel: "inbox",
    title: "Inbox",
    subtitle: "Your persistent queue — you actively clear it",
    icon: <Inbox className="h-4 w-4 text-muted-foreground" />,
  },
  {
    channel: "notifications",
    title: "Notifications",
    subtitle:
      "List anchored in the bottom of the sidebar — transient, not a queue",
    icon: <Bell className="h-4 w-4 text-muted-foreground" />,
  },
  {
    channel: "mobile",
    title: "Mobile",
    subtitle: "Web push to your phone or installed PWA",
    icon: <Smartphone className="h-4 w-4 text-muted-foreground" />,
    transportFields: ["badge", "sound"],
  },
  {
    channel: "desktop",
    title: "Desktop",
    subtitle: "In-app banner inside the Multica desktop app",
    icon: <Monitor className="h-4 w-4 text-muted-foreground" />,
    transportFields: ["badge", "banner", "sound"],
  },
  {
    channel: "mail",
    title: "Mail",
    subtitle: "Email digest — channel not enabled yet",
    icon: <Mail className="h-4 w-4 text-muted-foreground" />,
    comingSoon: true,
  },
];

const TRANSPORT_LABELS: Record<keyof ChannelTransport, { label: string; hint: string }> = {
  badge: {
    label: "Show unread badge",
    hint: "The red number on the Multica app icon.",
  },
  sound: {
    label: "Play sound",
    hint: "System default tone when a notification arrives.",
  },
  banner: {
    label: "Show banner",
    hint: "A small toast inside the Multica window when a notification arrives.",
  },
  digest: {
    label: "Digest",
    hint: "How often to bundle email notifications.",
  },
};

// Stable HTML id for the per-(channel, key) toggle row. Used by the inbox
// "turn this off" shortcut (P6) to scroll into view + highlight the right
// switch.
function prefId(channel: Channel, key: RoutingKey): string {
  return `notif-pref-${channel}-${key}`;
}

export function NotificationsTab() {
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);
  const [savingChannelKey, setSavingChannelKey] = useState<string | null>(null);
  const [savingTransport, setSavingTransport] = useState<string | null>(null);
  const [savingAutoSub, setSavingAutoSub] =
    useState<AutoSubscribeReason | null>(null);

  const handleChannelToggle = async (
    channel: Channel,
    key: RoutingKey,
    next: ChannelChoice,
  ) => {
    if (!user) return;
    const slot = `${channel}:${key}`;
    setSavingChannelKey(slot);
    try {
      const notifBlock =
        ((user.preferences?.notifications as Record<string, unknown>) ?? {});
      const channelBlock =
        ((notifBlock[channel] as Record<string, unknown>) ?? {});
      const updated = await api.updateMyPreferences({
        notifications: {
          ...notifBlock,
          [channel]: { ...channelBlock, [key]: next },
        },
      });
      setUser(updated);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : "Failed to update preference",
      );
    } finally {
      setSavingChannelKey(null);
    }
  };

  const handleTransportToggle = async (
    channel: Channel,
    field: keyof ChannelTransport,
    next: boolean,
  ) => {
    if (!user) return;
    const slot = `${channel}:${field}`;
    setSavingTransport(slot);
    try {
      const notifBlock =
        ((user.preferences?.notifications as Record<string, unknown>) ?? {});
      const channelsBlock =
        ((notifBlock.channels as Record<string, unknown>) ?? {});
      const channelOverride =
        ((channelsBlock[channel] as Record<string, unknown>) ?? {});
      const updated = await api.updateMyPreferences({
        notifications: {
          ...notifBlock,
          channels: {
            ...channelsBlock,
            [channel]: { ...channelOverride, [field]: next },
          },
        },
      });
      setUser(updated);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : "Failed to update preference",
      );
    } finally {
      setSavingTransport(null);
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
            Pick the channels each kind of notification lands on. The same
            event can ring multiple channels — you might want assignment in
            both your Inbox and as a phone push, but a status change just in
            Notifications.
          </p>
        </div>
      </section>

      <AutoSubscribeSection
        rows={autoSubscribeRows}
        user={user}
        savingReason={savingAutoSub}
        onChange={handleAutoSubscribeChange}
      />

      {CHANNEL_META.map((meta) => (
        <ChannelSection
          key={meta.channel}
          meta={meta}
          user={user}
          savingChannelKey={savingChannelKey}
          savingTransport={savingTransport}
          onToggle={handleChannelToggle}
          onTransportToggle={handleTransportToggle}
        />
      ))}

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

interface ChannelSectionProps {
  meta: ChannelMeta;
  user: ReturnType<typeof useAuthStore.getState>["user"];
  savingChannelKey: string | null;
  savingTransport: string | null;
  onToggle: (
    channel: Channel,
    key: RoutingKey,
    next: ChannelChoice,
  ) => void | Promise<void>;
  onTransportToggle: (
    channel: Channel,
    field: keyof ChannelTransport,
    next: boolean,
  ) => void | Promise<void>;
}

function ChannelSection({
  meta,
  user,
  savingChannelKey,
  savingTransport,
  onToggle,
  onTransportToggle,
}: ChannelSectionProps) {
  const transport = getChannelTransport(
    user?.preferences as Record<string, unknown> | undefined,
    meta.channel,
  );
  const disabled = !user || meta.comingSoon;

  return (
    <section className="space-y-3">
      <div className="flex items-center gap-2">
        {meta.icon}
        <h3 className="text-sm font-semibold">
          {meta.title}
          {meta.comingSoon && (
            <span className="ml-2 text-[10px] uppercase tracking-wide font-semibold rounded bg-muted text-muted-foreground px-1.5 py-0.5 align-middle">
              Soon
            </span>
          )}
        </h3>
        <span className="text-xs text-muted-foreground">— {meta.subtitle}</span>
      </div>

      {meta.transportFields && meta.transportFields.length > 0 && (
        <Card>
          <CardContent className="p-0">
            {meta.transportFields.map((field, idx) => {
              const checked = Boolean(transport[field]);
              const slot = `${meta.channel}:${field}`;
              const meta2 = TRANSPORT_LABELS[field];
              return (
                <div
                  key={field}
                  className={cn(
                    "flex items-start justify-between gap-4 px-4 py-3",
                    idx > 0 && "border-t",
                  )}
                >
                  <div>
                    <Label className="text-sm font-medium">{meta2.label}</Label>
                    <p className="text-xs text-muted-foreground mt-0.5">
                      {meta2.hint}
                    </p>
                  </div>
                  <Switch
                    checked={checked}
                    disabled={disabled || savingTransport === slot}
                    onCheckedChange={(next) =>
                      void onTransportToggle(meta.channel, field, next)
                    }
                  />
                </div>
              );
            })}
          </CardContent>
        </Card>
      )}

      <Card className={cn(meta.comingSoon && "opacity-60")}>
        <CardContent className="p-0">
          {ROW_GROUPS.map((group, groupIdx) => (
            <div key={group.title}>
              {(groupIdx > 0 || (meta.transportFields?.length ?? 0) > 0) && null}
              <div className="px-4 py-2 text-[10px] uppercase tracking-wide font-semibold text-muted-foreground bg-muted/40 border-b">
                {group.title}
              </div>
              {group.rows.map((row, rowIdx) => {
                const id = prefId(meta.channel, row.routingKey);
                const slot = `${meta.channel}:${row.routingKey}`;
                const value = getChannelChoice(
                  user?.preferences as Record<string, unknown> | undefined,
                  meta.channel,
                  row.routingKey,
                );
                return (
                  <div
                    key={row.routingKey}
                    id={id}
                    className={cn(
                      "flex items-start justify-between gap-4 px-4 py-3 scroll-mt-24",
                      rowIdx > 0 && "border-t",
                    )}
                  >
                    <div>
                      <Label className="text-sm font-medium">{row.label}</Label>
                      <p className="text-xs text-muted-foreground mt-0.5">
                        {row.hint}
                      </p>
                    </div>
                    <Switch
                      checked={value === "on"}
                      disabled={disabled || savingChannelKey === slot}
                      onCheckedChange={(next) =>
                        void onToggle(
                          meta.channel,
                          row.routingKey,
                          next ? "on" : "off",
                        )
                      }
                    />
                  </div>
                );
              })}
            </div>
          ))}
        </CardContent>
      </Card>
    </section>
  );
}
