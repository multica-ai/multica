"use client";

import { focusManager, useQuery } from "@tanstack/react-query";
import { ArchiveRestore } from "@multica/ui/icons";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { archivedInboxLookupOptions, useUnarchiveInbox } from "@multica/core/inbox";
import type { MenuPrimitives } from "./issue-actions-menu-items";
import { useT } from "../../i18n";

/** The reader's archived inbox notification for this issue, if any. An issue
 *  opened from search or a link has no other way out of Inbox → Archived. */
export function useArchivedInboxItemId(issueId: string, enabled: boolean): string | undefined {
  const wsId = useWorkspaceId();
  // Every inbox event refetches this lookup, so a hidden page skips it like the unread summary does.
  // The host calls this rather than the lazily mounted menu, so the entry doesn't pop in after opening.
  const { data } = useQuery({
    ...archivedInboxLookupOptions(wsId, issueId),
    enabled: () => enabled && focusManager.isFocused(),
    refetchOnWindowFocus: true,
  });
  return enabled ? data?.items.find((i) => i.archived)?.id : undefined;
}

export function InboxRestoreMenuItem({ itemId, Item }: { itemId: string; Item: MenuPrimitives["Item"] }) {
  const { t } = useT("issues");
  const { t: tInbox } = useT("inbox");
  const unarchive = useUnarchiveInbox();
  return (
    <Item
      onClick={() =>
        unarchive.mutate(itemId, {
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
      <ArchiveRestore className="h-3.5 w-3.5" />
      {t(($) => $.actions.restore_to_inbox)}
    </Item>
  );
}
