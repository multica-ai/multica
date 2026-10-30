"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AlertCircle,
  AlertTriangle,
  FileText,
  Loader2,
  Lock,
  PanelRight,
  Pencil,
  Plus,
  RotateCw,
  Save,
  Trash2,
  UserPlus,
} from "@multica/ui/icons";
import type {
  AgentRuntime,
  MemberWithUser,
  Skill,
  SkillFile,
  UpdateSkillRequest,
} from "@multica/core/types";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  agentListOptions,
  memberListOptions,
  selectSkillAssignments,
  skillDetailOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "../../navigation";
import { BreadcrumbHeader } from "../../layout/breadcrumb-header";
import { PAGE_GUTTER, PAGE_RAIL } from "../../layout/page-header";
import { useCanEditSkill } from "../hooks/use-can-edit-skill";
import { useSkillPermissions } from "@multica/core/permissions";
import { CapabilityBanner } from "@multica/ui/components/common/capability-banner";
import { isRefreshableOrigin, readOrigin } from "../lib/origin";
import { FileViewer } from "./file-viewer";
import {
  AddToAgentDialog,
  type SkillActionsContext,
} from "./skill-list-actions";
import { RefreshSkillDialog } from "./refresh-skill-dialog";
import { useT } from "../../i18n";
import { SkillSidePanel } from "./skill-side-panel";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";

const SKILL_MD = "SKILL.md";

type DraftFile = { id?: string; path: string; content: string };

/** The four editable fields, as one snapshot. */
type SkillDraft = {
  name: string;
  description: string;
  content: string;
  files: DraftFile[];
};

/**
 * Server skill -> editable draft.
 *
 * `name` and `description` are trimmed here because Save trims them too:
 * normalizing once, at the single seam where server data becomes a draft, is
 * what lets every later comparison be plain equality. Trimming at comparison
 * time instead is how the two sides drifted apart in the first place — the
 * dirty check trimmed one operand and not the other, so a description ending
 * in a newline (what `description: |` frontmatter yields) never compared equal
 * to itself and the page opened permanently dirty.
 *
 * `content` and file bodies are deliberately NOT normalized: leading and
 * trailing whitespace in a SKILL.md body is content, not formatting.
 */
function toDraft(s: Skill): SkillDraft {
  return {
    name: s.name.trim(),
    description: s.description.trim(),
    content: s.content,
    files: (s.files ?? []).map((f: SkillFile) => ({
      id: f.id,
      path: f.path,
      content: f.content,
    })),
  };
}

/**
 * Order-insensitive snapshot of a file set. The two endpoints that return
 * files disagree on order — GET sorts by path, PUT echoes request order — and
 * an order difference is not a content difference.
 */
function fileSignature(files: DraftFile[]): string {
  return JSON.stringify(
    files
      .map((f) => ({ path: f.path, content: f.content }))
      .sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0)),
  );
}

/**
 * Did the USER change anything, measured against the draft we handed them?
 *
 * Deliberately not "does the draft differ from the latest server skill": that
 * question conflates two independent causes — the user typed something, or the
 * server moved underneath us — and answering it as if only the first existed
 * is what made every remote update look like a local edit.
 */
function hasLocalEdits(draft: SkillDraft, baseline: SkillDraft | null): boolean {
  if (!baseline) return false;
  return (
    draft.name.trim() !== baseline.name ||
    draft.description.trim() !== baseline.description ||
    draft.content !== baseline.content ||
    fileSignature(draft.files) !== fileSignature(baseline.files)
  );
}

// File path validation + inline add

export function useValidateNewFilePath() {
  const { t } = useT("skills");
  return (path: string, existing: string[]): string => {
    const p = path.trim();
    if (!p) return t(($) => $.detail.add_file.errors.empty);
    if (p.startsWith("/")) return t(($) => $.detail.add_file.errors.absolute);
    if (p.split("/").includes("..")) return t(($) => $.detail.add_file.errors.double_dot);
    if (p === SKILL_MD) return t(($) => $.detail.add_file.errors.reserved);
    if (existing.includes(p)) return t(($) => $.detail.add_file.errors.exists);
    // Directories are inferred from paths, not stored, so a file named after
    // one merges into that folder's node when the tree is built — it drops off
    // the rail while still sitting in the draft. Rejected in both directions:
    // a file cannot take a folder's name, and cannot take a name that an
    // existing file is already nested under.
    if (existing.some((other) => other.startsWith(`${p}/`))) {
      return t(($) => $.detail.add_file.errors.is_directory);
    }
    if (existing.some((other) => p.startsWith(`${other}/`))) {
      return t(($) => $.detail.add_file.errors.under_file);
    }
    return "";
  };
}

function AddFileInline({
  existingPaths,
  initialPath = "",
  submitLabel,
  onAdd,
  onCancel,
}: {
  existingPaths: string[];
  initialPath?: string;
  submitLabel?: string;
  onAdd: (path: string) => void;
  onCancel: () => void;
}) {
  const { t } = useT("skills");
  const validate = useValidateNewFilePath();
  const [path, setPath] = useState(initialPath);
  const [error, setError] = useState("");

  const submit = () => {
    const err = validate(path, existingPaths);
    if (err) {
      setError(err);
      return;
    }
    onAdd(path.trim());
  };

  return (
    <div className="mt-1.5">
      <Input
        autoFocus
        value={path}
        onChange={(e) => {
          setPath(e.target.value);
          setError("");
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter") submit();
          if (e.key === "Escape") onCancel();
        }}
        placeholder={t(($) => $.detail.add_file.placeholder)}
        className="h-7 font-mono text-caption"
      />
      {error && (
        <p role="alert" className="mt-1 text-caption text-destructive">
          {error}
        </p>
      )}
      <div className="mt-1.5 flex items-center gap-1.5">
        <Button type="button" size="xs" onClick={submit}>
          {submitLabel ?? t(($) => $.detail.add_file.add)}
        </Button>
        <Button type="button" size="xs" variant="ghost" onClick={onCancel}>
          {t(($) => $.detail.add_file.cancel)}
        </Button>
      </div>
    </div>
  );
}

// Selected file

/**
 * The open file: a toolbar and its body. The file list lives in the
 * knowledge base panel's tree, so the page shows one file at a time; the
 * toolbar keeps the file actions that used to sit on the tree's rows.
 */
function FileSection({
  skillName,
  filePaths,
  selectedPath,
  selectedContent,
  canEdit,
  addingFile,
  focusEditor,
  onStartAddFile,
  onAddFile,
  onCancelAddFile,
  onDeleteFile,
  onRenameFile,
  onContentChange,
  onFocusHandled,
}: {
  skillName: string;
  filePaths: string[];
  selectedPath: string;
  selectedContent: string;
  canEdit: boolean;
  addingFile: boolean;
  focusEditor: boolean;
  onStartAddFile: () => void;
  onAddFile: (path: string) => void;
  onCancelAddFile: () => void;
  onDeleteFile: (path?: string) => void;
  onRenameFile: (from: string, to: string) => void;
  onContentChange: (content: string) => void;
  onFocusHandled: () => void;
}) {
  const { t } = useT("skills");
  const [renaming, setRenaming] = useState(false);
  const isMain = selectedPath === SKILL_MD;

  return (
    <section className="flex min-h-0 min-w-0 flex-1 flex-col">
      <div className="sticky top-0 z-10 shrink-0 border-b bg-background">
        <div className={cn(PAGE_RAIL, PAGE_GUTTER, "flex h-10 items-center gap-3")}>
          <span className="flex min-w-0 items-center gap-1.5 truncate text-caption text-muted-foreground">
            <FileText className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
            {!isMain && (
              <span className="hidden shrink-0 sm:inline">
                {t(($) => $.detail.files.reference_of, { name: skillName })}
              </span>
            )}
            <span className="truncate font-mono text-foreground">{selectedPath}</span>
          </span>
          <div className="ml-auto flex shrink-0 items-center gap-1">
            {canEdit && (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      onClick={onStartAddFile}
                      className="text-muted-foreground"
                      aria-label={t(($) => $.detail.files.add_file)}
                    >
                      <Plus className="h-3.5 w-3.5" />
                    </Button>
                  }
                />
                <TooltipContent>{t(($) => $.detail.files.add_file)}</TooltipContent>
              </Tooltip>
            )}
            {!isMain && canEdit && (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => setRenaming(true)}
                      className="text-muted-foreground"
                      aria-label={t(($) => $.file_tree.actions.rename)}
                    >
                      <Pencil className="h-3.5 w-3.5" />
                    </Button>
                  }
                />
                <TooltipContent>{t(($) => $.file_tree.actions.rename)}</TooltipContent>
              </Tooltip>
            )}
            {!isMain && canEdit && (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => onDeleteFile()}
                      className="text-muted-foreground hover:text-destructive"
                      aria-label={t(($) => $.detail.delete_file)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  }
                />
                <TooltipContent>{t(($) => $.detail.delete_file)}</TooltipContent>
              </Tooltip>
            )}
          </div>
        </div>
        {(addingFile || renaming) && canEdit && (
          <div className={cn(PAGE_RAIL, PAGE_GUTTER, "max-w-md pb-3")}>
            {renaming ? (
              <AddFileInline
                existingPaths={filePaths.filter((path) => path !== selectedPath)}
                initialPath={selectedPath}
                submitLabel={t(($) => $.file_tree.actions.rename)}
                onAdd={(to) => {
                  setRenaming(false);
                  if (to !== selectedPath) onRenameFile(selectedPath, to);
                }}
                onCancel={() => setRenaming(false)}
              />
            ) : (
              <AddFileInline existingPaths={filePaths} onAdd={onAddFile} onCancel={onCancelAddFile} />
            )}
          </div>
        )}
      </div>

      <div className="min-h-0 flex-1">
        <FileViewer
          key={selectedPath}
          path={selectedPath}
          content={selectedContent}
          readOnly={!canEdit}
          autoFocus={focusEditor}
          onChange={onContentChange}
          onFocusHandled={onFocusHandled}
        />
      </div>
    </section>
  );
}

// Main page

export function SkillDetailPage({ skillId }: { skillId: string }) {
  const { t } = useT("skills");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const currentUserId = useAuthStore((s) => s.user?.id ?? null);

  const {
    data: skill,
    isLoading,
    error,
  } = useQuery(skillDetailOptions(wsId, skillId));
  const { data: agents = [], error: agentsError } = useQuery(
    agentListOptions(wsId),
  );
  const { data: members = [], error: membersError } = useQuery(
    memberListOptions(wsId),
  );
  const { data: runtimes = [], error: runtimesError } = useQuery(
    runtimeListOptions(wsId),
  );

  const assignments = useMemo(() => selectSkillAssignments(agents), [agents]);

  const canEdit = useCanEditSkill(skill, wsId);
  const skillPermissions = useSkillPermissions(skill ?? null, wsId);

  // Context for the shared "Add to agent" dialog (also used by the skills
  // list). Members see their own agents; workspace owners/admins see all.
  const myRole = useMemo(
    () => members.find((m) => m.user_id === currentUserId)?.role ?? null,
    [members, currentUserId],
  );
  const actionsCtx: SkillActionsContext = {
    wsId,
    agents,
    currentUserId,
    isAdmin: myRole === "owner" || myRole === "admin",
  };

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [content, setContent] = useState("");
  const [files, setFiles] = useState<DraftFile[]>([]);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [confirmRefresh, setConfirmRefresh] = useState(false);
  const [showAddToAgents, setShowAddToAgents] = useState(false);
  const [addingFile, setAddingFile] = useState(false);
  const [conflictPending, setConflictPending] = useState(false);
  // Open by default where there is room beside the file; on a phone the panel
  // overlays the file, so it waits to be asked for.
  const isMobile = useIsMobile();
  const [panelChoice, setPanelChoice] = useState<boolean | null>(null);
  const panelOpen = panelChoice ?? !isMobile;

  // Which file's editor is waiting for the caret. A path rather than a boolean
  // so a request raised for one row cannot land in another row's editor if the
  // selection moves before the effect runs.
  const [focusPath, setFocusPath] = useState<string | null>(null);

  // The open file lives in the URL (`?file=`), so the knowledge base tree can
  // link straight to it and the selection survives a reload.
  const urlFile = navigation.searchParams.get("file");
  const setSelectedPath = useCallback(
    (path: string) => {
      const params = new URLSearchParams(navigation.searchParams);
      params.delete("view");
      if (path === SKILL_MD) params.delete("file");
      else params.set("file", path);
      const query = params.toString();
      navigation.replace(`${navigation.pathname}${query ? `?${query}` : ""}`);
    },
    [navigation],
  );

  const draftRef = useRef({ name, description, content, files });
  draftRef.current = { name, description, content, files };

  const seededKeyRef = useRef<string | null>(null);

  /**
   * The draft this page last handed the user, normalized by `toDraft`. Dirty
   * state is measured against THIS, never against the latest server skill.
   *
   * INVARIANT: `seedFromSkill` is the only writer, and it always writes the
   * same snapshot it pushes into state. `dirtySummary` is a `useMemo` keyed on
   * those state values, so every baseline write is paired with the state
   * change that recomputes it. Assigning this ref anywhere else breaks that
   * pairing and silently re-opens MUL-5645.
   */
  const baselineRef = useRef<SkillDraft | null>(null);

  const seedFromSkill = useCallback((s: Skill) => {
    const seeded = toDraft(s);
    setName(seeded.name);
    setDescription(seeded.description);
    setContent(seeded.content);
    setFiles(seeded.files);
    baselineRef.current = seeded;
  }, []);

  /**
   * Adopt a server version wholesale: draft, baseline, seeded key and conflict
   * flag all move together. Every path that accepts a server version goes
   * through here — first load, silent refresh, Save, Discard — so the four
   * pieces can never drift apart.
   */
  const adoptServerVersion = useCallback(
    (s: Skill) => {
      seededKeyRef.current = `${wsId}:${s.id}@${s.updated_at}`;
      setConflictPending(false);
      seedFromSkill(s);
    },
    [wsId, seedFromSkill],
  );

  useEffect(() => {
    if (!skill) return;
    if (seededKeyRef.current === `${wsId}:${skill.id}@${skill.updated_at}`) {
      return;
    }

    const sameSkill =
      seededKeyRef.current !== null &&
      seededKeyRef.current.startsWith(`${wsId}:${skill.id}@`);

    // Same skill, newer server version. Whether that is a conflict depends on
    // the local draft alone: with no local edits the user is simply reading
    // the page, so pull the new version in silently instead of accusing them
    // of an edit they never made and freezing the editor on stale text.
    //
    // Re-run on draft changes, not just on new server versions: a conflict the
    // user resolves by reverting their own edits has to release the page. The
    // save bar is dirty-gated, so holding the conflict past that point would
    // leave the banner above stale text with no Discard left to press.
    if (sameSkill && hasLocalEdits(draftRef.current, baselineRef.current)) {
      setConflictPending(true);
      return;
    }

    adoptServerVersion(skill);
  }, [skill, wsId, adoptServerVersion, name, description, content, files]);

  const creator = useMemo<MemberWithUser | null>(
    () =>
      skill?.created_by
        ? members.find((m) => m.user_id === skill.created_by) ?? null
        : null,
    [members, skill?.created_by],
  );

  const origin = useMemo(() => (skill ? readOrigin(skill) : null), [skill]);
  const originRuntime = useMemo<AgentRuntime | null>(() => {
    if (!origin || origin.type !== "runtime_local" || !origin.runtime_id)
      return null;
    return runtimes.find((r) => r.id === origin.runtime_id) ?? null;
  }, [origin, runtimes]);

  const skillAgents = useMemo(
    () => assignments.get(skillId) ?? [],
    [assignments, skillId],
  );

  const fileMap = useMemo(() => {
    const map = new Map<string, string>();
    map.set(SKILL_MD, content);
    for (const f of files) if (f.path.trim()) map.set(f.path, f.content);
    return map;
  }, [content, files]);
  const filePaths = useMemo(() => Array.from(fileMap.keys()), [fileMap]);
  const selectedPath = urlFile && fileMap.has(urlFile) ? urlFile : SKILL_MD;
  const selectedContent = fileMap.get(selectedPath) ?? "";

  // Compared against the seeded baseline, not against the latest server skill,
  // so a remote update can never read as a local edit. Files are matched by id
  // so a rename counts as one changed file, not a delete plus an add; SKILL.md
  // is its own entry since it lives in `content`.
  const dirtySummary = useMemo(() => {
    const baseline = baselineRef.current;
    if (!baseline) {
      return { nameChanged: false, descChanged: false, changedFileCount: 0 };
    }
    const baselineById = new Map(
      baseline.files.flatMap((f) => (f.id ? [[f.id, f] as const] : [])),
    );
    let changedFileCount = content !== baseline.content ? 1 : 0;
    const draftIds = new Set<string>();
    for (const f of files) {
      if (f.id) draftIds.add(f.id);
      const base = f.id ? baselineById.get(f.id) : undefined;
      if (!base || base.path !== f.path || base.content !== f.content) {
        changedFileCount += 1;
      }
    }
    for (const f of baseline.files) {
      if (f.id && !draftIds.has(f.id)) changedFileCount += 1;
    }
    return {
      nameChanged: name.trim() !== baseline.name,
      descChanged: description.trim() !== baseline.description,
      changedFileCount,
    };
  }, [name, description, content, files]);

  const isDirty =
    dirtySummary.nameChanged ||
    dirtySummary.descChanged ||
    dirtySummary.changedFileCount > 0;

  const handleSave = async () => {
    if (!skill || !canEdit) return;
    const trimmedName = name.trim();
    const trimmedDesc = description.trim();
    setSaving(true);
    try {
      const payload: UpdateSkillRequest = {
        name: trimmedName,
        description: trimmedDesc,
        content,
        files: files.filter((f) => f.path.trim()),
      };
      const updated = await api.updateSkill(skill.id, payload);
      qc.setQueryData(skillDetailOptions(wsId, skill.id).queryKey, updated);
      adoptServerVersion(updated);
      qc.invalidateQueries({
        queryKey: workspaceKeys.skills(wsId),
        exact: true,
      });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(t(($) => $.detail.toast_saved));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.detail.toast_save_failed));
    } finally {
      setSaving(false);
    }
  };

  const handleDiscard = () => {
    if (!skill) return;
    adoptServerVersion(skill);
  };

  const handleDelete = async () => {
    if (!skill) return;
    setDeleting(true);
    try {
      await api.deleteSkill(skill.id);
      navigation.replace(paths.skills());
      qc.removeQueries({
        queryKey: skillDetailOptions(wsId, skill.id).queryKey,
      });
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(t(($) => $.detail.toast_deleted));
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : t(($) => $.detail.toast_delete_failed),
      );
      setDeleting(false);
      setConfirmDelete(false);
    }
  };

  const handleAddFile = (path: string) => {
    setFiles((prev) => [...prev, { path, content: "" }]);
    setSelectedPath(path);
    setAddingFile(false);
    setFocusPath(path);
  };

  const handleDeleteFile = (path: string = selectedPath) => {
    if (path === SKILL_MD) return;
    setFiles((prev) => prev.filter((f) => f.path !== path));
    if (path === selectedPath) setSelectedPath(SKILL_MD);
  };

  const handleFocusHandled = useCallback(() => setFocusPath(null), []);

  const handleRenameFile = (from: string, to: string) => {
    if (from === SKILL_MD) return;
    setFiles((prev) =>
      prev.map((f) => (f.path === from ? { ...f, path: to } : f)),
    );
    // Follow the file: the selection is a path in the URL, so leaving it on
    // the old one would fall back to SKILL.md.
    if (from === selectedPath) setSelectedPath(to);
  };

  const handleFileContentChange = (newContent: string) => {
    if (!canEdit) return;
    if (selectedPath === SKILL_MD) {
      setContent(newContent);
    } else {
      setFiles((prev) =>
        prev.map((f) =>
          f.path === selectedPath ? { ...f, content: newContent } : f,
        ),
      );
    }
  };

  const supportingQueryDown = !!agentsError || !!membersError || !!runtimesError;

  if (isLoading) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
          <Skeleton className="h-4 w-16" />
          <Skeleton className="h-3 w-3 rounded-xs" />
          <Skeleton className="h-4 w-40" />
        </div>
        <div className={cn(PAGE_RAIL, PAGE_GUTTER, "space-y-3 py-6")}>
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-5/6" />
          <Skeleton className="h-4 w-3/4" />
        </div>
      </div>
    );
  }

  if (error || !skill) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 shrink-0 items-center gap-2 border-b px-3">
          <Button
            variant="ghost"
            size="xs"
            render={<AppLink href={paths.skills()} />}
            nativeButton={false}
          >
            {t(($) => $.detail.all_skills)}
          </Button>
        </div>
        <div className="flex flex-1 flex-col items-center justify-center gap-2 text-center">
          <AlertCircle className="h-8 w-8 text-faint-foreground" />
          <p className="text-body font-medium">{t(($) => $.detail.not_found.title)}</p>
          <p className="max-w-xs text-caption text-muted-foreground">
            {error instanceof Error ? error.message : t(($) => $.detail.not_found.fallback)}
          </p>
          <AppLink
            href={paths.skills()}
            className={`${buttonVariants({ variant: "outline", size: "xs" })} mt-2`}
          >
            {t(($) => $.detail.not_found.back)}
          </AppLink>
        </div>
      </div>
    );
  }

  // Segments reuse the overview field labels so the pill and the fields it
  // points at never use different words for the same thing.
  const changedParts = [
    dirtySummary.nameChanged ? t(($) => $.detail.overview.name) : null,
    dirtySummary.descChanged ? t(($) => $.detail.overview.description) : null,
    dirtySummary.changedFileCount > 0
      ? t(($) => $.detail.save_bar.changed_files, {
          count: dirtySummary.changedFileCount,
        })
      : null,
  ].filter((part): part is string => part !== null);

  const fileSection = (
    <FileSection
      skillName={skill.name}
      filePaths={filePaths}
      selectedPath={selectedPath}
      selectedContent={selectedContent}
      canEdit={canEdit}
      addingFile={addingFile}
      focusEditor={focusPath === selectedPath}
      onStartAddFile={() => setAddingFile(true)}
      onAddFile={handleAddFile}
      onCancelAddFile={() => setAddingFile(false)}
      onDeleteFile={handleDeleteFile}
      onRenameFile={handleRenameFile}
      onContentChange={handleFileContentChange}
      onFocusHandled={handleFocusHandled}
    />
  );

  return (
    // relative: positioning anchor for the floating save pill (page-centered,
    // same rule as the skills list batch toolbar).
    <div className="relative flex flex-1 min-h-0 flex-col">
      <BreadcrumbHeader
        segments={[
          { href: paths.skills(), label: t(($) => $.page.title) },
          ...(selectedPath === SKILL_MD
            ? []
            : [
                {
                  href: paths.skillDetail(skill.id),
                  label: skill.name,
                  className: "min-w-0 max-w-48 truncate font-mono text-caption",
                },
              ]),
        ]}
        leaf={
          <span className="truncate font-mono text-caption text-foreground">
            {selectedPath === SKILL_MD ? skill.name : selectedPath}
          </span>
        }
        actions={
          <>
            {!canEdit && (
              <span className="inline-flex items-center gap-1 text-caption text-muted-foreground">
                <Lock className="h-3 w-3" />
                {t(($) => $.detail.read_only)}
              </span>
            )}
            {canEdit && origin && isRefreshableOrigin(origin) && (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant="outline"
                      size="xs"
                      className="gap-1"
                      onClick={() => setConfirmRefresh(true)}
                    >
                      <RotateCw className="h-3 w-3" />
                      {t(($) => $.detail.refresh.button)}
                    </Button>
                  }
                />
                <TooltipContent>
                  {t(($) => $.detail.refresh.tooltip)}
                </TooltipContent>
              </Tooltip>
            )}
            <Button
              variant="outline"
              size="xs"
              className="gap-1"
              onClick={() => setShowAddToAgents(true)}
            >
              <UserPlus className="h-3 w-3" />
              {t(($) => $.actions.add_to_agent)}
            </Button>
            {canEdit && (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => setConfirmDelete(true)}
                      className="text-muted-foreground hover:text-destructive"
                      aria-label={t(($) => $.detail.delete_aria)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  }
                />
                <TooltipContent>{t(($) => $.detail.delete_tooltip)}</TooltipContent>
              </Tooltip>
            )}
            <IconButton
              label={t(($) => $.detail.toggle_panel)}
              tooltipSide="bottom"
              variant={panelOpen ? "secondary" : "ghost"}
              className={panelOpen ? "" : "text-muted-foreground"}
              aria-pressed={panelOpen}
              onClick={() => setPanelChoice(!panelOpen)}
            >
              <PanelRight />
            </IconButton>
          </>
        }
      />

      {!canEdit && (
        <div className={cn(PAGE_RAIL, PAGE_GUTTER, "pt-3")}>
          <CapabilityBanner
            reason={skillPermissions.canEdit.reason}
            resource="skill"
            ownerName={creator?.name}
          />
        </div>
      )}

      {supportingQueryDown && (
        <div
          role="status"
          className="shrink-0 border-b bg-warning/10 py-2 text-caption text-muted-foreground"
        >
          <div className={cn(PAGE_RAIL, PAGE_GUTTER, "flex items-start gap-2")}>
            <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
            <span>{t(($) => $.detail.supporting_data_warning)}</span>
          </div>
        </div>
      )}

      {conflictPending && canEdit && (
        <div
          role="status"
          aria-live="polite"
          className="shrink-0 border-b border-warning/30 bg-warning/10 py-2 text-caption"
        >
          <div className={cn(PAGE_RAIL, PAGE_GUTTER, "flex items-start gap-2")}>
            <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
            <div className="flex-1">
              <div className="font-medium text-foreground">
                {t(($) => $.detail.conflict_banner.title)}
              </div>
              <div className="mt-0.5 text-muted-foreground">
                {t(($) => $.detail.conflict_banner.body)}
              </div>
            </div>
          </div>
        </div>
      )}

      <div className="relative flex min-h-0 flex-1">
        <div className="flex min-w-0 flex-1 flex-col">{fileSection}</div>
        {panelOpen && (
          <aside className="w-80 shrink-0 border-l bg-background max-md:absolute max-md:inset-y-0 max-md:right-0 max-md:z-20 max-md:shadow-lg">
            <SkillSidePanel
              skill={skill}
              origin={origin}
              originRuntime={originRuntime}
              creator={creator}
              name={name}
              description={description}
              canEdit={canEdit}
              skillAgents={skillAgents}
              onNameChange={setName}
              onDescriptionChange={setDescription}
              onAddToAgents={() => setShowAddToAgents(true)}
            />
          </aside>
        )}
      </div>

      {/* Page-level so it covers edits made to any file or field. Dirty-only and
          floating, matching the skills list batch toolbar; anchored to the
          page root (relative), NOT the viewport. */}
      {canEdit && isDirty && (
        <div
          role="status"
          aria-live="polite"
          className="absolute bottom-6 left-1/2 z-50 flex -translate-x-1/2 animate-in items-center gap-1 rounded-lg border bg-background px-2 py-1.5 fade-in slide-in-from-bottom-2 shadow-lg max-md:above-chat-launcher"
        >
          <div className="mr-1 flex items-center border-r pl-1 pr-2">
            <span className="whitespace-nowrap text-caption text-muted-foreground">
              {t(($) => $.detail.save_bar.changed_summary, {
                parts: changedParts.join(" · "),
              })}
            </span>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="xs"
            onClick={handleDiscard}
            disabled={saving}
          >
            {t(($) => $.detail.save_bar.discard)}
          </Button>
          <Button
            type="button"
            size="xs"
            onClick={handleSave}
            disabled={saving || !name.trim()}
          >
            {saving ? (
              <>
                <Loader2 className="h-3 w-3 animate-spin" />
                {t(($) => $.detail.save_bar.saving)}
              </>
            ) : (
              <>
                <Save className="h-3 w-3" />
                {t(($) => $.detail.save_bar.save)}
              </>
            )}
          </Button>
        </div>
      )}

      {/* Delete confirmation */}
      <Dialog
        open={confirmDelete}
        onOpenChange={(v) => {
          if (!v) setConfirmDelete(false);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t(($) => $.detail.delete_dialog.title)}</DialogTitle>
            <DialogDescription>
              {skillAgents.length > 0
                ? t(($) => $.detail.delete_dialog.description_with_agents, {
                    name: skill.name,
                    count: skillAgents.length,
                  })
                : t(($) => $.detail.delete_dialog.description_no_agents, {
                    name: skill.name,
                  })}
            </DialogDescription>
          </DialogHeader>
          <div className="rounded-md bg-destructive/10 px-3 py-2 text-caption text-destructive">
            {t(($) => $.detail.delete_dialog.warning)}
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => setConfirmDelete(false)}
              disabled={deleting}
            >
              {t(($) => $.detail.delete_dialog.cancel)}
            </Button>
            <Button
              type="button"
              variant="destructive"
              onClick={handleDelete}
              disabled={deleting}
            >
              {deleting ? (
                <>
                  <Loader2 className="h-3 w-3 animate-spin" />
                  {t(($) => $.detail.delete_dialog.deleting)}
                </>
              ) : (
                <>
                  <Trash2 className="h-3 w-3" />
                  {t(($) => $.detail.delete_dialog.confirm)}
                </>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AddToAgentDialog
        skills={[skill]}
        ctx={actionsCtx}
        open={showAddToAgents}
        onOpenChange={setShowAddToAgents}
      />

      <RefreshSkillDialog
        skill={skill}
        origin={origin}
        wsId={wsId}
        open={confirmRefresh}
        onOpenChange={setConfirmRefresh}
        // Adopt explicitly: the user just confirmed the overwrite, so a dirty
        // draft must be replaced instead of tripping the conflict banner.
        onRefreshed={(updated) => adoptServerVersion(updated)}
      />
    </div>
  );
}
