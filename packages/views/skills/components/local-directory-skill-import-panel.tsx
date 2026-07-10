"use client";

import { useRef, useState } from "react";
import {
  AlertCircle,
  CheckCircle2,
  Download,
  FolderOpen,
  Loader2,
  SkipForward,
} from "lucide-react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { parseFrontmatter } from "@multica/core/skills/frontmatter";
import type { Skill } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  skillDetailOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Progress } from "@multica/ui/components/ui/progress";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { useT } from "../../i18n";
import { isNameConflictError } from "../lib/utils";

const MAX_LOCAL_SKILL_FILE_SIZE = 1 << 20;
const MAX_LOCAL_SKILL_BUNDLE_SIZE = 8 << 20;
const MAX_LOCAL_SKILL_FILE_COUNT = 128;

type LocalDirectorySkillFile = {
  path: string;
  content: string;
};

type LocalDirectorySkillCandidate = {
  key: string;
  name: string;
  description: string;
  content: string;
  sourcePath: string;
  fileCount: number;
  files: LocalDirectorySkillFile[];
};

type DirectoryScanResult = {
  rootLabel: string;
  skills: LocalDirectorySkillCandidate[];
};

type LocalDirectoryImportResult = {
  key: string;
  name: string;
  status: "created" | "skipped" | "failed";
  error?: string;
  skill?: Skill;
};

type LocalDirectoryImportState = {
  phase: "idle" | "loading" | "importing" | "done";
  total: number;
  completed: number;
  selectedCount: number;
  results: LocalDirectoryImportResult[];
};

const INITIAL_IMPORT_STATE: LocalDirectoryImportState = {
  phase: "idle",
  total: 0,
  completed: 0,
  selectedCount: 0,
  results: [],
};

type BrowserFile = File & { webkitRelativePath?: string };

function normalizePath(path: string): string {
  return path
    .replace(/\\/g, "/")
    .replace(/^\/+/, "")
    .split("/")
    .filter(Boolean)
    .join("/");
}

function dirname(path: string): string {
  const parts = path.split("/");
  parts.pop();
  return parts.join("/");
}

function basename(path: string): string {
  const clean = normalizePath(path);
  if (!clean) return "";
  return clean.split("/").pop() ?? "";
}

function isUnderDir(path: string, dir: string): boolean {
  if (!dir) return true;
  return path === dir || path.startsWith(`${dir}/`);
}

function relativeToDir(path: string, dir: string): string {
  if (!dir) return path;
  return path.slice(dir.length + 1);
}

function isIgnoredPath(path: string): boolean {
  const parts = normalizePath(path).split("/");
  return parts.some((part) => {
    if (!part) return true;
    if (part.startsWith(".")) return true;
    const lower = part.toLowerCase();
    return lower === "license" || lower === "license.md" || lower === "license.txt";
  });
}

function stripSelectedRoot(entries: Array<{ file: File; path: string }>) {
  const firstSegments = entries
    .map((entry) => entry.path.split("/"))
    .filter((parts) => parts.length > 1)
    .map((parts) => parts[0]);
  const rootLabel = firstSegments[0] ?? "";
  const canStrip =
    !!rootLabel &&
    firstSegments.length === entries.length &&
    firstSegments.every((part) => part === rootLabel);

  return {
    rootLabel,
    entries: entries.map((entry) => ({
      file: entry.file,
      path: canStrip ? entry.path.split("/").slice(1).join("/") : entry.path,
    })),
  };
}

async function discoverLocalDirectorySkills(
  files: File[],
): Promise<DirectoryScanResult> {
  const rawEntries = files
    .map((file) => {
      const browserFile = file as BrowserFile;
      return {
        file,
        path: normalizePath(browserFile.webkitRelativePath || file.name),
      };
    })
    .filter((entry) => entry.path);

  const { rootLabel, entries } = stripSelectedRoot(rawEntries);
  const skillDirs = entries
    .filter((entry) => basename(entry.path).toLowerCase() === "skill.md")
    .map((entry) => dirname(entry.path))
    .sort((a, b) => a.split("/").length - b.split("/").length || a.localeCompare(b));

  const selectedSkillDirs: string[] = [];
  for (const dir of skillDirs) {
    const nestedUnderExisting = selectedSkillDirs.some(
      (existing) => existing !== dir && isUnderDir(dir, existing),
    );
    if (!nestedUnderExisting) selectedSkillDirs.push(dir);
  }

  const skills: LocalDirectorySkillCandidate[] = [];
  for (const dir of selectedSkillDirs) {
    const mainPath = dir ? `${dir}/SKILL.md` : "SKILL.md";
    const mainEntry = entries.find((entry) => entry.path === mainPath);
    if (!mainEntry || mainEntry.file.size > MAX_LOCAL_SKILL_FILE_SIZE) {
      continue;
    }

    const content = await mainEntry.file.text();
    const parsed = parseFrontmatter(content).frontmatter;
    const key = dir || rootLabel || "skill";
    const name = parsed?.name?.trim() || basename(key) || "skill";
    const description = parsed?.description?.trim() || "";
    const descendantSkillDirs = selectedSkillDirs.filter(
      (other) => other !== dir && isUnderDir(other, dir),
    );

    const supportingEntries = entries
      .filter((entry) => {
        if (entry.path === mainPath) return false;
        if (!isUnderDir(entry.path, dir)) return false;
        if (descendantSkillDirs.some((child) => isUnderDir(entry.path, child))) {
          return false;
        }
        const rel = relativeToDir(entry.path, dir);
        if (!rel || isIgnoredPath(rel)) return false;
        return entry.file.size <= MAX_LOCAL_SKILL_FILE_SIZE;
      })
      .sort((a, b) => a.path.localeCompare(b.path));

    const supportingFiles: LocalDirectorySkillFile[] = [];
    let totalSize = 0;
    for (const entry of supportingEntries) {
      if (supportingFiles.length >= MAX_LOCAL_SKILL_FILE_COUNT) break;
      if (totalSize + entry.file.size > MAX_LOCAL_SKILL_BUNDLE_SIZE) break;
      totalSize += entry.file.size;
      supportingFiles.push({
        path: relativeToDir(entry.path, dir),
        content: await entry.file.text(),
      });
    }

    skills.push({
      key,
      name,
      description,
      content,
      sourcePath: key,
      fileCount: supportingFiles.length + 1,
      files: supportingFiles,
    });
  }

  skills.sort((a, b) => a.key.localeCompare(b.key));
  return { rootLabel, skills };
}

function ResultIcon({ status }: { status: LocalDirectoryImportResult["status"] }) {
  if (status === "created") {
    return <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-green-600" />;
  }
  if (status === "skipped") {
    return <SkipForward className="h-3.5 w-3.5 shrink-0 text-yellow-600" />;
  }
  return <AlertCircle className="h-3.5 w-3.5 shrink-0 text-destructive" />;
}

function LocalDirectorySkillItem({
  skill,
  checked,
  onToggle,
  disabled,
}: {
  skill: LocalDirectorySkillCandidate;
  checked: boolean;
  onToggle: () => void;
  disabled?: boolean;
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
          </div>
          {skill.description && (
            <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">
              {skill.description}
            </p>
          )}
          <p className="mt-1 truncate font-mono text-xs text-muted-foreground">
            {skill.sourcePath}
          </p>
        </div>
        <Badge variant="outline" className="shrink-0">
          {t(($) => $.runtime_import.skill_files, { count: skill.fileCount })}
        </Badge>
      </div>
    </div>
  );
}

function ImportSummary({ results }: { results: LocalDirectoryImportResult[] }) {
  const { t } = useT("skills");
  const created = results.filter((r) => r.status === "created");
  const skipped = results.filter((r) => r.status === "skipped");
  const failed = results.filter((r) => r.status === "failed");

  return (
    <div className="space-y-4 py-2">
      <div className="grid grid-cols-3 gap-2 text-center">
        <div className="rounded-md bg-green-50 px-3 py-2 dark:bg-green-950/30">
          <div className="text-lg font-semibold text-green-700 dark:text-green-400">
            {created.length}
          </div>
          <div className="text-xs text-muted-foreground">
            {t(($) => $.runtime_import.bulk_summary_created)}
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

export function LocalDirectorySkillImportPanel({
  onImported,
  onBulkDone,
}: {
  onImported?: (skill: Skill) => void;
  onBulkDone?: () => void;
}) {
  const { t } = useT("skills");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const inputRef = useRef<HTMLInputElement | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);
  const [directoryLabel, setDirectoryLabel] = useState("");
  const [skills, setSkills] = useState<LocalDirectorySkillCandidate[]>([]);
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const [error, setError] = useState("");
  const [importState, setImportState] =
    useState<LocalDirectoryImportState>(INITIAL_IMPORT_STATE);

  const importing = importState.phase === "importing";
  const loading = importState.phase === "loading";
  const busy = importing || loading;
  const allSelected = skills.length > 0 && selectedKeys.size === skills.length;
  const someSelected = selectedKeys.size > 0 && !allSelected;
  const canImport = selectedKeys.size > 0 && !busy;

  const setInputNode = (node: HTMLInputElement | null) => {
    inputRef.current = node;
    if (node) {
      node.setAttribute("webkitdirectory", "");
      node.setAttribute("directory", "");
    }
  };

  const handleFilesSelected = async (files: File[]) => {
    setImportState({ ...INITIAL_IMPORT_STATE, phase: "loading" });
    setError("");
    setSelectedKeys(new Set());
    try {
      const result = await discoverLocalDirectorySkills(files);
      setDirectoryLabel(result.rootLabel);
      setSkills(result.skills);
      setImportState(INITIAL_IMPORT_STATE);
      if (result.skills.length === 0) {
        setError(t(($) => $.local_directory_import.no_skills_hint));
      }
    } catch (err) {
      setSkills([]);
      setDirectoryLabel("");
      setImportState(INITIAL_IMPORT_STATE);
      setError(
        err instanceof Error
          ? err.message
          : t(($) => $.local_directory_import.load_failed),
      );
    }
  };

  const toggleSkill = (key: string) => {
    setSelectedKeys((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const toggleAll = () => {
    if (selectedKeys.size === skills.length) {
      setSelectedKeys(new Set());
    } else {
      setSelectedKeys(new Set(skills.map((s) => s.key)));
    }
  };

  const refreshImportedSkills = async (results: LocalDirectoryImportResult[]) => {
    await Promise.all([
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) }),
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) }),
    ]);

    for (const r of results) {
      if (r.status === "created" && r.skill) {
        qc.setQueryData(skillDetailOptions(wsId, r.skill.id).queryKey, r.skill);
      }
    }
  };

  const handleImport = async () => {
    const selected = skills.filter((skill) => selectedKeys.has(skill.key));
    if (selected.length === 0) return;

    setImportState({
      phase: "importing",
      total: selected.length,
      completed: 0,
      selectedCount: selected.length,
      results: [],
    });

    const results: LocalDirectoryImportResult[] = [];
    for (const skill of selected) {
      try {
        const created = await api.createSkill({
          name: skill.name,
          description: skill.description,
          content: skill.content,
          files: skill.files,
          config: {
            origin: {
              type: "local_directory",
              source_path: skill.sourcePath,
            },
          },
        });
        results.push({
          key: skill.key,
          name: created.name,
          status: "created",
          skill: created,
        });
      } catch (err) {
        const msg = err instanceof Error ? err.message : "";
        results.push({
          key: skill.key,
          name: skill.name,
          status: isNameConflictError(msg) ? "skipped" : "failed",
          error: msg || t(($) => $.local_directory_import.toast_import_failed),
        });
      }

      setImportState((prev) => ({
        ...prev,
        completed: prev.completed + 1,
        results: [...results],
      }));
    }

    await refreshImportedSkills(results);
    const createdCount = results.filter((r) => r.status === "created").length;
    if (createdCount > 0) {
      toast.success(
        t(($) => $.local_directory_import.toast_imported, {
          count: createdCount,
        }),
      );
    }
    setImportState((prev) => ({ ...prev, phase: "done" }));
  };

  const handleDone = () => {
    const succeeded = importState.results.filter((r) => r.status === "created");
    if (
      importState.selectedCount === 1 &&
      succeeded.length === 1 &&
      succeeded[0]!.skill
    ) {
      onImported?.(succeeded[0]!.skill);
    } else {
      onBulkDone?.();
    }
  };

  const middle = (() => {
    if (importing) {
      const pct =
        importState.total > 0
          ? Math.round((importState.completed / importState.total) * 100)
          : 0;
      return (
        <div className="space-y-4 py-4">
          <div className="text-center">
            <Loader2 className="mx-auto h-6 w-6 animate-spin text-primary" />
            <p className="mt-3 text-sm font-medium">
              {t(($) => $.runtime_import.bulk_progress, {
                completed: importState.completed,
                total: importState.total,
              })}
            </p>
          </div>
          <Progress value={pct} />
        </div>
      );
    }

    if (importState.phase === "done") {
      return <ImportSummary results={importState.results} />;
    }

    if (loading) {
      return (
        <div className="flex items-center justify-center gap-2 py-10 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          {t(($) => $.local_directory_import.loading)}
        </div>
      );
    }

    if (skills.length === 0) {
      return (
        <div className="rounded-lg border border-dashed px-4 py-10 text-center">
          <FolderOpen className="mx-auto h-6 w-6 text-muted-foreground/60" />
          <p className="mt-3 text-sm text-muted-foreground">
            {t(($) => $.local_directory_import.empty_title)}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {t(($) => $.local_directory_import.empty_hint)}
          </p>
        </div>
      );
    }

    return (
      <div className="space-y-2">
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
            {t(($) => $.runtime_import.select_all, { count: skills.length })}
          </span>
        </label>

        {skills.map((skill) => (
          <LocalDirectorySkillItem
            key={skill.key}
            skill={skill}
            checked={selectedKeys.has(skill.key)}
            onToggle={() => toggleSkill(skill.key)}
            disabled={busy}
          />
        ))}
      </div>
    );
  })();

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <input
        ref={setInputNode}
        data-testid="local-directory-input"
        type="file"
        multiple
        className="hidden"
        onClick={(e) => {
          e.currentTarget.value = "";
        }}
        onChange={(e) => {
          void handleFilesSelected(Array.from(e.currentTarget.files ?? []));
        }}
      />

      <div
        aria-disabled={busy || undefined}
        className={`shrink-0 space-y-2 border-b px-5 py-3 ${
          busy ? "pointer-events-none opacity-60" : ""
        }`}
      >
        <div className="flex items-center gap-2">
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => inputRef.current?.click()}
          >
            <FolderOpen className="h-3.5 w-3.5" />
            {skills.length > 0
              ? t(($) => $.local_directory_import.choose_again)
              : t(($) => $.local_directory_import.choose_button)}
          </Button>
          {directoryLabel && (
            <div className="min-w-0 flex-1 truncate rounded-md border bg-muted/20 px-3 py-1.5 font-mono text-xs text-muted-foreground">
              {directoryLabel}
            </div>
          )}
        </div>

        {error && (
          <div
            role="alert"
            className="flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive"
          >
            <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            <span>{error}</span>
          </div>
        )}
      </div>

      <div
        ref={scrollRef}
        style={fadeStyle}
        aria-disabled={busy || undefined}
        className={`min-h-0 flex-1 overflow-y-auto px-5 py-3 ${
          busy ? "pointer-events-none opacity-60" : ""
        }`}
      >
        {middle}
        {importState.phase === "idle" && skills.length > 0 && (
          <p className="mt-3 text-xs text-muted-foreground">
            {t(($) => $.runtime_import.ignored_files_hint)}
          </p>
        )}
      </div>

      <div className="flex shrink-0 items-center gap-3 border-t bg-muted/30 px-5 py-3">
        {importState.phase === "done" ? (
          <>
            <div className="min-w-0 flex-1 text-xs text-muted-foreground">
              {t(($) => $.runtime_import.bulk_complete_hint)}
            </div>
            <Button type="button" size="sm" onClick={handleDone}>
              {t(($) => $.runtime_import.bulk_done_button)}
            </Button>
          </>
        ) : importing ? (
          <div className="min-w-0 flex-1 text-xs text-muted-foreground">
            {t(($) => $.runtime_import.bulk_progress, {
              completed: importState.completed,
              total: importState.total,
            })}
          </div>
        ) : (
          <>
            <div className="min-w-0 flex-1 text-xs text-muted-foreground">
              {selectedKeys.size > 0
                ? t(($) => $.runtime_import.bulk_ready, {
                    count: selectedKeys.size,
                  })
                : t(($) => $.runtime_import.select_skill)}
            </div>
            <Button
              type="button"
              size="sm"
              onClick={() => void handleImport()}
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
