"use client";

import {
  AlertCircle,
  Ban,
  CheckCircle2,
  CircleDashed,
  Clock3,
  Info,
  TriangleAlert,
} from "lucide-react";
import type { AccessDiagnostic, AccessDiagnosticState } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";

const statePresentation: Record<AccessDiagnosticState, {
  icon: typeof Info;
  label: string;
  badge: "default" | "secondary" | "destructive" | "outline";
}> = {
  success: { icon: CheckCircle2, label: "Current", badge: "default" },
  info: { icon: Info, label: "Info", badge: "secondary" },
  partial: { icon: TriangleAlert, label: "Partial", badge: "outline" },
  stale: { icon: Clock3, label: "Stale", badge: "outline" },
  empty: { icon: CircleDashed, label: "Empty", badge: "secondary" },
  denied: { icon: Ban, label: "Denied", badge: "destructive" },
  unavailable: { icon: AlertCircle, label: "Unavailable", badge: "destructive" },
  error: { icon: AlertCircle, label: "Error", badge: "destructive" },
};

export function AccessDiagnostics({
  diagnostics,
  emptyMessage = "No access diagnostics were recorded.",
  className,
}: {
  diagnostics: AccessDiagnostic[];
  emptyMessage?: string;
  className?: string;
}) {
  if (diagnostics.length === 0) {
    return (
      <div className={cn("rounded-md border border-dashed p-3 text-xs text-muted-foreground", className)}>
        {emptyMessage}
      </div>
    );
  }

  return (
    <div className={cn("space-y-2", className)}>
      {diagnostics.map((diagnostic, index) => {
        const presentation = statePresentation[diagnostic.state] ?? statePresentation.info;
        const Icon = presentation.icon;
        return (
          <article
            key={`${diagnostic.code}-${diagnostic.affected_capability ?? index}`}
            className="rounded-md border bg-card p-3"
          >
            <div className="flex items-start justify-between gap-3">
              <div className="flex min-w-0 items-start gap-2">
                <Icon
                  className={cn(
                    "mt-0.5 size-4 shrink-0 text-muted-foreground",
                    (diagnostic.state === "denied" || diagnostic.state === "error" || diagnostic.state === "unavailable") && "text-destructive",
                  )}
                  aria-hidden="true"
                />
                <div className="min-w-0">
                  <h4 className="text-xs font-medium">{diagnostic.title}</h4>
                  <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
                    {diagnostic.message}
                  </p>
                </div>
              </div>
              <Badge variant={presentation.badge} className="shrink-0 text-[10px]">
                {presentation.label}
              </Badge>
            </div>

            <dl className="mt-3 grid gap-2 text-[11px] sm:grid-cols-2">
              {diagnostic.affected_capability ? (
                <DiagnosticField label="Affected capability" value={diagnostic.affected_capability} mono />
              ) : null}
              <DiagnosticField label="Source policy" value={diagnostic.source_policy} />
              {diagnostic.version ? (
                <DiagnosticField label="Discovery version" value={diagnostic.version} mono />
              ) : null}
              {diagnostic.observed_at ? (
                <DiagnosticField label="Observed" value={formatTimestamp(diagnostic.observed_at)} />
              ) : null}
              <div className="sm:col-span-2">
                <dt className="text-muted-foreground">Recovery</dt>
                <dd className="mt-0.5 text-foreground">{diagnostic.recovery_action}</dd>
              </div>
            </dl>
          </article>
        );
      })}
    </div>
  );
}

function DiagnosticField({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={cn("mt-0.5 break-all text-foreground", mono && "font-mono")}>{value}</dd>
    </div>
  );
}

function formatTimestamp(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}
