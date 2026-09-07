"use client";

import { RefreshCw } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";

export function ModelSearchHeader({
  value,
  onChange,
  onRefresh,
  refreshing,
  refreshDisabled = false,
  compact = false,
}: {
  value: string;
  onChange: (value: string) => void;
  onRefresh: () => void;
  refreshing: boolean;
  refreshDisabled?: boolean;
  compact?: boolean;
}) {
  const { t } = useT("agents");
  const refreshLabel = refreshing
    ? t(($) => $.pickers.model_refreshing)
    : t(($) => $.pickers.model_refresh);

  return (
    <div className={`flex items-center gap-1.5 ${compact ? "p-1.5" : "p-2"}`}>
      <Input
        autoFocus
        name="agent-model-search"
        autoComplete="off"
        aria-label={t(($) => $.pickers.model_search_placeholder)}
        placeholder={t(($) => $.pickers.model_search_placeholder)}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className={compact ? "h-7 min-w-0 flex-1 text-caption" : "h-8 min-w-0 flex-1"}
      />
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        aria-label={refreshLabel}
        onClick={onRefresh}
        disabled={refreshDisabled || refreshing}
      >
        <RefreshCw
          className={
            refreshing ? "animate-spin motion-reduce:animate-none" : undefined
          }
          aria-hidden="true"
        />
      </Button>
    </div>
  );
}
