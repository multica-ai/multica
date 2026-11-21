"use client";

import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../i18n";

export function RecentAgentLoadError({
  onRetry,
}: {
  onRetry: () => unknown;
}) {
  const { t } = useT("common");

  return (
    <div
      role="alert"
      className="flex items-center justify-between gap-2 px-2 py-1.5 text-caption text-muted-foreground"
    >
      <span>{t(($) => $.agents.recent.load_failed)}</span>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="shrink-0"
        onClick={() => void onRetry()}
      >
        {t(($) => $.agents.recent.retry)}
      </Button>
    </div>
  );
}
