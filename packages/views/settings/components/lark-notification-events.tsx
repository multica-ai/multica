"use client";

import { useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  DEFAULT_LARK_NOTIFICATION_EVENTS,
  updateLarkNotificationEventsMutationOptions,
} from "@multica/core/lark";
import type { LarkNotificationEventType } from "@multica/core/lark";
import type { LarkInstallation } from "@multica/core/types";
import type settings from "../../locales/en/settings.json";
import { useT } from "../../i18n";

type SettingsResource = typeof settings;

interface LarkNotificationEventsProps {
  workspaceId: string;
  eventTypes?: readonly LarkNotificationEventType[];
  /** @deprecated Compatibility input for older Settings callers. */
  installation?: LarkInstallation;
  canManage: boolean;
}

interface EventGroup {
  id: string;
  label: (resource: SettingsResource) => string;
  events: readonly {
    type: LarkNotificationEventType;
    label: (resource: SettingsResource) => string;
  }[];
}

const EVENT_GROUPS: readonly EventGroup[] = [
  {
    id: "assignee-changes",
    label: ($) => $.notifications.lark.events.groups.assignee_changes,
    events: [
      {
        type: "issue_assigned",
        label: ($) => $.notifications.lark.events.event_labels.issue_assigned,
      },
      {
        type: "unassigned",
        label: ($) => $.notifications.lark.events.event_labels.unassigned,
      },
      {
        type: "assignee_changed",
        label: ($) => $.notifications.lark.events.event_labels.assignee_changed,
      },
    ],
  },
  {
    id: "comments-mentions",
    label: ($) => $.notifications.lark.events.groups.comments_mentions,
    events: [
      {
        type: "mentioned",
        label: ($) => $.notifications.lark.events.event_labels.mentioned,
      },
      {
        type: "new_comment",
        label: ($) => $.notifications.lark.events.event_labels.new_comment,
      },
      {
        type: "reaction_added",
        label: ($) => $.notifications.lark.events.event_labels.reaction_added,
      },
    ],
  },
  {
    id: "issue-updates",
    label: ($) => $.notifications.lark.events.groups.issue_updates,
    events: [
      {
        type: "status_changed",
        label: ($) => $.notifications.lark.events.event_labels.status_changed,
      },
      {
        type: "priority_changed",
        label: ($) => $.notifications.lark.events.event_labels.priority_changed,
      },
      {
        type: "start_date_changed",
        label: ($) => $.notifications.lark.events.event_labels.start_date_changed,
      },
      {
        type: "due_date_changed",
        label: ($) => $.notifications.lark.events.event_labels.due_date_changed,
      },
    ],
  },
  {
    id: "agent-execution",
    label: ($) => $.notifications.lark.events.groups.agent_execution,
    events: [
      {
        type: "task_failed",
        label: ($) => $.notifications.lark.events.event_labels.task_failed,
      },
    ],
  },
  {
    id: "quick-create",
    label: ($) => $.notifications.lark.events.groups.quick_create,
    events: [
      {
        type: "quick_create_failed",
        label: ($) => $.notifications.lark.events.event_labels.quick_create_failed,
      },
      {
        type: "quick_create_done",
        label: ($) => $.notifications.lark.events.event_labels.quick_create_done,
      },
    ],
  },
  {
    id: "automation",
    label: ($) => $.notifications.lark.events.groups.automation,
    events: [
      {
        type: "autopilot_paused",
        label: ($) => $.notifications.lark.events.event_labels.autopilot_paused,
      },
      {
        type: "issue_subscribed",
        label: ($) => $.notifications.lark.events.event_labels.issue_subscribed,
      },
    ],
  },
];

const EVENT_CATALOG = EVENT_GROUPS.flatMap((group) =>
  group.events.map((event) => event.type),
);

function normalizeSelection(
  eventTypes: readonly LarkNotificationEventType[],
): LarkNotificationEventType[] {
  const selected = new Set(eventTypes);
  return EVENT_CATALOG.filter((eventType) => selected.has(eventType));
}

function policySelection(
  eventTypes: readonly LarkNotificationEventType[] | undefined,
): LarkNotificationEventType[] {
  return normalizeSelection(
    eventTypes ?? DEFAULT_LARK_NOTIFICATION_EVENTS,
  );
}

function selectionsMatch(
  left: readonly LarkNotificationEventType[],
  right: readonly LarkNotificationEventType[],
): boolean {
  return left.length === right.length && left.every((eventType, index) => eventType === right[index]);
}

export function LarkNotificationEvents({
  workspaceId,
  eventTypes,
  installation,
  canManage,
}: LarkNotificationEventsProps) {
  const { t } = useT("settings");
  const queryClient = useQueryClient();
  const mutation = useMutation(
    updateLarkNotificationEventsMutationOptions(workspaceId, queryClient),
  );
  const effectiveEventTypes = eventTypes ?? installation?.notification_event_types ?? DEFAULT_LARK_NOTIFICATION_EVENTS;
  const [draft, setDraft] = useState<LarkNotificationEventType[]>(() => policySelection(effectiveEventTypes));
  const [isDirty, setIsDirty] = useState(false);
  const eventTypesRef = useRef(effectiveEventTypes.join("\u0000"));
  const draftRef = useRef(draft);
  const eventListKey = effectiveEventTypes.join("\u0000");

  draftRef.current = draft;

  useEffect(() => {
    const serverDraft = policySelection(effectiveEventTypes);

    if (eventTypesRef.current !== eventListKey) {
      eventTypesRef.current = eventListKey;
      setDraft(serverDraft);
      setIsDirty(false);
      return;
    }

    setDraft(serverDraft);
    setIsDirty(false);
  }, [eventListKey]);

  function toggleEvent(eventType: LarkNotificationEventType, checked: boolean) {
    const next = new Set(draftRef.current);
    if (checked) {
      next.add(eventType);
    } else {
      next.delete(eventType);
    }

    const nextDraft = EVENT_CATALOG.filter((type) => next.has(type));
    const nextDirty = !selectionsMatch(nextDraft, policySelection(effectiveEventTypes));
    draftRef.current = nextDraft;
    setDraft(nextDraft);
    setIsDirty(nextDirty);
  }

  async function save() {
    if (!isDirty || mutation.isPending) return;
    try {
      await mutation.mutateAsync(draft);
      toast.success(t(($) => $.notifications.lark.events.toast_saved));
    } catch {
      toast.error(t(($) => $.notifications.lark.events.toast_failed));
    }
  }

  return (
    <section
      className="space-y-5"
      aria-labelledby="lark-notification-events"
    >
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h3
            id="lark-notification-events"
            className="text-sm font-semibold"
          >
            {t(($) => $.notifications.lark.events.title)}
          </h3>
          <p className="mt-1 text-xs text-muted-foreground">
            {t(($) => $.notifications.lark.events.description)}
          </p>
        </div>
        {canManage ? (
          <Button
            size="sm"
            className="w-full shrink-0 sm:w-auto"
            disabled={!isDirty || mutation.isPending}
            onClick={() => void save()}
          >
            {mutation.isPending
              ? t(($) => $.notifications.lark.events.saving)
              : t(($) => $.notifications.lark.events.save)}
          </Button>
        ) : (
          <p className="text-xs text-muted-foreground sm:max-w-64 sm:text-right">
            {t(($) => $.notifications.lark.events.read_only)}
          </p>
        )}
      </div>

      <div className="grid gap-x-8 gap-y-6 md:grid-cols-2">
        {EVENT_GROUPS.map((group) => (
          <div key={group.id} className="min-w-0 space-y-2">
            <h4 className="text-xs font-semibold text-muted-foreground">
              {t(group.label)}
            </h4>
            <div className="divide-y">
              {group.events.map((event) => {
                const controlId = `lark-notification-event-${event.type}`;
                return (
                  <div
                    key={event.type}
                    className="flex min-w-0 items-start gap-3 py-2.5 first:pt-0 last:pb-0"
                  >
                    <label
                      htmlFor={controlId}
                      className="min-w-0 flex-1 cursor-pointer text-sm leading-5"
                    >
                      {t(event.label)}
                    </label>
                    <Switch
                      id={controlId}
                      className="mt-0.5"
                      checked={draft.includes(event.type)}
                      disabled={!canManage || mutation.isPending}
                      onCheckedChange={(checked) => toggleEvent(event.type, checked)}
                    />
                  </div>
                );
              })}
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}

export type { LarkNotificationEventsProps };
