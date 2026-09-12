"use client";

import { useRelativeTimeTick } from "@multica/core/hooks/use-relative-time-tick";
import { useTimeAgo } from "../../i18n";

export function RelativeTime({ dateTime }: { dateTime: string }) {
  useRelativeTimeTick();
  const timeAgo = useTimeAgo();
  return <time dateTime={dateTime}>{timeAgo(dateTime)}</time>;
}
