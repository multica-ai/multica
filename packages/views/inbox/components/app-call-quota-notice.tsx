"use client";

import { useWorkspacePaths } from "@multica/core/paths";
import type { InboxItem } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";

function formatResetAt(value: string | undefined): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(date);
}

/**
 * The workspace used its monthly app calls (MUL-10047). Only owners and admins
 * receive it, so it leads to Billing, where they upgrade or add seats.
 */
export function AppCallQuotaNotice({ item }: { item: InboxItem }) {
  const { t } = useT("inbox");
  const navigation = useNavigation();
  const wsPaths = useWorkspacePaths();
  const details = item.details ?? {};
  const resetAt = formatResetAt(details.reset_at);
  const body =
    details.limit && resetAt
      ? t(($) => $.detail.app_tool_calls_quota_exceeded_body, { limit: details.limit, resetAt })
      : item.body;

  return (
    <>
      {body ? (
        <div className="mt-4 whitespace-pre-wrap text-body leading-relaxed text-foreground">{body}</div>
      ) : null}
      <Button className="mt-4" size="sm" onClick={() => navigation.push(`${wsPaths.settings()}?tab=billing`)}>
        {t(($) => $.detail.app_tool_calls_quota_action)}
      </Button>
    </>
  );
}
