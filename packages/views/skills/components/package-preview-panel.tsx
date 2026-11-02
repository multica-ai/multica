"use client";

import { AlertTriangle, CheckCircle2, Package } from "@multica/ui/icons";
import type {
  SkillPackageApplyResult,
  SkillPackageCandidate,
  SkillPackageItemResult,
  SkillPackagePreview,
} from "@multica/core/api/schemas";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import {
  canApplyOverwrite,
  hasSelectedConflict,
  isCandidateSelectable,
  isItemFailure,
  sameNameGroups,
} from "../lib/package-preview-model";
import { SkillDiagnosticRows } from "./skill-diagnostics-notice";

export type OnConflictChoice = "skip" | "rename" | "overwrite";

function stateLabelKey(state: SkillPackageCandidate["state"]) {
  switch (state) {
    case "new": return "state_new";
    case "adoptable": return "state_adoptable";
    case "changed": return "state_changed";
    case "unchanged": return "state_unchanged";
    case "removed": return "state_removed";
    case "conflict": return "state_conflict";
    case "failed": return "state_failed";
    default: return "state_unknown";
  }
}

function itemStatusKey(status: SkillPackageItemResult["status"]) {
  switch (status) {
    case "created": return "item_created";
    case "adopted": return "item_adopted";
    case "updated": return "item_updated";
    case "unchanged": return "item_unchanged";
    case "skipped": return "item_skipped";
    case "retained": return "item_retained";
    case "failed": return "item_failed";
    default: return "item_unknown";
  }
}

function CandidateRow({
  candidate,
  selected,
  onToggle,
}: {
  candidate: SkillPackageCandidate;
  selected: boolean;
  onToggle: (path: string, next: boolean) => void;
}) {
  const { t } = useT("skill-packages");
  const selectable = isCandidateSelectable(candidate);
  const badge = t(($) => $.preview[stateLabelKey(candidate.state)]);

  // Why a row cannot be checked, or what a conflict means — stated once,
  // beside the row it applies to.
  let hint: string | null = null;
  if (candidate.conflict === "forbidden") hint = t(($) => $.preview.forbidden_hint);
  else if (candidate.state === "removed") hint = t(($) => $.preview.removed_hint);
  else if (candidate.state === "failed") hint = t(($) => $.preview.failed_hint);
  else if (candidate.state === "unknown") hint = t(($) => $.preview.unknown_hint);
  else if (candidate.conflict === "already_packaged") hint = t(($) => $.preview.already_packaged_hint);
  else if (candidate.conflict === "ambiguous_source") hint = t(($) => $.preview.ambiguous_hint);

  const row = (
    <div
      className={cn(
        "flex items-start gap-2.5 rounded-md px-2.5 py-2",
        selectable ? "cursor-pointer hover:bg-accent/50" : "opacity-70",
        selectable && selected && "bg-accent",
      )}
    >
      {selectable ? (
        <Checkbox
          checked={selected}
          tabIndex={-1}
          className="pointer-events-none mt-0.5"
        />
      ) : (
        <span className="mt-0.5 inline-block size-4 shrink-0" aria-hidden="true" />
      )}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className="truncate text-body font-medium">{candidate.name}</span>
          <span
            className={cn(
              "shrink-0 rounded-xs px-1 py-0.5 text-caption",
              candidate.state === "conflict" || candidate.state === "failed"
                ? "bg-destructive/10 text-destructive"
                : candidate.state === "removed" || candidate.state === "unknown"
                  ? "bg-muted text-muted-foreground"
                  : "bg-muted text-muted-foreground",
            )}
          >
            {badge}
          </span>
        </div>
        <div className="truncate text-caption text-muted-foreground">
          {candidate.path || "/"}
          {candidate.file_count > 0
            ? ` · ${t(($) => $.preview.files, { count: candidate.file_count })}`
            : ""}
        </div>
        {hint && <div className="mt-0.5 text-caption text-muted-foreground">{hint}</div>}
        {candidate.diagnostics.length > 0 && (
          <div className="mt-1">
            <SkillDiagnosticRows diagnostics={candidate.diagnostics} />
          </div>
        )}
      </div>
    </div>
  );

  if (!selectable) return row;
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={() => onToggle(candidate.path, !selected)}
      className="block w-full text-left"
    >
      {row}
    </button>
  );
}

/**
 * The shared checklist half of the import and rescan dialogs: source summary,
 * per-candidate checks honoring server defaults and permission hints, the
 * conflict strategy choice, and the known same-name first-apply warning.
 */
export function PackagePreviewPanel({
  preview,
  existingPackageName,
  selected,
  onSelectedChange,
  onConflict,
  onConflictChange,
}: {
  preview: SkillPackagePreview;
  /** Set when the source matches an existing package (rescan always does). */
  existingPackageName?: string | null;
  selected: ReadonlySet<string>;
  onSelectedChange: (next: Set<string>) => void;
  onConflict: OnConflictChoice;
  onConflictChange: (choice: OnConflictChoice) => void;
}) {
  const { t } = useT("skill-packages");
  const candidates = preview.candidates;
  const sameNames = sameNameGroups(candidates, selected);
  const conflictVisible = hasSelectedConflict(candidates, selected);
  const overwriteAllowed = canApplyOverwrite(candidates, selected);
  const source = preview.source;
  const saved = preview.package;

  const toggle = (path: string, next: boolean) => {
    const updated = new Set(selected);
    if (next) updated.add(path);
    else updated.delete(path);
    onSelectedChange(updated);
  };

  return (
    <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-5 py-4">
      <div className="space-y-1">
        <div className="text-caption text-muted-foreground">
          {t(($) => $.preview.source)}
        </div>
        <div className="flex items-center gap-1.5 text-body">
          <Package className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate font-medium">{source.owner_repo}</span>
          <span className="shrink-0 text-caption text-muted-foreground">
            {source.subdirectory ? `${source.subdirectory} · ` : ""}
            {source.ref}
          </span>
        </div>
        {existingPackageName ? (
          <div className="text-caption text-muted-foreground">
            {t(($) => $.preview.existing_package, { name: existingPackageName })}
          </div>
        ) : null}
        {saved && saved.ref !== source.ref ? (
          <div className="text-caption text-muted-foreground">
            {t(($) => $.preview.ref_change, { from: saved.ref, to: source.ref })}
          </div>
        ) : null}
      </div>

      {preview.diagnostics.length > 0 && (
        <div className="rounded-md bg-warning/10 px-3 py-2">
          <div className="mb-1 flex items-center gap-1.5 text-caption text-muted-foreground">
            <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-warning" />
            {t(($) => $.preview.diagnostics_title)}
          </div>
          <SkillDiagnosticRows diagnostics={preview.diagnostics} />
        </div>
      )}

      <div className="space-y-0.5">
        {candidates.map((candidate) => (
          <CandidateRow
            key={candidate.path}
            candidate={candidate}
            selected={selected.has(candidate.path)}
            onToggle={toggle}
          />
        ))}
      </div>

      {sameNames.length > 0 && (
        <div className="rounded-md bg-warning/10 px-3 py-2 text-caption text-muted-foreground">
          <div className="flex items-start gap-1.5">
            <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
            <span>{t(($) => $.preview.same_name_warning, { name: sameNames.join(", ") })}</span>
          </div>
        </div>
      )}

      {conflictVisible && (
        <fieldset className="space-y-1.5">
          <legend className="text-caption text-muted-foreground">
            {t(($) => $.preview.conflict_label)}
          </legend>
          <div className="flex flex-wrap gap-3">
            {(["skip", "rename", "overwrite"] as const).map((choice) => {
              const disabled = choice === "overwrite" && !overwriteAllowed;
              return (
                <label
                  key={choice}
                  className={cn(
                    "flex items-center gap-1.5 text-body",
                    disabled && "opacity-50",
                  )}
                >
                  <input
                    type="radio"
                    name="package-on-conflict"
                    value={choice}
                    checked={onConflict === choice}
                    disabled={disabled}
                    onChange={() => onConflictChange(choice)}
                  />
                  {t(($) => $.preview[
                    choice === "skip"
                      ? "conflict_skip"
                      : choice === "rename"
                        ? "conflict_rename"
                        : "conflict_overwrite"
                  ])}
                </label>
              );
            })}
          </div>
          {!overwriteAllowed && (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.preview.overwrite_unavailable)}
            </p>
          )}
        </fieldset>
      )}
    </div>
  );
}

/** Per-item apply report. Every candidate appears, including deselected and
 *  failed rows — the server contract requires showing their diagnostics. */
export function PackageApplyReport({
  result,
}: {
  result: SkillPackageApplyResult;
}) {
  const { t } = useT("skill-packages");
  return (
    <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-5 py-4">
      <div
        role="status"
        className={cn(
          "flex items-center gap-1.5 rounded-md px-3 py-2 text-caption",
          result.failed
            ? "bg-destructive/10 text-destructive"
            : "bg-success/10 text-success",
        )}
      >
        {result.failed ? (
          <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
        ) : (
          <CheckCircle2 className="h-3.5 w-3.5 shrink-0" />
        )}
        {result.failed ? t(($) => $.report.partial) : t(($) => $.report.success)}
      </div>

      <div className="space-y-0.5">
        {result.results.map((item) => (
          <div key={item.path} className="rounded-md px-2.5 py-1.5">
            <div className="flex items-center gap-2">
              <span className="min-w-0 flex-1 truncate text-body">
                {item.path || "/"}
              </span>
              <span
                className={cn(
                  "shrink-0 rounded-xs px-1 py-0.5 text-caption",
                  isItemFailure(item)
                    ? "bg-destructive/10 text-destructive"
                    : "bg-muted text-muted-foreground",
                )}
              >
                {t(($) => $.report[itemStatusKey(item.status)])}
              </span>
            </div>
            {(item.reason || item.code) && (
              <div className="text-caption text-muted-foreground">
                {[item.code, item.reason].filter(Boolean).join(" · ")}
              </div>
            )}
            {item.diagnostics.length > 0 && (
              <div className="mt-1">
                <SkillDiagnosticRows diagnostics={item.diagnostics} />
              </div>
            )}
          </div>
        ))}
      </div>

      {result.diagnostics.length > 0 && (
        <div className="rounded-md bg-warning/10 px-3 py-2">
          <div className="mb-1 flex items-center gap-1.5 text-caption text-muted-foreground">
            <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-warning" />
            {t(($) => $.preview.diagnostics_title)}
          </div>
          <SkillDiagnosticRows diagnostics={result.diagnostics} />
        </div>
      )}
    </div>
  );
}
