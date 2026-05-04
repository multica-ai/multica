"use client";

import { useMemo, useState, useCallback, useRef, useEffect } from "react";
import { useDefaultLayout, usePanelRef } from "react-resizable-panels";
import { Check, ChevronRight, Copy, Link2, ListTodo, MoreHorizontal, PanelRight, Pin, PinOff, Trash2, UserMinus } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import type { Issue, IssueStatus, ProjectStatus, ProjectPriority } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { projectDetailOptions } from "@multica/core/projects/queries";
import { useUpdateProject, useDeleteProject } from "@multica/core/projects/mutations";
import { pinListOptions } from "@multica/core/pins";
import { useCreatePin, useDeletePin } from "@multica/core/pins";
import { myIssueListOptions, childIssueProgressOptions, type MyIssuesFilter } from "@multica/core/issues/queries";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { PROJECT_STATUS_ORDER, PROJECT_STATUS_CONFIG, PROJECT_PRIORITY_ORDER, PROJECT_PRIORITY_CONFIG } from "@multica/core/projects/config";
import { BOARD_STATUSES } from "@multica/core/issues/config";
import { createIssueViewStore } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider, useViewStore } from "@multica/core/issues/stores/view-store-context";
import { filterIssues } from "../../issues/utils/filter";
import { getProjectIssueMetrics } from "./project-issue-metrics";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink, useNavigation } from "../../navigation";
import { TitleEditor, ContentEditor, type ContentEditorRef } from "../../editor";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { ProjectResourcesSection } from "./project-resources-section";
import { IssuesHeader } from "../../issues/components/issues-header";
import { BoardView } from "../../issues/components/board-view";
import { ListView } from "../../issues/components/list-view";
import { BatchActionToolbar } from "../../issues/components/batch-action-toolbar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { ResizablePanelGroup, ResizablePanel, ResizableHandle } from "@multica/ui/components/ui/resizable";
import { Sheet, SheetContent } from "@multica/ui/components/ui/sheet";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { EmojiPicker } from "@multica/ui/components/common/emoji-picker";
import { PageHeader } from "../../layout/page-header";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";

function getAfterCreateHook(settings: unknown): string {
  if (!settings || typeof settings !== "object") return "";
  const hooks = (settings as { hooks?: unknown }).hooks;
  if (!hooks || typeof hooks !== "object") return "";
  const value = (hooks as { after_create?: unknown }).after_create;
  return typeof value === "string" ? value : "";
}

function settingsWithAfterCreateHook(settings: unknown, script: string) {
  const base =
    settings && typeof settings === "object" && !Array.isArray(settings)
      ? { ...(settings as Record<string, unknown>) }
      : {};
  const hooks =
    base.hooks && typeof base.hooks === "object" && !Array.isArray(base.hooks)
      ? { ...(base.hooks as Record<string, unknown>) }
      : {};

  if (script) {
    hooks.after_create = script;
    base.hooks = hooks;
    return base;
  }

  delete hooks.after_create;
  if (Object.keys(hooks).length > 0) {
    base.hooks = hooks;
  } else {
    delete base.hooks;
  }
  return base;
}

const BASH_WORDS = new Set([
  "if",
  "then",
  "else",
  "elif",
  "fi",
  "for",
  "while",
  "until",
  "do",
  "done",
  "case",
  "esac",
  "in",
  "function",
  "select",
  "set",
  "export",
  "local",
  "readonly",
  "return",
  "exit",
  "source",
  "cd",
  "echo",
  "test",
  "bash",
  "git",
  "pnpm",
  "npm",
  "node",
  "mkdir",
  "cp",
  "rm",
  "mv",
]);

function renderBashPlain(text: string, keyPrefix: string): React.ReactNode[] {
  const nodes: React.ReactNode[] = [];
  let cursor = 0;
  const wordRe = /[A-Za-z_][A-Za-z0-9_-]*/g;
  for (const match of text.matchAll(wordRe)) {
    const index = match.index ?? 0;
    const word = match[0];
    if (index > cursor) nodes.push(text.slice(cursor, index));
    if (BASH_WORDS.has(word)) {
      nodes.push(
        <span key={`${keyPrefix}-${index}`} className="text-amber-600 dark:text-amber-400">
          {word}
        </span>,
      );
    } else {
      nodes.push(word);
    }
    cursor = index + word.length;
  }
  if (cursor < text.length) nodes.push(text.slice(cursor));
  return nodes;
}

function renderBashLine(line: string, lineIndex: number): React.ReactNode[] {
  const nodes: React.ReactNode[] = [];
  let i = 0;
  let plain = "";

  const flushPlain = () => {
    if (!plain) return;
    nodes.push(...renderBashPlain(plain, `plain-${lineIndex}-${i}`));
    plain = "";
  };

  while (i < line.length) {
    const char = line[i];
    if (char === "#" && (i === 0 || /\s/.test(line[i - 1] ?? ""))) {
      flushPlain();
      nodes.push(
        <span key={`comment-${lineIndex}-${i}`} className="text-muted-foreground">
          {line.slice(i)}
        </span>,
      );
      return nodes;
    }

    if (char === "'" || char === '"') {
      flushPlain();
      const quote = char;
      let end = i + 1;
      while (end < line.length) {
        if (line[end] === "\\" && quote === '"') {
          end += 2;
          continue;
        }
        if (line[end] === quote) {
          end += 1;
          break;
        }
        end += 1;
      }
      nodes.push(
        <span key={`string-${lineIndex}-${i}`} className="text-emerald-700 dark:text-emerald-400">
          {line.slice(i, end)}
        </span>,
      );
      i = end;
      continue;
    }

    if (char === "$") {
      flushPlain();
      let end = i + 1;
      if (line[end] === "{") {
        end += 1;
        while (end < line.length && line[end] !== "}") end += 1;
        if (line[end] === "}") end += 1;
      } else if (line[end] === "(") {
        end += 1;
        while (end < line.length && line[end] !== ")") end += 1;
        if (line[end] === ")") end += 1;
      } else {
        while (end < line.length && /[A-Za-z0-9_]/.test(line[end] ?? "")) end += 1;
      }
      nodes.push(
        <span key={`var-${lineIndex}-${i}`} className="text-sky-700 dark:text-sky-400">
          {line.slice(i, end)}
        </span>,
      );
      i = end;
      continue;
    }

    plain += char;
    i += 1;
  }

  flushPlain();
  return nodes;
}

function renderBashScript(script: string): React.ReactNode[] {
  return script.split("\n").flatMap((line, index, lines) => [
    ...renderBashLine(line, index),
    ...(index < lines.length - 1 ? ["\n"] : []),
  ]);
}

function BashScriptEditor({
  value,
  onChange,
  disabled,
}: {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const highlightRef = useRef<HTMLPreElement>(null);
  const [copied, setCopied] = useState(false);

  const handleScroll = useCallback((e: React.UIEvent<HTMLTextAreaElement>) => {
    const target = e.currentTarget;
    if (!highlightRef.current) return;
    highlightRef.current.scrollTop = target.scrollTop;
    highlightRef.current.scrollLeft = target.scrollLeft;
  }, []);

  const handleCopy = useCallback(async () => {
    if (!value) return;
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1200);
    } catch {
      toast.error("Failed to copy setup script");
    }
  }, [value]);

  return (
    <div className="h-64 overflow-hidden rounded-lg border border-input bg-muted/50 transition-colors focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50">
      <div className="flex h-8 items-center justify-between border-b border-border/60 px-2.5">
        <span className="font-mono text-xs text-muted-foreground">bash</span>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          className="text-muted-foreground hover:text-foreground"
          disabled={!value}
          onClick={handleCopy}
          aria-label="Copy setup script"
        >
          {copied ? <Check className="size-3" /> : <Copy className="size-3" />}
        </Button>
      </div>
      <div className="relative h-[calc(100%-2rem)]">
        <pre
          ref={highlightRef}
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 m-0 overflow-hidden p-3 font-mono text-[13px] leading-5 text-foreground"
        >
          <code className="block min-w-max whitespace-pre">
            {value ? renderBashScript(value) : null}
          </code>
        </pre>
        <textarea
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onScroll={handleScroll}
          disabled={disabled}
          spellCheck={false}
          wrap="off"
          aria-label="Project setup bash script"
          placeholder="git clone https://github.com/org/repo.git ."
          className="absolute inset-0 h-full w-full resize-none overflow-auto border-0 bg-transparent p-3 font-mono text-[13px] leading-5 text-transparent caret-foreground outline-none placeholder:text-muted-foreground selection:bg-primary/20 disabled:cursor-not-allowed disabled:opacity-50"
        />
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Property row — sidebar property display
// ---------------------------------------------------------------------------

function PropRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex min-h-8 items-center gap-2 rounded-md px-2 -mx-2 hover:bg-accent/50 transition-colors">
      <span className="w-16 shrink-0 text-xs text-muted-foreground">{label}</span>
      <div className="flex min-w-0 flex-1 items-center gap-1.5 text-xs truncate">
        {children}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Project Issues — reuses the existing issues list/board components
// ---------------------------------------------------------------------------

const projectViewStore = createIssueViewStore("project_issues_view");

function ProjectIssuesContent({
  projectIssues,
  scope,
  filter,
}: {
  projectIssues: Issue[];
  scope: string;
  filter: MyIssuesFilter;
}) {
  const wsId = useWorkspaceId();
  const viewMode = useViewStore((s) => s.viewMode);
  const statusFilters = useViewStore((s) => s.statusFilters);
  const priorityFilters = useViewStore((s) => s.priorityFilters);
  const assigneeFilters = useViewStore((s) => s.assigneeFilters);
  const includeNoAssignee = useViewStore((s) => s.includeNoAssignee);
  const creatorFilters = useViewStore((s) => s.creatorFilters);
  const labelFilters = useViewStore((s) => s.labelFilters);

  const issues = useMemo(
    () => filterIssues(projectIssues, { statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters, projectFilters: [], includeNoProject: false, labelFilters }),
    [projectIssues, statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters, labelFilters],
  );

  const { data: childProgressMap = new Map() } = useQuery(childIssueProgressOptions(wsId));

  const visibleStatuses = useMemo(() => {
    if (statusFilters.length > 0)
      return BOARD_STATUSES.filter((s) => statusFilters.includes(s));
    return BOARD_STATUSES;
  }, [statusFilters]);

  const hiddenStatuses = useMemo(
    () => BOARD_STATUSES.filter((s) => !visibleStatuses.includes(s)),
    [visibleStatuses],
  );

  const updateIssueMutation = useUpdateIssue();
  const handleMoveIssue = useCallback(
    (issueId: string, newStatus: IssueStatus, newPosition?: number) => {
      const updates: Partial<{ status: IssueStatus; position: number }> = { status: newStatus };
      if (newPosition !== undefined) updates.position = newPosition;
      updateIssueMutation.mutate(
        { id: issueId, ...updates },
        { onError: () => toast.error("Failed to move issue") },
      );
    },
    [updateIssueMutation],
  );

  if (projectIssues.length === 0) {
    return (
      <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
        <ListTodo className="h-10 w-10 text-muted-foreground/40" />
        <p className="text-sm">No issues linked</p>
        <p className="text-xs">Assign issues to this project from the issue detail page.</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col flex-1 min-h-0">
      {viewMode === "board" ? (
        <BoardView
          issues={issues}
          visibleStatuses={visibleStatuses}
          hiddenStatuses={hiddenStatuses}
          onMoveIssue={handleMoveIssue}
          childProgressMap={childProgressMap}
          myIssuesScope={scope}
          myIssuesFilter={filter}
        />
      ) : (
        <ListView
          issues={issues}
          visibleStatuses={visibleStatuses}
          childProgressMap={childProgressMap}
          myIssuesScope={scope}
          myIssuesFilter={filter}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// ProjectDetail
// ---------------------------------------------------------------------------

export function ProjectDetail({ projectId }: { projectId: string }) {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const userId = useAuthStore((s) => s.user?.id);
  const workspace = useCurrentWorkspace();
  const workspaceName = workspace?.name;
  const { data: project, isLoading } = useQuery(projectDetailOptions(wsId, projectId));
  const projectScope = `project:${projectId}`;
  const projectFilter = useMemo<MyIssuesFilter>(
    () => ({ project_id: projectId }),
    [projectId],
  );
  const { data: projectIssues = [] } = useQuery(
    myIssueListOptions(wsId, projectScope, projectFilter),
  );
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { getActorName } = useActorName();
  const updateProject = useUpdateProject();
  const deleteProject = useDeleteProject();
  const { data: pinnedItems = [] } = useQuery({
    ...pinListOptions(wsId, userId ?? ""),
    enabled: !!userId,
  });
  const isPinned = pinnedItems.some((p) => p.item_type === "project" && p.item_id === projectId);
  const createPin = useCreatePin();
  const deletePinMut = useDeletePin();
  const descEditorRef = useRef<ContentEditorRef>(null);
  const isMobile = useIsMobile();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [iconPickerOpen, setIconPickerOpen] = useState(false);
  const [propertiesOpen, setPropertiesOpen] = useState(true);
  const [progressOpen, setProgressOpen] = useState(true);
  const [descriptionOpen, setDescriptionOpen] = useState(true);
  const [setupOpen, setSetupOpen] = useState(true);
  const [afterCreateDraft, setAfterCreateDraft] = useState("");

  // Sidebar panel
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_project_detail_layout",
  });
  const sidebarRef = usePanelRef();
  const [sidebarOpen, setSidebarOpen] = useState(true);

  useEffect(() => {
    if (isMobile) {
      setSidebarOpen(false);
      sidebarRef.current?.collapse();
    }
  }, [isMobile]);

  useEffect(() => {
    setAfterCreateDraft(getAfterCreateHook(project?.settings));
  }, [project?.id, project?.settings]);

  // Lead popover
  const [leadOpen, setLeadOpen] = useState(false);
  const [leadFilter, setLeadFilter] = useState("");
  const leadQuery = leadFilter.toLowerCase();
  const filteredMembers = members.filter((m) => m.name.toLowerCase().includes(leadQuery));
  const filteredAgents = agents.filter((a) => !a.archived_at && a.name.toLowerCase().includes(leadQuery));

  const handleUpdateField = useCallback(
    (data: Parameters<typeof updateProject.mutate>[0] extends { id: string } & infer R ? R : never) => {
      if (!project) return;
      updateProject.mutate({ id: project.id, ...data });
    },
    [project, updateProject],
  );

  const handleDelete = useCallback(() => {
    if (!project) return;
    deleteProject.mutate(project.id, {
      onSuccess: () => {
        toast.success("Project deleted");
        router.push(wsPaths.projects());
      },
    });
  }, [project, deleteProject, router, wsPaths]);

  const currentAfterCreate = getAfterCreateHook(project?.settings);
  const saveAfterCreateHook = useCallback(() => {
    if (!project) return;
    updateProject.mutate(
      {
        id: project.id,
        settings: settingsWithAfterCreateHook(
          project.settings,
          afterCreateDraft,
        ),
      },
      {
        onSuccess: () => toast.success("Project setup hook saved"),
        onError: () => toast.error("Failed to save setup hook"),
      },
    );
  }, [afterCreateDraft, project, updateProject]);
  const clearAfterCreateHook = useCallback(() => {
    if (!project) {
      setAfterCreateDraft("");
      return;
    }
    updateProject.mutate(
      {
        id: project.id,
        settings: settingsWithAfterCreateHook(project.settings, ""),
      },
      {
        onSuccess: () => {
          setAfterCreateDraft("");
          toast.success("Project setup hook cleared");
        },
        onError: () => toast.error("Failed to clear setup hook"),
      },
    );
  }, [project, updateProject]);

  if (isLoading) {
    return (
      <div className="mx-auto w-full max-w-4xl px-8 py-10 space-y-4">
        <Skeleton className="h-5 w-32" />
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-4 w-96" />
        <Skeleton className="h-40 w-full mt-8" />
      </div>
    );
  }

  if (!project) {
    return <div className="flex items-center justify-center h-full text-muted-foreground">Project not found</div>;
  }

  const issueMetrics = getProjectIssueMetrics(project);
  const statusCfg = PROJECT_STATUS_CONFIG[project.status];
  const priorityCfg = PROJECT_PRIORITY_CONFIG[project.priority];

  const sidebarContent = (
    <div className="space-y-5">
      {/* Icon + Title */}
      <div>
        <Popover open={iconPickerOpen} onOpenChange={setIconPickerOpen}>
          <PopoverTrigger
            render={
              <button
                type="button"
                className="text-2xl cursor-pointer rounded-lg p-1 -ml-1 hover:bg-accent/60 transition-colors"
                title="Change icon"
              >
                {project.icon || "📁"}
              </button>
            }
          />
          <PopoverContent align="start" className="w-auto p-0">
            <EmojiPicker
              onSelect={(emoji) => {
                handleUpdateField({ icon: emoji });
                setIconPickerOpen(false);
              }}
            />
          </PopoverContent>
        </Popover>
        <TitleEditor
          key={`title-${projectId}`}
          defaultValue={project.title}
          placeholder="Project title"
          className="mt-2 w-full text-base font-semibold leading-snug tracking-tight"
          onBlur={(value) => {
            const trimmed = value.trim();
            if (trimmed && trimmed !== project.title) handleUpdateField({ title: trimmed });
          }}
        />
      </div>

      {/* Properties */}
      <div>
        <button
          className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${propertiesOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
          onClick={() => setPropertiesOpen(!propertiesOpen)}
        >
          Properties
          <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${propertiesOpen ? "rotate-90" : ""}`} />
        </button>
        {propertiesOpen && <div className="space-y-0.5 pl-2">
          <PropRow label="Status">
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <button type="button" className="inline-flex items-center gap-1.5 text-xs hover:text-foreground transition-colors">
                    <span className={cn("size-2 rounded-full", statusCfg.dotColor)} />
                    <span>{statusCfg.label}</span>
                  </button>
                }
              />
              <DropdownMenuContent align="start" className="w-44">
                {PROJECT_STATUS_ORDER.map((s) => (
                  <DropdownMenuItem key={s} onClick={() => handleUpdateField({ status: s as ProjectStatus })}>
                    <span className={cn("size-2 rounded-full", PROJECT_STATUS_CONFIG[s].dotColor)} />
                    <span>{PROJECT_STATUS_CONFIG[s].label}</span>
                    {s === project.status && <Check className="ml-auto h-3.5 w-3.5" />}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </PropRow>
          <PropRow label="Priority">
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <button type="button" className="inline-flex items-center gap-1.5 text-xs hover:text-foreground transition-colors">
                    <PriorityIcon priority={project.priority} />
                    <span>{priorityCfg.label}</span>
                  </button>
                }
              />
              <DropdownMenuContent align="start" className="w-44">
                {PROJECT_PRIORITY_ORDER.map((p) => (
                  <DropdownMenuItem key={p} onClick={() => handleUpdateField({ priority: p as ProjectPriority })}>
                    <PriorityIcon priority={p} />
                    <span>{PROJECT_PRIORITY_CONFIG[p].label}</span>
                    {p === project.priority && <Check className="ml-auto h-3.5 w-3.5" />}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </PropRow>
          <PropRow label="Lead">
            <Popover open={leadOpen} onOpenChange={(v) => { setLeadOpen(v); if (!v) setLeadFilter(""); }}>
              <PopoverTrigger
                render={
                  <button type="button" className="inline-flex items-center gap-1.5 text-xs hover:text-foreground transition-colors">
                    {project.lead_type && project.lead_id ? (
                      <>
                        <ActorAvatar actorType={project.lead_type} actorId={project.lead_id} size={16} enableHoverCard showStatusDot />
                        <span className="cursor-pointer">{getActorName(project.lead_type, project.lead_id)}</span>
                      </>
                    ) : (
                      <span className="text-muted-foreground">No lead</span>
                    )}
                  </button>
                }
              />
              <PopoverContent align="start" className="w-52 p-0">
                <div className="px-2 py-1.5 border-b">
                  <input
                    type="text"
                    value={leadFilter}
                    onChange={(e) => setLeadFilter(e.target.value)}
                    placeholder="Assign lead..."
                    className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
                  />
                </div>
                <div className="p-1 max-h-60 overflow-y-auto">
                  <button
                    type="button"
                    onClick={() => { handleUpdateField({ lead_type: null, lead_id: null }); setLeadOpen(false); }}
                    className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                  >
                    <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
                    <span className="text-muted-foreground">No lead</span>
                  </button>
                  {filteredMembers.length > 0 && (
                    <>
                      <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">Members</div>
                      {filteredMembers.map((m) => (
                        <button
                          type="button"
                          key={m.user_id}
                          onClick={() => { handleUpdateField({ lead_type: "member", lead_id: m.user_id }); setLeadOpen(false); }}
                          className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                        >
                          <ActorAvatar actorType="member" actorId={m.user_id} size={16} />
                          <span>{m.name}</span>
                        </button>
                      ))}
                    </>
                  )}
                  {filteredAgents.length > 0 && (
                    <>
                      <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">Agents</div>
                      {filteredAgents.map((a) => (
                        <button
                          type="button"
                          key={a.id}
                          onClick={() => { handleUpdateField({ lead_type: "agent", lead_id: a.id }); setLeadOpen(false); }}
                          className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                        >
                          <ActorAvatar actorType="agent" actorId={a.id} size={16} showStatusDot />
                          <span>{a.name}</span>
                        </button>
                      ))}
                    </>
                  )}
                  {filteredMembers.length === 0 && filteredAgents.length === 0 && leadFilter && (
                    <div className="px-2 py-3 text-center text-sm text-muted-foreground">No results</div>
                  )}
                </div>
              </PopoverContent>
            </Popover>
          </PropRow>
        </div>}
      </div>

      {/* Progress */}
      {issueMetrics.totalCount > 0 && (() => {
        const pct = Math.round((issueMetrics.completedCount / issueMetrics.totalCount) * 100);
        return (
          <div>
            <button
              className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${progressOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
              onClick={() => setProgressOpen(!progressOpen)}
            >
              Progress
              <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${progressOpen ? "rotate-90" : ""}`} />
            </button>
            {progressOpen && <div className="pl-2 flex items-center gap-3">
              <div className="relative h-2 flex-1 rounded-full bg-muted overflow-hidden">
                <div
                  className="absolute inset-y-0 left-0 rounded-full bg-emerald-500 transition-all"
                  style={{ width: `${pct}%` }}
                />
              </div>
              <span className="text-xs text-muted-foreground tabular-nums shrink-0">
                {issueMetrics.completedCount}/{issueMetrics.totalCount}
              </span>
            </div>}
          </div>
        );
      })()}

      {/* Description */}
      <div>
        <button
          className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${descriptionOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
          onClick={() => setDescriptionOpen(!descriptionOpen)}
        >
          Description
          <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${descriptionOpen ? "rotate-90" : ""}`} />
        </button>
        {descriptionOpen && <div className="pl-2">
          <ContentEditor
            ref={descEditorRef}
            key={projectId}
            defaultValue={project.description || ""}
            placeholder="Add description..."
            onUpdate={(md) => handleUpdateField({ description: md || null })}
            debounceMs={1500}
          />
        </div>}
      </div>

      {/* Resources */}
      <div>
        <button
          className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${setupOpen ? "" : "text-muted-foreground hover:text-foreground"}`}
          onClick={() => setSetupOpen(!setupOpen)}
        >
          Setup
          <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${setupOpen ? "rotate-90" : ""}`} />
        </button>
        {setupOpen && (
          <div className="space-y-2 pl-2">
            <BashScriptEditor
              value={afterCreateDraft}
              onChange={setAfterCreateDraft}
              disabled={updateProject.isPending}
            />
            <div className="flex items-center justify-end gap-2">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={(!afterCreateDraft && !currentAfterCreate) || updateProject.isPending}
                onClick={clearAfterCreateHook}
              >
                Clear
              </Button>
              <Button
                type="button"
                size="sm"
                disabled={afterCreateDraft === currentAfterCreate || updateProject.isPending}
                onClick={saveAfterCreateHook}
              >
                Save
              </Button>
            </div>
          </div>
        )}
      </div>

      <ProjectResourcesSection projectId={projectId} />
    </div>
  );

  return (
    <>
    <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0" defaultLayout={defaultLayout} onLayoutChanged={onLayoutChanged}>
      <ResizablePanel id="content" minSize="50%">
        <div className="flex h-full flex-col">
          <PageHeader className="gap-2 bg-background text-sm">
            <div className="flex flex-1 items-center gap-1.5 min-w-0">
              <AppLink href={wsPaths.projects()} className="text-muted-foreground hover:text-foreground transition-colors shrink-0">
                {workspaceName ?? "Projects"}
              </AppLink>
              <ChevronRight className="h-3 w-3 text-muted-foreground/50 shrink-0" />
              <span className="truncate">{project.title}</span>
            </div>
            <div className="flex items-center gap-1 shrink-0">
              <Button
                variant="ghost"
                size="icon-sm"
                className={cn("text-muted-foreground", isPinned && "text-foreground")}
                title={isPinned ? "Unpin from sidebar" : "Pin to sidebar"}
                onClick={() => {
                  if (isPinned) {
                    deletePinMut.mutate({ itemType: "project", itemId: projectId });
                  } else {
                    createPin.mutate({ item_type: "project", item_id: projectId });
                  }
                }}
              >
                {isPinned ? <PinOff /> : <Pin />}
              </Button>
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button variant="ghost" size="icon-sm" className="text-muted-foreground">
                      <MoreHorizontal />
                    </Button>
                  }
                />
                <DropdownMenuContent align="end" className="w-auto">
                  <DropdownMenuItem onClick={() => {
                    navigator.clipboard.writeText(window.location.href);
                    toast.success("Link copied");
                  }}>
                    <Link2 className="h-3.5 w-3.5" />
                    Copy link
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    variant="destructive"
                    onClick={() => setDeleteDialogOpen(true)}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                    Delete project
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant={sidebarOpen ? "secondary" : "ghost"}
                      size="icon-sm"
                      className={sidebarOpen ? "" : "text-muted-foreground"}
                      onClick={() => {
                        if (isMobile) {
                          setSidebarOpen(!sidebarOpen);
                        } else {
                          const panel = sidebarRef.current;
                          if (!panel) return;
                          if (panel.isCollapsed()) panel.expand();
                          else panel.collapse();
                        }
                      }}
                    >
                      <PanelRight />
                    </Button>
                  }
                />
                <TooltipContent side="bottom">Toggle sidebar</TooltipContent>
              </Tooltip>
            </div>
          </PageHeader>

          <ViewStoreProvider store={projectViewStore}>
              <IssuesHeader scopedIssues={projectIssues} />
              <ProjectIssuesContent
                projectIssues={projectIssues}
                scope={projectScope}
                filter={projectFilter}
              />
              <BatchActionToolbar />
            </ViewStoreProvider>
          </div>
        </ResizablePanel>
        {!isMobile && <ResizableHandle />}
        {!isMobile && (
        <ResizablePanel
          id="sidebar"
          defaultSize={sidebarOpen ? 320 : 0}
          minSize={260}
          maxSize={420}
          collapsible
          groupResizeBehavior="preserve-pixel-size"
          panelRef={sidebarRef}
          onResize={(size) => setSidebarOpen(size.inPixels > 0)}
        >
          <div className="overflow-y-auto border-l h-full">
            <div className="p-4">
              {sidebarContent}
            </div>
          </div>
        </ResizablePanel>
        )}
        {isMobile && (
          <Sheet open={sidebarOpen} onOpenChange={setSidebarOpen}>
            <SheetContent side="right" showCloseButton={false} className="w-[320px] overflow-y-auto p-4">
              {sidebarContent}
            </SheetContent>
          </Sheet>
        )}
      </ResizablePanelGroup>

      {/* Delete confirmation */}
      <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete project</AlertDialogTitle>
            <AlertDialogDescription>
              This will delete the project. Issues will not be deleted but will be unlinked.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} className="bg-destructive text-white hover:bg-destructive/90">
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
