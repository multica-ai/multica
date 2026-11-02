"use client";

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  AlertCircle,
  AlertTriangle,
  ChevronRight,
  File,
  FileText,
  Link2,
  Loader2,
  Lock,
  MoreHorizontal,
  Pencil,
  Plus,
  RotateCw,
  Save,
  Trash2,
} from "@multica/ui/icons";
import { SkillIcon } from "../lib/skill-icon";
import type {
  Agent,
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
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { runtimeListOptions } from "@multica/core/runtimes";
import { parseFrontmatter } from "@multica/core/skills/frontmatter";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "../../navigation";
import { PAGE_GUTTER, PAGE_RAIL } from "../../layout/page-header";
import { CollectionPageState } from "../../layout/collection-page";
import {
  OBJECT_PAGE_BODY,
  ObjectIconMark,
  ObjectIdentity,
  ObjectPage,
  ObjectSection,
  ObjectSectionAction,
  ObjectSectionCard,
} from "../../layout/object-page";
import { RichContent } from "../../rich-content";
import { useCanEditSkill } from "../hooks/use-can-edit-skill";
import { useSkillPermissions } from "@multica/core/permissions";
import { CapabilityBanner } from "@multica/ui/components/common/capability-banner";
import { isRefreshableOrigin, readOrigin } from "../lib/origin";
import { isMarkdownPath, type FileMode } from "./file-viewer";
import { SKILL_MD, SkillFilesEditor } from "./skill-files-editor";
import { SkillPropertiesPanel } from "./skill-properties-panel";
import {
  AddToAgentDialog,
  type SkillActionsContext,
} from "./skill-list-actions";
import { RefreshSkillDialog } from "./refresh-skill-dialog";
import { useT } from "../../i18n";

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

/**
 * The page body: the skill's sections, or its files beside an editor. The
 * files need more width than the reading column, so they take the body over
 * instead of being a section of it. `?view=files` is also what the retired
 * Files tab wrote, so its links still land on the files.
 */
type DetailView = "overview" | "files";

function readView(value: string | null): DetailView {
  return value === "files" ? "files" : "overview";
}

const SKILL_DETAIL_LAYOUT_ID = "multica_skill_detail_layout";

function Notice({
  title,
  children,
}: {
  title?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div
      role="status"
      aria-live="polite"
      className="flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-caption"
    >
      <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
      <div className="min-w-0 flex-1">
        {title && <div className="font-medium text-foreground">{title}</div>}
        <div className={cn("text-muted-foreground", title && "mt-0.5")}>
          {children}
        </div>
      </div>
    </div>
  );
}

/**
 * SKILL.md as it reads, frontmatter stripped since name and description are
 * properties of their own. Clamped, with a toggle only when something is
 * clamped: a long skill would otherwise push every section below it out of
 * sight.
 */
function SkillContentPreview({ content }: { content: string }) {
  const { t } = useT("skills");
  const { t: tCommon } = useT("common");
  const body = useMemo(() => parseFrontmatter(content).body, [content]);
  const [expanded, setExpanded] = useState(false);
  const [clamped, setClamped] = useState(false);
  const frameRef = useRef<HTMLDivElement | null>(null);
  const innerRef = useRef<HTMLDivElement | null>(null);

  const measure = useCallback(() => {
    const frame = frameRef.current;
    const inner = innerRef.current;
    if (!frame || !inner || expanded) return;
    setClamped(inner.scrollHeight > frame.clientHeight + 1);
  }, [expanded]);

  useLayoutEffect(() => {
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    if (frameRef.current) observer.observe(frameRef.current);
    if (innerRef.current) observer.observe(innerRef.current);
    return () => observer.disconnect();
  }, [measure, body]);

  return (
    <ObjectSectionCard>
      <div
        ref={frameRef}
        className={cn("relative px-4 py-3", !expanded && "max-h-80 overflow-hidden")}
      >
        <div ref={innerRef} data-testid="skill-content-preview">
          <RichContent
            content={body || t(($) => $.file_viewer.no_content)}
            density="document"
            phase="settled"
          />
        </div>
        {clamped && !expanded && (
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-x-0 bottom-0 h-12 bg-gradient-to-t from-card to-transparent"
          />
        )}
      </div>
      {(clamped || expanded) && (
        <button
          type="button"
          onClick={() => setExpanded((value) => !value)}
          className="flex h-9 w-full items-center px-4 text-caption text-muted-foreground transition-colors hover:text-foreground focus-visible:text-foreground focus-visible:outline-none"
        >
          {expanded
            ? tCommon(($) => $.expandable_description.collapse)
            : tCommon(($) => $.expandable_description.expand)}
        </button>
      )}
    </ObjectSectionCard>
  );
}

function SupportingFileList({
  paths,
  onOpen,
}: {
  paths: string[];
  onOpen: (path: string) => void;
}) {
  const { t } = useT("skills");
  if (paths.length === 0) {
    return (
      <div className="rounded-lg border border-dashed px-3 py-6 text-center text-caption text-muted-foreground">
        {t(($) => $.detail.files.supporting_empty)}
      </div>
    );
  }
  return (
    <ObjectSectionCard>
      {[...paths].sort().map((path) => {
        const Icon = isMarkdownPath(path) ? FileText : File;
        return (
          <button
            key={path}
            type="button"
            onClick={() => onOpen(path)}
            className="group/file flex h-10 w-full items-center gap-2.5 px-3 text-left text-body transition-colors hover:bg-accent/50 focus-visible:bg-accent/50 focus-visible:outline-none"
          >
            <Icon className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
            <span className="min-w-0 flex-1 truncate font-mono text-caption">
              {path}
            </span>
            <ChevronRight
              className="size-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover/file:opacity-100"
              aria-hidden="true"
            />
          </button>
        );
      })}
    </ObjectSectionCard>
  );
}

function UsedByList({ agents }: { agents: Agent[] }) {
  const { t } = useT("skills");
  if (agents.length === 0) {
    return (
      <div className="rounded-lg border border-dashed px-3 py-6 text-center text-caption text-muted-foreground">
        {t(($) => $.detail.overview.used_by_empty)}
      </div>
    );
  }
  return (
    <ObjectSectionCard>
      {agents.map((a) => (
        <div key={a.id} className="flex min-h-11 items-center gap-2.5 px-3 py-2">
          <ActorAvatar
            name={a.name}
            initials={a.name.slice(0, 2).toUpperCase()}
            avatarUrl={resolvePublicFileUrl(a.avatar_url)}
            isAgent
            size="md"
          />
          <div className="min-w-0 flex-1">
            <div className="truncate text-body font-medium">{a.name}</div>
            {a.description && (
              <div className="truncate text-caption text-muted-foreground">
                {a.description}
              </div>
            )}
          </div>
        </div>
      ))}
    </ObjectSectionCard>
  );
}

// Main page

export function SkillDetailPage({ skillId }: { skillId: string }) {
  const { t } = useT("skills");
  const { t: tCommon } = useT("common");
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
  const [selectedPath, setSelectedPath] = useState(SKILL_MD);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [confirmRefresh, setConfirmRefresh] = useState(false);
  const [showAddToAgents, setShowAddToAgents] = useState(false);
  const [addingFile, setAddingFile] = useState(false);
  const [conflictPending, setConflictPending] = useState(false);

  // Preview/raw is a property of how the user wants to work, so it lives here
  // and survives switching files. It used to live inside FileViewer, which the
  // per-file `key` remounted — every file switch silently snapped back.
  const [fileMode, setFileMode] = useState<FileMode>("preview");
  // Which file's editor is waiting for the caret. A path rather than a boolean
  // so a request raised for one row cannot land in another row's editor if the
  // selection moves before the effect runs.
  const [focusPath, setFocusPath] = useState<string | null>(null);

  const urlView = navigation.searchParams.get("view");
  const [activeView, setActiveView] = useState<DetailView>(() =>
    readView(urlView),
  );
  const lastUrlViewRef = useRef(urlView);

  useEffect(() => {
    if (urlView === lastUrlViewRef.current) return;
    lastUrlViewRef.current = urlView;
    setActiveView(readView(urlView));
  }, [urlView]);

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
    (s: Skill, resetSelection = false) => {
      seededKeyRef.current = `${wsId}:${s.id}@${s.updated_at}`;
      setConflictPending(false);
      seedFromSkill(s);
      if (resetSelection) setSelectedPath(SKILL_MD);
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

    adoptServerVersion(skill, !sameSkill);
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
  const selectedContent = fileMap.get(selectedPath) ?? "";

  useEffect(() => {
    if (selectedPath !== SKILL_MD && !fileMap.has(selectedPath)) {
      setSelectedPath(SKILL_MD);
    }
  }, [fileMap, selectedPath]);

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

  const viewHref = (view: DetailView) => {
    const params = new URLSearchParams(navigation.searchParams);
    if (view === "overview") params.delete("view");
    else params.set("view", view);
    const query = params.toString();
    return `${navigation.pathname}${query ? `?${query}` : ""}`;
  };

  /**
   * Opens the files on `path`. "Edit" owes three things in one gesture: the
   * file is open, the pane is the editor rather than the preview, and the
   * caret is in it — fewer would leave the user a click away from typing.
   */
  const openFiles = (
    path: string,
    { edit = false, add = false }: { edit?: boolean; add?: boolean } = {},
  ) => {
    setSelectedPath(path);
    if (edit && canEdit) {
      setFileMode("raw");
      setFocusPath(path);
    }
    if (add && canEdit) setAddingFile(true);
    setActiveView("files");
    navigation.push(viewHref("files"));
  };

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

  const handleCopyLink = () => {
    void copyText(navigation.getShareableUrl(paths.skillDetail(skillId))).then(
      (ok) => {
        if (ok) toast.success(t(($) => $.detail.menu.link_copied));
      },
    );
  };

  const handleAddFile = (path: string) => {
    setFiles((prev) => [...prev, { path, content: "" }]);
    setSelectedPath(path);
    setAddingFile(false);
  };

  // Defaults to the open file so the editor's own delete button keeps working;
  // the tree passes the row that was acted on, which need not be the open one.
  const handleDeleteFile = (path: string = selectedPath) => {
    if (path === SKILL_MD) return;
    setFiles((prev) => prev.filter((f) => f.path !== path));
    if (path === selectedPath) setSelectedPath(SKILL_MD);
  };

  const handleEditFile = useCallback(
    (path: string) => {
      if (!canEdit) return;
      setSelectedPath(path);
      setFileMode("raw");
      setFocusPath(path);
    },
    [canEdit],
  );

  const handleFocusHandled = useCallback(() => setFocusPath(null), []);

  const handleRenameFile = (from: string, to: string) => {
    if (from === SKILL_MD) return;
    setFiles((prev) =>
      prev.map((f) => (f.path === from ? { ...f, path: to } : f)),
    );
    // Follow the file: the rail is keyed by path, so leaving the selection on
    // the old one would land on a row that no longer exists.
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
  const listSegment = { href: paths.skills(), label: t(($) => $.page.title) };

  if (isLoading) {
    return (
      <ObjectPage
        layoutId={SKILL_DETAIL_LAYOUT_ID}
        segments={[listSegment]}
        leaf={<Skeleton className="h-4 w-40" />}
      >
        <div className={cn(OBJECT_PAGE_BODY, "space-y-8")}>
          <div className="flex items-center gap-3">
            <Skeleton className="size-12 rounded-xl" />
            <div className="flex-1 space-y-2">
              <Skeleton className="h-5 w-48" />
              <Skeleton className="h-4 w-3/4" />
            </div>
          </div>
          <div className="space-y-3">
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-5/6" />
            <Skeleton className="h-4 w-3/4" />
          </div>
        </div>
      </ObjectPage>
    );
  }

  if (error || !skill) {
    return (
      <ObjectPage
        layoutId={SKILL_DETAIL_LAYOUT_ID}
        segments={[listSegment]}
        leaf={
          <span className="truncate text-muted-foreground">
            {t(($) => $.detail.not_found.title)}
          </span>
        }
      >
        <CollectionPageState
          role="alert"
          icon={AlertCircle}
          title={t(($) => $.detail.not_found.title)}
          description={
            error instanceof Error ? error.message : t(($) => $.detail.not_found.fallback)
          }
          actions={
            <AppLink
              href={paths.skills()}
              className={buttonVariants({ variant: "outline", size: "xs" })}
            >
              {t(($) => $.detail.not_found.back)}
            </AppLink>
          }
        />
      </ObjectPage>
    );
  }

  // Segments reuse the property labels so the pill and the fields it points
  // at never use different words for the same thing.
  const changedParts = [
    dirtySummary.nameChanged ? t(($) => $.detail.overview.name) : null,
    dirtySummary.descChanged ? t(($) => $.detail.overview.description) : null,
    dirtySummary.changedFileCount > 0
      ? t(($) => $.detail.save_bar.changed_files, {
          count: dirtySummary.changedFileCount,
        })
      : null,
  ].filter((part): part is string => part !== null);

  const canRefresh = canEdit && !!origin && isRefreshableOrigin(origin);
  const displayName = name.trim() || skill.name;
  const supportingPaths = filePaths.filter((p) => p !== SKILL_MD);

  const notices = [
    !canEdit && (
      <CapabilityBanner
        key="capability"
        reason={skillPermissions.canEdit.reason}
        resource="skill"
        ownerName={creator?.name}
      />
    ),
    supportingQueryDown && (
      <Notice key="supporting">{t(($) => $.detail.supporting_data_warning)}</Notice>
    ),
    conflictPending && canEdit && (
      <Notice key="conflict" title={t(($) => $.detail.conflict_banner.title)}>
        {t(($) => $.detail.conflict_banner.body)}
      </Notice>
    ),
  ].filter(Boolean);

  const headerActions = (
    <>
      <Button size="sm" onClick={() => setShowAddToAgents(true)}>
        <Plus />
        {t(($) => $.actions.add_to_agent)}
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <IconButton
              label={tCommon(($) => $.more_actions)}
              tooltipSide="bottom"
              className="text-muted-foreground"
            >
              <MoreHorizontal />
            </IconButton>
          }
        />
        <DropdownMenuContent align="end" className="w-auto min-w-48">
          <DropdownMenuItem onClick={handleCopyLink}>
            <Link2 />
            {t(($) => $.detail.menu.copy_link)}
          </DropdownMenuItem>
          {canRefresh && (
            <DropdownMenuItem onClick={() => setConfirmRefresh(true)}>
              <RotateCw />
              {t(($) => $.actions.refresh)}
            </DropdownMenuItem>
          )}
          {canEdit && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                variant="destructive"
                onClick={() => setConfirmDelete(true)}
              >
                <Trash2 />
                {t(($) => $.detail.menu.delete)}
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </>
  );

  const panel = origin && (
    <SkillPropertiesPanel
      skill={skill}
      name={name}
      description={description}
      canEdit={canEdit}
      origin={origin}
      originRuntime={originRuntime}
      creator={creator}
      onNameChange={setName}
      onDescriptionChange={setDescription}
    />
  );

  const skillLeaf = (
    <span className="flex min-w-0 items-center gap-1.5 text-foreground">
      <SkillIcon className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
      <span className="truncate">{displayName}</span>
    </span>
  );

  const overview = (
    <div className={cn(OBJECT_PAGE_BODY, "space-y-8 pb-24")}>
      <div className="space-y-4">
        <ObjectIdentity
          media={<ObjectIconMark icon={SkillIcon} />}
          title={displayName}
          description={description.trim() || undefined}
          trailing={
            !canEdit && (
              <span className="inline-flex shrink-0 items-center gap-1 rounded-md bg-muted px-1.5 py-0.5 text-caption text-muted-foreground">
                <Lock className="size-3" aria-hidden="true" />
                {t(($) => $.detail.read_only)}
              </span>
            )
          }
        />
        {notices.length > 0 && <div className="space-y-2">{notices}</div>}
      </div>

      <ObjectSection
        title={t(($) => $.detail.sections.content)}
        count={<span className="font-mono">{SKILL_MD}</span>}
        action={
          canEdit ? (
            <ObjectSectionAction
              icon={Pencil}
              onClick={() => openFiles(SKILL_MD, { edit: true })}
            >
              {t(($) => $.detail.sections.edit)}
            </ObjectSectionAction>
          ) : (
            <ObjectSectionAction
              icon={FileText}
              onClick={() => openFiles(SKILL_MD)}
            >
              {t(($) => $.detail.sections.open)}
            </ObjectSectionAction>
          )
        }
      >
        <SkillContentPreview content={content} />
      </ObjectSection>

      <ObjectSection
        title={t(($) => $.detail.sections.files)}
        count={supportingPaths.length}
        action={
          canEdit && (
            <ObjectSectionAction
              icon={Plus}
              onClick={() => openFiles(selectedPath, { add: true })}
            >
              {t(($) => $.detail.sections.add)}
            </ObjectSectionAction>
          )
        }
      >
        <SupportingFileList paths={supportingPaths} onOpen={openFiles} />
      </ObjectSection>

      <ObjectSection
        title={t(($) => $.table.used_by)}
        count={skillAgents.length}
        action={
          <ObjectSectionAction icon={Plus} onClick={() => setShowAddToAgents(true)}>
            {t(($) => $.actions.add_to_agent)}
          </ObjectSectionAction>
        }
      >
        <UsedByList agents={skillAgents} />
      </ObjectSection>
    </div>
  );

  const filesView = (
    <div className="flex min-h-full flex-col md:h-full">
      {notices.length > 0 && (
        <div className={cn(PAGE_RAIL, PAGE_GUTTER, "space-y-2 pt-3")}>
          {notices}
        </div>
      )}
      <div className="flex min-h-0 flex-1 flex-col">
        <SkillFilesEditor
          filePaths={filePaths}
          selectedPath={selectedPath}
          selectedContent={selectedContent}
          mode={fileMode}
          canEdit={canEdit}
          addingFile={addingFile}
          focusEditor={focusPath === selectedPath}
          onSelectPath={setSelectedPath}
          onModeChange={setFileMode}
          onStartAddFile={() => setAddingFile(true)}
          onAddFile={handleAddFile}
          onCancelAddFile={() => setAddingFile(false)}
          onDeleteFile={handleDeleteFile}
          onRenameFile={handleRenameFile}
          onEditFile={handleEditFile}
          onContentChange={handleFileContentChange}
          onFocusHandled={handleFocusHandled}
        />
      </div>
    </div>
  );

  return (
    // relative: positioning anchor for the floating save pill (page-centered,
    // same rule as the skills list batch toolbar).
    <div className="relative flex min-h-0 flex-1 flex-col">
      <ObjectPage
        layoutId={SKILL_DETAIL_LAYOUT_ID}
        segments={
          activeView === "files"
            ? [
                listSegment,
                {
                  href: paths.skillDetail(skill.id),
                  label: displayName,
                  className: "min-w-0 max-w-72 truncate",
                },
              ]
            : [listSegment]
        }
        leaf={
          activeView === "files" ? (
            <span className="truncate font-mono text-caption text-foreground">
              {selectedPath}
            </span>
          ) : (
            skillLeaf
          )
        }
        actions={headerActions}
        panel={panel}
      >
        {activeView === "files" ? filesView : overview}
      </ObjectPage>

      {/* Dirty-only and floating, matching the skills list batch toolbar;
          anchored to the page root (relative), NOT the viewport. */}
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
