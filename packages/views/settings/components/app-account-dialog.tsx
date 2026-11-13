"use client";

import { useQuery } from "@tanstack/react-query";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useWorkspaceId } from "@multica/core/hooks";
import { appActivityOptions } from "@multica/core/connected-apps";
import type { AppAccountScope } from "@multica/core/types";
import { useT, useTimeAgo } from "../../i18n";

/** Recent calls through one connected account. */
export function AppAccountDialog({
  scope,
  slug,
  name,
  onClose,
}: {
  scope: AppAccountScope;
  slug: string;
  name: string;
  onClose: () => void;
}) {
  const { t } = useT("settings");
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>
            {scope === "team"
              ? t(($) => $.connected_apps.detail.team_title, { app: name })
              : t(($) => $.connected_apps.detail.personal_title, { app: name })}
          </DialogTitle>
        </DialogHeader>
        <ActivityList scope={scope} slug={slug} />
      </DialogContent>
    </Dialog>
  );
}

function ActivityList({ scope, slug }: { scope: AppAccountScope; slug: string }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const timeAgo = useTimeAgo();
  const calls = useQuery(appActivityOptions(wsId, slug, scope));
  const outcomeLabel = (outcome: string) => {
    switch (outcome) {
      case "ok":
      case "error":
      case "blocked":
      case "pending":
      case "approved":
      case "rejected":
      case "rate_limited":
        return t(($) => $.connected_apps.outcomes[outcome]);
      default:
        return outcome;
    }
  };

  if (calls.isLoading) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.connected_apps.loading)}</p>;
  }
  if (calls.isError) {
    return <p className="text-caption text-destructive">{t(($) => $.connected_apps.detail.activity_failed)}</p>;
  }
  if (!calls.data?.length) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.connected_apps.detail.activity_empty)}</p>;
  }
  return (
    <ul className="max-h-80 divide-y overflow-y-auto rounded-lg border">
      {calls.data.map((call) => (
        <li key={call.id} className="flex items-center gap-3 px-3 py-2 text-caption">
          <span className="min-w-0 flex-1 truncate font-mono">{call.tool_slug}</span>
          <span className={call.outcome === "error" || call.outcome === "blocked" ? "text-destructive" : "text-muted-foreground"}>
            {outcomeLabel(call.outcome)}
          </span>
          <span className="w-24 shrink-0 text-right text-muted-foreground">{timeAgo(call.created_at)}</span>
        </li>
      ))}
    </ul>
  );
}
