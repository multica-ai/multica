"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, Download, FileText, HardDrive, Loader2 } from "lucide-react";
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
import { useT } from "@multica/i18n/react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { toast } from "sonner";

function runtimeLabel(runtime: AgentRuntime): string {
  return `${runtime.name} (${runtime.provider})`;
}

// ---------------------------------------------------------------------------
// Skill row with inline-expanded name/description editor when selected
// ---------------------------------------------------------------------------

function SkillItem({
  skill,
  selected,
  onSelect,
  name,
  description,
  onNameChange,
  onDescriptionChange,
}: {
  skill: RuntimeLocalSkillSummary;
  selected: boolean;
  onSelect: () => void;
  name: string;
  description: string;
  onNameChange: (v: string) => void;
  onDescriptionChange: (v: string) => void;
}) {
  const t = useT("skills");

  return (
    <div
      className={`overflow-hidden rounded-lg border transition-colors ${
        selected ? "border-primary bg-primary/5" : "hover:bg-accent/40"
      }`}
    >
      <button
        type="button"
        onClick={onSelect}
        className="flex w-full items-start gap-3 px-4 py-3 text-left"
      >
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-muted">
          <FileText className="h-4 w-4 text-muted-foreground" />
        </div>
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
          {skill.file_count === 1
            ? t("import_files_count", { count: skill.file_count })
            : t("import_files_count_plural", { count: skill.file_count })}
        </Badge>
      </button>

      {selected && (
        <div className="space-y-2.5 border-t bg-card px-4 py-3">
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">
              {t("import_workspace_name")}
            </Label>
            <Input
              value={name}
              onChange={(e) => onNameChange(e.target.value)}
              placeholder={skill.name}
              className="h-8 text-sm"
            />
          </div>
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">
              {t("description_label")}
            </Label>
            <Textarea
              value={description}
              onChange={(e) => onDescriptionChange(e.target.value)}
              placeholder={t("import_desc_placeholder")}
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
// Panel — three-section layout: sticky top / scrollable middle / sticky bottom
//
// Previously took an `active` prop to defer work inside a tabbed parent; the
// parent now unmounts the panel when it's not the active method, so `active`
// is always implicitly true here.
// ---------------------------------------------------------------------------

export function RuntimeLocalSkillImportPanel({
  onImported,
}: {
  onImported?: (skill: Skill) => void;
}) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const t = useT("skills");

  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  // Only the runtime owner can browse + import local skills (server-side ACL).
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
  const [selectedSkillKey, setSelectedSkillKey] = useState<string>("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [importing, setImporting] = useState(false);

  // Default to the first local runtime once the list lands.
  useEffect(() => {
    setSelectedRuntimeId((prev) => prev || localRuntimes[0]?.id || "");
  }, [localRuntimes]);

  // Switching runtimes: clear the stale skill selection immediately so the
  // old highlight / inline editor don't flash during the next query's fetch
  // window. The auto-seed effect below picks the new first-skill once the
  // scan lands.
  useEffect(() => {
    setSelectedSkillKey("");
    setName("");
    setDescription("");
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
  const selectedSkill = runtimeSkills.find((s) => s.key === selectedSkillKey);

  // After a scan, auto-select the first skill so the Import button has a
  // valid target without requiring a click.
  useEffect(() => {
    if (runtimeSkills.length === 0) return;
    if (runtimeSkills.some((s) => s.key === selectedSkillKey)) return;
    const first = runtimeSkills[0]!;
    setSelectedSkillKey(first.key);
    setName(first.name);
    setDescription(first.description ?? "");
  }, [runtimeSkills, selectedSkillKey]);

  const handleRowSelect = (s: RuntimeLocalSkillSummary) => {
    setSelectedSkillKey(s.key);
    setName(s.name);
    setDescription(s.description ?? "");
  };

  const handleImport = async () => {
    if (!selectedRuntimeId || !selectedSkill) return;
    setImporting(true);
    try {
      const result = await resolveRuntimeLocalSkillImport(selectedRuntimeId, {
        skill_key: selectedSkill.key,
        name: name.trim() || undefined,
        description: description.trim() || undefined,
      });
      // Seed the detail cache so navigation lands with data pre-populated.
      qc.setQueryData(
        skillDetailOptions(wsId, result.skill.id).queryKey,
        result.skill,
      );
      await Promise.all([
        qc.invalidateQueries({
          queryKey: runtimeLocalSkillsKeys.forRuntime(selectedRuntimeId),
        }),
        qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) }),
        qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) }),
      ]);
      toast.success(t("toast_imported"));
      onImported?.(result.skill);
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t("import_failed"),
      );
    } finally {
      setImporting(false);
    }
  };

  const canImport =
    !!selectedRuntime &&
    selectedRuntime.status === "online" &&
    !!selectedSkill &&
    !!name.trim() &&
    !importing;

  // --- Scroll fade for the middle region ---
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);

  // --- Middle body — depends on discovery state ---
  const middle = (() => {
    if (localRuntimes.length === 0) {
      return (
        <div className="rounded-lg border border-dashed px-4 py-10 text-center">
          <p className="text-sm text-muted-foreground">
            {t("import_no_runtimes")}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {t("import_no_runtimes_desc")}
          </p>
        </div>
      );
    }
    if (!selectedRuntime) {
      return (
        <div className="rounded-lg border border-dashed px-4 py-10 text-center">
          <p className="text-sm text-muted-foreground">
            {t("import_choose_runtime")}
          </p>
        </div>
      );
    }
    if (selectedRuntime.status !== "online") {
      return (
        <div className="flex items-start gap-2 rounded-md bg-warning/10 px-3 py-2 text-xs text-muted-foreground">
          <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
          {t("import_runtime_offline")}
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
            : t("import_failed_load")}
        </div>
      );
    }
    if (!skillsQuery.data?.supported) {
      return (
        <div className="flex items-start gap-2 rounded-md bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
          <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          {t("import_no_inventory")}
        </div>
      );
    }
    if (runtimeSkills.length === 0) {
      return (
        <div className="rounded-lg border border-dashed px-4 py-10 text-center">
          <p className="text-sm text-muted-foreground">{t("import_no_skills")}</p>
          <p className="mt-1 text-xs text-muted-foreground">
            {t("import_no_skills")}
          </p>
        </div>
      );
    }
    return (
      <div className="space-y-2">
        {runtimeSkills.map((s) => (
          <SkillItem
            key={s.key}
            skill={s}
            selected={selectedSkillKey === s.key}
            onSelect={() => handleRowSelect(s)}
            name={selectedSkillKey === s.key ? name : ""}
            description={selectedSkillKey === s.key ? description : ""}
            onNameChange={setName}
            onDescriptionChange={setDescription}
          />
        ))}
      </div>
    );
  })();

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* Sticky top: runtime picker + status */}
      <div
        // While importing, lock the whole runtime/skill selection so the user
        // can't switch targets out from under the in-flight request.
        aria-disabled={importing || undefined}
        className={`shrink-0 space-y-2 border-b px-5 py-3 ${
          importing ? "pointer-events-none opacity-60" : ""
        }`}
      >
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">{t("import_runtime")}</Label>
          <Select
            value={selectedRuntimeId}
            onValueChange={(v) => v && setSelectedRuntimeId(v)}
          >
            <SelectTrigger className="w-full">
              <SelectValue placeholder={t("import_select_runtime")}>
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

      {/* Scrollable middle — also locked during import. */}
      <div
        ref={scrollRef}
        style={fadeStyle}
        aria-disabled={importing || undefined}
        className={`flex-1 min-h-0 overflow-y-auto px-5 py-3 ${
          importing ? "pointer-events-none opacity-60" : ""
        }`}
      >
        {middle}
        <p className="mt-3 text-xs text-muted-foreground">
          {t("import_footer_note")}
        </p>
      </div>

      {/* Sticky bottom: Import button + context */}
      <div className="flex shrink-0 items-center gap-3 border-t bg-muted/30 px-5 py-3">
        <div className="min-w-0 flex-1 text-xs text-muted-foreground">
          {selectedSkill ? (
            <>
              {t("import_ready")}{" "}
              <span className="font-medium text-foreground">
                {name.trim() || selectedSkill.name}
              </span>
            </>
          ) : (
            t("import_select")
          )}
        </div>
        <Button
          type="button"
          size="sm"
          onClick={handleImport}
          disabled={!canImport}
        >
          {importing ? (
            <>
              <Loader2 className="h-3 w-3 animate-spin" />
              {t("create_url_importing")}
            </>
          ) : (
            <>
              <Download className="h-3 w-3" />
              {t("import_button")}
            </>
          )}
        </Button>
      </div>
    </div>
  );
}
