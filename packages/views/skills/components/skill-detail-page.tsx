"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AlertCircle,
  AlertTriangle,
  Clock3,
  Download,
  FileText,
  HardDrive,
  Loader2,
  Lock,
  Pencil,
  Plus,
  Save,
  Trash2,
  UserPlus,
  Users,
} from "lucide-react";
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
import { useTimeAgo } from "../../i18n";
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
import {
  runtimeDisplayLabel,
  runtimeListOptions,
} from "@multica/core/runtimes";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
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
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "../../navigation";
import { BreadcrumbHeader } from "../../layout/breadcrumb-header";
import { useCanEditSkill } from "../hooks/use-can-edit-skill";
import { useSkillPermissions } from "@multica/core/permissions";
import { CapabilityBanner } from "@multica/ui/components/common/capability-banner";
import { readOrigin, totalFileCount, type OriginInfo } from "../lib/origin";
import { FileTree } from "./file-tree";
import { FileViewer, isMarkdownPath, type FileMode } from "./file-viewer";
import {
  AddToAgentDialog,
  type SkillActionsContext,
} from "./skill-list-actions";
import { useT } from "../../i18n";
import {
  ResourceLabelPicker,
  useResourceLabelsEnabled,
} from "../../labels/resource-label-picker";

const SKILL_MD = "SKILL.md";

type DraftFile = { id?: string; path: string; content: string };

/**
 * Two tabs, not three. A skill has no settings axis to speak of:
 * `UpdateSkillRequest` carries only name / description / content / config /
 * files, and edit rights are derived (creator or workspace admin) with no
 * writable counterpart — so a Settings tab would hold a delete button and a
 * read-only sentence. Delete lives in the header instead, matching the agent
 * detail page where Archive sits in the header.
 */
type DetailView = "overview" | "files";

function isDetailView(value: string | null): value is DetailView {
  return value === "overview" || value === "files";
}

// ---------------------------------------------------------------------------
// File path validation + inline add
// ---------------------------------------------------------------------------

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
  onAdd,
  onCancel,
}: {
  existingPaths: string[];
  onAdd: (path: string) => void;
  onCancel: () => void;
}) {
  const { t } = useT("skills");
  const validate = useValidateNewFilePath();
  const [path, setPath] = useState("");
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
          {t(($) => $.detail.add_file.add)}
        </Button>
        <Button type="button" size="xs" variant="ghost" onClick={onCancel}>
          {t(($) => $.detail.add_file.cancel)}
        </Button>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

function useOriginLabel(origin: OriginInfo | null, runtime: AgentRuntime | null) {
  const { t } = useT("skills");
  if (!origin) return null;
  if (origin.type === "runtime_local") {
    return runtime
      ? t(($) => $.detail.subline.origin_runtime_named, {
          name: runtimeDisplayLabel(runtime),
        })
      : origin.provider
        ? t(($) => $.detail.subline.origin_runtime_provider, { provider: origin.provider })
        : t(($) => $.detail.subline.origin_runtime_unknown);
  }
  if (origin.type === "clawhub") return t(($) => $.detail.subline.origin_clawhub);
  if (origin.type === "skills_sh") return t(($) => $.detail.subline.origin_skills_sh);
  if (origin.type === "github") return t(($) => $.detail.subline.origin_github);
  return t(($) => $.detail.subline.origin_workspace);
}

/**
 * Identity strip under the breadcrumb: mark, name, and the counts that say
 * what this skill is made of. One line, because everything a reader would
 * scroll past it for is a field on the Overview tab.
 *
 * The description is not repeated here. It used to be, above an editable copy
 * of itself two tabs' worth of chrome below — every visit to a page whose only
 * verbs are edit, add and delete paid for a read-only restatement of a field
 * the next screenful lets you change. The list this page is reached from
 * already carries the description for anyone deciding whether to open it.
 *
 * The agent detail header still has the taller form. Bringing it across is a
 * separate change to a page this branch does not otherwise touch.
 */
function SkillIdentity({
  skill,
  origin,
  originRuntime,
  agentCount,
  creator,
}: {
  skill: Skill;
  origin: OriginInfo | null;
  originRuntime: AgentRuntime | null;
  agentCount: number;
  creator: MemberWithUser | null;
}) {
  const { t } = useT("skills");
  const timeAgo = useTimeAgo();
  const originLabel = useOriginLabel(origin, originRuntime);
  const isRuntimeOrigin = origin?.type === "runtime_local";

  return (
    <div className="shrink-0 border-b px-4 py-3 sm:px-6">
      <div className="mx-auto flex max-w-[1440px] flex-wrap items-center gap-x-4 gap-y-1.5">
        <div className="flex min-w-0 items-center gap-2.5">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border bg-muted text-muted-foreground">
            <SkillIcon className="h-4 w-4" aria-hidden="true" />
          </div>
          <h1 className="min-w-0 truncate font-mono text-title font-semibold tracking-tight">

            {skill.name}
          </h1>
        </div>
        <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 text-caption text-muted-foreground sm:ml-auto">
          {originLabel && (
            <span className="inline-flex min-w-0 items-center gap-1.5">
              {/* Same three-way split as the list's Source column: runtime,
                  created here, imported. */}
              {isRuntimeOrigin ? (
                <HardDrive className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
              ) : origin?.type === "manual" ? (
                <Pencil className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
              ) : (
                <Download className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
              )}
              <span className="truncate">{originLabel}</span>
            </span>
          )}
          <span className="inline-flex items-center gap-1.5">
            <FileText className="h-3.5 w-3.5" aria-hidden="true" />
            {t(($) => $.detail.header.files, { count: totalFileCount(skill) })}
          </span>
          <span className="inline-flex items-center gap-1.5">
            <Users className="h-3.5 w-3.5" aria-hidden="true" />
            {t(($) => $.detail.header.used_by, { count: agentCount })}
          </span>
          <span className="inline-flex items-center gap-1.5">
            <Clock3 className="h-3.5 w-3.5" aria-hidden="true" />
            {creator
              ? t(($) => $.detail.header.updated_by, {
                  when: timeAgo(skill.updated_at),
                  name: creator.name,
                })
              : t(($) => $.detail.header.updated, { when: timeAgo(skill.updated_at) })}
          </span>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Overview tab
// ---------------------------------------------------------------------------

function PropertyRow({
  label,
  htmlFor,
  children,
}: {
  label: string;
  htmlFor?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="grid gap-1.5 py-2.5 sm:grid-cols-[128px_minmax(0,1fr)] sm:gap-4">
      <label
        htmlFor={htmlFor}
        className="pt-1.5 text-caption text-muted-foreground sm:text-body"
      >
        {label}
      </label>
      <div className="min-w-0">{children}</div>
    </div>
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
    <ul className="divide-y overflow-hidden rounded-lg border bg-card">
      {agents.map((a) => (
        <li key={a.id} className="flex items-center gap-2.5 px-3 py-2.5">
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
        </li>
      ))}
    </ul>
  );
}

function OverviewTab({
  skill,
  name,
  description,
  canEdit,
  creatorName,
  skillAgents,
  onNameChange,
  onDescriptionChange,
  onAddToAgents,
}: {
  skill: Skill;
  name: string;
  description: string;
  canEdit: boolean;
  creatorName: string | null;
  skillAgents: Agent[];
  onNameChange: (value: string) => void;
  onDescriptionChange: (value: string) => void;
  onAddToAgents: () => void;
}) {
  const { t } = useT("skills");
  const labelsEnabled = useResourceLabelsEnabled();

  return (
    <div className="mx-auto w-full max-w-3xl p-4 sm:p-6 md:p-8">
      <section>
        <h2 className="text-title-sm font-medium">{t(($) => $.detail.overview.properties)}</h2>
        <p className="mt-1 text-caption text-muted-foreground">
          {t(($) => $.detail.overview.properties_hint)}
        </p>
        <div className="mt-4 divide-y">
          <PropertyRow label={t(($) => $.detail.overview.name)} htmlFor="skill-name">
            <Input
              id="skill-name"
              value={name}
              readOnly={!canEdit}
              onChange={(e) => onNameChange(e.target.value)}
              placeholder={t(($) => $.detail.name_placeholder)}
              className="font-mono text-body read-only:cursor-default"
            />
          </PropertyRow>

          <PropertyRow
            label={t(($) => $.detail.overview.description)}
            htmlFor="skill-description"
          >
            {/* Real descriptions run 500–900 characters (they carry the
                trigger vocabulary an agent matches on), so this field is
                sized for the data rather than the two rows it had before. */}
            <Textarea
              id="skill-description"
              value={description}
              readOnly={!canEdit}
              onChange={(e) => onDescriptionChange(e.target.value)}
              placeholder={t(($) => $.detail.description_placeholder)}
              rows={6}
              className="text-body leading-relaxed read-only:cursor-default"
            />
            <p className="mt-1.5 text-caption text-muted-foreground">
              {t(($) => $.detail.overview.description_hint, {
                count: description.length,
              })}
            </p>
          </PropertyRow>

          {labelsEnabled && (
            <PropertyRow label={t(($) => $.detail.overview.labels)}>
              <ResourceLabelPicker
                resourceType="skill"
                resourceId={skill.id}
                canEdit={canEdit}
              />
            </PropertyRow>
          )}
        </div>
      </section>

      <section className="mt-10">
        <div className="flex items-center justify-between gap-3">
          <h2 className="min-w-0 text-title-sm font-medium">
            {t(($) => $.detail.overview.used_by, { count: skillAgents.length })}
          </h2>
          <Button
            variant="outline"
            size="xs"
            className="shrink-0 gap-1"
            onClick={onAddToAgents}
          >
            <UserPlus className="h-3 w-3" />
            {t(($) => $.actions.add_to_agent)}
          </Button>
        </div>
        <div className="mt-3">
          <UsedByList agents={skillAgents} />
        </div>
      </section>

      <p className="mt-10 rounded-lg bg-muted px-3 py-2.5 text-caption leading-relaxed text-muted-foreground">
        {canEdit
          ? t(($) => $.detail.overview.permissions_owner)
          : creatorName
            ? t(($) => $.detail.overview.permissions_locked_creator, { name: creatorName })
            : t(($) => $.detail.overview.permissions_locked)}
      </p>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Files tab
// ---------------------------------------------------------------------------

function FilesTab({
  filePaths,
  selectedPath,
  selectedContent,
  mode,
  canEdit,
  addingFile,
  onSelectPath,
  onModeChange,
  onStartAddFile,
  onAddFile,
  onCancelAddFile,
  onDeleteFile,
  onRenameFile,
  onContentChange,
}: {
  filePaths: string[];
  selectedPath: string;
  selectedContent: string;
  mode: FileMode;
  canEdit: boolean;
  addingFile: boolean;
  onSelectPath: (path: string) => void;
  onModeChange: (mode: FileMode) => void;
  onStartAddFile: () => void;
  onAddFile: (path: string) => void;
  onCancelAddFile: () => void;
  onDeleteFile: (path?: string) => void;
  onRenameFile: (from: string, to: string) => void;
  onContentChange: (content: string) => void;
}) {
  const { t } = useT("skills");
  const validatePath = useValidateNewFilePath();
  const supportingPaths = filePaths.filter((p) => p !== SKILL_MD);
  const isMd = isMarkdownPath(selectedPath);
  // Absent for read-only viewers so the tree never offers an action it would
  // then refuse. SKILL.md is excluded inside the tree by reservedPath.
  const treeActions = canEdit
    ? {
        validatePath,
        onRename: onRenameFile,
        onDelete: onDeleteFile,
        reservedPath: SKILL_MD,
      }
    : undefined;

  return (
    <div className="flex min-h-full flex-col md:h-full md:flex-row">
      {/* The file list IS the second-level navigation, so it uses the same
          rail treatment as the agent detail page's capability/settings nav
          instead of inventing a third sidebar style. */}
      <aside
        role="tablist"
        aria-orientation="vertical"
        aria-label={t(($) => $.detail.files.list_aria)}
        className="shrink-0 border-b border-surface-border p-3 md:w-52 md:overflow-y-auto md:border-b-0 md:border-r md:p-4"
      >
        <p className="px-2.5 pb-1 text-micro font-semibold uppercase tracking-wider text-muted-foreground">
          {t(($) => $.detail.files.main)}
        </p>
        <FileTree
          filePaths={[SKILL_MD]}
          selectedPath={selectedPath}
          onSelect={onSelectPath}
        />

        <p className="px-2.5 pb-1 pt-3 text-micro font-semibold uppercase tracking-wider text-muted-foreground">
          {t(($) => $.detail.files.supporting, { count: supportingPaths.length })}
        </p>
        {supportingPaths.length > 0 ? (
          <FileTree
            actions={treeActions}
            filePaths={supportingPaths}
            selectedPath={selectedPath}
            onSelect={onSelectPath}
          />
        ) : (
          !addingFile && (
            <p className="px-2.5 py-1 text-caption text-muted-foreground">
              {t(($) => $.detail.files.supporting_empty)}
            </p>
          )
        )}

        {canEdit &&
          (addingFile ? (
            <AddFileInline
              existingPaths={filePaths}
              onAdd={onAddFile}
              onCancel={onCancelAddFile}
            />
          ) : (
            <Button
              type="button"
              variant="ghost"
              size="xs"
              onClick={onStartAddFile}
              className="mt-2 h-8 w-full justify-start px-2.5 text-muted-foreground"
            >
              <Plus className="h-3.5 w-3.5" />
              {t(($) => $.detail.files.add_file)}
            </Button>
          ))}
      </aside>

      <section className="flex min-h-[32rem] min-w-0 flex-1 flex-col md:min-h-0">
        <div className="flex h-10 shrink-0 items-center gap-3 border-b px-3 sm:px-4">
          <span className="truncate font-mono text-caption text-muted-foreground">
            {selectedPath}
          </span>
          <div className="ml-auto flex shrink-0 items-center gap-1">
            {isMd && (
              <div
                role="group"
                aria-label={t(($) => $.detail.files.mode_aria)}
                className="flex items-center gap-0.5 rounded-md bg-muted p-0.5"
              >
                {(["preview", "raw"] as const).map((value) => (
                  <button
                    key={value}
                    type="button"
                    aria-pressed={mode === value}
                    onClick={() => onModeChange(value)}
                    className={cn(
                      "h-6 rounded px-2 text-caption font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                      mode === value
                        ? "bg-surface text-foreground shadow-sm"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    {value === "preview"
                      ? t(($) => $.detail.files.mode_preview)
                      : t(($) => $.detail.files.mode_raw)}
                  </button>
                ))}
              </div>
            )}
            {selectedPath !== SKILL_MD && canEdit && (
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

        <div className="min-h-0 flex-1">
          <FileViewer
            key={selectedPath}
            path={selectedPath}
            content={selectedContent}
            mode={mode}
            readOnly={!canEdit}
            onChange={onContentChange}
          />
        </div>
      </section>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main page
// ---------------------------------------------------------------------------

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
  const [selectedPath, setSelectedPath] = useState(SKILL_MD);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [showAddToAgents, setShowAddToAgents] = useState(false);
  const [addingFile, setAddingFile] = useState(false);
  const [conflictPending, setConflictPending] = useState(false);

  // Preview/raw is a property of how the user wants to work, so it lives here
  // and survives switching files. It used to live inside FileViewer, which the
  // per-file `key` remounted — every file switch silently snapped back.
  const [fileMode, setFileMode] = useState<FileMode>("preview");

  const urlView = navigation.searchParams.get("view");
  const [activeView, setActiveView] = useState<DetailView>(() =>
    isDetailView(urlView) ? urlView : "overview",
  );
  const lastUrlViewRef = useRef(urlView);

  useEffect(() => {
    if (urlView === lastUrlViewRef.current) return;
    lastUrlViewRef.current = urlView;
    setActiveView(isDetailView(urlView) ? urlView : "overview");
  }, [urlView]);

  const selectView = useCallback(
    (next: DetailView) => {
      setActiveView(next);
      const params = new URLSearchParams(navigation.searchParams);
      if (next === "overview") params.delete("view");
      else params.set("view", next);
      const query = params.toString();
      navigation.replace(`${navigation.pathname}${query ? `?${query}` : ""}`);
    },
    [navigation],
  );

  const draftRef = useRef({ name, description, content, files });
  draftRef.current = { name, description, content, files };

  const seededKeyRef = useRef<string | null>(null);

  useEffect(() => {
    if (!skill) return;
    const key = `${wsId}:${skill.id}@${skill.updated_at}`;
    if (seededKeyRef.current === key) return;

    const sameSkill =
      seededKeyRef.current !== null &&
      seededKeyRef.current.startsWith(`${wsId}:${skill.id}@`);

    if (sameSkill) {
      const d = draftRef.current;
      const serverFilesJson = JSON.stringify(
        (skill.files ?? []).map((f) => ({ path: f.path, content: f.content })),
      );
      const draftFilesJson = JSON.stringify(
        d.files.map((f) => ({ path: f.path, content: f.content })),
      );
      const hasEdits =
        d.name.trim() !== skill.name ||
        d.description.trim() !== skill.description ||
        d.content !== skill.content ||
        draftFilesJson !== serverFilesJson;
      if (hasEdits) {
        setConflictPending(true);
        return;
      }
    }

    seededKeyRef.current = key;
    setConflictPending(false);
    setName(skill.name);
    setDescription(skill.description);
    setContent(skill.content);
    setFiles(
      (skill.files ?? []).map((f: SkillFile) => ({
        id: f.id,
        path: f.path,
        content: f.content,
      })),
    );
    if (!sameSkill) setSelectedPath(SKILL_MD);
  }, [skill, wsId]);

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

  // Files are matched by id so a rename counts as one changed file, not a
  // delete plus an add; SKILL.md is its own entry since it lives in `content`.
  const dirtySummary = useMemo(() => {
    if (!skill) return { nameChanged: false, descChanged: false, changedFileCount: 0 };
    const serverFiles: SkillFile[] = skill.files ?? [];
    const serverById = new Map(serverFiles.map((f) => [f.id, f]));
    let changedFileCount = content !== skill.content ? 1 : 0;
    const draftIds = new Set<string>();
    for (const f of files) {
      if (f.id) draftIds.add(f.id);
      const server = f.id ? serverById.get(f.id) : undefined;
      if (!server || server.path !== f.path || server.content !== f.content) {
        changedFileCount += 1;
      }
    }
    for (const f of serverFiles) {
      if (!draftIds.has(f.id)) changedFileCount += 1;
    }
    return {
      nameChanged: name.trim() !== skill.name,
      descChanged: description.trim() !== skill.description,
      changedFileCount,
    };
  }, [skill, name, description, content, files]);

  const isDirty =
    dirtySummary.nameChanged ||
    dirtySummary.descChanged ||
    dirtySummary.changedFileCount > 0;

  const seedFromSkill = (s: Skill) => {
    setName(s.name);
    setDescription(s.description);
    setContent(s.content);
    setFiles(
      (s.files ?? []).map((f: SkillFile) => ({
        id: f.id,
        path: f.path,
        content: f.content,
      })),
    );
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
      seedFromSkill(updated);
      seededKeyRef.current = `${wsId}:${updated.id}@${updated.updated_at}`;
      setConflictPending(false);
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
    seedFromSkill(skill);
    seededKeyRef.current = `${wsId}:${skill.id}@${skill.updated_at}`;
    setConflictPending(false);
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
  };

  // Defaults to the open file so the editor's own delete button keeps working;
  // the tree passes the row that was acted on, which need not be the open one.
  const handleDeleteFile = (path: string = selectedPath) => {
    if (path === SKILL_MD) return;
    setFiles((prev) => prev.filter((f) => f.path !== path));
    if (path === selectedPath) setSelectedPath(SKILL_MD);
  };

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

  if (isLoading) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
          <Skeleton className="h-4 w-16" />
          <Skeleton className="h-3 w-3 rounded" />
          <Skeleton className="h-4 w-40" />
        </div>
        <div className="space-y-3 p-6">
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
          <AlertCircle className="h-8 w-8 text-muted-foreground/40" />
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

  const TABS: { id: DetailView; label: string }[] = [
    { id: "overview", label: t(($) => $.detail.tabs.overview) },
    {
      id: "files",
      label: t(($) => $.detail.tabs.files, { count: totalFileCount(skill) }),
    },
  ];

  return (
    // relative: positioning anchor for the floating save pill (page-centered,
    // same rule as the skills list batch toolbar).
    <div className="relative flex flex-1 min-h-0 flex-col">
      <BreadcrumbHeader
        segments={[{ href: paths.skills(), label: t(($) => $.page.title) }]}
        leaf={
          <span className="truncate font-mono text-caption text-foreground">
            {skill.name}
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
          </>
        }
      />

      {!canEdit && (
        <div className="px-4 pt-3 sm:px-6">
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
          className="flex shrink-0 items-start gap-2 border-b bg-warning/10 px-4 py-2 text-caption text-muted-foreground sm:px-6"
        >
          <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
          <span>{t(($) => $.detail.supporting_data_warning)}</span>
        </div>
      )}

      <SkillIdentity
        skill={skill}
        origin={origin}
        originRuntime={originRuntime}
        agentCount={skillAgents.length}
        creator={creator}
      />

      <div
        className="shrink-0 overflow-x-auto border-b px-4 sm:px-6"
        role="tablist"
        aria-label={t(($) => $.detail.tabs.aria)}
      >
        <div className="mx-auto flex max-w-[1440px] items-center gap-6">
          {TABS.map((tab) => (
            <button
              key={tab.id}
              type="button"
              role="tab"
              aria-selected={activeView === tab.id}
              onClick={() => selectView(tab.id)}
              className={cn(
                "relative shrink-0 py-3 text-body font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring",
                activeView === tab.id
                  ? "text-foreground after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:bg-foreground"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {tab.label}
            </button>
          ))}
        </div>
      </div>

      {conflictPending && canEdit && (
        <div
          role="status"
          aria-live="polite"
          className="flex shrink-0 items-start gap-2 border-b border-warning/30 bg-warning/10 px-4 py-2 text-caption sm:px-6"
        >
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
      )}

      <div
        className={cn(
          "min-h-0 flex-1 overflow-y-auto",
          activeView === "files" && "md:overflow-hidden",
        )}
      >
        {activeView === "overview" ? (
          <OverviewTab
            skill={skill}
            name={name}
            description={description}
            canEdit={canEdit}
            creatorName={creator?.name ?? null}
            skillAgents={skillAgents}
            onNameChange={setName}
            onDescriptionChange={setDescription}
            onAddToAgents={() => setShowAddToAgents(true)}
          />
        ) : (
          <FilesTab
            filePaths={filePaths}
            selectedPath={selectedPath}
            selectedContent={selectedContent}
            mode={fileMode}
            canEdit={canEdit}
            addingFile={addingFile}
            onSelectPath={setSelectedPath}
            onModeChange={setFileMode}
            onStartAddFile={() => setAddingFile(true)}
            onAddFile={handleAddFile}
            onCancelAddFile={() => setAddingFile(false)}
            onDeleteFile={handleDeleteFile}
            onRenameFile={handleRenameFile}
            onContentChange={handleFileContentChange}
          />
        )}
      </div>

      {/* Page-level so it covers edits made on either tab. Dirty-only and
          floating, matching the skills list batch toolbar; anchored to the
          page root (relative), NOT the viewport. */}
      {canEdit && isDirty && (
        <div
          role="status"
          aria-live="polite"
          className="absolute bottom-6 left-1/2 z-50 flex -translate-x-1/2 animate-in items-center gap-1 rounded-lg border bg-background px-2 py-1.5 fade-in slide-in-from-bottom-2 shadow-lg"
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
    </div>
  );
}
