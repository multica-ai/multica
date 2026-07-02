"use client";

import { Archive } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useModalStore } from "@multica/core/modals";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";
import { useT } from "../../i18n";
import { InboxListItem, useTimeAgo } from "../components/inbox-list-item";
import { useTypeLabels } from "../components/inbox-detail-label";
import { getInboxDisplayTitle } from "../components/inbox-display";
import {
  registerInboxItemType,
  type InboxItemDetailProps,
  type InboxItemRowProps,
} from "./contract";

/**
 * Renderer for the `issue_notification` kind — every notification the server
 * issues today. The list row reuses {@link InboxListItem}; the detail pane
 * below is only used for notifications that have NO backing issue (e.g. a
 * failed quick-create). Notifications that point at an issue defer to the
 * shared `IssueDetail` surface, so this `Detail` deliberately omits that path.
 */
function IssueNotificationRow({
  entry,
  isSelected,
  onSelect,
  onArchive,
}: InboxItemRowProps) {
  if (entry.kind !== "issue_notification") return null;
  return (
    <InboxListItem
      item={entry.notification}
      isSelected={isSelected}
      onClick={onSelect}
      onArchive={onArchive}
    />
  );
}

function IssueNotificationDetail({ entry, onArchive }: InboxItemDetailProps) {
  const { t } = useT("inbox");
  const typeLabels = useTypeLabels();
  const timeAgo = useTimeAgo();
  if (entry.kind !== "issue_notification") return null;
  const item = entry.notification;

  return (
    <div className="p-6">
      <h2 className="text-lg font-semibold">{getInboxDisplayTitle(item)}</h2>
      <p className="mt-1 text-sm text-muted-foreground">
        {typeLabels[item.type]} · {timeAgo(item.created_at)}
      </p>
      {item.body && (
        <div className="mt-4 whitespace-pre-wrap text-sm leading-relaxed text-foreground/80">
          {item.body}
        </div>
      )}
      {item.type === "quick_create_failed" && item.details?.original_prompt && (
        <div className="mt-4 rounded-md border bg-muted/40 p-3">
          <p className="text-xs font-medium text-muted-foreground">
            {t(($) => $.detail.original_input)}
          </p>
          <p className="mt-1 whitespace-pre-wrap text-sm">
            {item.details.original_prompt}
          </p>
        </div>
      )}
      <div className="mt-4 flex gap-2">
        {item.type === "quick_create_failed" && (
          <Button
            size="sm"
            onClick={() => {
              // Seed the legacy advanced form with the original prompt so the
              // user can recover their input in the full editor instead of
              // retyping. The agent picker hint becomes the assignee
              // candidate (still editable).
              const prompt = item.details?.original_prompt ?? "";
              const agentId = item.details?.agent_id;
              useIssueDraftStore.getState().setDraft({
                description: prompt,
                ...(agentId
                  ? { assigneeType: "agent" as const, assigneeId: agentId }
                  : {}),
              });
              useModalStore.getState().open("create-issue");
            }}
          >
            {t(($) => $.detail.edit_advanced)}
          </Button>
        )}
        <Button variant="outline" size="sm" onClick={onArchive}>
          <Archive className="mr-1.5 h-3.5 w-3.5" />
          {t(($) => $.detail.archive)}
        </Button>
      </div>
    </div>
  );
}

registerInboxItemType({
  kind: "issue_notification",
  Row: IssueNotificationRow,
  Detail: IssueNotificationDetail,
});
