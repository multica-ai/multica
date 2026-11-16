import {
  Cloud,
  Laptop,
  Monitor,
  Wifi,
  WifiHigh,
  WifiOff,
  type IconComponent,
} from "@multica/ui/icons";
import { Badge } from "@multica/ui/components/ui/badge";
import type { RuntimeHealth } from "@multica/core/runtimes";
import { ProviderLogo } from "./provider-logo";
import type { RuntimeMachine } from "./runtime-machines";
import { useT } from "../../i18n";

export function RuntimeModeIcon({ mode }: { mode: string }) {
  return mode === "cloud" ? (
    <Cloud className="h-3.5 w-3.5" />
  ) : (
    <Monitor className="h-3.5 w-3.5" />
  );
}

export function machineIcon(
  machine: Pick<RuntimeMachine, "isCurrent" | "section">,
): IconComponent {
  if (machine.isCurrent) return Laptop;
  return machine.section === "cloud" ? Cloud : Monitor;
}

export function shortDaemonId(id: string | null): string | null {
  if (!id) return null;
  if (id.length <= 10) return id;
  return `${id.slice(0, 6)}··${id.slice(-2)}`;
}

export function ProviderIconStack({ providers }: { providers: string[] }) {
  const visible = providers.slice(0, 4);
  const extra = providers.length - visible.length;
  if (visible.length === 0) return null;
  return (
    <span className="flex min-w-0 items-center -space-x-1">
      {visible.map((provider) => (
        <span
          key={provider}
          className="inline-flex h-5 w-5 items-center justify-center rounded-xs bg-background ring-1 ring-border"
        >
          <ProviderLogo provider={provider} className="h-3.5 w-3.5" />
        </span>
      ))}
      {extra > 0 && (
        <span className="inline-flex h-5 min-w-5 items-center justify-center rounded-xs bg-muted px-1 text-micro font-medium text-muted-foreground ring-1 ring-border">
          +{extra}
        </span>
      )}
    </span>
  );
}

// Compact provider tag: small logo square + provider name. Used in dense
// list rows to identify which CLI / model provider a runtime is wired to.
export function ProviderChip({ provider }: { provider: string }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-md border bg-muted/40 px-1.5 py-0.5 text-caption font-medium text-muted-foreground">
      <ProviderLogo provider={provider} className="h-3 w-3" />
      <span className="capitalize">{provider}</span>
    </span>
  );
}

// Maps each derived 4-state runtime health to a semantic colour class.
// The mapping intentionally reuses our existing tokens (success/warning/
// muted-foreground/destructive) instead of introducing runtime-specific
// colours — keeps the palette small and consistent with Skills.
// Labels flow through useT — see useHealthLabel below.
const HEALTH_VISUAL: Record<RuntimeHealth, { dot: string; tone: string }> = {
  online: { dot: "bg-success", tone: "bg-success/10 text-success" },
  recently_lost: { dot: "bg-warning", tone: "bg-warning/10 text-warning" },
  offline: { dot: "bg-muted-foreground/40", tone: "bg-muted text-muted-foreground" },
  long_offline: { dot: "bg-destructive", tone: "bg-destructive/10 text-destructive" },
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
//   long_offline  → WifiOff (slashed, destructive) — prolonged outage
const HEALTH_ICON: Record<
  RuntimeHealth,
  { Icon: typeof Wifi; tone: string }
> = {
  online: { Icon: Wifi, tone: "text-success" },
  recently_lost: { Icon: WifiHigh, tone: "text-warning" },
  offline: { Icon: WifiOff, tone: "text-muted-foreground" },
  long_offline: { Icon: WifiOff, tone: "text-destructive" },
};

export function HealthIcon({
  health,
  className = "h-3 w-3",
}: {
  health: RuntimeHealth | "loading";
  className?: string;
}) {
  if (health === "loading") {
    return <Wifi className={`${className} text-faint-foreground`} />;
  }
  const { Icon, tone } = HEALTH_ICON[health];
  return <Icon className={`${className} ${tone}`} />;
}

// English-only fallback. Pure function form for non-component callers
// (e.g. column factory builders). Translated call sites should use the
// `useHealthLabel` hook below instead.
const HEALTH_LABEL_EN: Record<RuntimeHealth, string> = {
  online: "Online",
  recently_lost: "Recently lost",
  offline: "Offline",
  long_offline: "Long offline",
};

export function healthLabel(health: RuntimeHealth | "loading"): string {
  if (health === "loading") return "—";
  return HEALTH_LABEL_EN[health];
}

// Hook form: usable inside React components (preferred for new call sites
// that aren't running in non-component contexts).
export function useHealthLabel(): (health: RuntimeHealth | "loading") => string {
  const { t } = useT("runtimes");
  return (health) => {
    if (health === "loading") return "—";
    return t(($) => $.health[health].label);
  };
}

export function HealthBadge({
  health,
}: {
  health: RuntimeHealth | "loading";
}) {
  const labelOf = useHealthLabel();
  if (health === "loading") {
    return (
      <Badge variant="secondary" className="bg-muted text-muted-foreground">
        —
      </Badge>
    );
  }
  const v = HEALTH_VISUAL[health];
  return (
    <Badge variant="secondary" className={v.tone}>
      <span className={`h-1.5 w-1.5 rounded-full ${v.dot}`} />
      {labelOf(health)}
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
      <div className="text-caption text-muted-foreground">{label}</div>
      <div
        className={`mt-0.5 text-body truncate ${mono ? "font-mono text-caption" : ""}`}
      >
        {value}
      </div>
    </div>
  );
}

export function TokenCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border px-3 py-2">
      <div className="text-caption text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-body font-semibold tabular-nums">{value}</div>
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
  value: React.ReactNode;
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
      <div className="text-micro font-medium uppercase tracking-wider text-muted-foreground">
        {label}
      </div>
      <div className={`text-display font-semibold leading-none tabular-nums ${valueClass}`}>
        {value}
      </div>
      {hint != null && (
        <div className="text-caption text-muted-foreground">{hint}</div>
      )}
    </div>
  );
}
