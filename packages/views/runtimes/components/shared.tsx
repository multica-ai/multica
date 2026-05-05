import { Cloud, Monitor, Wifi, WifiHigh, WifiOff } from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import type { RuntimeHealth } from "@multica/core/runtimes";
import { useT } from "@multica/i18n/react";
import { ProviderLogo } from "./provider-logo";

export function RuntimeModeIcon({ mode }: { mode: string }) {
  return mode === "cloud" ? (
    <Cloud className="h-3.5 w-3.5" />
  ) : (
    <Monitor className="h-3.5 w-3.5" />
  );
}

// Compact provider tag: small logo square + provider name. Used in dense
// list rows to identify which CLI / model provider a runtime is wired to.
export function ProviderChip({ provider }: { provider: string }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-md border bg-muted/40 px-1.5 py-0.5 text-xs font-medium text-muted-foreground">
      <ProviderLogo provider={provider} className="h-3 w-3" />
      <span className="capitalize">{provider}</span>
    </span>
  );
}

// Maps each derived 4-state runtime health to a semantic colour class.
// The mapping intentionally reuses our existing tokens (success/warning/
// muted-foreground/destructive) instead of introducing runtime-specific
// colours — keeps the palette small and consistent with Skills.
const HEALTH_VISUAL: Record<
  RuntimeHealth,
  { dot: string; labelKey: string; tone: string }
> = {
  online: {
    dot: "bg-success",
    labelKey: "health_online",
    tone: "bg-success/10 text-success",
  },
  recently_lost: {
    dot: "bg-warning",
    labelKey: "health_recently_lost",
    tone: "bg-warning/10 text-warning",
  },
  offline: {
    dot: "bg-muted-foreground/40",
    labelKey: "health_offline",
    tone: "bg-muted text-muted-foreground",
  },
  about_to_gc: {
    dot: "bg-destructive",
    labelKey: "health_about_to_gc",
    tone: "bg-destructive/10 text-destructive",
  },
};

export function HealthDot({
  health,
  className = "",
}: {
  health: RuntimeHealth | "loading";
  className?: string;
}) {
  if (health === "loading") {
    return (
      <span
        className={`inline-block h-2 w-2 rounded-full bg-muted ${className}`}
      />
    );
  }
  return (
    <span
      className={`inline-block h-2 w-2 rounded-full ${HEALTH_VISUAL[health].dot} ${className}`}
    />
  );
}

// Wifi-style runtime health indicator. The icon shape carries the rough
// state ("can it talk to us?") and the colour carries severity. Used
// wherever a richer signal than the bare dot is appropriate (agent
// hover-card runtime row, runtime list health column).
//
//   online        → Wifi (full bars, success)
//   recently_lost → WifiHigh (fewer bars, warning) — transient hiccup
//   offline       → WifiOff (slashed, muted) — long unreachable
//   about_to_gc   → WifiOff (slashed, destructive) — sweeper coming
const HEALTH_ICON: Record<
  RuntimeHealth,
  { Icon: typeof Wifi; tone: string }
> = {
  online: { Icon: Wifi, tone: "text-success" },
  recently_lost: { Icon: WifiHigh, tone: "text-warning" },
  offline: { Icon: WifiOff, tone: "text-muted-foreground" },
  about_to_gc: { Icon: WifiOff, tone: "text-destructive" },
};

export function HealthIcon({
  health,
  className = "h-3 w-3",
}: {
  health: RuntimeHealth | "loading";
  className?: string;
}) {
  if (health === "loading") {
    return <Wifi className={`${className} text-muted-foreground/40`} />;
  }
  const { Icon, tone } = HEALTH_ICON[health];
  return <Icon className={`${className} ${tone}`} />;
}

export function healthLabel(health: RuntimeHealth | "loading", t?: (key: string) => string): string {
  if (health === "loading") return t ? t("health_loading") : "—";
  return t ? t(HEALTH_VISUAL[health].labelKey) : HEALTH_VISUAL[health].labelKey;
}

export function HealthBadge({
  health,
}: {
  health: RuntimeHealth | "loading";
}) {
  const t = useT("runtimes");
  if (health === "loading") {
    return (
      <Badge variant="secondary" className="bg-muted text-muted-foreground">
        {t("health_loading")}
      </Badge>
    );
  }
  const v = HEALTH_VISUAL[health];
  return (
    <Badge variant="secondary" className={v.tone}>
      <span className={`h-1.5 w-1.5 rounded-full ${v.dot}`} />
      {t(v.labelKey)}
    </Badge>
  );
}

export function InfoField({
  label,
  value,
  mono,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div
        className={`mt-0.5 text-sm truncate ${mono ? "font-mono text-xs" : ""}`}
      >
        {value}
      </div>
    </div>
  );
}

export function TokenCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border px-3 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-sm font-semibold tabular-nums">{value}</div>
    </div>
  );
}

// KPI tile used in the Runtime detail "story numbers" row. The big number
// is the visual anchor of the whole left column — sized large enough that
// it dominates over the chart hierarchy below it. Label sits as a small
// caps eyebrow; hint is a thin caption beneath the number for deltas /
// ratios / savings context.
export function KpiCard({
  label,
  value,
  hint,
  accent,
}: {
  label: string;
  value: string;
  hint?: React.ReactNode;
  accent?: "brand" | "success" | "default";
}) {
  const valueClass =
    accent === "brand"
      ? "text-brand"
      : accent === "success"
        ? "text-success"
        : "";
  return (
    <div className="flex flex-col gap-2 p-5">
      <div className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
        {label}
      </div>
      <div className={`text-3xl font-semibold leading-none tabular-nums ${valueClass}`}>
        {value}
      </div>
      {hint != null && (
        <div className="text-xs text-muted-foreground">{hint}</div>
      )}
    </div>
  );
}
