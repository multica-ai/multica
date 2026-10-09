"use client";

import { Bot, Cloud, KeyRound, Plus } from "lucide-react";
import type { QoderConnection } from "@multica/core/runtimes/qoder";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { QoderStatusBadge } from "./qoder-status-badge";

export function QoderCloudCard({
  connection,
  agentCount,
  canAddAgent,
  detailHref,
  onConfigure,
  onAddAgent,
}: {
  connection: QoderConnection;
  agentCount: number;
  canAddAgent: boolean;
  detailHref?: string;
  onConfigure: () => void;
  onAddAgent: () => void;
}) {
  const { t } = useT("runtimes");
  return (
    <div>
      <div className="flex flex-col gap-5 rounded-xl border bg-background p-5 sm:flex-row sm:items-center sm:justify-between sm:p-6">
        <div className="flex min-w-0 gap-4">
          <div className="flex size-12 shrink-0 items-center justify-center rounded-xl bg-info/10 text-info">
            <Cloud aria-hidden="true" className="size-6" />
          </div>
          <div className="min-w-0 space-y-2">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="text-body font-semibold">
                {detailHref ? (
                  <AppLink
                    href={detailHref}
                    className="rounded-sm hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    {t(($) => $.qoder.cloud_title)}
                  </AppLink>
                ) : (
                  t(($) => $.qoder.cloud_title)
                )}
              </h3>
              <Badge variant="secondary" className="bg-info/10 text-info">
                {t(($) => $.qoder.managed)}
              </Badge>
              <QoderStatusBadge connection={connection} />
            </div>
            <p className="text-caption leading-relaxed text-muted-foreground">
              {t(($) => $.qoder.cloud_description)}
            </p>
            <p className="inline-flex items-center gap-1.5 rounded-md bg-muted px-2 py-1 text-caption text-muted-foreground">
              <Bot aria-hidden="true" className="size-3.5" />
              {t(($) => $.qoder.agent_count, { count: agentCount })}
            </p>
          </div>
        </div>
        <div className="flex shrink-0 flex-wrap gap-2 sm:justify-end">
          <Button variant="outline" onClick={onConfigure}>
            <KeyRound aria-hidden="true" className="size-4" />
            {t(($) => $.qoder.configure_credentials)}
          </Button>
          <Button onClick={onAddAgent} disabled={!canAddAgent}>
            <Plus aria-hidden="true" className="size-4" />
            {t(($) => $.qoder.add_agent)}
          </Button>
        </div>
      </div>
    </div>
  );
}
