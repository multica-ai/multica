"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  CheckCircle2,
  Download,
  HardDrive,
  Loader2,
  SkipForward,
} from "lucide-react";
import type {
  AgentRuntime,
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
import { useT } from "../../i18n";
import { isNameConflictError } from "../lib/utils";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type BulkImportResult = {
  key: string;
  name: string;
  status: "success" | "skipped" | "failed";
  error?: string;
  skill?: Skill;
};

type BulkImportState = {
  phase: "idle" | "importing" | "done" | "cancelled";
  total: number;
  completed: number;
  results: BulkImportResult[];
};

const INITIAL_BULK_STATE: BulkImportState = {
  phase: "idle",
  total: 0,
  completed: 0,
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
            <span className="truncate text-sm font-medium">{skill.name}</span>
            <Badge variant="secondary">{skill.provider}</Badge>
          </div>
          {skill.description && (
            <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">
              {skill.description}
            </p>
          )}
          <p className="mt-1 truncate font-mono text-xs text-muted-foreground">
            {skill.source_path}
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
  const succeeded = results.filter((r) => r.status === "success");
  const skipped = results.filter((r) => r.status === "skipped");
  const failed = results.filter((r) => r.status === "failed");

  return (
    <div className="space-y-4 py-2">
      {/* Summary counts */}
      <div className="grid grid-cols-3 gap-2 text-center">
        <div className="rounded-md bg-green-50 px-3 py-2 dark:bg-green-950/30">
          <div className="text-lg font-semibold text-green-700 dark:text-green-400">
            {succeeded.length}
          </div>
          <div className="text-xs text-muted-foreground">
            {t(($) => $.runtime_import.bulk_summary_imported)}
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
            {r.status === "success" && (
              <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-green-600" />
            )}
            {r.status === "skipped" && (
              <SkipForward className="h-3.5 w-3.5 shrink-0 text-yellow-600" />
            )}
            {r.status === "failed" && (
              <AlertCircle className="h-3.5 w-3.5 shrink-0 text-destructive" />
            )}
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
  const cancelRef = useRef(false);
  // Single-select inline edit fields (shown when exactly 1 skill is checked)
  const [editName, setEditName] = useState("");
  const [editDescription, setEditDescription] = useState("");

  const importing = bulkState.phase === "importing";

  useEffect(() => {
    setSelectedRuntimeId((prev) => prev || localRuntimes[0]?.id || "");
  }, [localRuntimes]);

  useEffect(() => {
    setSelectedKeys(new Set());
    setBulkState(INITIAL_BULK_STATE);
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
    if (selectedKeys.size === runtimeSkills.length) {
      setSelectedKeys(new Set());
    } else {
      setSelectedKeys(new Set(runtimeSkills.map((s) => s.key)));
    }
  };

  const allSelected =
    runtimeSkills.length > 0 && selectedKeys.size === runtimeSkills.length;
  const someSelected = selectedKeys.size > 0 && !allSelected;

  // -- Bulk import handler --

  const handleBulkImport = async () => {
    if (!selectedRuntimeId || selectedKeys.size === 0) return;

    const skillsToImport = runtimeSkills.filter((s) => selectedKeys.has(s.key));
    const total = skillsToImport.length;

    cancelRef.current = false;
    setBulkState({ phase: "importing", total, completed: 0, results: [] });

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
        });
        results.push({
          key: skill.key,
          name: importName,
          status: "success",
          skill: result.skill,
        });
      } catch (error) {
        const msg = error instanceof Error ? error.message : "";
        const isSkipped = isNameConflictError(msg);
        results.push({
          key: skill.key,
          name: skill.name,
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

    // Invalidate queries ONCE at the end
    await Promise.all([
      qc.invalidateQueries({
        queryKey: runtimeLocalSkillsKeys.forRuntime(selectedRuntimeId),
      }),
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) }),
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) }),
    ]);

    // Seed query cache for each successfully imported skill
    for (const r of results) {
      if (r.status === "success" && r.skill) {
        qc.setQueryData(
          skillDetailOptions(wsId, r.skill.id).queryKey,
          r.skill,
        );
      }
    }

    setBulkState((prev) => ({
      ...prev,
      phase: cancelRef.current ? "cancelled" : "done",
    }));
  };

  const canImport =
    !!selectedRuntime &&
    selectedRuntime.status === "online" &&
    selectedKeys.size > 0 &&
    // Single-select requires a non-empty name (user may be renaming)
    (selectedKeys.size > 1 || !!editName.trim()) &&
    !importing;

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
                {r.status === "success" && (
                  <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-green-600" />
                )}
                {r.status === "skipped" && (
                  <SkipForward className="h-3.5 w-3.5 shrink-0 text-yellow-600" />
                )}
                {r.status === "failed" && (
                  <AlertCircle className="h-3.5 w-3.5 shrink-0 text-destructive" />
                )}
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
      <div className="space-y-2">
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
              count: runtimeSkills.length,
            })}
          </span>
        </label>

        {runtimeSkills.map((s) => (
          <SkillItem
            key={s.key}
            skill={s}
            checked={selectedKeys.has(s.key)}
            onToggle={() => toggleSkill(s.key)}
            disabled={importing}
            expanded={singleSelectedSkill?.key === s.key}
            editName={singleSelectedSkill?.key === s.key ? editName : undefined}
            editDescription={
              singleSelectedSkill?.key === s.key ? editDescription : undefined
            }
            onNameChange={setEditName}
            onDescriptionChange={setEditDescription}
          />
        ))}
      </div>
    );
  })();

  // -- Handle "Done" button after import --

  const handleDone = () => {
    const succeeded = bulkState.results.filter((r) => r.status === "success");
    // Single-import flow: navigate to the imported skill detail page.
    // Multi-import flow: close the dialog even if only one succeeded.
    if (bulkState.total === 1 && succeeded.length === 1 && succeeded[0]!.skill) {
      onImported?.(succeeded[0]!.skill);
    } else {
      onBulkDone?.();
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* Sticky top: runtime picker + status */}
      <div
        aria-disabled={importing || undefined}
        className={`shrink-0 space-y-2 border-b px-5 py-3 ${
          importing ? "pointer-events-none opacity-60" : ""
        }`}
      >
        <div className="space-y-1.5">
          <label className="text-xs text-muted-foreground">
            {t(($) => $.runtime_import.runtime_label)}
          </label>
          <Select
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
          importing && bulkState.phase !== "importing"
            ? "pointer-events-none opacity-60"
            : ""
        }`}
      >
        {middle}
        {bulkState.phase === "idle" && (
          <p className="mt-3 text-xs text-muted-foreground">
            {t(($) => $.runtime_import.ignored_files_hint)}
          </p>
        )}
      </div>

      {/* Sticky bottom: contextual actions per phase */}
      <div className="flex shrink-0 items-center gap-3 border-t bg-muted/30 px-5 py-3">
        {bulkState.phase === "done" || bulkState.phase === "cancelled" ? (
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
        )}
      </div>
    </div>
  );
}
