"use client";

import { AlertTriangle } from "@multica/ui/icons";
import type { SkillImportDiagnostic } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";

/**
 * Structured import/refresh diagnostics (OL-103 wire shape). Server messages
 * are data, not UI copy, so they render verbatim; the surrounding chrome is
 * localized by the caller through `title`.
 */
export function SkillDiagnosticRows({
  diagnostics,
}: {
  diagnostics: readonly SkillImportDiagnostic[];
}) {
  return (
    <ul className="space-y-1.5">
      {diagnostics.map((d, i) => (
        <li key={`${d.code}-${d.path ?? ""}-${d.target ?? ""}-${i}`} className="text-caption">
          <span className="text-foreground">{d.message}</span>
          <span className="ml-1.5 text-muted-foreground">
            {d.code}
            {d.path ? ` · ${d.path}` : ""}
            {d.target ? ` · ${d.target}` : ""}
          </span>
        </li>
      ))}
    </ul>
  );
}

/** Warning-tinted panel listing diagnostics under a localized title. */
export function SkillDiagnosticsNotice({
  title,
  diagnostics,
  className,
}: {
  title: string;
  diagnostics: readonly SkillImportDiagnostic[];
  className?: string;
}) {
  if (diagnostics.length === 0) return null;
  return (
    <div
      role="status"
      className={cn(
        "rounded-md bg-warning/10 px-3 py-2 text-caption text-muted-foreground",
        className,
      )}
    >
      <div className="mb-1.5 flex items-center gap-1.5 text-foreground">
        <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-warning" />
        <span>{title}</span>
      </div>
      <SkillDiagnosticRows diagnostics={diagnostics} />
    </div>
  );
}
