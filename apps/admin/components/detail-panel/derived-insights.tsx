import { Info } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import type { DerivedInsights as DerivedInsightsData } from "@/lib/types";

const HEALTH_CLASSES: Record<DerivedInsightsData["health"], string> = {
  good: "bg-success/10 text-success",
  warning: "bg-warning/10 text-warning",
  critical: "bg-destructive/10 text-destructive",
};

const HEALTH_LABELS: Record<DerivedInsightsData["health"], string> = {
  good: "Good",
  warning: "Needs attention",
  critical: "Critical",
};

// Mirrors the thresholds in lib/derive.ts's deriveHealth — keep in sync if
// that rubric changes.
const HEALTH_EXPLANATION =
  "Good: success rate is 90% or higher and issues resolve within 24h on " +
  "average (or there isn't enough history yet). Needs attention: success " +
  "rate is between 70–90%, or average resolution time is over 24h. " +
  "Critical: success rate is below 70%, or the workspace is in an error " +
  "state.";

export function DerivedInsights({ insights }: { insights: DerivedInsightsData }) {
  return (
    <section>
      <h3 className="mb-3 text-label font-medium text-muted-foreground uppercase tracking-wide">
        Health
      </h3>
      <div className="flex items-center gap-4">
        <span
          className={cn(
            "inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-label font-medium",
            HEALTH_CLASSES[insights.health],
          )}
        >
          {HEALTH_LABELS[insights.health]}
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger aria-label={`What does "${HEALTH_LABELS[insights.health]}" mean?`}>
                <Info className="size-3.5" aria-hidden />
              </TooltipTrigger>
              <TooltipContent>{HEALTH_EXPLANATION}</TooltipContent>
            </Tooltip>
          </TooltipProvider>
        </span>
        <div>
          <p className="text-title-lg font-medium text-foreground">
            {insights.successRate !== null ? `${insights.successRate}%` : "—"}
          </p>
          <p className="text-caption text-muted-foreground">
            {insights.successRate !== null ? "Success rate (30d)" : "Not enough data"}
          </p>
        </div>
      </div>
    </section>
  );
}
