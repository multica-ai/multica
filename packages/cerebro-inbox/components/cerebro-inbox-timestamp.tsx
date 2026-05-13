"use client";

import { useTranslation } from "react-i18next";
import type { InboxItem } from "@multica/core/types";
import { formatMutedUntilTime } from "../mute-time";
import { useCerebroInboxStrings } from "../strings";

/**
 * Row-timestamp slot for inbox notif rows. When the item is currently muted
 * (and therefore only visible because the user selected the "Muted" filter),
 * we replace the normal relative timestamp with "Muted til 08:00" so the
 * user can see when the mute expires — the only signal they get for
 * undoing a mute. For everything else we render the upstream `fallback`
 * string unchanged.
 */
export function CerebroInboxTimestamp({
  item,
  fallback,
}: {
  item: InboxItem;
  fallback: string;
}) {
  const { i18n } = useTranslation();
  const strings = useCerebroInboxStrings();
  const time = formatMutedUntilTime(item.muted_until, i18n.language);
  if (!time) return <>{fallback}</>;
  return <>{`${strings.muted_until_prefix} ${time}`}</>;
}
