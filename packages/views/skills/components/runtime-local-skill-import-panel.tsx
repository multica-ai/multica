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
import { useT } from "../../i18n";

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
  const { t } = useT("skills");
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
          {t(($) => $.runtime_import.skill_files, { count: skill.file_count })}
        </Badge>
      </button>

      {selected && (
        <div className="space-y-2.5 border-t bg-card px-4 py-3">
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">
              {t(($) => $.runtime_import.skill_name_label)}
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
              {t(($) => $.runtime_import.skill_description_label)}
            </Label>
            <Textarea
              value={description}
              onChange={(e) => onDescriptionChange(e.target.value)}
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
// Panel
// ---------------------------------------------------------------------------

export function RuntimeLocalSkillImportPanel({
  onImported,
}: {
  onImported?: (skill: Skill) => void;
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
  const [selectedSkillKey, setSelectedSkillKey] = useState<string>("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [importing, setImporting] = useState(false);

  useEffect(() => {
    setSelectedRuntimeId((prev) => prev || localRuntimes[0]?.id || "");
  }, [localRuntimes]);

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
      toast.success(t(($) => $.runtime_import.toast_imported));
      onImported?.(result.skill);
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t(($) => $.runtime_import.toast_import_failed),
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

  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);

  const middle = (() => {
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
        aria-disabled={importing || undefined}
        className={`shrink-0 space-y-2 border-b px-5 py-3 ${
          importing ? "pointer-events-none opacity-60" : ""
        }`}
      >
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">
            {t(($) => $.runtime_import.runtime_label)}
          </Label>
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
        className={`flex-1 min-h-0 overflow-y-auto px-5 py-3 ${
          importing ? "pointer-events-none opacity-60" : ""
        }`}
      >
        {middle}
        <p className="mt-3 text-xs text-muted-foreground">
          {t(($) => $.runtime_import.ignored_files_hint)}
        </p>
      </div>

      {/* Sticky bottom: Import button + context */}
      <div className="flex shrink-0 items-center gap-3 border-t bg-muted/30 px-5 py-3">
        <div className="min-w-0 flex-1 text-xs text-muted-foreground">
          {selectedSkill ? (
            <>
              {t(($) => $.runtime_import.ready)}{" "}
              <span className="font-medium text-foreground">
                {name.trim() || selectedSkill.name}
              </span>{" "}
              {t(($) => $.runtime_import.into_workspace)}
            </>
          ) : (
            t(($) => $.runtime_import.select_skill)
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
              {t(($) => $.runtime_import.importing)}
            </>
          ) : (
            <>
              <Download className="h-3 w-3" />
              {t(($) => $.runtime_import.import_button)}
            </>
          )}
        </Button>
      </div>
    </div>
  );
}
