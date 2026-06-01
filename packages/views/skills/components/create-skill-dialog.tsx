"use client";

import { type ChangeEvent, useMemo, useRef, useState } from "react";
import {
  AlertCircle,
  ArrowLeft,
  ChevronRight,
  Download,
  FolderOpen,
  HardDrive,
  Loader2,
  Pencil,
  Plus,
  X as XIcon,
} from "lucide-react";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@multica/core/api";
import type {
  BatchImportSkillsResponse,
  CreateSkillRequest,
  DiscoveredImportSkill,
  Skill,
} from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { isImeComposing } from "@multica/core/utils";
import {
  skillListOptions,
  skillDetailOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import {
  Dialog,
  DialogDescription,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Badge } from "@multica/ui/components/ui/badge";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { cn } from "@multica/ui/lib/utils";
import { openExternal } from "../../platform";
import { RuntimeLocalSkillImportPanel } from "./runtime-local-skill-import-panel";
import { useT } from "../../i18n";
import { parseSkillDirectory } from "../lib/parse-skill-directory";
import { isNameConflictError } from "../lib/utils";

type Method = "chooser" | "manual" | "url" | "local" | "runtime";

function seedAfterCreate(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  skill: Skill,
) {
  qc.setQueryData(skillDetailOptions(wsId, skill.id).queryKey, skill);
  qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
  qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
}

function buildConflictKey(name: string): string {
  return name.trim();
}

type SkillConflictItem = {
  key: string;
  name: string;
  description?: string;
};

type ExistingSkillConflict = {
  name: string;
  description?: string;
};

function useExistingSkillsByName(wsId: string): Map<string, ExistingSkillConflict> {
  const workspaceSkillsQuery = useQuery(skillListOptions(wsId));
  return useMemo(() => {
    const byName = new Map<string, ExistingSkillConflict>();
    for (const skill of workspaceSkillsQuery.data ?? []) {
      byName.set(buildConflictKey(skill.name), skill);
    }
    return byName;
  }, [workspaceSkillsQuery.data]);
}

function SkillConflictDialog({
  open,
  title,
  description,
  skills,
  existingByName,
  overwriteKeys,
  onToggle,
  onCancel,
  onConfirm,
}: {
  open: boolean;
  title: string;
  description: string;
  skills: SkillConflictItem[];
  existingByName: Map<string, ExistingSkillConflict>;
  overwriteKeys: Set<string>;
  onToggle: (key: string) => void;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useT("skills");

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && onCancel()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>

        <div className="max-h-72 space-y-2 overflow-y-auto">
          {skills.map((skill) => {
            const checked = overwriteKeys.has(skill.key);
            const existing = existingByName.get(buildConflictKey(skill.name));

            return (
              <label
                key={skill.key}
                className="flex cursor-pointer items-start gap-3 rounded-md border px-3 py-2 transition-colors hover:bg-accent/40"
              >
                <Checkbox
                  checked={checked}
                  onCheckedChange={() => onToggle(skill.key)}
                  className="mt-0.5"
                />
                <div className="min-w-0 flex-1 space-y-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-medium">{skill.name}</span>
                    <Badge variant={checked ? "secondary" : "outline"}>
                      {checked
                        ? t(($) => $.runtime_import.conflict_overwrite_badge)
                        : t(($) => $.runtime_import.conflict_skip_badge)}
                    </Badge>
                  </div>
                  <div className="grid gap-1 text-xs text-muted-foreground">
                    <span className="truncate">
                      {t(($) => $.runtime_import.conflict_local_label)}:{" "}
                      {skill.description ||
                        t(($) => $.runtime_import.conflict_no_description)}
                    </span>
                    <span className="truncate">
                      {t(($) => $.runtime_import.conflict_existing_label)}:{" "}
                      {existing?.description ||
                        t(($) => $.runtime_import.conflict_no_description)}
                    </span>
                  </div>
                </div>
              </label>
            );
          })}
        </div>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={onCancel}>
            {t(($) => $.runtime_import.conflict_cancel_button)}
          </Button>
          <Button type="button" onClick={onConfirm}>
            {t(($) => $.runtime_import.conflict_confirm_button)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function getImportConflict(body: unknown): SkillConflictItem | null {
  if (!body || typeof body !== "object") return null;
  const record = body as Record<string, unknown>;
  if (typeof record.name !== "string" || !record.name.trim()) return null;
  return {
    key: "url-import",
    name: record.name,
    description:
      typeof record.description === "string" ? record.description : undefined,
  };
}

// ---------------------------------------------------------------------------
// Chooser — initial method picker (3 cards)
// ---------------------------------------------------------------------------

function MethodChooser({ onChoose }: { onChoose: (m: Method) => void }) {
  const { t } = useT("skills");
  const methods: {
    key: Method;
    icon: typeof Plus;
    titleKey: "manual" | "url" | "local" | "runtime";
  }[] = [
    { key: "manual", icon: Plus, titleKey: "manual" },
    { key: "url", icon: Download, titleKey: "url" },
    { key: "local", icon: FolderOpen, titleKey: "local" },
    { key: "runtime", icon: HardDrive, titleKey: "runtime" },
  ];
  return (
    <div className="grid gap-2 p-5">
      {methods.map(({ key, icon: Icon, titleKey }) => (
        <button
          key={key}
          type="button"
          onClick={() => onChoose(key)}
          className="group flex items-start gap-3 rounded-lg border bg-card p-4 text-left transition-colors hover:border-primary/40 hover:bg-accent/40"
        >
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground group-hover:text-foreground">
            <Icon className="h-4 w-4" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium">
              {t(($) => $.create.method_card[`${titleKey}_title`])}
            </div>
            <div className="mt-0.5 text-xs text-muted-foreground">
              {t(($) => $.create.method_card[`${titleKey}_desc`])}
            </div>
          </div>
          <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground/40 transition-colors group-hover:text-muted-foreground" />
        </button>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Manual form
// ---------------------------------------------------------------------------

function ManualForm({
  onCreated,
  onCancel,
}: {
  onCreated: (skill: Skill) => void;
  onCancel: () => void;
}) {
  const { t } = useT("skills");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);

  const submit = async () => {
    const trimmed = name.trim();
    if (!trimmed) return;
    setLoading(true);
    setError("");
    try {
      const skill = await api.createSkill({
        name: trimmed,
        description: description.trim(),
      });
      seedAfterCreate(qc, wsId, skill);
      toast.success(t(($) => $.create.manual.toast_created));
      onCreated(skill);
    } catch (err) {
      setError(err instanceof Error ? err.message : t(($) => $.create.manual.fallback_error));
      setLoading(false);
    }
  };

  return (
    <>
      <div
        ref={scrollRef}
        style={fadeStyle}
        className="flex-1 min-h-0 space-y-4 overflow-y-auto px-5 py-4"
      >
        <div className="space-y-1.5">
          <Label
            htmlFor="create-skill-name"
            className="text-xs text-muted-foreground"
          >
            {t(($) => $.create.manual.name_label)}
          </Label>
          <Input
            id="create-skill-name"
            autoFocus
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              setError("");
            }}
            placeholder={t(($) => $.create.manual.name_placeholder)}
            onKeyDown={(e) => {
              if (isImeComposing(e)) return;
              if (e.key === "Enter") submit();
            }}
          />
          <p className="text-xs text-muted-foreground">
            {t(($) => $.create.manual.name_hint)}
          </p>
        </div>

        <div className="space-y-1.5">
          <Label
            htmlFor="create-skill-desc"
            className="text-xs text-muted-foreground"
          >
            <Pencil className="h-3 w-3" />
            {t(($) => $.create.manual.description_label)}
          </Label>
          <Textarea
            id="create-skill-desc"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder={t(($) => $.create.manual.description_placeholder)}
            rows={3}
            className="resize-none"
          />
        </div>

        {error && (
          <div
            role="alert"
            className="flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive"
          >
            <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            <span>
              {error}
              {isNameConflictError(error) && (
                <>{t(($) => $.create.manual.name_conflict_hint)}</>
              )}
            </span>
          </div>
        )}
      </div>

      <div className="flex shrink-0 items-center justify-end gap-2 border-t bg-muted/30 px-5 py-3">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={onCancel}
          disabled={loading}
        >
          {t(($) => $.create.manual.cancel)}
        </Button>
        <Button
          type="button"
          size="sm"
          onClick={submit}
          disabled={!name.trim() || loading}
        >
          {loading ? (
            <>
              <Loader2 className="h-3 w-3 animate-spin" />
              {t(($) => $.create.manual.submitting)}
            </>
          ) : (
            t(($) => $.create.manual.submit)
          )}
        </Button>
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// Local directory import form
// ---------------------------------------------------------------------------

function LocalDirectoryForm({
  onCreated,
  onBulkDone,
  onCancel,
}: {
  onCreated: (skill: Skill) => void;
  onBulkDone: () => void;
  onCancel: () => void;
}) {
  const { t } = useT("skills");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const inputRef = useRef<HTMLInputElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);
  const [parsedSkills, setParsedSkills] = useState<CreateSkillRequest[]>([]);
  const [batchResult, setBatchResult] = useState<BatchImportSkillsResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [conflicts, setConflicts] = useState<SkillConflictItem[]>([]);
  const [overwriteKeys, setOverwriteKeys] = useState<Set<string>>(new Set());
  const workspaceSkillsByName = useExistingSkillsByName(wsId);

  const handleDirectorySelect = async (e: ChangeEvent<HTMLInputElement>) => {
    const fileList = e.target.files;
    if (!fileList || fileList.length === 0) return;

    setLoading(true);
    setError("");
    setBatchResult(null);
    try {
      const skills = await parseSkillDirectory(fileList);
      if (skills.length === 0) {
        setParsedSkills([]);
        setError(t(($) => $.create.local.no_skills_error));
        return;
      }
      setParsedSkills(skills);
      setConflicts([]);
      setOverwriteKeys(new Set());
    } catch (err) {
      setParsedSkills([]);
      setError(err instanceof Error ? err.message : t(($) => $.create.local.parse_failed));
    } finally {
      setLoading(false);
      e.target.value = "";
    }
  };

  const runImportParsedSkills = async (
    skillsToImport: CreateSkillRequest[],
    skippedConflicts: SkillConflictItem[],
  ) => {
    const skippedNames = skippedConflicts.map((skill) => skill.name);
    if (skillsToImport.length === 0) {
      setBatchResult({
        created: [],
        skipped: skippedNames,
      });
      await qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
      toast.success(
        t(($) => $.create.local.toast_imported, {
          count: 0,
        }),
      );
      return;
    }
    const result = await api.batchImportSkills({ skills: skillsToImport });
    setBatchResult({
      created: result.created,
      skipped: [...skippedNames, ...result.skipped],
    });
    await Promise.all([
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) }),
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) }),
    ]);
    for (const skill of result.created) {
      qc.setQueryData(skillDetailOptions(wsId, skill.id).queryKey, skill);
    }
    toast.success(
      t(($) => $.create.local.toast_imported, {
        count: result.created.length,
      }),
    );
  };

  const importParsedSkills = async () => {
    if (parsedSkills.length === 0) return;
    setLoading(true);
    setError("");
    try {
      const detectedConflicts = parsedSkills
        .map((skill, index) => ({
          key: `${skill.name}-${index}`,
          name: skill.name,
          description: skill.description,
        }))
        .filter((skill) => workspaceSkillsByName.has(buildConflictKey(skill.name)));
      if (detectedConflicts.length > 0) {
        setConflicts(detectedConflicts);
        setOverwriteKeys(new Set());
        return;
      }
      await runImportParsedSkills(parsedSkills, []);
    } catch (err) {
      setError(err instanceof Error ? err.message : t(($) => $.create.local.import_failed));
    } finally {
      setLoading(false);
    }
  };

  const handleDone = () => {
    if (batchResult?.created.length === 1) {
      onCreated(batchResult.created[0]!);
      return;
    }
    onBulkDone();
  };

  const toggleConflictOverwrite = (key: string) => {
    setOverwriteKeys((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const handleConfirmConflicts = async () => {
    const conflictKeys = new Set(conflicts.map((skill) => skill.key));
    const skillsToImport = parsedSkills
      .map((skill, index) => ({ skill, key: `${skill.name}-${index}` }))
      .filter(({ key }) => !conflictKeys.has(key) || overwriteKeys.has(key))
      .map(({ skill, key }) => ({
        ...skill,
        overwrite: overwriteKeys.has(key) || undefined,
      }));
    const skippedConflicts = conflicts.filter((skill) => !overwriteKeys.has(skill.key));

    setConflicts([]);
    setOverwriteKeys(new Set());
    setLoading(true);
    setError("");
    try {
      await runImportParsedSkills(skillsToImport, skippedConflicts);
    } catch (err) {
      setError(err instanceof Error ? err.message : t(($) => $.create.local.import_failed));
    } finally {
      setLoading(false);
    }
  };

  const handleCancelConflicts = () => {
    setConflicts([]);
    setOverwriteKeys(new Set());
  };

  return (
    <>
      <SkillConflictDialog
        open={conflicts.length > 0}
        title={t(($) => $.runtime_import.conflict_dialog_title)}
        description={t(($) => $.create.local.conflict_dialog_description)}
        skills={conflicts}
        existingByName={workspaceSkillsByName}
        overwriteKeys={overwriteKeys}
        onToggle={toggleConflictOverwrite}
        onCancel={handleCancelConflicts}
        onConfirm={handleConfirmConflicts}
      />

      <input
        ref={inputRef}
        type="file"
        className="hidden"
        onChange={handleDirectorySelect}
        // @ts-expect-error webkitdirectory is Chromium/WebKit directory picker API.
        webkitdirectory=""
        directory=""
      />

      <div
        ref={scrollRef}
        style={fadeStyle}
        className="flex-1 min-h-0 space-y-4 overflow-y-auto px-5 py-4"
      >
        {!batchResult && parsedSkills.length === 0 && (
          <div className="flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed px-4 py-10 text-center">
            <Button
              type="button"
              variant="outline"
              onClick={() => inputRef.current?.click()}
              disabled={loading}
            >
              {loading ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <FolderOpen className="h-3.5 w-3.5" />
              )}
              {t(($) => $.create.local.select_directory)}
            </Button>
            <p className="max-w-xs text-xs text-muted-foreground">
              {t(($) => $.create.local.supported_hint)}
            </p>
          </div>
        )}

        {!batchResult && parsedSkills.length > 0 && (
          <div className="space-y-3">
            <div className="flex items-center justify-between gap-3">
              <p className="text-xs text-muted-foreground">
                {t(($) => $.create.local.detected_count, {
                  count: parsedSkills.length,
                })}
              </p>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => inputRef.current?.click()}
                disabled={loading}
              >
                {t(($) => $.create.local.choose_another)}
              </Button>
            </div>
            <div className="space-y-1.5">
              {parsedSkills.map((skill, index) => (
                <div
                  key={`${skill.name}-${index}`}
                  className="flex items-center gap-2 rounded-md border px-3 py-2"
                >
                  <FolderOpen className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-medium">{skill.name}</div>
                    {skill.description && (
                      <div className="truncate text-xs text-muted-foreground">
                        {skill.description}
                      </div>
                    )}
                  </div>
                  {(skill.files?.length ?? 0) > 0 && (
                    <Badge variant="outline" className="shrink-0">
                      {t(($) => $.create.local.file_count, {
                        count: skill.files?.length ?? 0,
                      })}
                    </Badge>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}

        {batchResult && (
          <div className="space-y-3 rounded-lg border px-4 py-3">
            <p className="text-sm font-medium">
              {t(($) => $.create.local.import_complete)}
            </p>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.create.local.import_summary, {
                created: batchResult.created.length,
                skipped: batchResult.skipped.length,
              })}
            </p>
            {batchResult.skipped.length > 0 && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.create.local.skipped_names, {
                  names: batchResult.skipped.join(", "),
                })}
              </p>
            )}
          </div>
        )}

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

      <div className="flex shrink-0 items-center justify-end gap-2 border-t bg-muted/30 px-5 py-3">
        {batchResult ? (
          <Button type="button" size="sm" onClick={handleDone}>
            {t(($) => $.create.local.done)}
          </Button>
        ) : (
          <>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={onCancel}
              disabled={loading}
            >
              {t(($) => $.create.local.cancel)}
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={importParsedSkills}
              disabled={loading || parsedSkills.length === 0}
            >
              {loading ? (
                <>
                  <Loader2 className="h-3 w-3 animate-spin" />
                  {t(($) => $.create.local.importing)}
                </>
              ) : (
                <>
                  <Download className="h-3 w-3" />
                  {t(($) => $.create.local.import_button, {
                    count: parsedSkills.length,
                  })}
                </>
              )}
            </Button>
          </>
        )}
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// URL import form
// ---------------------------------------------------------------------------

type DetectedSource = "clawhub" | "skills.sh" | "github" | "gitee" | null;

type DiscoveredSkillSelection = {
  key: string;
  skill: DiscoveredImportSkill;
};

function detectUrlSource(url: string): DetectedSource {
  const u = url.trim().toLowerCase();
  if (u.includes("clawhub.ai")) return "clawhub";
  if (u.includes("skills.sh")) return "skills.sh";
  if (u.includes("github.com") || u.startsWith("git@github.com:")) return "github";
  if (u.includes("gitee.com") || u.startsWith("git@gitee.com:")) return "gitee";
  return null;
}

function SourceCard({
  label,
  exampleHost,
  browseUrl,
  active,
}: {
  label: string;
  exampleHost: string;
  browseUrl: string;
  active: boolean;
}) {
  return (
    <div
      className={`rounded-md border px-3 py-2.5 transition-colors ${
        active ? "border-primary bg-primary/5" : ""
      }`}
    >
      <div className="text-xs font-medium">{label}</div>
      <button
        type="button"
        onClick={() => openExternal(browseUrl)}
        className="mt-0.5 block max-w-full truncate text-left font-mono text-xs text-brand underline decoration-brand/40 underline-offset-2 hover:decoration-brand"
      >
        {exampleHost}
      </button>
    </div>
  );
}

function buildDiscoveredSkillKey(name: string, index: number): string {
  return `${name}-${index}`;
}

function selectedDiscoveredSkills(
  skills: DiscoveredImportSkill[],
  selectedKeys: Set<string>,
): DiscoveredSkillSelection[] {
  return skills
    .map((skill, index) => ({
      key: buildDiscoveredSkillKey(skill.name, index),
      skill,
    }))
    .filter(({ key }) => selectedKeys.has(key));
}

function importConflictItemsFromDiscovered(
  skills: DiscoveredSkillSelection[],
): SkillConflictItem[] {
  return skills.map(({ key, skill }) => ({
    key,
    name: skill.name,
    description: skill.description,
  }));
}

function SkillCandidateList({
  skills,
  selectedKeys,
  onToggle,
  emptyText,
}: {
  skills: DiscoveredImportSkill[];
  selectedKeys: Set<string>;
  onToggle: (key: string) => void;
  emptyText: string;
}) {
  const { t } = useT("skills");
  if (skills.length === 0) {
    return (
      <div className="rounded-md border border-dashed px-3 py-6 text-center text-xs text-muted-foreground">
        {emptyText}
      </div>
    );
  }

  return (
    <div className="space-y-1.5">
      {skills.map((skill, index) => {
        const key = buildDiscoveredSkillKey(skill.name, index);
        const checked = selectedKeys.has(key);
        return (
          <div
            key={key}
            role="button"
            tabIndex={0}
            onClick={() => onToggle(key)}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                onToggle(key);
              }
            }}
            className={cn(
              "flex cursor-pointer items-start gap-3 rounded-md border px-3 py-2 transition-colors",
              checked ? "border-primary bg-primary/5" : "hover:bg-accent/40",
            )}
          >
            <Checkbox
              checked={checked}
              tabIndex={-1}
              className="pointer-events-none mt-0.5"
            />
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium">{skill.name}</div>
              {skill.description && (
                <div className="truncate text-xs text-muted-foreground">
                  {skill.description}
                </div>
              )}
              <div className="truncate font-mono text-xs text-muted-foreground">
                {skill.source_path}
              </div>
            </div>
            {(skill.files?.length ?? 0) > 0 && (
              <Badge variant="outline" className="shrink-0">
                {t(($) => $.create.local.file_count, {
                  count: skill.files?.length ?? 0,
                })}
              </Badge>
            )}
          </div>
        );
      })}
    </div>
  );
}

function UrlForm({
  onCreated,
  onBulkDone,
  onCancel,
}: {
  onCreated: (skill: Skill) => void;
  onBulkDone: () => void;
  onCancel: () => void;
}) {
  const { t } = useT("skills");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const [url, setUrl] = useState("");
  const [giteeToken, setGiteeToken] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [conflict, setConflict] = useState<SkillConflictItem | null>(null);
  const [conflicts, setConflicts] = useState<SkillConflictItem[]>([]);
  const [overwriteKeys, setOverwriteKeys] = useState<Set<string>>(new Set());
  const [discoveredSkills, setDiscoveredSkills] = useState<DiscoveredImportSkill[]>([]);
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const [batchResult, setBatchResult] = useState<BatchImportSkillsResponse | null>(null);
  const source = detectUrlSource(url);
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);
  const workspaceSkillsByName = useExistingSkillsByName(wsId);

  const resetUrlState = () => {
    setError("");
    setConflict(null);
    setConflicts([]);
    setOverwriteKeys(new Set());
    setDiscoveredSkills([]);
    setSelectedKeys(new Set());
    setBatchResult(null);
  };

  const runLegacyImport = async (overwrite = false) => {
    const trimmed = url.trim();
    if (!trimmed) return;
    setLoading(true);
    setError("");
    try {
      const token = giteeToken.trim();
      const skill = await api.importSkill({
        url: trimmed,
        ...(source === "gitee" && token ? { gitee_token: token } : {}),
        overwrite: overwrite || undefined,
      });
      seedAfterCreate(qc, wsId, skill);
      toast.success(t(($) => $.create.url.toast_imported));
      onCreated(skill);
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        const importConflict = getImportConflict(err.body);
        if (importConflict) {
          setConflict(importConflict);
          setOverwriteKeys(new Set());
          return;
        }
      }
      setError(err instanceof Error ? err.message : t(($) => $.create.url.fallback_error));
      setLoading(false);
    }
  };

  const runURLImports = async (
    skillsToImport: DiscoveredSkillSelection[],
    overwriteKeysToApply: Set<string>,
    skippedConflicts: SkillConflictItem[],
  ) => {
    const created: Skill[] = [];
    const skipped = skippedConflicts.map((skill) => skill.name);
    for (const { key, skill } of skillsToImport) {
      const imported = await api.importSkill({
        url: skill.source_url,
        ...(source === "gitee" && giteeToken.trim()
          ? { gitee_token: giteeToken.trim() }
          : {}),
        overwrite: overwriteKeysToApply.has(key) || undefined,
      });
      created.push(imported);
      seedAfterCreate(qc, wsId, imported);
    }
    setBatchResult({ created, skipped });
    await Promise.all([
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) }),
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) }),
    ]);
    toast.success(
      t(($) => $.create.local.toast_imported, {
        count: created.length,
      }),
    );
  };

  const discover = async () => {
    const trimmed = url.trim();
    if (!trimmed) return;
    if (source === "clawhub" || source === "skills.sh") {
      await runLegacyImport(false);
      return;
    }
    setLoading(true);
    setError("");
    setConflict(null);
    setConflicts([]);
    setBatchResult(null);
    try {
      const token = giteeToken.trim();
      const result = await api.discoverImportSkills({
        url: trimmed,
        ...(source === "gitee" && token ? { gitee_token: token } : {}),
      });
      if (result.skills.length === 0) {
        setDiscoveredSkills([]);
        setSelectedKeys(new Set());
        setError(t(($) => $.create.url.no_skills_error));
        return;
      }
      setDiscoveredSkills(result.skills);
      setSelectedKeys(new Set(result.skills.map((skill, index) => buildDiscoveredSkillKey(skill.name, index))));
      if (result.skills.length === 1) {
        await importSelectedSkills(result.skills, new Set([buildDiscoveredSkillKey(result.skills[0]!.name, 0)]));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t(($) => $.create.url.fallback_error));
    } finally {
      setLoading(false);
    }
  };

  const importSelectedSkills = async (
    sourceSkills = discoveredSkills,
    sourceSelectedKeys = selectedKeys,
  ) => {
    const selected = selectedDiscoveredSkills(sourceSkills, sourceSelectedKeys);
    if (selected.length === 0) return;
    setLoading(true);
    setError("");
    try {
      const detectedConflicts = importConflictItemsFromDiscovered(selected).filter((skill) =>
        workspaceSkillsByName.has(buildConflictKey(skill.name)),
      );
      if (detectedConflicts.length > 0) {
        setConflicts(detectedConflicts);
        setOverwriteKeys(new Set());
        return;
      }
      await runURLImports(selected, new Set(), []);
    } catch (err) {
      setError(err instanceof Error ? err.message : t(($) => $.create.url.fallback_error));
    } finally {
      setLoading(false);
    }
  };

  const handleConfirmConflict = async () => {
    if (!conflict) return;
    const shouldOverwrite = overwriteKeys.has(conflict.key);
    setConflict(null);
    if (!shouldOverwrite) {
      setLoading(false);
      setError(t(($) => $.runtime_import.conflict_skipped));
      return;
    }
    await runLegacyImport(true);
  };

  const handleCancelConflict = () => {
    setConflict(null);
    setOverwriteKeys(new Set());
    setLoading(false);
  };

  const handleConfirmConflicts = async () => {
    const selected = selectedDiscoveredSkills(discoveredSkills, selectedKeys);
    const conflictKeys = new Set(conflicts.map((skill) => skill.key));
    const skillsToImport = selected
      .filter(({ key }) => !conflictKeys.has(key) || overwriteKeys.has(key))
      .map(({ key, skill }) => ({ key, skill }));
    const skippedConflicts = conflicts.filter((skill) => !overwriteKeys.has(skill.key));

    setConflicts([]);
    setOverwriteKeys(new Set());
    setLoading(true);
    setError("");
    try {
      await runURLImports(skillsToImport, overwriteKeys, skippedConflicts);
    } catch (err) {
      setError(err instanceof Error ? err.message : t(($) => $.create.url.fallback_error));
    } finally {
      setLoading(false);
    }
  };

  const toggleSelected = (key: string) => {
    setSelectedKeys((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const handleDone = () => {
    if (batchResult?.created.length === 1) {
      onCreated(batchResult.created[0]!);
      return;
    }
    onBulkDone();
  };

  const submittingLabel = (() => {
    if (!loading) return t(($) => $.create.url.import);
    if (source === "clawhub") return t(($) => $.create.url.importing_clawhub);
    if (source === "skills.sh") return t(($) => $.create.url.importing_skills_sh);
    if (source === "github") return t(($) => $.create.url.importing_github);
    if (source === "gitee") return t(($) => $.create.url.importing_gitee);
    return t(($) => $.create.url.importing);
  })();

  return (
    <>
      <SkillConflictDialog
        open={conflict != null}
        title={t(($) => $.runtime_import.conflict_dialog_title)}
        description={t(($) => $.create.url.conflict_dialog_description)}
        skills={conflict ? [conflict] : []}
        existingByName={workspaceSkillsByName}
        overwriteKeys={overwriteKeys}
        onToggle={(key) =>
          setOverwriteKeys((prev) => {
            const next = new Set(prev);
            if (next.has(key)) next.delete(key);
            else next.add(key);
            return next;
          })
        }
        onCancel={handleCancelConflict}
        onConfirm={handleConfirmConflict}
      />
      <SkillConflictDialog
        open={conflicts.length > 0}
        title={t(($) => $.runtime_import.conflict_dialog_title)}
        description={t(($) => $.create.url.conflict_dialog_description)}
        skills={conflicts}
        existingByName={workspaceSkillsByName}
        overwriteKeys={overwriteKeys}
        onToggle={(key) =>
          setOverwriteKeys((prev) => {
            const next = new Set(prev);
            if (next.has(key)) next.delete(key);
            else next.add(key);
            return next;
          })
        }
        onCancel={() => {
          setConflicts([]);
          setOverwriteKeys(new Set());
          setLoading(false);
        }}
        onConfirm={handleConfirmConflicts}
      />

      <div
        ref={scrollRef}
        style={fadeStyle}
        className="flex-1 min-h-0 space-y-4 overflow-y-auto px-5 py-4"
      >
        <div className="space-y-1.5">
          <Label htmlFor="import-url" className="text-xs text-muted-foreground">
            {t(($) => $.create.url.url_label)}
          </Label>
          <Input
            id="import-url"
            autoFocus
            value={url}
            onChange={(e) => {
              setUrl(e.target.value);
              resetUrlState();
            }}
            placeholder="https://clawhub.ai/owner/skill"
            className="font-mono text-sm"
            onKeyDown={(e) => {
              if (e.key === "Enter") discover();
            }}
          />
        </div>

        {source === "gitee" && (
          <div className="space-y-1.5">
            <Label htmlFor="gitee-token" className="text-xs text-muted-foreground">
              {t(($) => $.create.url.gitee_token_label)}
            </Label>
            <Input
              id="gitee-token"
              type="password"
              autoComplete="off"
              value={giteeToken}
              onChange={(e) => {
                setGiteeToken(e.target.value);
                resetUrlState();
              }}
              placeholder={t(($) => $.create.url.gitee_token_placeholder)}
              className="font-mono text-sm"
              onKeyDown={(e) => {
                if (e.key === "Enter") discover();
              }}
            />
            <p className="text-xs text-muted-foreground">
              {t(($) => $.create.url.gitee_token_hint)}
            </p>
          </div>
        )}

        <div>
          <p className="mb-2 text-xs text-muted-foreground">
            {t(($) => $.create.url.supported_sources)}
          </p>
          <div className="grid grid-cols-2 gap-2">
            <SourceCard
              label="ClawHub"
              exampleHost="clawhub.ai/owner/skill"
              browseUrl="https://clawhub.ai"
              active={source === "clawhub"}
            />
            <SourceCard
              label="Skills.sh"
              exampleHost="skills.sh/owner/repo/skill"
              browseUrl="https://skills.sh"
              active={source === "skills.sh"}
            />
            <SourceCard
              label="GitHub"
              exampleHost="github.com/owner/repo"
              browseUrl="https://github.com"
              active={source === "github"}
            />
            <SourceCard
              label="Gitee"
              exampleHost="gitee.com/owner/repo"
              browseUrl="https://gitee.com"
              active={source === "gitee"}
            />
          </div>
        </div>

        {!batchResult && discoveredSkills.length > 0 && (
          <div className="space-y-3">
            <div className="flex items-center justify-between gap-3">
              <p className="text-xs text-muted-foreground">
                {t(($) => $.create.local.detected_count, {
                  count: discoveredSkills.length,
                })}
              </p>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={discover}
                disabled={loading}
              >
                {t(($) => $.create.url.rescan)}
              </Button>
            </div>
            <SkillCandidateList
              skills={discoveredSkills}
              selectedKeys={selectedKeys}
              onToggle={toggleSelected}
              emptyText={t(($) => $.create.url.no_skills_error)}
            />
          </div>
        )}

        {batchResult && (
          <div className="space-y-3 rounded-lg border px-4 py-3">
            <p className="text-sm font-medium">
              {t(($) => $.create.local.import_complete)}
            </p>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.create.local.import_summary, {
                created: batchResult.created.length,
                skipped: batchResult.skipped.length,
              })}
            </p>
            {batchResult.skipped.length > 0 && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.create.local.skipped_names, {
                  names: batchResult.skipped.join(", "),
                })}
              </p>
            )}
          </div>
        )}

        {error && (
          <div
            role="alert"
            className="flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive"
          >
            <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            <span>
              {error}
              {isNameConflictError(error) && (
                <>{t(($) => $.create.url.name_conflict_hint)}</>
              )}
            </span>
          </div>
        )}
      </div>

      <div className="flex shrink-0 items-center justify-end gap-2 border-t bg-muted/30 px-5 py-3">
        {batchResult ? (
          <Button type="button" size="sm" onClick={handleDone}>
            {t(($) => $.create.local.done)}
          </Button>
        ) : (
          <>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={onCancel}
              disabled={loading}
            >
              {t(($) => $.create.url.cancel)}
            </Button>
            {discoveredSkills.length > 0 ? (
              <Button
                type="button"
                size="sm"
                onClick={() => importSelectedSkills()}
                disabled={loading || selectedKeys.size === 0}
              >
                {loading ? (
                  <>
                    <Loader2 className="h-3 w-3 animate-spin" />
                    {t(($) => $.create.local.importing)}
                  </>
                ) : (
                  <>
                    <Download className="h-3 w-3" />
                    {t(($) => $.create.local.import_button, {
                      count: selectedKeys.size,
                    })}
                  </>
                )}
              </Button>
            ) : (
              <Button
                type="button"
                size="sm"
                onClick={discover}
                disabled={!url.trim() || loading}
              >
                {loading ? (
                  <>
                    <Loader2 className="h-3 w-3 animate-spin" />
                    {submittingLabel}
                  </>
                ) : (
                  <>
                    <Download className="h-3 w-3" />
                    {submittingLabel}
                  </>
                )}
              </Button>
            )}
          </>
        )}
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// Root dialog
// ---------------------------------------------------------------------------

export function CreateSkillDialog({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated?: (skill: Skill) => void;
}) {
  const { t } = useT("skills");
  const [method, setMethod] = useState<Method>("chooser");

  const handleCreated = (skill: Skill) => {
    onCreated?.(skill);
    onClose();
  };

  const wide = method === "runtime";

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent
        showCloseButton={false}
        className={cn(
          "flex flex-col gap-0 overflow-hidden p-0",
          "!transition-all !duration-300 !ease-out",
          wide
            ? "!h-[min(600px,85vh)] !max-w-2xl !w-full"
            : "!h-auto !max-h-[85vh] !max-w-md !w-full",
        )}
      >
        {/* Header */}
        <div className="flex shrink-0 items-start justify-between gap-3 border-b px-5 pt-4 pb-3">
          <div className="flex items-center gap-2 min-w-0">
            {method !== "chooser" && (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <button
                      type="button"
                      onClick={() => setMethod("chooser")}
                      className="-ml-1 rounded-sm p-1 text-muted-foreground opacity-70 transition-opacity hover:bg-accent/60 hover:opacity-100"
                      aria-label={t(($) => $.create.back_aria)}
                    >
                      <ArrowLeft className="h-3.5 w-3.5" />
                    </button>
                  }
                />
                <TooltipContent side="bottom">{t(($) => $.create.back)}</TooltipContent>
              </Tooltip>
            )}
            <div className="min-w-0">
              <DialogTitle className="truncate text-base font-medium">
                {t(($) => $.create.method[method].title)}
              </DialogTitle>
              <p className="mt-0.5 text-xs text-muted-foreground">
                {t(($) => $.create.method[method].desc)}
              </p>
            </div>
          </div>
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  onClick={onClose}
                  className="rounded-sm p-1 text-muted-foreground opacity-70 transition-opacity hover:bg-accent/60 hover:opacity-100"
                  aria-label={t(($) => $.create.close_aria)}
                >
                  <XIcon className="h-3.5 w-3.5" />
                </button>
              }
            />
            <TooltipContent side="bottom">{t(($) => $.create.close)}</TooltipContent>
          </Tooltip>
        </div>

        {/* Method body — each form owns its scroll middle + footer */}
        {method === "chooser" && <MethodChooser onChoose={setMethod} />}
        {method === "manual" && (
          <ManualForm
            onCreated={handleCreated}
            onCancel={() => setMethod("chooser")}
          />
        )}
        {method === "url" && (
          <UrlForm
            onCreated={handleCreated}
            onBulkDone={onClose}
            onCancel={() => setMethod("chooser")}
          />
        )}
        {method === "local" && (
          <LocalDirectoryForm
            onCreated={handleCreated}
            onBulkDone={onClose}
            onCancel={() => setMethod("chooser")}
          />
        )}
        {method === "runtime" && (
          <RuntimeLocalSkillImportPanel
            onImported={handleCreated}
            onBulkDone={onClose}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}
