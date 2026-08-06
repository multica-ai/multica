"use client";

import { Server } from "lucide-react";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { CHAT_COLUMN, CHAT_GUTTER } from "./chat-column";

export function RuntimeRequiredBanner({
  agentId,
  agentName,
}: {
  agentId: string;
  agentName?: string;
}) {
  const { t } = useT("chat");
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const name = agentName?.trim() || t(($) => $.runtime_required_banner.fallback_name);

  return (
    <div className={cn(CHAT_GUTTER, "mb-1.5")}>
      <div
        className={cn(
          CHAT_COLUMN,
          "flex items-center gap-2 rounded-md bg-amber-50 px-2.5 py-1.5 text-caption text-amber-900 ring-1 ring-amber-200/60 dark:bg-amber-950/40 dark:text-amber-200 dark:ring-amber-900/40",
        )}
      >
        <Server className="size-3.5 shrink-0" />
        <span className="min-w-0 flex-1 truncate">
          {t(($) => $.runtime_required_banner.message, { name })}
        </span>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-6 shrink-0 bg-background/70 text-caption"
          onClick={() =>
            navigation.push(`${paths.agentDetail(agentId)}?view=general`)
          }
        >
          {t(($) => $.runtime_required_banner.action)}
        </Button>
      </div>
    </div>
  );
}
