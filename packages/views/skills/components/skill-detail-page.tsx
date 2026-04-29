"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import {
  AlertCircle,
  AlertTriangle,
  ArrowLeft,
  ChevronRight,
  HardDrive,
  Loader2,
  Lock,
  Pencil,
  Plus,
  Save,
  Sparkles,
  Trash2,
} from "lucide-react";
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
import { timeAgo } from "@multica/core/utils";
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
import { Label } from "@multica/ui/components/ui/label";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { AppLink, useNavigation } from "../../navigation";
import { useCanEditSkill } from "../hooks/use-can-edit-skill";
import { readOrigin, totalFileCount, type OriginInfo } from "../lib/origin";
import { FileTree } from "./file-tree";
import { FileViewer } from "./file-viewer";

const SKILL_MD = "SKILL.md";

type DraftFile = { id?: string; path: string; content: string };

// ---------------------------------------------------------------------------
// File path validation + inline add
// ---------------------------------------------------------------------------

function validateNewFilePath(path: string, existing: string[]): string {
  const p = path.trim();
  if (!p) return "Path cannot be empty.";
  if (p.startsWith("/")) return "Absolute paths are not allowed.";
  if (p.split("/").includes("..")) return 'Paths cannot contain "..".';
  if (p === SKILL_MD) return "SKILL.md is reserved for the main file.";
  if (existing.includes(p)) return "A file at this path already exists.";
  return "";
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
  const [path, setPath] = useState("");
  const [error, setError] = useState("");

  const submit = () => {
    const err = validateNewFilePath(path, existingPaths);
    if (err) {
      setError(err);
      return;
    }
    onAdd(path.trim());
  };

  return (
    <div className="border-b bg-muted/30 px-2 py-2">
      <Input
        autoFocus
        value={path}
        onChange={(e) => {
          setPath(e.target.value);
          setError("");
        }}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            onCancel();
            return;
          }
          if (e.key !== "Enter" || e.nativeEvent.isComposing) return;
          submit();
        }}
        placeholder="templates/review.md"
        className="h-7 font-mono text-xs"
      />
      {error && (
        <p role="alert" className="mt-1 text-xs text-destructive">
          {error}
        </p>
      )}
      <div className="mt-1.5 flex items-center gap-1.5">
        <Button type="button" size="xs" onClick={submit}>
          Add
        </Button>
        <Button type="button" size="xs" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Sidebar sections
// ---------------------------------------------------------------------------

function UsedBySection({ agents }: { agents: Agent[] }) {
  if (agents.length === 0) {
    return (
      <div className="rounded-md border border-dashed px-3 py-4 text-center text-xs text-muted-foreground">
        Not assigned to any agent yet. Open an agent&rsquo;s Skills tab to
        assign.
      </div>
    );
  }
  return (
    <ul className="space-y-1.5">
      {agents.map((a) => (
        <li
          key={a.id}
          className="flex items-center gap-2 rounded-md border bg-card px-2.5 py-1.5"
        >
          <ActorAvatar
            name={a.name}
            initials={a.name.slice(0, 2).toUpperCase()}
            avatarUrl={a.avatar_url}
            isAgent
            size={22}
          />
          <div className="min-w-0 flex-1">
            <div className="truncate text-xs font-medium">{a.name}</div>
            {a.description && (
              <div className="truncate text-xs text-muted-foreground">
                {a.description}
              </div>
            )}
          </div>
        </li>
      ))}
    </ul>
  );
}

function OriginSidebarCard({
  origin,
  runtime,
}: {
  origin: OriginInfo;
  runtime: AgentRuntime | null;
}) {
  if (origin.type === "manual") return null;

  const isRuntime = origin.type === "runtime_local";
  const label =
    origin.type === "runtime_local"
      ? "Imported from local runtime"
      : origin.type === "clawhub"
        ? "Imported from ClawHub"
        : "Imported from Skills.sh";

  return (
    <div className="rounded-md border bg-muted/30 p-3">
      <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
        {isRuntime ? (
          <HardDrive className="h-3 w-3" />
        ) : (
          <Sparkles className="h-3 w-3" />
        )}
        {label}
      </div>
      {runtime && (
        <div className="mt-1 break-all text-xs text-foreground">
          {runtime.name}
        </div>
      )}
      {origin.source_path && (
        <div className="mt-1 break-all font-mono text-xs text-foreground">
          {origin.source_path}
        </div>
      )}
      {origin.source_url && (
        <div className="mt-1 break-all font-mono text-xs text-foreground">
          {origin.source_url}
        </div>
      )}
      {origin.provider && (
        <div className="mt-1 font-mono text-xs text-muted-foreground">
          provider · {origin.provider}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main page
// ---------------------------------------------------------------------------

export function SkillDetailPage({ skillId }: { skillId: string }) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();

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

  // Build the skillId → agents map once per agent-list identity, not on every
  // render — see selectSkillAssignments' doc comment.
  const assignments = useMemo(
    () => selectSkillAssignments(agents),
    [agents],
  );

  const canEdit = useCanEditSkill(skill, wsId);

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [content, setContent] = useState("");
  const [files, setFiles] = useState<DraftFile[]>([]);
  const [selectedPath, setSelectedPath] = useState(SKILL_MD);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [addingFile, setAddingFile] = useState(false);
  // When a WS refetch lands newer server state while we have in-progress
  // edits, we surface a banner instead of silently clobbering the draft.
  const [conflictPending, setConflictPending] = useState(false);

  // Ref to latest draft — lets the seeding effect decide whether user has
  // in-progress edits without itself depending on draft state (which would
  // fire the effect on every keystroke).
  const draftRef = useRef({ name, description, content, files });
  draftRef.current = { name, description, content, files };

  // Tracks `${wsId}:${id}@${updated_at}` we last seeded from. `wsId` guards
  // against the rare cross-workspace race where `skillId` collides across
  // workspaces (same UUID wouldn't in practice, but the scope is correct).
  const seededKeyRef = useRef<string | null>(null);

  // Seed draft from server state. Preserves in-progress edits when a WS
  // invalidation refetches the same skill; surfaces a conflict banner if
  // server state has drifted under unedited-yet-mid-edit conditions.
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

  const origin = useMemo(
    () => (skill ? readOrigin(skill) : null),
    [skill],
  );
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

  // If the selected file disappeared (user deleted it, or WS refresh removed
  // it), jump back to SKILL.md so the viewer never shows an empty body with
  // a ghost path label.
  useEffect(() => {
    if (selectedPath !== SKILL_MD && !fileMap.has(selectedPath)) {
      setSelectedPath(SKILL_MD);
    }
  }, [fileMap, selectedPath]);

  const isDirty = useMemo(() => {
    if (!skill) return false;
    const serverFiles = (skill.files ?? []).map((f: SkillFile) => ({
      path: f.path,
      content: f.content,
    }));
    const draftFiles = files.map((f) => ({ path: f.path, content: f.content }));
    return (
      name.trim() !== skill.name ||
      description.trim() !== skill.description ||
      content !== skill.content ||
      JSON.stringify(draftFiles) !== JSON.stringify(serverFiles)
    );
  }, [skill, name, description, content, files]);

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
      // Seed local state + cache with the authoritative server response so
      // draft → clean transition is immediate. Syncs ALL fields (not just
      // name/desc) — otherwise server-side normalization of content/files
      // would leave isDirty stuck at true.
      qc.setQueryData(
        skillDetailOptions(wsId, skill.id).queryKey,
        updated,
      );
      seedFromSkill(updated);
      seededKeyRef.current = `${wsId}:${updated.id}@${updated.updated_at}`;
      setConflictPending(false);
      // Invalidate list (list rows carry `updated_at`, name, description) AND
      // agents (each agent inlines its `skills` array, so a rename here must
      // sync there too). `exact: true` on skills keeps the detail cache we
      // just wrote from getting re-fetched.
      qc.invalidateQueries({
        queryKey: workspaceKeys.skills(wsId),
        exact: true,
      });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success("Skill saved");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to save skill");
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
      // Navigate first so the detail route unmounts BEFORE invalidation
      // refetches the now-404 row — otherwise users see a "Skill not found"
      // flash. Deleting also cascade-removes junction rows on the server,
      // so agents cache must refresh too.
      navigation.replace(paths.skills());
      qc.removeQueries({
        queryKey: skillDetailOptions(wsId, skill.id).queryKey,
      });
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success("Skill deleted");
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to delete skill",
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

  const handleDeleteFile = () => {
    if (selectedPath === SKILL_MD) return;
    setFiles((prev) => prev.filter((f) => f.path !== selectedPath));
    setSelectedPath(SKILL_MD);
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

  const supportingQueryDown =
    !!agentsError || !!membersError || !!runtimesError;

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
          >
            <ArrowLeft className="h-3 w-3" />
            All skills
          </Button>
        </div>
        <div className="flex flex-1 flex-col items-center justify-center gap-2 text-center">
          <AlertCircle className="h-8 w-8 text-muted-foreground/40" />
          <p className="text-sm font-medium">Skill not found</p>
          <p className="max-w-xs text-xs text-muted-foreground">
            {error instanceof Error
              ? error.message
              : "This skill may have been deleted or you lost access."}
          </p>
          <AppLink
            href={paths.skills()}
            className={`${buttonVariants({ variant: "outline", size: "xs" })} mt-2`}
          >
            Back to Skills
          </AppLink>
        </div>
      </div>
    );
  }

  // --- Sub-line metadata for the header ---
  const originLabel = (() => {
    if (!origin) return null;
    if (origin.type === "runtime_local") {
      return originRuntime
        ? `Local runtime · ${originRuntime.name}`
        : origin.provider
          ? `Local runtime · ${origin.provider}`
          : "Local runtime";
    }
    if (origin.type === "clawhub") return "Imported · ClawHub";
    if (origin.type === "skills_sh") return "Imported · Skills.sh";
    return "Workspace";
  })();

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      {/* Topbar */}
      <div className="flex h-12 shrink-0 items-center gap-2 border-b px-3">
        <Button
          variant="ghost"
          size="xs"
          render={<AppLink href={paths.skills()} />}
        >
          <ArrowLeft className="h-3 w-3" />
          All skills
        </Button>
        <ChevronRight className="h-3 w-3 text-muted-foreground" />
        <span className="truncate font-mono text-xs text-foreground">
          {skill.name}
        </span>
        <div className="ml-auto flex items-center gap-2">
          {!canEdit && (
            <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
              <Lock className="h-3 w-3" />
              Read-only
            </span>
          )}
          {canEdit && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => setConfirmDelete(true)}
                    className="text-muted-foreground hover:text-destructive"
                    aria-label="Delete skill"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                }
              />
              <TooltipContent>Delete skill</TooltipContent>
            </Tooltip>
          )}
        </div>
      </div>

      {/* Supporting query error banner (non-blocking — the page still works
          but agent attribution / runtime names / permission checks are
          partial). */}
      {supportingQueryDown && (
        <div
          role="status"
          className="flex shrink-0 items-start gap-2 border-b bg-warning/10 px-4 py-2 text-xs text-muted-foreground"
        >
          <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
          <span>
            Some workspace data failed to load. Creator attribution, runtime
            names, or edit permissions may appear incomplete until the next
            refresh.
          </span>
        </div>
      )}

      {/* Body: file tree | editor | sidebar */}
      <div className="flex flex-1 min-h-0">
        {/* File tree */}
        <aside className="flex w-56 shrink-0 flex-col border-r">
          <div className="flex h-10 shrink-0 items-center justify-between border-b px-3">
            <span className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
              Files · {totalFileCount(skill)}
            </span>
            {canEdit && (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => setAddingFile(true)}
                      className="text-muted-foreground"
                      aria-label="Add file"
                    >
                      <Plus className="h-3.5 w-3.5" />
                    </Button>
                  }
                />
                <TooltipContent>Add file</TooltipContent>
              </Tooltip>
            )}
          </div>
          {addingFile && (
            <AddFileInline
              existingPaths={filePaths}
              onAdd={handleAddFile}
              onCancel={() => setAddingFile(false)}
            />
          )}
          <div className="flex-1 overflow-y-auto">
            <FileTree
              filePaths={filePaths}
              selectedPath={selectedPath}
              onSelect={setSelectedPath}
            />
          </div>
          {selectedPath !== SKILL_MD && canEdit && (
            <div className="border-t px-3 py-2">
              <Button
                type="button"
                variant="ghost"
                size="xs"
                onClick={handleDeleteFile}
                className="text-muted-foreground hover:text-destructive"
              >
                <Trash2 className="h-3 w-3" />
                Delete file
              </Button>
            </div>
          )}
        </aside>

        {/* Editor */}
        <section className="flex min-w-0 flex-1 flex-col">
          {/* Name + description + subline */}
          <div className="space-y-2 border-b px-5 py-4">
            <Input
              value={name}
              readOnly={!canEdit}
              onChange={(e) => setName(e.target.value)}
              placeholder="skill-name"
              className="h-9 border-0 bg-transparent px-0 text-lg font-semibold shadow-none focus-visible:ring-0 read-only:cursor-default"
              aria-label="Skill name"
            />
            <div className="space-y-1">
              <Label
                htmlFor="skill-description"
                className="text-xs text-muted-foreground"
              >
                <Pencil className="h-3 w-3" />
                Description
              </Label>
              <Textarea
                id="skill-description"
                value={description}
                readOnly={!canEdit}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="One sentence describing when an agent should use this skill…"
                rows={2}
                className="resize-none text-sm read-only:cursor-default"
              />
            </div>
            {/* Subline: origin · updated · creator — each segment atomic so
                flex-wrap doesn't orphan the separators. */}
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
              {originLabel && (
                <span className="inline-flex items-center gap-1">
                  {origin?.type === "runtime_local" ? (
                    <HardDrive className="h-3 w-3" />
                  ) : (
                    <Sparkles className="h-3 w-3" />
                  )}
                  {originLabel}
                </span>
              )}
              <span className="inline-flex items-center gap-2">
                <span aria-hidden>·</span>
                <span>Updated {timeAgo(skill.updated_at)}</span>
              </span>
              {creator && (
                <span className="inline-flex items-center gap-2">
                  <span aria-hidden>·</span>
                  <span className="inline-flex items-center gap-1">
                    <ActorAvatar
                      name={creator.name}
                      initials={creator.name.slice(0, 2).toUpperCase()}
                      avatarUrl={creator.avatar_url}
                      size={14}
                    />
                    by {creator.name}
                  </span>
                </span>
              )}
            </div>
          </div>

          {/* Conflict banner — surfaces when a WS refetch arrived with newer
              server state while the user had edits in flight. */}
          {conflictPending && canEdit && (
            <div
              role="status"
              aria-live="polite"
              className="flex items-start gap-2 border-b border-warning/30 bg-warning/10 px-4 py-2 text-xs"
            >
              <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
              <div className="flex-1">
                <div className="font-medium text-foreground">
                  Someone else updated this skill
                </div>
                <div className="mt-0.5 text-muted-foreground">
                  Your edits are preserved. Discard to pull their changes, or
                  Save to overwrite.
                </div>
              </div>
            </div>
          )}

          {/* File viewer */}
          <div className="flex-1 min-h-0">
            <FileViewer
              key={selectedPath}
              path={selectedPath}
              content={selectedContent}
              onChange={handleFileContentChange}
            />
          </div>

          {/* Save bar */}
          {isDirty && canEdit && (
            <div
              role="status"
              aria-live="polite"
              className="flex items-center gap-2 border-t bg-muted/30 px-4 py-2"
            >
              <span className="h-1.5 w-1.5 rounded-full bg-brand" />
              <span className="text-xs text-muted-foreground">
                Unsaved changes — will overwrite the live skill on save
              </span>
              <div className="ml-auto flex items-center gap-1.5">
                <Button
                  type="button"
                  variant="ghost"
                  size="xs"
                  onClick={handleDiscard}
                >
                  Discard
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
                      Saving…
                    </>
                  ) : (
                    <>
                      <Save className="h-3 w-3" />
                      Save changes
                    </>
                  )}
                </Button>
              </div>
            </div>
          )}
        </section>

        {/* Sidebar */}
        <aside className="flex w-72 shrink-0 flex-col gap-4 overflow-y-auto border-l bg-muted/20 px-4 py-4">
          <div>
            <h3 className="mb-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
              Metadata
            </h3>
            <dl className="space-y-1.5 text-xs">
              <div className="flex gap-2">
                <dt className="min-w-20 text-muted-foreground">Created</dt>
                <dd className="min-w-0 flex-1">
                  {timeAgo(skill.created_at)}
                </dd>
              </div>
              <div className="flex gap-2">
                <dt className="min-w-20 text-muted-foreground">Updated</dt>
                <dd className="min-w-0 flex-1">
                  {timeAgo(skill.updated_at)}
                </dd>
              </div>
              {creator && (
                <div className="flex gap-2">
                  <dt className="min-w-20 text-muted-foreground">Created by</dt>
                  <dd className="min-w-0 flex-1">{creator.name}</dd>
                </div>
              )}
              <div className="flex gap-2">
                <dt className="min-w-20 text-muted-foreground">Files</dt>
                <dd className="min-w-0 flex-1">{totalFileCount(skill)}</dd>
              </div>
              <div
                className="flex gap-2"
                title={skill.id}
              >
                <dt className="min-w-20 text-muted-foreground">ID</dt>
                <dd className="min-w-0 flex-1 truncate font-mono text-muted-foreground">
                  {skill.id.slice(0, 8)}…
                </dd>
              </div>
            </dl>
          </div>

          {origin && origin.type !== "manual" && (
            <div>
              <h3 className="mb-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                Origin
              </h3>
              <OriginSidebarCard origin={origin} runtime={originRuntime} />
            </div>
          )}

          <div>
            <h3 className="mb-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
              Used by {skillAgents.length} agent
              {skillAgents.length === 1 ? "" : "s"}
            </h3>
            <UsedBySection agents={skillAgents} />
          </div>

          <div>
            <h3 className="mb-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
              Permissions
            </h3>
            <p className="text-xs leading-relaxed text-muted-foreground">
              {canEdit
                ? "You can edit and delete this skill. Changes take effect on the next agent run."
                : `Only the creator${creator ? ` (${creator.name})` : ""} or a workspace admin can edit this skill.`}
            </p>
          </div>
        </aside>
      </div>

      {/* Delete confirmation */}
      <Dialog
        open={confirmDelete}
        onOpenChange={(v) => {
          if (!v) setConfirmDelete(false);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Delete skill?</DialogTitle>
            <DialogDescription>
              This will permanently delete &ldquo;{skill.name}&rdquo; and remove
              it from{" "}
              {skillAgents.length > 0
                ? `${skillAgents.length} agent${skillAgents.length === 1 ? "" : "s"} currently using it.`
                : "all agents."}
            </DialogDescription>
          </DialogHeader>
          <div className="rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive">
            This action cannot be undone.
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => setConfirmDelete(false)}
              disabled={deleting}
            >
              Cancel
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
                  Deleting…
                </>
              ) : (
                <>
                  <Trash2 className="h-3 w-3" />
                  Delete permanently
                </>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
