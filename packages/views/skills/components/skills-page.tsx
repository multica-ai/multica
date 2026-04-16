"use client";

import { useState, useEffect, useMemo } from "react";
import { useDefaultLayout } from "react-resizable-panels";
import {
  Sparkles,
  Plus,
  Trash2,
  Save,
  AlertCircle,
  Download,
} from "lucide-react";
import type { Skill, CreateSkillRequest, UpdateSkillRequest } from "@multica/core/types";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@multica/ui/components/ui/resizable";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { toast } from "sonner";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { api } from "@multica/core/api";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { skillListOptions, workspaceKeys } from "@multica/core/workspace/queries";

import { PageHeader } from "../../layout/page-header";
import { FileTree } from "./file-tree";
import { FileViewer } from "./file-viewer";

// ---------------------------------------------------------------------------
// Create Skill Dialog
// ---------------------------------------------------------------------------

function CreateSkillDialog({
  onClose,
  onCreate,
  onImport,
}: {
  onClose: () => void;
  onCreate: (data: CreateSkillRequest) => Promise<void>;
  onImport: (url: string) => Promise<void>;
}) {
  const [tab, setTab] = useState<"create" | "import">("create");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [importUrl, setImportUrl] = useState("");
  const [loading, setLoading] = useState(false);
  const [importError, setImportError] = useState("");

  const detectedSource = (() => {
    const url = importUrl.trim().toLowerCase();
    if (url.includes("clawhub.ai")) return "clawhub" as const;
    if (url.includes("skills.sh")) return "skills.sh" as const;
    return null;
  })();

  const handleCreate = async () => {
    if (!name.trim()) return;
    setLoading(true);
    try {
      await onCreate({ name: name.trim(), description: description.trim() });
      onClose();
    } catch {
      setLoading(false);
    }
  };

  const handleImport = async () => {
    if (!importUrl.trim()) return;
    setLoading(true);
    setImportError("");
    try {
      await onImport(importUrl.trim());
      onClose();
    } catch (err) {
      setImportError(err instanceof Error ? err.message : "Import failed");
      setLoading(false);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Add Workspace Skill</DialogTitle>
          <DialogDescription>
            Create a new skill or import from ClawHub / Skills.sh. Workspace skills are shared with your team and automatically injected into agent runs.
          </DialogDescription>
        </DialogHeader>

        <Tabs value={tab} onValueChange={(v) => setTab(v as "create" | "import")}>
          <TabsList className="w-full">
            <TabsTrigger value="create" className="flex-1">
              <Plus className="mr-1.5 h-3 w-3" />
              Create
            </TabsTrigger>
            <TabsTrigger value="import" className="flex-1">
              <Download className="mr-1.5 h-3 w-3" />
              Import
            </TabsTrigger>
          </TabsList>

          <TabsContent value="create" className="space-y-4 mt-4 min-h-[180px]">
            <div>
              <Label className="text-xs text-muted-foreground">Name</Label>
              <Input
                autoFocus
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. Code Review, Bug Triage"
                className="mt-1"
                onKeyDown={(e) => e.key === "Enter" && handleCreate()}
              />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Description</Label>
              <Input
                type="text"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Brief description of what this skill does"
                className="mt-1"
              />
            </div>
          </TabsContent>

          <TabsContent value="import" className="space-y-4 mt-4 min-h-[180px]">
            <div>
              <Label className="text-xs text-muted-foreground">Skill URL</Label>
              <Input
                autoFocus
                type="text"
                value={importUrl}
                onChange={(e) => { setImportUrl(e.target.value); setImportError(""); }}
                placeholder="Paste a skill URL..."
                className="mt-1"
                onKeyDown={(e) => e.key === "Enter" && handleImport()}
              />
            </div>

            {/* Supported sources — highlight on detection */}
            <div>
              <p className="text-xs text-muted-foreground mb-2">Supported sources</p>
              <div className="grid grid-cols-2 gap-2">
                <div className={`rounded-lg border px-3 py-2.5 transition-colors ${
                  detectedSource === "clawhub"
                    ? "border-primary bg-primary/5"
                    : ""
                }`}>
                  <div className="text-xs font-medium">ClawHub</div>
                  <div className="mt-0.5 truncate text-[11px] text-muted-foreground font-mono">
                    clawhub.ai/owner/skill
                  </div>
                </div>
                <div className={`rounded-lg border px-3 py-2.5 transition-colors ${
                  detectedSource === "skills.sh"
                    ? "border-primary bg-primary/5"
                    : ""
                }`}>
                  <div className="text-xs font-medium">Skills.sh</div>
                  <div className="mt-0.5 truncate text-[11px] text-muted-foreground font-mono">
                    skills.sh/owner/repo/skill
                  </div>
                </div>
              </div>
            </div>

            {importError && (
              <div className="flex items-center gap-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive">
                <AlertCircle className="h-3.5 w-3.5 shrink-0" />
                {importError}
              </div>
            )}
          </TabsContent>
        </Tabs>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          {tab === "create" ? (
            <Button onClick={handleCreate} disabled={loading || !name.trim()}>
              {loading ? "Creating..." : "Create"}
            </Button>
          ) : (
            <Button onClick={handleImport} disabled={loading || !importUrl.trim()}>
              {loading ? (
                detectedSource === "clawhub"
                  ? "Importing from ClawHub..."
                  : detectedSource === "skills.sh"
                    ? "Importing from Skills.sh..."
                    : "Importing..."
              ) : (
                <>
                  <Download className="mr-1.5 h-3 w-3" />
                  Import
                </>
              )}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Skill List Item
// ---------------------------------------------------------------------------

function SkillListItem({
  skill,
  isSelected,
  onClick,
}: {
  skill: Skill;
  isSelected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className={`flex w-full items-center gap-3 px-4 py-3 text-left transition-colors ${
        isSelected ? "bg-accent" : "hover:bg-accent/50"
      }`}
    >
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-muted">
        <Sparkles className="h-4 w-4 text-muted-foreground" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{skill.name}</div>
        {skill.description && (
          <div className="mt-0.5 truncate text-xs text-muted-foreground">
            {skill.description}
          </div>
        )}
      </div>
      {(skill.files?.length ?? 0) > 0 && (
        <Badge variant="secondary">
          {skill.files.length} file{skill.files.length !== 1 ? "s" : ""}
        </Badge>
      )}
    </button>
  );
}

// ---------------------------------------------------------------------------
// Helpers: virtual file list for the tree
// ---------------------------------------------------------------------------

const SKILL_MD = "SKILL.md";

/** Merge skill.content (as SKILL.md) + skill.files into a single map */
function buildFileMap(
  content: string,
  files: { path: string; content: string }[],
): Map<string, string> {
  const map = new Map<string, string>();
  map.set(SKILL_MD, content);
  for (const f of files) {
    if (f.path.trim()) map.set(f.path, f.content);
  }
  return map;
}

// ---------------------------------------------------------------------------
// Add File Dialog
// ---------------------------------------------------------------------------

function AddFileDialog({
  existingPaths,
  onClose,
  onAdd,
}: {
  existingPaths: string[];
  onClose: () => void;
  onAdd: (path: string) => void;
}) {
  const [path, setPath] = useState("");
  const duplicate = existingPaths.includes(path.trim());

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="max-w-sm" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle className="text-sm font-semibold">Add File</DialogTitle>
          <DialogDescription className="text-xs">
            Add a supporting file to this skill.
          </DialogDescription>
        </DialogHeader>
        <div>
          <Label className="text-xs text-muted-foreground">File Path</Label>
          <Input
            autoFocus
            type="text"
            value={path}
            onChange={(e) => setPath(e.target.value)}
            placeholder="e.g. templates/review.md"
            className="mt-1 font-mono text-sm"
            onKeyDown={(e) => {
              if (e.key === "Enter" && path.trim() && !duplicate) {
                onAdd(path.trim());
                onClose();
              }
            }}
          />
          {duplicate && (
            <p className="mt-1 text-xs text-destructive">File already exists</p>
          )}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button
            disabled={!path.trim() || duplicate}
            onClick={() => { onAdd(path.trim()); onClose(); }}
          >
            Add
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Skill Detail — file-browser layout
// ---------------------------------------------------------------------------

function SkillDetail({
  skill,
  onUpdate,
  onDelete,
}: {
  skill: Skill;
  onUpdate: (id: string, data: UpdateSkillRequest) => Promise<void>;
  onDelete: (id: string) => Promise<void>;
}) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const [name, setName] = useState(skill.name);
  const [description, setDescription] = useState(skill.description);
  const [content, setContent] = useState(skill.content);
  const [files, setFiles] = useState<{ path: string; content: string }[]>(
    (skill.files ?? []).map((f) => ({ path: f.path, content: f.content })),
  );
  const [selectedPath, setSelectedPath] = useState(SKILL_MD);
  const [saving, setSaving] = useState(false);
  const [loadingFiles, setLoadingFiles] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [showAddFile, setShowAddFile] = useState(false);

  // Sync basic fields from store updates
  useEffect(() => {
    setName(skill.name);
    setDescription(skill.description);
    setContent(skill.content);
  }, [skill.id, skill.name, skill.description, skill.content]);

  // Fetch full skill (with files) on selection change
  useEffect(() => {
    setSelectedPath(SKILL_MD);
    setLoadingFiles(true);
    api.getSkill(skill.id).then((full) => {
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
      setFiles((full.files ?? []).map((f) => ({ path: f.path, content: f.content })));
    }).catch((e) => {
      toast.error(e instanceof Error ? e.message : "Failed to load skill files");
    }).finally(() => setLoadingFiles(false));
  }, [skill.id, qc, wsId]);

  // Build the virtual file map
  const fileMap = useMemo(() => buildFileMap(content, files), [content, files]);
  const filePaths = useMemo(() => Array.from(fileMap.keys()), [fileMap]);
  const selectedContent = fileMap.get(selectedPath) ?? "";

  const isDirty =
    name !== skill.name ||
    description !== skill.description ||
    content !== skill.content ||
    JSON.stringify(files) !==
      JSON.stringify((skill.files ?? []).map((f) => ({ path: f.path, content: f.content })));

  const handleSave = async () => {
    setSaving(true);
    try {
      await onUpdate(skill.id, {
        name: name.trim(),
        description: description.trim(),
        content,
        files: files.filter((f) => f.path.trim()),
      });
    } catch {
      // toast handled by parent
    } finally {
      setSaving(false);
    }
  };

  const handleFileContentChange = (newContent: string) => {
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

  const handleAddFile = (path: string) => {
    setFiles((prev) => [...prev, { path, content: "" }]);
    setSelectedPath(path);
  };

  const handleDeleteFile = () => {
    if (selectedPath === SKILL_MD) return;
    setFiles((prev) => prev.filter((f) => f.path !== selectedPath));
    setSelectedPath(SKILL_MD);
  };

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* Header */}
      <div className="flex items-center justify-between border-b px-4 py-3">
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-muted">
            <Sparkles className="h-4 w-4 text-muted-foreground" />
          </div>
          <div className="grid grid-cols-2 gap-3 flex-1 min-w-0">
            <Input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="h-8 text-sm font-medium"
              placeholder="Skill name"
            />
            <Input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              className="h-8 text-sm"
              placeholder="Description"
            />
          </div>
        </div>
        <div className="flex items-center gap-2 ml-3">
          {isDirty && (
            <Button onClick={handleSave} disabled={saving || !name.trim()} size="xs">
              <Save className="h-3 w-3" />
              {saving ? "Saving..." : "Save"}
            </Button>
          )}
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="xs"
                  onClick={() => setConfirmDelete(true)}
                  className="text-muted-foreground hover:text-destructive"
                >
                  <Trash2 className="h-3 w-3" />
                </Button>
              }
            />
            <TooltipContent>Delete skill</TooltipContent>
          </Tooltip>
        </div>
      </div>

      {/* File browser: tree + viewer */}
      <div className="flex flex-1 min-h-0">
        {/* File tree */}
        <div className="w-52 shrink-0 border-r flex flex-col">
          <div className="flex h-10 items-center justify-between border-b px-3">
            <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
              Files
            </span>
            <div className="flex items-center gap-1">
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => setShowAddFile(true)}
                      className="text-muted-foreground"
                    >
                      <Plus className="h-3.5 w-3.5" />
                    </Button>
                  }
                />
                <TooltipContent>Add file</TooltipContent>
              </Tooltip>
              {selectedPath !== SKILL_MD && (
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        onClick={handleDeleteFile}
                        className="text-muted-foreground hover:text-destructive"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    }
                  />
                  <TooltipContent>Delete file</TooltipContent>
                </Tooltip>
              )}
            </div>
          </div>
          <div className="flex-1 overflow-y-auto">
            {loadingFiles ? (
              <div className="p-3 space-y-2">
                <Skeleton className="h-4 w-full" />
                <Skeleton className="h-4 w-3/4" />
                <Skeleton className="h-4 w-1/2" />
              </div>
            ) : (
              <FileTree
                filePaths={filePaths}
                selectedPath={selectedPath}
                onSelect={setSelectedPath}
              />
            )}
          </div>
        </div>

        {/* File viewer */}
        <div className="flex-1 min-w-0">
          {loadingFiles ? (
            <div className="p-4 space-y-3">
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-5/6" />
              <Skeleton className="h-4 w-4/6" />
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-3/4" />
            </div>
          ) : (
          <FileViewer
            key={selectedPath}
            path={selectedPath}
            content={selectedContent}
            onChange={handleFileContentChange}
          />
          )}
        </div>
      </div>

      {/* Add file dialog */}
      {showAddFile && (
        <AddFileDialog
          existingPaths={filePaths}
          onClose={() => setShowAddFile(false)}
          onAdd={handleAddFile}
        />
      )}

      {/* Delete Confirmation */}
      {confirmDelete && (
        <Dialog open onOpenChange={(v) => { if (!v) setConfirmDelete(false); }}>
          <DialogContent className="max-w-sm" showCloseButton={false}>
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-destructive/10">
                <AlertCircle className="h-5 w-5 text-destructive" />
              </div>
              <DialogHeader className="flex-1 gap-1">
                <DialogTitle className="text-sm font-semibold">Delete skill?</DialogTitle>
                <DialogDescription className="text-xs">
                  This will permanently delete &quot;{skill.name}&quot; and remove it from all agents.
                </DialogDescription>
              </DialogHeader>
            </div>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setConfirmDelete(false)}>
                Cancel
              </Button>
              <Button
                variant="destructive"
                onClick={() => {
                  setConfirmDelete(false);
                  onDelete(skill.id);
                }}
              >
                Delete
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export default function SkillsPage() {
  const isLoading = useAuthStore((s) => s.isLoading);
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const { data: skills = [] } = useQuery(skillListOptions(wsId));
  const [selectedId, setSelectedId] = useState<string>("");
  const [showCreate, setShowCreate] = useState(false);
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_skills_layout",
  });

  useEffect(() => {
    if (skills.length > 0 && !selectedId) {
      setSelectedId(skills[0]!.id);
    }
  }, [skills, selectedId]);

  const handleCreate = async (data: CreateSkillRequest) => {
    const skill = await api.createSkill(data);
    qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
    setSelectedId(skill.id);
    toast.success("Skill created");
  };

  const handleImport = async (url: string) => {
    const skill = await api.importSkill({ url });
    qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
    setSelectedId(skill.id);
    toast.success("Skill imported");
  };

  const handleUpdate = async (id: string, data: UpdateSkillRequest) => {
    try {
      await api.updateSkill(id, data);
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
      toast.success("Skill saved");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to save skill");
      throw e;
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await api.deleteSkill(id);
      if (selectedId === id) {
        const remaining = skills.filter((s) => s.id !== id);
        setSelectedId(remaining[0]?.id ?? "");
      }
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
      toast.success("Skill deleted");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to delete skill");
    }
  };

  const selected = skills.find((s) => s.id === selectedId) ?? null;

  if (isLoading) {
    return (
      <div className="flex flex-1 min-h-0">
        {/* List skeleton */}
        <div className="w-72 border-r">
          <div className="flex h-12 items-center justify-between border-b px-4">
            <Skeleton className="h-4 w-16" />
            <Skeleton className="h-6 w-6 rounded" />
          </div>
          <div className="divide-y">
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 px-4 py-3">
                <Skeleton className="h-8 w-8 rounded-lg" />
                <div className="flex-1 space-y-1.5">
                  <Skeleton className="h-4 w-28" />
                  <Skeleton className="h-3 w-40" />
                </div>
              </div>
            ))}
          </div>
        </div>
        {/* Detail skeleton */}
        <div className="flex-1 flex flex-col">
          <div className="flex items-center gap-3 border-b px-4 py-3">
            <Skeleton className="h-8 w-8 rounded-lg" />
            <Skeleton className="h-8 w-40" />
            <Skeleton className="h-8 w-56" />
          </div>
          <div className="flex flex-1 min-h-0">
            <div className="w-48 border-r p-3 space-y-2">
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-3/4" />
            </div>
            <div className="flex-1 p-4 space-y-2">
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-5/6" />
              <Skeleton className="h-4 w-2/3" />
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <ResizablePanelGroup
      orientation="horizontal"
      className="flex-1 min-h-0"
      defaultLayout={defaultLayout}
      onLayoutChanged={onLayoutChanged}
    >
      <ResizablePanel id="list" defaultSize={280} minSize={240} maxSize={400} groupResizeBehavior="preserve-pixel-size">
        {/* Left column — skill list */}
        <div className="overflow-y-auto h-full border-r">
          <PageHeader className="justify-between">
            <h1 className="text-sm font-semibold">Skills</h1>
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => setShowCreate(true)}
                  >
                    <Plus className="h-4 w-4 text-muted-foreground" />
                  </Button>
                }
              />
              <TooltipContent side="bottom">Create skill</TooltipContent>
            </Tooltip>
          </PageHeader>
          {skills.length === 0 ? (
            <div className="flex flex-col items-center justify-center px-4 py-12">
              <Sparkles className="h-8 w-8 text-muted-foreground/40" />
              <p className="mt-3 text-sm text-muted-foreground">No workspace skills yet</p>
              <p className="mt-1 text-xs text-muted-foreground text-center max-w-[280px]">
                Workspace skills are shared across your team and injected into agent runs. Skills already installed in your local runtime are used automatically.
              </p>
              <Button
                onClick={() => setShowCreate(true)}
                size="xs"
                className="mt-3"
              >
                <Plus className="h-3 w-3" />
                Create Skill
              </Button>
            </div>
          ) : (
            <div className="divide-y">
              {skills.map((skill) => (
                <SkillListItem
                  key={skill.id}
                  skill={skill}
                  isSelected={skill.id === selectedId}
                  onClick={() => setSelectedId(skill.id)}
                />
              ))}
            </div>
          )}
        </div>
      </ResizablePanel>

      <ResizableHandle />

      <ResizablePanel id="detail" minSize="50%">
        {/* Right column — skill detail */}
        <div className="flex-1 overflow-hidden h-full">
          {selected ? (
            <SkillDetail
              key={selected.id}
              skill={selected}
              onUpdate={handleUpdate}
              onDelete={handleDelete}
            />
          ) : (
            <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
              <Sparkles className="h-10 w-10 text-muted-foreground/30" />
              <p className="mt-3 text-sm">Select a skill to view details</p>
              <p className="mt-1 text-xs text-center max-w-[260px]">
                Workspace skills supplement your local skills and are shared across the team.
              </p>
              <Button
                onClick={() => setShowCreate(true)}
                size="xs"
                className="mt-3"
              >
                <Plus className="h-3 w-3" />
                Create Skill
              </Button>
            </div>
          )}
        </div>
      </ResizablePanel>

      {showCreate && (
        <CreateSkillDialog
          onClose={() => setShowCreate(false)}
          onCreate={handleCreate}
          onImport={handleImport}
        />
      )}
    </ResizablePanelGroup>
  );
}
