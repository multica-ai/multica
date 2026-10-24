"use client";

import { useLayoutEffect, useRef, useState, type ChangeEvent } from "react";
import {
  AlertCircle,
  ArrowLeft,
  ChevronRight,
  Download,
  FileArchive,
  FolderOpen,
  HardDrive,
  Loader2,
  Pencil,
  Plus,
  X as XIcon,
} from "lucide-react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@multica/core/api";
import {
  getCurrentSlug,
  getCurrentWsId,
  subscribeToCurrentSlug,
} from "@multica/core/platform";
import type { Skill } from "@multica/core/types";
import {
  discoverSkillArchivesFromPickerFiles,
  wrapExistingSkillArchive,
  type SkillArchiveDiscovery,
  type PreparedSkillArchiveError,
} from "@multica/core/skills";
import { useWorkspaceId } from "@multica/core/hooks";
import { isImeComposing } from "@multica/core/utils";
import {
  skillDetailOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { cn } from "@multica/ui/lib/utils";
import { openExternal } from "../../platform";
import { RuntimeLocalSkillImportPanel } from "./runtime-local-skill-import-panel";
import { useT } from "../../i18n";
import { isNameConflictError } from "../lib/utils";

type Method = "chooser" | "manual" | "local" | "url" | "runtime";

function seedAfterCreate(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  skill: Skill,
  refetchType: "active" | "none" = "active",
) {
  qc.setQueryData(skillDetailOptions(wsId, skill.id).queryKey, skill);
  qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId), refetchType });
  qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId), refetchType });
}

// Chooser — initial method picker (4 cards)

function MethodChooser({ onChoose }: { onChoose: (m: Method) => void }) {
  const { t } = useT("skills");
  const methods: {
    key: Exclude<Method, "chooser">;
    icon: typeof Plus;
  }[] = [
    { key: "manual", icon: Plus },
    { key: "local", icon: FolderOpen },
    { key: "url", icon: Download },
    { key: "runtime", icon: HardDrive },
  ];
  return (
    <div className="grid gap-2 p-5">
      {methods.map(({ key, icon: Icon }) => (
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
            <div className="text-body font-medium">
              {t(($) => $.create.method[key].title)}
            </div>
            <div className="mt-0.5 text-caption text-muted-foreground">
              {t(($) => $.create.method[key].desc)}
            </div>
          </div>
          <ChevronRight className="h-4 w-4 shrink-0 text-faint-foreground transition-colors group-hover:text-muted-foreground" />
        </button>
      ))}
    </div>
  );
}

// Manual form

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
            className="text-caption text-muted-foreground"
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
          <p className="text-caption text-muted-foreground">
            {t(($) => $.create.manual.name_hint)}
          </p>
        </div>

        <div className="space-y-1.5">
          <Label
            htmlFor="create-skill-desc"
            className="text-caption text-muted-foreground"
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
            className="flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-caption text-destructive"
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

// URL import form

type DetectedSource = "clawhub" | "skills.sh" | "github" | null;

function detectUrlSource(url: string): DetectedSource {
  const u = url.trim().toLowerCase();
  if (u.includes("clawhub.ai")) return "clawhub";
  if (u.includes("skills.sh")) return "skills.sh";
  if (u.includes("github.com")) return "github";
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
      <div className="text-caption font-medium">{label}</div>
      <button
        type="button"
        onClick={() => openExternal(browseUrl)}
        className="mt-0.5 block max-w-full truncate text-left font-mono text-caption text-brand underline decoration-brand/40 underline-offset-2 hover:decoration-brand"
      >
        {exampleHost}
      </button>
    </div>
  );
}

function UrlForm({
  onCreated,
  onCancel,
}: {
  onCreated: (skill: Skill) => void;
  onCancel: () => void;
}) {
  const { t } = useT("skills");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const [url, setUrl] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const source = detectUrlSource(url);
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);

  const submit = async () => {
    const trimmed = url.trim();
    if (!trimmed) return;
    setLoading(true);
    setError("");
    try {
      const skill = await api.importSkill({ url: trimmed });
      seedAfterCreate(qc, wsId, skill);
      toast.success(t(($) => $.create.url.toast_imported));
      onCreated(skill);
    } catch (err) {
      setError(err instanceof Error ? err.message : t(($) => $.create.url.fallback_error));
      setLoading(false);
    }
  };

  const submittingLabel = (() => {
    if (!loading) return t(($) => $.create.url.import);
    if (source === "clawhub") return t(($) => $.create.url.importing_clawhub);
    if (source === "skills.sh") return t(($) => $.create.url.importing_skills_sh);
    if (source === "github") return t(($) => $.create.url.importing_github);
    return t(($) => $.create.url.importing);
  })();

  return (
    <>
      <div
        ref={scrollRef}
        style={fadeStyle}
        className="flex-1 min-h-0 space-y-4 overflow-y-auto px-5 py-4"
      >
        <div className="space-y-1.5">
          <Label htmlFor="import-url" className="text-caption text-muted-foreground">
            {t(($) => $.create.url.url_label)}
          </Label>
          <Input
            id="import-url"
            autoFocus
            value={url}
            onChange={(e) => {
              setUrl(e.target.value);
              setError("");
            }}
            placeholder="https://clawhub.ai/owner/skill"
            className="font-mono text-body"
            onKeyDown={(e) => {
              if (e.key === "Enter") submit();
            }}
          />
        </div>

        <div>
          <p className="mb-2 text-caption text-muted-foreground">
            {t(($) => $.create.url.supported_sources)}
          </p>
          <div className="grid grid-cols-3 gap-2">
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
          </div>
        </div>

        {error && (
          <div
            role="alert"
            className="flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-caption text-destructive"
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
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={onCancel}
          disabled={loading}
        >
          {t(($) => $.create.url.cancel)}
        </Button>
        <Button
          type="button"
          size="sm"
          onClick={submit}
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
      </div>
    </>
  );
}

// Local folder / archive import

function LocalForm({
  prepared,
  preparing,
  selectionWorkspace,
  onCreated,
  onCancel,
  onDone,
  onChooseFolder,
  onChooseArchive,
  onBusyChange,
}: {
  prepared: SkillArchiveDiscovery | null;
  selectionWorkspace: { wsId: string; slug: string | null };
  preparing: boolean;
  onCreated: (skill: Skill) => void;
  onCancel: () => void;
  onDone: () => void;
  onChooseFolder: () => void;
  onChooseArchive: () => void;
  onBusyChange: (busy: boolean) => void;
}) {
  const { t } = useT("skills");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const [loading, setLoading] = useState(false);
  const running = useRef(false);
  const stop = useRef(false);
  const mounted = useRef(false);
  const cancelled = useRef(false);
  const owner = useRef(selectionWorkspace).current;
  const [workspaceChanged, setWorkspaceChanged] = useState(false);
  const [checkError, setCheckError] = useState("");
  const isOwnerWorkspace = () =>
    owner.wsId === getCurrentWsId() && owner.slug === getCurrentSlug();
  const canContinue = () =>
    mounted.current && !cancelled.current && isOwnerWorkspace();

  useLayoutEffect(() => {
    mounted.current = true;
    const checkWorkspace = () => {
      if (
        wsId !== owner.wsId ||
        owner.wsId !== getCurrentWsId() ||
        owner.slug !== getCurrentSlug()
      ) {
        cancelled.current = true;
        stop.current = true;
        setWorkspaceChanged(true);
      }
    };
    checkWorkspace();
    const unsubscribe = subscribeToCurrentSlug(checkWorkspace);
    return () => {
      mounted.current = false;
      stop.current = true;
      unsubscribe();
    };
  }, [wsId, owner]);
  const [excluded, setExcluded] = useState<Set<string>>(new Set());
  const [results, setResults] = useState<
    Record<
      string,
      {
        status: "success" | "failed" | "uncertain";
        error?: string;
        reconciliationName?: string | null;
      }
    >
  >({});
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);
  const items = prepared?.ok ? prepared.items : [];
  const bulk = items.length > 1;
  const eligible = items.filter(
    (item) =>
      !workspaceChanged &&
      !item.error &&
      !["success", "uncertain"].includes(results[item.id]?.status ?? ""),
  );
  const selected = eligible.filter((item) => !excluded.has(item.id));
  const completed = Object.keys(results).length > 0;
  const errorLabel = (error: PreparedSkillArchiveError) =>
    ({
      missing_skill_md: t(($) => $.create.local.missing_skill_md),
      too_large: t(($) => $.create.local.too_large),
      empty: t(($) => $.create.local.empty),
      too_many_files: t(($) => $.create.local.too_many_files),
      unreadable: t(($) => $.create.local.unreadable),
    })[error];

  const setBusy = (busy: boolean) => {
    running.current = busy;
    if (mounted.current) {
      setLoading(busy);
      onBusyChange(busy);
    }
  };

  const submit = async () => {
    if (running.current || !selected.length || !canContinue() || !owner.slug)
      return;
    setBusy(true);
    stop.current = false;
    setCheckError("");
    try {
      for (const item of selected) {
        if (stop.current || !canContinue()) break;
        let uploading = false;
        let reconciliationName: string | null = null;
        try {
          const archive = await item.prepare();
          if (!canContinue()) break;
          if (!archive.ok) {
            setResults((prev) => ({
              ...prev,
              [item.id]: { status: "failed", error: errorLabel(archive.error) },
            }));
            continue;
          }
          reconciliationName = archive.reconciliationName;
          if (!canContinue()) break;
          uploading = true;
          const skill = await api.importSkillArchive(archive.file, "fail", {
            workspaceSlug: owner.slug,
          });
          if (skill.workspace_id !== owner.wsId)
            throw new Error("Import response workspace mismatch");
          // An in-flight success still belongs to A after the UI moves to B.
          // Do not refetch A through query functions that read the new global slug.
          seedAfterCreate(
            qc,
            owner.wsId,
            skill,
            canContinue() ? "active" : "none",
          );
          if (mounted.current)
            setResults((prev) => ({
              ...prev,
              [item.id]: { status: "success" },
            }));
          if (!bulk && canContinue()) {
            toast.success(t(($) => $.create.local.toast_imported));
            onCreated(skill);
          }
        } catch (err) {
          // A transport/server failure may occur after the server committed the import.
          const uncertain =
            uploading &&
            (!(err instanceof ApiError) ||
              err.status >= 500 ||
              err.status === 408);
          if (mounted.current)
            setResults((prev) => ({
              ...prev,
              [item.id]: {
                reconciliationName,
                status: uncertain ? "uncertain" : "failed",
                error: uncertain
                  ? reconciliationName === null
                    ? t(($) => $.create.local.uncertain_manual)
                    : t(($) => $.create.local.uncertain)
                  : err instanceof Error
                    ? err.message
                    : t(($) => $.create.local.fallback_error),
              },
            }));
          if (uncertain)
            await qc.invalidateQueries({
              queryKey: workspaceKeys.skills(owner.wsId),
              refetchType: canContinue() ? "active" : "none",
            });
        }
      }
    } finally {
      setBusy(false);
    }
  };

  const checkResults = async () => {
    if (running.current || !canContinue() || !owner.slug) return;
    setBusy(true);
    setCheckError("");
    try {
      const skills = await api.listSkills({
        requireValidResponse: true,
        workspaceSlug: owner.slug,
      });
      if (!canContinue()) return;
      if (skills.some((skill) => skill.workspace_id !== owner.wsId))
        throw new Error("Skill list workspace mismatch");
      await qc.invalidateQueries({
        queryKey: workspaceKeys.skills(owner.wsId),
        refetchType: "none",
      });
      if (!canContinue()) return;
      setResults((prev) => {
        const next = { ...prev };
        for (const item of items) {
          if (
            next[item.id]?.status === "uncertain" &&
            next[item.id]?.reconciliationName != null &&
            !skills.some(
              (skill) => skill.name === next[item.id]?.reconciliationName,
            )
          ) {
            next[item.id] = {
              status: "failed",
              error: t(($) => $.create.local.retry_ready),
            };
          }
        }
        return next;
      });
    } catch {
      if (canContinue()) setCheckError(t(($) => $.create.local.check_failed));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div
        ref={scrollRef}
        style={fadeStyle}
        className="flex-1 min-h-0 space-y-4 overflow-y-auto px-5 py-4"
        aria-busy={loading || preparing}
      >
        {workspaceChanged && (
          <p role="alert" className="text-caption text-destructive">
            {t(($) => $.create.local.workspace_changed)}
          </p>
        )}
        {checkError && (
          <p role="alert" className="text-caption text-destructive">
            {checkError}
          </p>
        )}
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onChooseFolder}
            disabled={loading || workspaceChanged}
          >
            <FolderOpen className="h-3 w-3" />
            {t(($) => $.create.local.choose_folder)}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onChooseArchive}
            disabled={loading || workspaceChanged}
          >
            <FileArchive className="h-3 w-3" />
            {t(($) => $.create.local.choose_archive)}
          </Button>
        </div>
        {preparing && (
          <p role="status" className="text-caption text-muted-foreground">
            {t(($) => $.create.local.importing)}
          </p>
        )}
        {prepared && !prepared.ok && (
          <p role="alert" className="text-caption text-destructive">
            {errorLabel(prepared.error)}
          </p>
        )}
        {bulk && (
          <label className="flex items-center gap-2 text-caption">
            <Checkbox
              disabled={loading || !eligible.length}
              checked={
                eligible.length > 0 && selected.length === eligible.length
              }
              indeterminate={
                selected.length > 0 && selected.length < eligible.length
              }
              onCheckedChange={(checked) =>
                setExcluded(
                  checked
                    ? new Set()
                    : new Set(eligible.map((item) => item.id)),
                )
              }
            />
            {t(($) => $.create.local.select_all)}
          </label>
        )}
        {items.map((item) => (
          <div key={item.id} className="space-y-1 rounded-md border p-3">
            <label className="flex items-start gap-2 text-body font-medium">
              {bulk && (
                <Checkbox
                  disabled={loading || !eligible.includes(item)}
                  checked={!item.error && !excluded.has(item.id)}
                  onCheckedChange={(checked) =>
                    setExcluded((prev) => {
                      const next = new Set(prev);
                      if (checked) next.delete(item.id);
                      else next.add(item.id);
                      return next;
                    })
                  }
                />
              )}
              <span className="min-w-0 break-words">
                {item.preview.skillName}
              </span>
            </label>
            <p className="break-all text-caption text-muted-foreground">
              {item.id}
            </p>
            {item.preview.description && (
              <p className="text-caption text-muted-foreground">
                {item.preview.description}
              </p>
            )}
            {item.preview.fileCount != null && (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.create.local.files, {
                  count: item.preview.fileCount,
                })}
              </p>
            )}
            {item.error && (
              <p role="alert" className="text-caption text-destructive">
                {errorLabel(item.error)}
              </p>
            )}
            {results[item.id]?.status === "success" && (
              <p role="status" className="text-caption">
                {t(($) => $.create.local.toast_imported)}
              </p>
            )}
            {results[item.id]?.error && (
              <p role="alert" className="text-caption text-destructive">
                {results[item.id]?.error}
                {isNameConflictError(results[item.id]?.error ?? "") &&
                  t(($) => $.create.local.name_conflict_hint)}
              </p>
            )}
          </div>
        ))}
        {completed && (
          <p role="status" className="text-caption">
            {t(($) => $.create.local.summary, {
              success: Object.values(results).filter(
                (result) => result.status === "success",
              ).length,
              failed: Object.values(results).filter(
                (result) => result.status !== "success",
              ).length,
            })}
          </p>
        )}
        {Object.values(results).some(
          (result) => result.status === "uncertain",
        ) && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={checkResults}
            disabled={loading || workspaceChanged}
          >
            {t(($) => $.create.local.check_results)}
          </Button>
        )}
      </div>
      <div className="flex shrink-0 flex-wrap items-center justify-end gap-2 border-t bg-muted/30 px-5 py-3">
        {loading ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => {
              stop.current = true;
            }}
          >
            {t(($) => $.create.local.stop)}
          </Button>
        ) : (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={completed ? onDone : onCancel}
          >
            {completed
              ? t(($) => $.create.local.done)
              : t(($) => $.create.local.cancel)}
          </Button>
        )}
        <Button
          type="button"
          size="sm"
          onClick={submit}
          disabled={!selected.length || loading || preparing}
          aria-busy={loading}
        >
          {loading ? (
            <>
              <Loader2 className="h-3 w-3 animate-spin" />
              {t(($) => $.create.local.importing)}
            </>
          ) : bulk ? (
            t(($) => $.create.local.import_count, { count: selected.length })
          ) : (
            t(($) => $.create.local.import)
          )}
        </Button>
      </div>
    </>
  );
}

// Root dialog

export function CreateSkillDialog({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated?: (skill: Skill) => void;
}) {
  const { t } = useT("skills");
  const wsId = useWorkspaceId();
  const localSelectionWorkspace = useRef({ wsId, slug: getCurrentSlug() });
  const [method, setMethod] = useState<Method>("chooser");
  const [localPrepared, setLocalPrepared] =
    useState<SkillArchiveDiscovery | null>(null);
  const [localPreparing, setLocalPreparing] = useState(false);
  const [localEpoch, setLocalEpoch] = useState(0);
  const localGeneration = useRef(0);
  const [localBusy, setLocalBusy] = useState(false);
  const localBusyRef = useRef(false);
  const setLocalBusyState = (busy: boolean) => {
    localBusyRef.current = busy;
    setLocalBusy(busy);
  };
  const folderInputRef = useRef<HTMLInputElement>(null);
  const archiveInputRef = useRef<HTMLInputElement>(null);

  const handleCreated = (skill: Skill) => {
    onCreated?.(skill);
    onClose();
  };

  const beginLocalSelection = (): number => {
    localSelectionWorkspace.current = { wsId, slug: getCurrentSlug() };
    const next = localGeneration.current + 1;
    localGeneration.current = next;
    setLocalEpoch(next);
    return next;
  };

  const resetLocal = () => {
    beginLocalSelection();
    setLocalPrepared(null);
    setLocalPreparing(false);
  };

  const bindDirectoryInput = (el: HTMLInputElement | null) => {
    folderInputRef.current = el;
    if (!el) return;
    el.setAttribute("webkitdirectory", "");
    el.setAttribute("directory", "");
  };

  const openFolderPicker = () => {
    folderInputRef.current?.click();
  };

  const openArchivePicker = () => {
    archiveInputRef.current?.click();
  };

  const handleChoose = (next: Method) => {
    resetLocal();
    setMethod(next);
  };

  const onFolderPicked = async (e: ChangeEvent<HTMLInputElement>) => {
    // FileList is live: clearing the input empties the same object. Snapshot
    // File handles first so resetting `value` (to allow picking the same
    // folder again) cannot drop the selection.
    if (localBusyRef.current) return;
    const files = Array.from(e.target.files ?? []);
    e.target.value = "";
    if (files.length === 0) return;
    const gen = beginLocalSelection();
    setMethod("local");
    setLocalPreparing(true);
    setLocalPrepared(null);
    try {
      const prepared = await discoverSkillArchivesFromPickerFiles(files);
      if (gen !== localGeneration.current) return;
      setLocalPrepared(prepared);
    } catch {
      if (gen !== localGeneration.current) return;
      setLocalPrepared({ ok: false, error: "unreadable" });
    } finally {
      if (gen === localGeneration.current) setLocalPreparing(false);
    }
  };

  const onArchivePicked = (e: ChangeEvent<HTMLInputElement>) => {
    if (localBusyRef.current) return;
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    beginLocalSelection();
    setMethod("local");
    setLocalPreparing(false);
    const archive = wrapExistingSkillArchive(file);
    setLocalPrepared(
      archive.ok
        ? {
            ok: true,
            items: [
              {
                id: file.name,
                preview: archive.preview,
                prepare: async () => archive,
              },
            ],
          }
        : archive,
    );
  };

  const wide = method === "runtime";

  return (
    <Dialog
      open
      onOpenChange={(v) => {
        if (!v && !localBusyRef.current) onClose();
      }}
    >
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
          <div className="flex items-start gap-2 min-w-0">
            {method !== "chooser" && (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <button
                      type="button"
                      disabled={localBusy}
                      onClick={() => {
                        resetLocal();
                        setMethod("chooser");
                      }}
                      className="-ml-1 mt-px rounded-sm p-1 text-faint-foreground transition-colors hover:bg-accent/60 hover:text-muted-foreground"
                      aria-label={t(($) => $.create.back_aria)}
                    >
                      <ArrowLeft className="h-3.5 w-3.5" />
                    </button>
                  }
                />
                <TooltipContent side="bottom">
                  {t(($) => $.create.back)}
                </TooltipContent>
              </Tooltip>
            )}
            <div className="min-w-0">
              <DialogTitle className="truncate text-title-sm font-medium">
                {t(($) => $.create.method[method].title)}
              </DialogTitle>
              <p className="mt-0.5 text-caption text-muted-foreground">
                {t(($) => $.create.method[method].desc)}
              </p>
            </div>
          </div>
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  disabled={localBusy}
                  onClick={onClose}
                  className="rounded-sm p-1 text-faint-foreground transition-colors hover:bg-accent/60 hover:text-muted-foreground"
                  aria-label={t(($) => $.create.close_aria)}
                >
                  <XIcon className="h-3.5 w-3.5" />
                </button>
              }
            />
            <TooltipContent side="bottom">
              {t(($) => $.create.close)}
            </TooltipContent>
          </Tooltip>
        </div>

        {/* Source buttons open these pickers synchronously in their click handlers. */}
        <input
          ref={bindDirectoryInput}
          type="file"
          multiple
          className="sr-only"
          tabIndex={-1}
          aria-hidden
          onChange={onFolderPicked}
        />
        <input
          ref={archiveInputRef}
          type="file"
          accept=".skill,.zip,application/zip"
          className="sr-only"
          tabIndex={-1}
          aria-hidden
          onChange={onArchivePicked}
        />

        {/* Method body — each form owns its scroll middle + footer */}
        {method === "chooser" && <MethodChooser onChoose={handleChoose} />}
        {method === "manual" && (
          <ManualForm
            onCreated={handleCreated}
            onCancel={() => setMethod("chooser")}
          />
        )}
        {method === "local" && (
          <LocalForm
            key={localEpoch}
            onDone={onClose}
            onBusyChange={setLocalBusyState}
            selectionWorkspace={localSelectionWorkspace.current}
            prepared={localPrepared}
            preparing={localPreparing}
            onCreated={handleCreated}
            onCancel={() => {
              resetLocal();
              setMethod("chooser");
            }}
            onChooseFolder={openFolderPicker}
            onChooseArchive={openArchivePicker}
          />
        )}
        {method === "url" && (
          <UrlForm
            onCreated={handleCreated}
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
