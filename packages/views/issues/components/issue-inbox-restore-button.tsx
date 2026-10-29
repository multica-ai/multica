"use client";

import { focusManager, useQuery } from "@tanstack/react-query";
import { ArchiveRestore } from "@multica/ui/icons";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { archivedInboxLookupOptions, useUnarchiveInbox } from "@multica/core/inbox";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { useT } from "../../i18n";

// An issue opened from search or a link has no other way out of Inbox → Archived.
export function IssueInboxRestoreButton({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const { t: tInbox } = useT("inbox");
  const wsId = useWorkspaceId();
  // Every inbox event refetches this lookup, so a hidden page skips it like the unread summary does.
  const { data } = useQuery({
    ...archivedInboxLookupOptions(wsId, issueId),
    enabled: () => focusManager.isFocused(),
    refetchOnWindowFocus: true,
  });
  const unarchive = useUnarchiveInbox();
  const item = data?.items.find((i) => i.archived);
  if (!item) return null;

  return (
    <IconButton
      label={t(($) => $.detail.restore_to_inbox_tooltip)}
      tooltipSide="bottom"
      className="text-muted-foreground"
      onClick={() =>
        unarchive.mutate(item.id, {
          onSuccess: () => toast.success(tInbox(($) => $.toasts.unarchived)),
          onError: (err) =>
            toast.error(
              err instanceof Error && err.message
                ? err.message
                : tInbox(($) => $.errors.unarchive_failed),
            ),
        })
      }
    >
      <ArchiveRestore />
    </IconButton>
  );
}
