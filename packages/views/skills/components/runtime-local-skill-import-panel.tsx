"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  AlertTriangle,
  CheckCircle2,
  Download,
  HardDrive,
  Loader2,
  RefreshCw,
  Search,
  SkipForward,
  XCircle,
} from "lucide-react";
import type {
  AgentRuntime,
  RuntimeLocalSkillImportConflict,
  RuntimeLocalSkillSummary,
  Skill,
} from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  runtimeListOptions,
  runtimeLocalSkillsKeys,
  runtimeLocalSkillsOptions,
  resolveRuntimeLocalSkillImport,
} from "@multica/core/runtimes";
import {
  memberListOptions,
  skillDetailOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Progress } from "@multica/ui/components/ui/progress";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import {
  UI_EASE_OUT,
  UI_MOTION_DURATION,
} from "@multica/ui/lib/motion";
import { useT } from "../../i18n";
import { HighlightText } from "../../search/highlight-text";
import { isNameConflictError } from "../lib/utils";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type BulkImportResult = {
  key: string;
  name: string;
  description?: string;
  status: "created" | "updated" | "conflict" | "skipped" | "failed";
  conflict?: RuntimeLocalSkillImportConflict;
  error?: string;
  skill?: Skill;
};

type BulkImportState = {
  phase: "idle" | "importing" | "resolving" | "done" | "cancelled";
  total: number;
  completed: number;
  selectedCount: number;
  results: BulkImportResult[];
};

type ConflictResolutionAction = "overwrite" | "rename" | "skip";

type ConflictResolutionState = {
  action: ConflictResolutionAction;
  renameName: string;
};

const INITIAL_BULK_STATE: BulkImportState = {
  phase: "idle",
  total: 0,
  completed: 0,
  selectedCount: 0,
  results: [],
};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Max concurrent imports. Higher = faster but more daemon/network pressure.
 *
 * Timeout invariant: IMPORT_CONCURRENCY × heartbeat period (~15s) must stay
 * within runtimeLocalSkillPendingTimeout (server/internal/handler/runtime_local_skills.go)
 * and IMPORT_POLL_TIMEOUT_MS (packages/core/runtimes/local-skills.ts).
 * See also maxLocalSkillImportBatch in server/internal/handler/daemon.go.
 */
const IMPORT_CONCURRENCY = 10;

function runtimeLabel(runtime: AgentRuntime): string {
  return `${runtime.name} (${runtime.provider})`;
}

function defaultRenameName(name: string): string {
  return `${name} copy`;
}

function ResultIcon({ status }: { status: BulkImportResult["status"] }) {
  switch (status) {
    case "created":
      return <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-green-600" />;
    case "updated":
      return <RefreshCw className="h-3.5 w-3.5 shrink-0 text-blue-600" />;
    case "conflict":
      return <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-amber-600" />;
    case "skipped":
      return <SkipForward className="h-3.5 w-3.5 shrink-0 text-yellow-600" />;
    case "failed":
      return <AlertCircle className="h-3.5 w-3.5 shrink-0 text-destructive" />;
  }
}

// ---------------------------------------------------------------------------
// Skill row with checkbox
// ---------------------------------------------------------------------------

function SkillItem({
  skill,
  checked,
  onToggle,
  disabled,
  expanded,
  editName,
  editDescription,
  onNameChange,
  onDescriptionChange,
  query = "",
}: {
  skill: RuntimeLocalSkillSummary;
  checked: boolean;
  onToggle: () => void;
  disabled?: boolean;
  expanded?: boolean;
  editName?: string;
  editDescription?: string;
  onNameChange?: (v: string) => void;
  onDescriptionChange?: (v: string) => void;
  /** Active search term; matched substrings are highlighted in this row. */
  query?: string;
}) {
  const { t } = useT("skills");
  return (
    <div
      className={`overflow-hidden rounded-lg border transition-colors ${
        checked ? "border-primary bg-primary/5" : "hover:bg-accent/40"
      } ${disabled ? "pointer-events-none opacity-60" : ""}`}
    >
      <div
        role="button"
        tabIndex={disabled ? -1 : 0}
        onClick={onToggle}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onToggle();
          }
        }}
        className="flex w-full items-start gap-3 px-4 py-3 text-left"
      >
        <Checkbox
          checked={checked}
          tabIndex={-1}
          className="pointer-events-none mt-0.5"
        />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="truncate text-sm font-medium">
              <HighlightText text={skill.name} query={query} />
            </span>
            <Badge variant="secondary">
              <HighlightText text={skill.provider} query={query} />
            </Badge>
          </div>
          {skill.description && (
            // Drop the 2-line clamp while searching so a highlighted match that
            // falls past the first two lines stays visible instead of being
            // clipped out of view.
            <p
              className={`mt-1 text-xs text-muted-foreground ${
                query.trim() ? "" : "line-clamp-2"
              }`}
            >
              <HighlightText text={skill.description} query={query} />
            </p>
          )}
          <p className="mt-1 truncate font-mono text-xs text-muted-foreground">
            <HighlightText text={skill.source_path} query={query} />
          </p>
        </div>
        <Badge variant="outline" className="shrink-0">
          {t(($) => $.runtime_import.skill_files, { count: skill.file_count })}
        </Badge>
      </div>

      {expanded && (
        <div className="space-y-2.5 border-t bg-card px-4 py-3">
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">
              {t(($) => $.runtime_import.skill_name_label)}
            </Label>
            <Input
              value={editName ?? ""}
              onChange={(e) => onNameChange?.(e.target.value)}
              placeholder={skill.name}
              className="h-8 text-sm"
            />
          </div>
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">
              {t(($) => $.runtime_import.skill_description_label)}
            </Label>
            <Textarea
              value={editDescription ?? ""}
              onChange={(e) => onDescriptionChange?.(e.target.value)}
              placeholder={t(($) => $.runtime_import.skill_description_placeholder)}
              rows={2}
              className="resize-none text-sm"
            />
          </div>
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Summary view (shown after bulk import completes)
// ---------------------------------------------------------------------------

function BulkImportSummary({ results }: { results: BulkImportResult[] }) {
  const { t } = useT("skills");
  const created = results.filter((r) => r.status === "created");
  const updated = results.filter((r) => r.status === "updated");
  const conflicts = results.filter((r) => r.status === "conflict");
  const skipped = results.filter((r) => r.status === "skipped");
  const failed = results.filter((r) => r.status === "failed");

  return (
    <div className="space-y-4 py-2">
      {/* Summary counts */}
      <div className="grid grid-cols-2 gap-2 text-center sm:grid-cols-5">
        <div className="rounded-md bg-green-50 px-3 py-2 dark:bg-green-950/30">
          <div className="text-lg font-semibold text-green-700 dark:text-green-400">
            {created.length}
          </div>
          <div className="text-xs text-muted-foreground">
            {t(($) => $.runtime_import.bulk_summary_created)}
          </div>
        </div>
        <div className="rounded-md bg-blue-50 px-3 py-2 dark:bg-blue-950/30">
          <div className="text-lg font-semibold text-blue-700 dark:text-blue-400">
            {updated.length}
          </div>
          <div className="text-xs text-muted-foreground">
            {t(($) => $.runtime_import.bulk_summary_updated)}
          </div>
        </div>
        <div className="rounded-md bg-amber-50 px-3 py-2 dark:bg-amber-950/30">
          <div className="text-lg font-semibold text-amber-700 dark:text-amber-400">
            {conflicts.length}
          </div>
          <div className="text-xs text-muted-foreground">
            {t(($) => $.runtime_import.bulk_summary_conflicts)}
          </div>
        </div>
        <div className="rounded-md bg-yellow-50 px-3 py-2 dark:bg-yellow-950/30">
          <div className="text-lg font-semibold text-yellow-700 dark:text-yellow-400">
            {skipped.length}
          </div>
          <div className="text-xs text-muted-foreground">
            {t(($) => $.runtime_import.bulk_summary_skipped)}
          </div>
        </div>
        <div className="rounded-md bg-red-50 px-3 py-2 dark:bg-red-950/30">
          <div className="text-lg font-semibold text-red-700 dark:text-red-400">
            {failed.length}
          </div>
          <div className="text-xs text-muted-foreground">
            {t(($) => $.runtime_import.bulk_summary_failed)}
          </div>
        </div>
      </div>

      {/* Detailed results list */}
      <div className="max-h-64 space-y-1 overflow-y-auto rounded-md border p-2">
        {results.map((r) => (
          <div
            key={r.key}
            className="flex items-center gap-2 rounded px-2 py-1.5 text-xs"
          >
            <ResultIcon status={r.status} />
            <span className="min-w-0 flex-1 truncate">{r.name}</span>
            {r.error && (
              <span className="max-w-[200px] shrink-0 truncate text-muted-foreground">
                {r.error}
              </span>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

function ConflictResolutionPanel({
  conflicts,
  resolutions,
  onChange,
  onResolveNow,
  onOverwriteAll,
  onSkipAll,
}: {
  conflicts: BulkImportResult[];
  resolutions: Record<string, ConflictResolutionState>;
  onChange: (key: string, next: ConflictResolutionState) => void;
  onResolveNow?: (key: string, next: ConflictResolutionState) => void;
  onOverwriteAll: () => void;
  onSkipAll: () => void;
}) {
  const { t } = useT("skills");
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const single = conflicts.length === 1;
  const canOverwriteAny = conflicts.some((r) => r.conflict?.can_overwrite);

  return (
    <div className="space-y-4 py-2">
      <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-950 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-100">
        <div className="flex items-start gap-2">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
          <div className="min-w-0">
            <p className="font-medium">
              {single
                ? t(($) => $.runtime_import.conflict_single_title)
                : t(($) => $.runtime_import.conflict_bulk_title, {
                    count: conflicts.length,
                  })}
            </p>
            <p className="mt-1 text-xs opacity-85">
              {t(($) => $.runtime_import.conflict_hint)}
            </p>
          </div>
        </div>
      </div>

      {!single && (
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={onOverwriteAll}
            disabled={!canOverwriteAny}
          >
            <RefreshCw className="h-3 w-3" />
            {t(($) => $.runtime_import.conflict_overwrite_all)}
          </Button>
          <Button type="button" size="sm" variant="outline" onClick={onSkipAll}>
            <SkipForward className="h-3 w-3" />
            {t(($) => $.runtime_import.conflict_skip_all)}
          </Button>
        </div>
      )}

      <div className="space-y-2">
        {conflicts.map((r) => {
          const resolution =
            resolutions[r.key] ??
            ({
              action: r.conflict?.can_overwrite ? "overwrite" : "rename",
              renameName: defaultRenameName(r.name),
            } satisfies ConflictResolutionState);
          const creatorId = r.conflict?.existing_created_by;
          const creatorName = creatorId
            ? members.find((m) => m.user_id === creatorId)?.name
            : undefined;
          return (
            <div key={r.key} className="rounded-lg border bg-card p-3">
              <div className="flex items-start gap-2">
                <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-amber-600" />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{r.name}</div>
                  {r.error && (
                    <p className="mt-1 text-xs text-destructive">{r.error}</p>
                  )}
                  {!r.conflict?.can_overwrite && (
                    <p className="mt-1 text-xs text-muted-foreground">
                      {creatorName
                        ? t(($) => $.runtime_import.conflict_locked_creator, {
                            creator: creatorName,
                          })
                        : t(($) => $.runtime_import.conflict_locked)}
                    </p>
                  )}
                </div>
              </div>

              <div className="mt-3 flex flex-wrap gap-2">
                <Button
                  type="button"
                  size="sm"
                  variant={
                    resolution.action === "overwrite" ? "default" : "outline"
                  }
                  onClick={() => {
                    const next = {
                      action: "overwrite",
                      renameName: resolution.renameName,
                    } satisfies ConflictResolutionState;
                    if (single && r.conflict?.can_overwrite && onResolveNow) {
                      onResolveNow(r.key, next);
                    } else {
                      onChange(r.key, next);
                    }
                  }}
                  disabled={!r.conflict?.can_overwrite}
                >
                  <RefreshCw className="h-3 w-3" />
                  {t(($) => $.runtime_import.conflict_overwrite)}
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant={resolution.action === "rename" ? "default" : "outline"}
                  onClick={() =>
                    onChange(r.key, {
                      action: "rename",
                      renameName: resolution.renameName,
                    })
                  }
                >
                  {t(($) => $.runtime_import.conflict_rename)}
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant={resolution.action === "skip" ? "default" : "outline"}
                  onClick={() =>
                    onChange(r.key, {
                      action: "skip",
                      renameName: resolution.renameName,
                    })
                  }
                >
                  <XCircle className="h-3 w-3" />
                  {single
                    ? t(($) => $.runtime_import.conflict_cancel)
                    : t(($) => $.runtime_import.conflict_skip)}
                </Button>
              </div>

              {resolution.action === "rename" && (
                <div className="mt-3 space-y-1">
                  <Label className="text-xs text-muted-foreground">
                    {t(($) => $.runtime_import.conflict_rename_label)}
                  </Label>
                  <Input
                    value={resolution.renameName}
                    onChange={(e) =>
                      onChange(r.key, {
                        action: "rename",
                        renameName: e.target.value,
                      })
                    }
                    className="h-8 text-sm"
                  />
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Panel
// ---------------------------------------------------------------------------

export function RuntimeLocalSkillImportPanel({
  onImported,
  onBulkDone,
}: {
  onImported?: (skill: Skill) => void;
  onBulkDone?: () => void;
}) {
  const { t } = useT("skills");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const shouldReduceMotion = useReducedMotion() ?? false;

  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const localRuntimes = useMemo(
    () =>
      runtimes.filter(
        (r) =>
          r.runtime_mode === "local" &&
          (userId == null || r.owner_id === userId),
      ),
    [runtimes, userId],
  );

  const [selectedRuntimeId, setSelectedRuntimeId] = useState<string>("");
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const [bulkState, setBulkState] = useState<BulkImportState>(INITIAL_BULK_STATE);
  const [conflictResolutions, setConflictResolutions] = useState<
    Record<string, ConflictResolutionState>
  >({});
  const [skillSearchQuery, setSkillSearchQuery] = useState("");
  const cancelRef = useRef(false);
  // Single-select inline edit fields (shown when exactly 1 skill is checked)
  const [editName, setEditName] = useState("");
  const [editDescription, setEditDescription] = useState("");

  const importing = bulkState.phase === "importing";
  const resolvingConflicts = bulkState.phase === "resolving";
  const busy = importing || resolvingConflicts;

  useEffect(() => {
    setSelectedRuntimeId((prev) => prev || localRuntimes[0]?.id || "");
  }, [localRuntimes]);

  useEffect(() => {
    setSelectedKeys(new Set());
    setBulkState(INITIAL_BULK_STATE);
    setConflictResolutions({});
    setSkillSearchQuery("");
    setEditName("");
    setEditDescription("");
  }, [selectedRuntimeId]);

  const selectedRuntime = localRuntimes.find((r) => r.id === selectedRuntimeId);
  const canBrowseSkills =
    !!selectedRuntimeId && selectedRuntime?.status === "online";
  const skillsQuery = useQuery({
    ...runtimeLocalSkillsOptions(selectedRuntimeId || null),
    enabled: canBrowseSkills,
  });
  const runtimeSkills = useMemo(
    () => skillsQuery.data?.skills ?? [],
    [skillsQuery.data],
  );
  const filteredRuntimeSkills = useMemo(() => {
    const query = skillSearchQuery.trim().toLowerCase();
    if (!query) return runtimeSkills;

    // Search only over fields the row actually renders (name, provider,
    // description, path) so every match can be highlighted in the result —
    // `skill.key` is intentionally excluded because it is not displayed, and a
    // key-only match would surface a result with no visible reason.
    return runtimeSkills.filter((skill) =>
      [skill.name, skill.description, skill.provider, skill.source_path].some(
        (value) => value?.toLowerCase().includes(query) ?? false,
      ),
    );
  }, [runtimeSkills, skillSearchQuery]);

  // The single selected skill (for inline editing). Only valid when exactly 1.
  const singleSelectedSkill =
    selectedKeys.size === 1
      ? runtimeSkills.find((s) => selectedKeys.has(s.key))
      : undefined;

  // -- Selection helpers --

  const toggleSkill = (key: string) => {
    setSelectedKeys((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      // Seed edit fields when toggling to exactly-one selection
      if (next.size === 1) {
        const only = runtimeSkills.find((s) => next.has(s.key));
        if (only) {
          setEditName(only.name);
          setEditDescription(only.description ?? "");
        }
      }
      return next;
    });
  };

  const toggleAll = () => {
    const visibleKeys = new Set(filteredRuntimeSkills.map((s) => s.key));
    const visibleSelected =
      visibleKeys.size > 0 &&
      filteredRuntimeSkills.every((s) => selectedKeys.has(s.key));

    setSelectedKeys((prev) => {
      const next = new Set(prev);
      if (visibleSelected) {
        for (const key of visibleKeys) next.delete(key);
      } else {
        for (const key of visibleKeys) next.add(key);
      }
      if (next.size === 1) {
        const only = runtimeSkills.find((s) => next.has(s.key));
        if (only) {
          setEditName(only.name);
          setEditDescription(only.description ?? "");
        }
      }
      return next;
    });
  };

  const allSelected =
    filteredRuntimeSkills.length > 0 &&
    filteredRuntimeSkills.every((s) => selectedKeys.has(s.key));
  const someSelected =
    filteredRuntimeSkills.some((s) => selectedKeys.has(s.key)) && !allSelected;
  const pendingConflicts = bulkState.results.filter(
    (r) => r.status === "conflict" && r.conflict,
  );
  const canApplyConflictResolutions =
    pendingConflicts.length > 0 &&
    pendingConflicts.every((r) => {
      const resolution = conflictResolutions[r.key];
      if (!resolution) return false;
      if (resolution.action === "overwrite") return !!r.conflict?.can_overwrite;
      if (resolution.action === "rename") return !!resolution.renameName.trim();
      return true;
    });

  const setConflictResolution = (
    key: string,
    next: ConflictResolutionState,
  ) => {
    setConflictResolutions((prev) => ({ ...prev, [key]: next }));
  };

  const seedConflictResolutions = (results: BulkImportResult[]) => {
    const next: Record<string, ConflictResolutionState> = {};
    for (const r of results) {
      if (r.status !== "conflict" || !r.conflict) continue;
      next[r.key] = {
        action: r.conflict.can_overwrite ? "overwrite" : "rename",
        renameName: defaultRenameName(r.name),
      };
    }
    setConflictResolutions(next);
  };

  const refreshImportedSkills = async (results: BulkImportResult[]) => {
    await Promise.all([
      qc.invalidateQueries({
        queryKey: runtimeLocalSkillsKeys.forRuntime(selectedRuntimeId),
      }),
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) }),
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) }),
    ]);

    for (const r of results) {
      if ((r.status === "created" || r.status === "updated") && r.skill) {
        qc.setQueryData(
          skillDetailOptions(wsId, r.skill.id).queryKey,
          r.skill,
        );
      }
    }
  };

  // -- Bulk import handler --

  const handleBulkImport = async () => {
    if (!selectedRuntimeId || selectedKeys.size === 0) return;

    const skillsToImport = runtimeSkills.filter((s) => selectedKeys.has(s.key));
    const total = skillsToImport.length;

    cancelRef.current = false;
    setBulkState({
      phase: "importing",
      total,
      completed: 0,
      selectedCount: total,
      results: [],
    });

    const results: BulkImportResult[] = [];

    const importOne = async (skill: RuntimeLocalSkillSummary) => {
      // Single-select: use the user-edited name/description
      const importName =
        total === 1 ? editName.trim() || skill.name : skill.name;
      const importDescription =
        total === 1
          ? editDescription.trim() || skill.description || undefined
          : skill.description || undefined;
      try {
        const result = await resolveRuntimeLocalSkillImport(selectedRuntimeId, {
          skill_key: skill.key,
          name: importName,
          description: importDescription,
          supports_conflict: true,
        });
        if (result.status === "conflict") {
          results.push({
            key: skill.key,
            name: importName,
            description: importDescription,
            status: "conflict",
            conflict: result.conflict,
          });
        } else {
          results.push({
            key: skill.key,
            name: result.skill?.name ?? importName,
            description: importDescription,
            status: result.status,
            skill: result.skill,
          });
        }
      } catch (error) {
        const msg = error instanceof Error ? error.message : "";
        const isSkipped = isNameConflictError(msg);
        results.push({
          key: skill.key,
          name: skill.name,
          description: importDescription,
          status: isSkipped ? "skipped" : "failed",
          error: msg || t(($) => $.runtime_import.toast_import_failed),
        });
      }

      setBulkState((prev) => ({
        ...prev,
        completed: prev.completed + 1,
        results: [...results],
      }));
    };

    // Concurrent pool — up to IMPORT_CONCURRENCY in-flight at once
    const executing = new Set<Promise<void>>();
    for (const skill of skillsToImport) {
      if (cancelRef.current) break;
      const p = importOne(skill).then(() => {
        executing.delete(p);
      });
      executing.add(p);
      if (executing.size >= IMPORT_CONCURRENCY) {
        await Promise.race(executing);
      }
    }
    await Promise.all(executing);

    await refreshImportedSkills(results);

    const conflicts = results.filter((r) => r.status === "conflict");
    if (!cancelRef.current && conflicts.length > 0) {
      seedConflictResolutions(results);
      setBulkState((prev) => ({
        ...prev,
        phase: "resolving",
      }));
      return;
    }

    setBulkState((prev) => ({
      ...prev,
      phase: cancelRef.current ? "cancelled" : "done",
    }));
  };

  const handleApplyConflictResolutions = async (
    resolutionOverrides: Record<string, ConflictResolutionState> = {},
  ) => {
    if (!selectedRuntimeId || pendingConflicts.length === 0) return;

    const conflicts = [...pendingConflicts];
    let nextResults = bulkState.results;
    const applyResult = (key: string, next: Partial<BulkImportResult>) => {
      nextResults = nextResults.map((r) =>
        r.key === key ? { ...r, ...next } : r,
      );
      setBulkState((prev) => ({
        ...prev,
        results: prev.results.map((r) =>
          r.key === key ? { ...r, ...next } : r,
        ),
      }));
    };

    setBulkState((prev) => ({
      ...prev,
      phase: "importing",
      total: conflicts.length,
      completed: 0,
    }));

    for (const r of conflicts) {
      const resolution =
        resolutionOverrides[r.key] ?? conflictResolutions[r.key];
      if (!resolution) {
        applyResult(r.key, {
          status: "failed",
          error: t(($) => $.runtime_import.conflict_missing_resolution),
        });
        setBulkState((prev) => ({ ...prev, completed: prev.completed + 1 }));
        continue;
      }

      if (resolution.action === "skip") {
        applyResult(r.key, { status: "skipped", error: undefined });
        setBulkState((prev) => ({ ...prev, completed: prev.completed + 1 }));
        continue;
      }

      try {
        const result = await resolveRuntimeLocalSkillImport(selectedRuntimeId, {
          skill_key: r.key,
          name:
            resolution.action === "rename"
              ? resolution.renameName.trim()
              : r.name,
          description: r.description,
          supports_conflict: true,
          ...(resolution.action === "overwrite" && r.conflict
            ? {
                action: "overwrite" as const,
                target_skill_id: r.conflict.existing_skill_id,
              }
            : {}),
        });

        if (result.status === "conflict") {
          applyResult(r.key, {
            name:
              resolution.action === "rename"
                ? resolution.renameName.trim()
                : r.name,
            status: "conflict",
            conflict: result.conflict,
            error: t(($) => $.runtime_import.conflict_name_still_exists),
          });
          if (result.conflict) {
            setConflictResolution(r.key, {
              action: result.conflict.can_overwrite ? "overwrite" : "rename",
              renameName: defaultRenameName(
                resolution.action === "rename"
                  ? resolution.renameName.trim()
                  : r.name,
              ),
            });
          }
        } else {
          applyResult(r.key, {
            name: result.skill?.name ?? r.name,
            status: result.status,
            skill: result.skill,
            conflict: undefined,
            error: undefined,
          });
        }
      } catch (error) {
        const msg = error instanceof Error ? error.message : "";
        applyResult(r.key, {
          status: "failed",
          error: msg || t(($) => $.runtime_import.toast_import_failed),
        });
      }

      setBulkState((prev) => ({ ...prev, completed: prev.completed + 1 }));
    }

    await refreshImportedSkills(nextResults);
    const unresolved = nextResults.some((r) => r.status === "conflict");
    setBulkState((prev) => ({
      ...prev,
      results: nextResults,
      phase: unresolved ? "resolving" : "done",
    }));
  };

  const canImport =
    !!selectedRuntime &&
    selectedRuntime.status === "online" &&
    selectedKeys.size > 0 &&
    // Single-select requires a non-empty name (user may be renaming)
    (selectedKeys.size > 1 || !!editName.trim()) &&
    !busy;

  const handleCancel = () => {
    cancelRef.current = true;
  };

  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);

  // -- Middle content --

  const middle = (() => {
    // Progress view during bulk import
    if (bulkState.phase === "importing") {
      const pct =
        bulkState.total > 0
          ? Math.round((bulkState.completed / bulkState.total) * 100)
          : 0;
      return (
        <div className="space-y-4 py-4">
          <div className="text-center">
            <Loader2 className="mx-auto h-6 w-6 animate-spin text-primary" />
            <p className="mt-3 text-sm font-medium">
              {t(($) => $.runtime_import.bulk_progress, {
                completed: bulkState.completed,
                total: bulkState.total,
              })}
            </p>
          </div>
          <Progress value={pct} />
          {/* Live result feed */}
          <div className="max-h-48 space-y-1 overflow-y-auto">
            {bulkState.results.map((r) => (
              <div
                key={r.key}
                className="flex items-center gap-2 rounded px-2 py-1 text-xs"
              >
                <ResultIcon status={r.status} />
                <span className="truncate">{r.name}</span>
              </div>
            ))}
          </div>
        </div>
      );
    }

    // Summary view after bulk import (complete or cancelled)
    if (bulkState.phase === "done" || bulkState.phase === "cancelled") {
      return <BulkImportSummary results={bulkState.results} />;
    }

    if (bulkState.phase === "resolving") {
      return (
        <ConflictResolutionPanel
          conflicts={pendingConflicts}
          resolutions={conflictResolutions}
          onChange={setConflictResolution}
          onResolveNow={(key, next) => {
            setConflictResolution(key, next);
            void handleApplyConflictResolutions({ [key]: next });
          }}
          onOverwriteAll={() => {
            setConflictResolutions((prev) => {
              const next = { ...prev };
              for (const r of pendingConflicts) {
                if (!r.conflict?.can_overwrite) continue;
                next[r.key] = {
                  action: "overwrite",
                  renameName: prev[r.key]?.renameName ?? defaultRenameName(r.name),
                };
              }
              return next;
            });
          }}
          onSkipAll={() => {
            setConflictResolutions((prev) => {
              const next = { ...prev };
              for (const r of pendingConflicts) {
                next[r.key] = {
                  action: "skip",
                  renameName: prev[r.key]?.renameName ?? defaultRenameName(r.name),
                };
              }
              return next;
            });
          }}
        />
      );
    }

    // --- Idle phase: skill selection list ---

    if (localRuntimes.length === 0) {
      return (
        <div className="rounded-lg border border-dashed px-4 py-10 text-center">
          <p className="text-sm text-muted-foreground">
            {t(($) => $.runtime_import.no_local_runtimes_title)}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {t(($) => $.runtime_import.no_local_runtimes_hint)}
          </p>
        </div>
      );
    }
    if (!selectedRuntime) {
      return (
        <div className="rounded-lg border border-dashed px-4 py-10 text-center">
          <p className="text-sm text-muted-foreground">
            {t(($) => $.runtime_import.choose_runtime)}
          </p>
        </div>
      );
    }
    if (selectedRuntime.status !== "online") {
      return (
        <div className="flex items-start gap-2 rounded-md bg-warning/10 px-3 py-2 text-xs text-muted-foreground">
          <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
          {t(($) => $.runtime_import.must_be_online)}
        </div>
      );
    }
    if (skillsQuery.isLoading) {
      return (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="rounded-lg border px-4 py-3">
              <Skeleton className="h-4 w-32" />
              <Skeleton className="mt-2 h-3 w-48" />
            </div>
          ))}
        </div>
      );
    }
    if (skillsQuery.error) {
      return (
        <div className="flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive">
          <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          {skillsQuery.error instanceof Error
            ? skillsQuery.error.message
            : t(($) => $.runtime_import.load_failed)}
        </div>
      );
    }
    if (!skillsQuery.data?.supported) {
      return (
        <div className="flex items-start gap-2 rounded-md bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
          <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          {t(($) => $.runtime_import.not_supported)}
        </div>
      );
    }
    if (runtimeSkills.length === 0) {
      return (
        <div className="rounded-lg border border-dashed px-4 py-10 text-center">
          <p className="text-sm text-muted-foreground">
            {t(($) => $.runtime_import.no_skills_title)}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {t(($) => $.runtime_import.no_skills_hint)}
          </p>
        </div>
      );
    }
    return (
      <div className="flex flex-col gap-2">
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={skillSearchQuery}
            onChange={(e) => setSkillSearchQuery(e.target.value)}
            placeholder={t(($) => $.runtime_import.search_placeholder)}
            className="h-9 pl-8 text-sm"
          />
        </div>

        {filteredRuntimeSkills.length === 0 ? (
          <div className="rounded-lg border border-dashed px-4 py-8 text-center">
            <p className="text-sm text-muted-foreground">
              {t(($) => $.runtime_import.no_search_results_title)}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              {t(($) => $.runtime_import.no_search_results_hint, {
                query: skillSearchQuery.trim(),
              })}
            </p>
          </div>
        ) : (
          <>
            {/* Select all header */}
            <label className="flex cursor-pointer items-center gap-2 px-1 py-1">
              <input
                type="checkbox"
                checked={allSelected}
                ref={(el) => {
                  if (el) el.indeterminate = someSelected;
                }}
                onChange={toggleAll}
                className="cursor-pointer accent-primary"
              />
              <span className="text-xs text-muted-foreground">
                {t(($) => $.runtime_import.select_all, {
                  count: filteredRuntimeSkills.length,
                })}
              </span>
            </label>

            {filteredRuntimeSkills.map((s) => (
              <SkillItem
                key={s.key}
                skill={s}
                checked={selectedKeys.has(s.key)}
                onToggle={() => toggleSkill(s.key)}
                disabled={importing}
                expanded={singleSelectedSkill?.key === s.key}
                editName={
                  singleSelectedSkill?.key === s.key ? editName : undefined
                }
                editDescription={
                  singleSelectedSkill?.key === s.key
                    ? editDescription
                    : undefined
                }
                onNameChange={setEditName}
                onDescriptionChange={setEditDescription}
                query={skillSearchQuery}
              />
            ))}
          </>
        )}
      </div>
    );
  })();

  // -- Handle "Done" button after import --

  const handleDone = () => {
    const succeeded = bulkState.results.filter(
      (r) => r.status === "created" || r.status === "updated",
    );
    // Single-import flow: navigate to the imported skill detail page.
    // Multi-import flow: close the dialog even if only one succeeded.
    if (
      bulkState.selectedCount === 1 &&
      succeeded.length === 1 &&
      succeeded[0]!.skill
    ) {
      onImported?.(succeeded[0]!.skill);
    } else {
      onBulkDone?.();
    }
  };

  const footerContent =
    bulkState.phase === "done" || bulkState.phase === "cancelled" ? (
      <>
        <div className="min-w-0 flex-1 text-xs text-muted-foreground">
          {bulkState.phase === "cancelled"
            ? t(($) => $.runtime_import.bulk_cancelled_hint)
            : t(($) => $.runtime_import.bulk_complete_hint)}
        </div>
        <Button type="button" size="sm" onClick={handleDone}>
          {t(($) => $.runtime_import.bulk_done_button)}
        </Button>
      </>
    ) : resolvingConflicts ? (
      <>
        <div className="min-w-0 flex-1 text-xs text-muted-foreground">
          {t(($) => $.runtime_import.conflict_footer, {
            count: pendingConflicts.length,
          })}
        </div>
        <Button
          type="button"
          size="sm"
          onClick={() => void handleApplyConflictResolutions()}
          disabled={!canApplyConflictResolutions}
        >
          {t(($) => $.runtime_import.conflict_apply_button)}
        </Button>
      </>
    ) : importing ? (
      <>
        <div className="min-w-0 flex-1 text-xs text-muted-foreground">
          {t(($) => $.runtime_import.bulk_progress, {
            completed: bulkState.completed,
            total: bulkState.total,
          })}
        </div>
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={handleCancel}
        >
          {t(($) => $.runtime_import.bulk_cancel_button)}
        </Button>
      </>
    ) : (
      <>
        <div className="min-w-0 flex-1 text-xs text-muted-foreground">
          {singleSelectedSkill ? (
            <>
              {t(($) => $.runtime_import.ready)}{" "}
              <span className="font-medium text-foreground">
                {editName.trim() || singleSelectedSkill.name}
              </span>{" "}
              {t(($) => $.runtime_import.into_workspace)}
            </>
          ) : selectedKeys.size > 1 ? (
            t(($) => $.runtime_import.bulk_ready, {
              count: selectedKeys.size,
            })
          ) : (
            t(($) => $.runtime_import.select_skill)
          )}
        </div>
        <Button
          type="button"
          size="sm"
          onClick={handleBulkImport}
          disabled={!canImport}
        >
          <Download className="h-3 w-3" />
          {selectedKeys.size > 1
            ? t(($) => $.runtime_import.bulk_import_button, {
                count: selectedKeys.size,
              })
            : t(($) => $.runtime_import.import_button)}
        </Button>
      </>
    );

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* Sticky top: runtime picker + status */}
      <div
        aria-disabled={busy || undefined}
        className={`shrink-0 space-y-2 border-b px-5 py-3 ${
          busy ? "pointer-events-none opacity-60" : ""
        }`}
      >
        <div className="space-y-1.5">
          <label className="text-xs text-muted-foreground">
            {t(($) => $.runtime_import.runtime_label)}
          </label>
          <Select
            items={localRuntimes.map((runtime) => ({
              value: runtime.id,
              label: runtimeLabel(runtime),
            }))}
            value={selectedRuntimeId}
            onValueChange={(v) => v && setSelectedRuntimeId(v)}
          >
            <SelectTrigger className="w-full">
              <SelectValue placeholder={t(($) => $.runtime_import.runtime_placeholder)}>
                {selectedRuntime ? runtimeLabel(selectedRuntime) : null}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {localRuntimes.map((r) => (
                <SelectItem key={r.id} value={r.id}>
                  {runtimeLabel(r)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {selectedRuntime && (
          <div className="flex items-center gap-2 rounded-md border bg-muted/20 px-3 py-1.5 text-xs text-muted-foreground">
            <HardDrive className="h-3.5 w-3.5 shrink-0" />
            <span className="min-w-0 flex-1 truncate">
              {runtimeLabel(selectedRuntime)}
            </span>
            <Badge
              variant={
                selectedRuntime.status === "online" ? "secondary" : "outline"
              }
            >
              {selectedRuntime.status}
            </Badge>
          </div>
        )}
      </div>

      {/* Scrollable middle */}
      <div
        ref={scrollRef}
        style={fadeStyle}
        aria-disabled={importing || undefined}
        className={`min-h-0 flex-1 overflow-y-auto px-5 py-3 ${
          importing && bulkState.phase !== "importing" ? "pointer-events-none opacity-60" : ""
        }`}
      >
        <AnimatePresence mode="wait" initial={false}>
          <motion.div
            key={bulkState.phase}
            initial={{
              opacity: 0,
              transform: shouldReduceMotion
                ? "translateY(0)"
                : "translateY(8px)",
            }}
            animate={{
              opacity: 1,
              transform: "translateY(0)",
              transition: {
                duration: shouldReduceMotion
                  ? UI_MOTION_DURATION.fast
                  : UI_MOTION_DURATION.standard,
                ease: UI_EASE_OUT,
              },
            }}
            exit={{
              opacity: 0,
              transform: shouldReduceMotion
                ? "translateY(0)"
                : "translateY(-8px)",
              transition: {
                duration: shouldReduceMotion
                  ? UI_MOTION_DURATION.fast
                  : UI_MOTION_DURATION.micro,
                ease: UI_EASE_OUT,
              },
            }}
          >
            {middle}
            {bulkState.phase === "idle" && (
              <p className="mt-3 text-xs text-muted-foreground">
                {t(($) => $.runtime_import.ignored_files_hint)}
              </p>
            )}
          </motion.div>
        </AnimatePresence>
      </div>

      {/* Sticky bottom: contextual actions per phase */}
      <div className="flex shrink-0 items-center gap-3 border-t bg-muted/30 px-5 py-3">
        <AnimatePresence mode="wait" initial={false}>
          <motion.div
            key={bulkState.phase}
            className="flex min-w-0 flex-1 items-center gap-3"
            initial={{ opacity: 0 }}
            animate={{
              opacity: 1,
              transition: {
                duration: UI_MOTION_DURATION.fast,
                ease: UI_EASE_OUT,
              },
            }}
            exit={{
              opacity: 0,
              transition: {
                duration: UI_MOTION_DURATION.micro,
                ease: UI_EASE_OUT,
              },
            }}
          >
            {footerContent}
          </motion.div>
        </AnimatePresence>
      </div>
    </div>
  );
}
