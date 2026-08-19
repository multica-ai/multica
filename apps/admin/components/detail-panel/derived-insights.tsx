import { cn } from "@multica/ui/lib/utils";
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

export function DerivedInsights({ insights }: { insights: DerivedInsightsData }) {
  return (
    <section>
      <h3 className="mb-3 text-label font-medium text-muted-foreground uppercase tracking-wide">
        Health
      </h3>
      <div className="flex items-center gap-4">
        <span className={cn("rounded-full px-2.5 py-1 text-label font-medium", HEALTH_CLASSES[insights.health])}>
          {HEALTH_LABELS[insights.health]}
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
