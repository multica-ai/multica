"use client";

import { useState } from "react";
import { Cloud, Settings2, Sparkles } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { CloudRuntimeDialog } from "./cloud-runtime-dialog";
import { useCloudPreview } from "./cloud-preview";
import { useT } from "../../i18n";

/**
 * CloudCapabilityCard — the redesigned Cloud entry point (MUL-5385).
 *
 * Product intent: Cloud stops being an inventory of machines the user
 * provisions and becomes a workspace capability with one switch, a balance,
 * and an elastic concurrency figure. No instance type, no disk size, no
 * region, no start/stop — those are the scheduler's job, not the user's.
 *
 * The raw Fleet node list is NOT deleted; it moves behind "Advanced" so
 * power users and support can still reach it. This component is UI-only:
 * flipping the switch mutates local preview state (see cloud-preview.ts).
 */
export function CloudCapabilityCard({
  /** Whether the legacy Fleet node dialog can be opened from Advanced. */
  nodeInspectorAvailable = false,
}: {
  nodeInspectorAvailable?: boolean;
}) {
  const { t } = useT("runtimes");
  const cloudOn = useCloudPreview((state) => state.cloudOn);
  const setCloudOn = useCloudPreview((state) => state.setCloudOn);
  const creditsRemaining = useCloudPreview((state) => state.creditsRemaining);
  const activeRuns = useCloudPreview((state) => state.activeRuns);
  const queuedRuns = useCloudPreview((state) => state.queuedRuns);
  const [showNodeInspector, setShowNodeInspector] = useState(false);

  return (
    <section className="overflow-hidden rounded-lg border bg-card">
      <div className="flex min-w-0 items-center gap-3 px-4 py-3.5">
        <span className="relative flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border bg-background">
          <Cloud aria-hidden="true" className="h-4 w-4 text-muted-foreground" />
          {cloudOn && (
            <span
              aria-hidden="true"
              className="absolute -right-0.5 -bottom-0.5 h-2.5 w-2.5 rounded-full bg-success ring-2 ring-background"
            />
          )}
        </span>

        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-medium">
            {t(($) => $.cloud_preview.title)}
          </span>
          <span className="mt-1 block truncate text-xs text-muted-foreground">
            {t(($) => $.cloud_preview.tagline)}
          </span>
        </span>

        {cloudOn && (
          <>
            <span className="hidden w-28 shrink-0 items-center gap-1.5 text-xs text-success md:flex">
              <span
                className="h-2 w-2 rounded-full bg-success"
                aria-hidden="true"
              />
              {t(($) => $.cloud_preview.status_ready)}
            </span>
            <span className="hidden w-36 shrink-0 text-xs text-muted-foreground lg:block">
              {t(($) => $.cloud_preview.stats.credits)}{" "}
              <span className="font-medium text-foreground tabular-nums">
                {creditsRemaining.toLocaleString()}
              </span>
            </span>
            <span className="hidden w-40 shrink-0 text-xs text-muted-foreground xl:block">
              {t(($) => $.machine.metrics.workload_hint, {
                running: activeRuns,
                queued: queuedRuns,
              })}
            </span>
          </>
        )}

        <div className="flex shrink-0 items-center gap-1">
          {nodeInspectorAvailable && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="text-muted-foreground"
              aria-label={t(($) => $.cloud_preview.advanced)}
              onClick={() => setShowNodeInspector(true)}
            >
              <Settings2 aria-hidden="true" className="h-3.5 w-3.5" />
              <span className="hidden sm:inline">
                {t(($) => $.cloud_preview.advanced)}
              </span>
            </Button>
          )}
          {cloudOn ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="text-muted-foreground"
              onClick={() => setCloudOn(false)}
            >
              {t(($) => $.cloud_preview.disable)}
            </Button>
          ) : (
            <Button type="button" size="sm" onClick={() => setCloudOn(true)}>
              <Sparkles aria-hidden="true" className="h-3.5 w-3.5" />
              {t(($) => $.cloud_preview.enable)}
            </Button>
          )}
        </div>
      </div>

      {showNodeInspector && (
        <CloudRuntimeDialog onClose={() => setShowNodeInspector(false)} />
      )}
    </section>
  );
}

export default CloudCapabilityCard;
