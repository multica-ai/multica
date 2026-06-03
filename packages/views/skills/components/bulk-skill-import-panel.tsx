"use client";

import { useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  AlertCircle,
  CheckCircle2,
  Download,
  FolderUp,
  Loader2,
  SkipForward,
} from "lucide-react";
import type { Skill, SkillCandidate } from "@multica/core/types";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { skillListOptions } from "@multica/core/workspace/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Progress } from "@multica/ui/components/ui/progress";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { useT } from "../../i18n";
import {
  discoverFolderSkills,
  type FolderCandidate,
  type FolderEntry,
} from "../lib/folder-discovery";
import {
  useBulkSkillImport,
  type BulkResult,
  type BulkTask,
} from "../hooks/use-bulk-skill-import";

type Source = "folder" | "github";

// A unified candidate the checklist renders, regardless of source.
type Candidate = {
  key: string;
  name: string;
  description: string;
  path: string;
  fileCount: number;
  toTask: () => BulkTask;
};

function folderToCandidate(c: FolderCandidate): Candidate {
  return {
    key: c.path + "::" + c.name,
    name: c.name,
    description: c.description,
    path: c.path,
    fileCount: (c.data.files?.length ?? 0) + 1,
    toTask: () => ({
      key: c.path + "::" + c.name,
      name: c.name,
      kind: "payload",
      data: c.data,
    }),
  };
}

function githubToCandidate(c: SkillCandidate): Candidate {
  return {
    key: "gh::" + c.import_url,
    name: c.name,
    description: c.description,
    path: c.path,
    fileCount: 0,
    toTask: () => ({
      key: "gh::" + c.import_url,
      name: c.name,
      kind: "url",
      url: c.import_url,
      importName: c.name,
    }),
  };
}

export function BulkSkillImportPanel({
  onBulkDone,
}: {
  onImported?: (skill: Skill) => void;
  onBulkDone?: () => void;
}) {
  const { t } = useT("skills");
  const wsId = useWorkspaceId();
  const [source, setSource] = useState<Source>("folder");
  const [candidates, setCandidates] = useState<Candidate[]>([]);
  const [truncated, setTruncated] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [reading, setReading] = useState(false);
  const [error, setError] = useState("");
  const [githubUrl, setGithubUrl] = useState("");
  const folderInputRef = useRef<HTMLInputElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);

  const { phase, total, results, completed, start, cancel } =
    useBulkSkillImport();
  const importing = phase === "importing";

  // Existing names → "already exists" badge + default-unchecked.
  const { data: existing = [] } = useQuery(skillListOptions(wsId));
  const existingNames = useMemo(
    () => new Set(existing.map((s) => s.name.toLowerCase())),
    [existing],
  );

  const applyCandidates = (list: Candidate[], wasTruncated: boolean) => {
    setCandidates(list);
    setTruncated(wasTruncated);
    setSelected(
      new Set(
        list
          .filter((c) => !existingNames.has(c.name.toLowerCase()))
          .map((c) => c.key),
      ),
    );
  };

  const onFolderPicked = async (fileList: FileList | null) => {
    if (!fileList || fileList.length === 0) return;
    setReading(true);
    setError("");
    try {
      const entries: FolderEntry[] = Array.from(fileList).map((f) => ({
        relativePath:
          (f as File & { webkitRelativePath?: string }).webkitRelativePath ||
          f.name,
        size: f.size,
        text: () => f.text(),
      }));
      const { candidates: fc, truncated: tr } =
        await discoverFolderSkills(entries);
      applyCandidates(fc.map(folderToCandidate), tr);
    } catch {
      setError(t(($) => $.bulk_import.discover_failed));
    } finally {
      setReading(false);
    }
  };

  const onDiscover = async () => {
    const url = githubUrl.trim();
    if (!url) return;
    setReading(true);
    setError("");
    try {
      const res = await api.discoverSkills({ url });
      if (!res?.candidates?.length) {
        applyCandidates([], false);
        setError(t(($) => $.bulk_import.empty_no_skills));
        return;
      }
      applyCandidates(
        res.candidates.map(githubToCandidate),
        res.truncated === true,
      );
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : t(($) => $.bulk_import.discover_failed),
      );
    } finally {
      setReading(false);
    }
  };

  const toggle = (key: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      next.has(key) ? next.delete(key) : next.add(key);
      return next;
    });

  const allSelected =
    candidates.length > 0 && selected.size === candidates.length;
  const toggleAll = () =>
    setSelected(
      allSelected ? new Set() : new Set(candidates.map((c) => c.key)),
    );

  const handleImport = () => {
    const tasks = candidates
      .filter((c) => selected.has(c.key))
      .map((c) => c.toTask());
    if (tasks.length) start(tasks);
  };

  const middle = (() => {
    if (importing) {
      const pct =
        total > 0 ? Math.round((completed / total) * 100) : 0;
      return (
        <div className="space-y-4 py-4">
          <div className="text-center">
            <Loader2 className="mx-auto h-6 w-6 animate-spin text-primary" />
            <p className="mt-3 text-sm font-medium">
              {t(($) => $.runtime_import.bulk_progress, { completed, total })}
            </p>
          </div>
          <Progress value={pct} />
        </div>
      );
    }

    if (phase === "done" || phase === "cancelled") {
      return <Summary results={results} />;
    }

    if (reading) {
      return (
        <div className="flex items-center justify-center gap-2 py-10 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          {t(($) => $.bulk_import.folder_reading)}
        </div>
      );
    }

    if (source === "folder" && candidates.length === 0) {
      return (
        <button
          type="button"
          onClick={() => folderInputRef.current?.click()}
          className="flex w-full flex-col items-center gap-2 rounded-lg border border-dashed px-4 py-12 text-center hover:bg-accent/40"
        >
          <FolderUp className="h-6 w-6 text-muted-foreground" />
          <span className="text-sm font-medium">
            {t(($) => $.bulk_import.folder_pick)}
          </span>
          <span className="text-xs text-muted-foreground">
            {t(($) => $.bulk_import.folder_drop_hint)}
          </span>
        </button>
      );
    }

    if (source === "github" && candidates.length === 0) {
      return (
        <div className="py-8 text-center text-sm text-muted-foreground">
          {t(($) => $.bulk_import.empty_hint)}
        </div>
      );
    }

    return (
      <div className="space-y-2">
        {truncated && (
          <p className="rounded-md bg-warning/10 px-3 py-2 text-xs text-muted-foreground">
            {t(($) => $.bulk_import.capped_notice, {
              count: candidates.length,
            })}
          </p>
        )}
        <label className="flex cursor-pointer items-center gap-2 px-1 py-1">
          <input
            type="checkbox"
            checked={allSelected}
            onChange={toggleAll}
            className="cursor-pointer accent-primary"
          />
          <span className="text-xs text-muted-foreground">
            {t(($) => $.runtime_import.select_all, {
              count: candidates.length,
            })}
          </span>
        </label>
        {candidates.map((c) => {
          const exists = existingNames.has(c.name.toLowerCase());
          return (
            <div
              key={c.key}
              role="button"
              tabIndex={0}
              onClick={() => toggle(c.key)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  toggle(c.key);
                }
              }}
              className={`flex items-start gap-3 rounded-lg border px-4 py-3 text-left transition-colors ${
                selected.has(c.key)
                  ? "border-primary bg-primary/5"
                  : "hover:bg-accent/40"
              }`}
            >
              <Checkbox
                checked={selected.has(c.key)}
                tabIndex={-1}
                className="pointer-events-none mt-0.5"
              />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate text-sm font-medium">{c.name}</span>
                  {exists && (
                    <Badge variant="outline">
                      {t(($) => $.bulk_import.already_exists)}
                    </Badge>
                  )}
                </div>
                {c.description && (
                  <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">
                    {c.description}
                  </p>
                )}
                <p className="mt-1 truncate font-mono text-xs text-muted-foreground">
                  {c.path}
                </p>
              </div>
              <Badge variant="outline" className="shrink-0">
                {t(($) => $.runtime_import.skill_files, {
                  count: c.fileCount,
                })}
              </Badge>
            </div>
          );
        })}
      </div>
    );
  })();

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* Source toggle */}
      <div
        className={`shrink-0 space-y-2 border-b px-5 py-3 ${importing ? "pointer-events-none opacity-60" : ""}`}
      >
        <span className="text-xs text-muted-foreground">
          {t(($) => $.bulk_import.source_label)}
        </span>
        <div className="flex gap-2">
          {(["folder", "github"] as Source[]).map((s) => (
            <button
              key={s}
              type="button"
              onClick={() => {
                setSource(s);
                setCandidates([]);
                setError("");
              }}
              className={`rounded-md border px-3 py-1.5 text-xs ${
                source === s
                  ? "border-primary bg-primary/5 font-medium"
                  : "text-muted-foreground hover:border-foreground/30"
              }`}
            >
              {s === "folder"
                ? t(($) => $.bulk_import.source_folder)
                : t(($) => $.bulk_import.source_github)}
            </button>
          ))}
        </div>
      </div>

      {/* GitHub URL input */}
      {source === "github" && (
        <div
          className={`shrink-0 space-y-1.5 border-b px-5 py-3 ${importing ? "pointer-events-none opacity-60" : ""}`}
        >
          <Input
            value={githubUrl}
            onChange={(e) => {
              setGithubUrl(e.target.value);
              setError("");
            }}
            placeholder={t(($) => $.bulk_import.github_url_placeholder)}
            className="font-mono text-sm"
            onKeyDown={(e) => {
              if (e.key === "Enter") onDiscover();
            }}
          />
          <div className="flex justify-end">
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={!githubUrl.trim() || reading}
              onClick={onDiscover}
            >
              {reading ? (
                <>
                  <Loader2 className="h-3 w-3 animate-spin" />
                  {t(($) => $.bulk_import.discovering)}
                </>
              ) : (
                <>
                  <Download className="h-3 w-3" />
                  {t(($) => $.bulk_import.discover)}
                </>
              )}
            </Button>
          </div>
        </div>
      )}

      {/* Hidden folder input */}
      <input
        ref={folderInputRef}
        type="file"
        hidden
        multiple
        // @ts-expect-error webkitdirectory is non-standard but supported in Chromium/Electron
        webkitdirectory=""
        directory=""
        onChange={(e) => onFolderPicked(e.target.files)}
      />

      {/* Scrollable middle */}
      <div
        ref={scrollRef}
        style={fadeStyle}
        className="min-h-0 flex-1 overflow-y-auto px-5 py-3"
      >
        {error && (
          <div
            role="alert"
            className="mb-2 flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive"
          >
            <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            {error}
          </div>
        )}
        {middle}
      </div>

      {/* Footer */}
      <div className="flex shrink-0 items-center gap-3 border-t bg-muted/30 px-5 py-3">
        {phase === "done" || phase === "cancelled" ? (
          <>
            <div className="min-w-0 flex-1 text-xs text-muted-foreground">
              {phase === "cancelled"
                ? t(($) => $.runtime_import.bulk_cancelled_hint)
                : t(($) => $.runtime_import.bulk_complete_hint)}
            </div>
            <Button type="button" size="sm" onClick={onBulkDone}>
              {t(($) => $.runtime_import.bulk_done_button)}
            </Button>
          </>
        ) : importing ? (
          <>
            <div className="min-w-0 flex-1 text-xs text-muted-foreground">
              {t(($) => $.runtime_import.bulk_progress, { completed, total })}
            </div>
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={cancel}
            >
              {t(($) => $.runtime_import.bulk_cancel_button)}
            </Button>
          </>
        ) : (
          <>
            <div className="min-w-0 flex-1" />
            <Button
              type="button"
              size="sm"
              disabled={selected.size === 0}
              onClick={handleImport}
            >
              <Download className="h-3 w-3" />
              {t(($) => $.bulk_import.import_selected, {
                count: selected.size,
              })}
            </Button>
          </>
        )}
      </div>
    </div>
  );
}

function Summary({ results }: { results: BulkResult[] }) {
  const { t } = useT("skills");
  const by = (s: BulkResult["status"]) => results.filter((r) => r.status === s);
  return (
    <div className="space-y-4 py-2">
      <div className="grid grid-cols-3 gap-2 text-center">
        <Counter
          n={by("success").length}
          label={t(($) => $.runtime_import.bulk_summary_imported)}
          tone="green"
        />
        <Counter
          n={by("skipped").length}
          label={t(($) => $.runtime_import.bulk_summary_skipped)}
          tone="yellow"
        />
        <Counter
          n={by("failed").length}
          label={t(($) => $.runtime_import.bulk_summary_failed)}
          tone="red"
        />
      </div>
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

function Counter({
  n,
  label,
  tone,
}: {
  n: number;
  label: string;
  tone: "green" | "yellow" | "red";
}) {
  const cls = {
    green: "bg-green-50 text-green-700 dark:bg-green-950/30 dark:text-green-400",
    yellow:
      "bg-yellow-50 text-yellow-700 dark:bg-yellow-950/30 dark:text-yellow-400",
    red: "bg-red-50 text-red-700 dark:bg-red-950/30 dark:text-red-400",
  }[tone];
  return (
    <div className={`rounded-md px-3 py-2 ${cls}`}>
      <div className="text-lg font-semibold">{n}</div>
      <div className="text-xs text-muted-foreground">{label}</div>
    </div>
  );
}
