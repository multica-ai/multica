"use client";

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { FolderGit2, X, Bot, GitCompare } from "lucide-react";
import { useDefaultLayout } from "react-resizable-panels";
import { useWSEvent } from "@multica/core/realtime";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@multica/ui/components/ui/resizable";
import { useTaskFileTree } from "../hooks/use-task-file-tree";
import { WorkspaceFileTree } from "./workspace-file-tree";
import { WorkspaceFilePreview } from "./workspace-file-preview";
import { api } from "@multica/core/api";
import { useWorktreeViewStore } from "@multica/core/issues/stores";
import type { AgentTask, TaskAgentInfo } from "@multica/core/types";

interface WorkspaceBrowserProps {
  issueId: string;
  onClose: () => void;
}

interface AgentWorktree {
  agent: TaskAgentInfo;
  /** Most relevant task: running/dispatched first, then most recent with work_dir. */
  taskId: string;
  isActive: boolean;
}

/** Group tasks by agent, picking the best task per agent for the workspace view. */
function buildAgentWorktrees(tasks: AgentTask[]): AgentWorktree[] {
  const byAgent = new Map<string, AgentWorktree>();

  // Tasks arrive newest-first; iterate in order so first-seen = most recent.
  for (const t of tasks) {
    if (!t.agent) continue;
    const agentId = t.agent_id;

    const isActive = t.status === "running" || t.status === "dispatched";
    const hasWorktree = isActive || !!t.work_dir;
    if (!hasWorktree) continue;

    const existing = byAgent.get(agentId);
    if (!existing) {
      byAgent.set(agentId, { agent: t.agent, taskId: t.id, isActive });
    } else if (isActive && !existing.isActive) {
      // Upgrade to the active task if one exists.
      byAgent.set(agentId, { agent: t.agent, taskId: t.id, isActive });
    }
  }

  return Array.from(byAgent.values());
}

/**
 * Workspace browser: file tree (left) + file preview (right).
 * Agent tabs at the top of the tree panel let users switch between
 * the worktrees of different agents who have worked on this issue.
 */
export function WorkspaceBrowser({ issueId, onClose }: WorkspaceBrowserProps) {
  const { data: taskRuns, refetch } = useQuery({
    queryKey: ["issues", "taskRuns", issueId],
    queryFn: () => api.listTasksByIssue(issueId),
  });

  // Derive one worktree entry per agent.
  const agentWorktrees = useMemo(
    () => buildAgentWorktrees(taskRuns ?? []),
    [taskRuns],
  );

  // Selected agent tab — default to the first (most recently active).
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);

  // Delta filter: show only files with git changes.
  const [deltaOnly, setDeltaOnly] = useState(false);

  // Persist the tree ↔ preview split so a user's resize survives toggling
  // the worktree panel closed and reopening it.
  const { defaultLayout: splitLayout, onLayoutChanged: onSplitLayoutChanged } =
    useDefaultLayout({ id: "agent-worktree-split" });

  const activeWorktree = useMemo(() => {
    if (!agentWorktrees.length) return null;
    // Find the explicitly selected agent, or fall back to the first entry.
    return (
      agentWorktrees.find((w) => w.agent.id === selectedAgentId) ??
      agentWorktrees[0]
    );
  }, [agentWorktrees, selectedAgentId]);

  const taskId = activeWorktree?.taskId;
  const activeAgentId = activeWorktree?.agent.id;

  // Refetch when task status changes.
  useWSEvent("task:dispatch", () => { refetch(); });
  useWSEvent("task:progress", () => { refetch(); });
  useWSEvent("task:completed", () => { refetch(); });
  useWSEvent("task:failed", () => { refetch(); });

  const {
    tree,
    gitStatus,
    selectedPath,
    selectFile,
    fileContent,
    contentLoading,
    fileDiff,
    diffLoading,
  } = useTaskFileTree(issueId, taskId, activeAgentId, deltaOnly);

  // ── Scroll position persistence per (issue, agent) ───────────────────────
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const setPersistedScroll = useWorktreeViewStore((s) => s.setScrollTop);

  // Restore scroll on agent / tree change — layout effect so we land on the
  // saved row before the user sees the default-top position.
  useLayoutEffect(() => {
    if (!activeAgentId || !tree || tree.length === 0) return;
    const el = scrollContainerRef.current;
    if (!el) return;
    const k = `${issueId}:${activeAgentId}`;
    const saved =
      useWorktreeViewStore.getState().entries[k]?.scrollTop ?? 0;
    el.scrollTop = saved;
  }, [issueId, activeAgentId, tree]);

  // Persist scroll position with a light debounce.
  useEffect(() => {
    if (!activeAgentId) return;
    const el = scrollContainerRef.current;
    if (!el) return;
    let raf = 0;
    const handler = () => {
      if (raf) cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() => {
        setPersistedScroll(issueId, activeAgentId, el.scrollTop);
      });
    };
    el.addEventListener("scroll", handler, { passive: true });
    return () => {
      el.removeEventListener("scroll", handler);
      if (raf) cancelAnimationFrame(raf);
    };
  }, [issueId, activeAgentId, setPersistedScroll]);

  // ── Header (always shown) ────────────────────────────────────────────────
  const header = (
    <div className="flex h-10 shrink-0 items-center justify-between border-b px-3">
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <FolderGit2 className="h-3.5 w-3.5" />
        <span>Agent Worktree</span>
      </div>
      <Button variant="ghost" size="icon-xs" onClick={onClose}>
        <X className="h-3.5 w-3.5" />
      </Button>
    </div>
  );

  // ── Empty state ──────────────────────────────────────────────────────────
  if (!agentWorktrees.length) {
    return (
      <div className="flex h-full flex-col">
        {header}
        <div className="flex flex-1 items-center justify-center px-4 text-center text-xs text-muted-foreground">
          No worktree yet — assign an agent to this issue to see files here
        </div>
      </div>
    );
  }

  // Body content: either the resizable split, or a loading state — both
  // render underneath the shared header + agent tabs so the tabs are always
  // visible (and not constrained by the narrow tree panel).
  const body =
    !tree || tree.length === 0 ? (
      <div className="flex flex-1 items-center justify-center px-4 text-center text-xs text-muted-foreground">
        {activeWorktree?.isActive
          ? "Agent is working — waiting for first file tree update..."
          : "Waiting for file tree..."}
      </div>
    ) : (
      <ResizablePanelGroup
        orientation="horizontal"
        className="flex-1 min-h-0"
        defaultLayout={splitLayout}
        onLayoutChanged={onSplitLayoutChanged}
      >
        {/* File tree panel — percentage default, pixel minimum, capped so
            it can't eat the entire width and hide the preview. */}
        <ResizablePanel id="ws-tree" defaultSize="40%" minSize={140} maxSize="70%">
          <div className="flex h-full flex-col min-h-0">
            <div className="flex shrink-0 items-center justify-between border-b px-2 py-1">
              <span className="text-[10px] uppercase tracking-wider text-muted-foreground">
                Files
              </span>
              <button
                type="button"
                onClick={() => setDeltaOnly((v) => !v)}
                title={deltaOnly ? "Show all files" : "Show only changed files"}
                className={cn(
                  "flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] transition-colors",
                  deltaOnly
                    ? "bg-accent text-accent-foreground"
                    : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                )}
              >
                <GitCompare className="h-3 w-3" />
                <span>Delta</span>
              </button>
            </div>
            <div
              ref={scrollContainerRef}
              className="flex-1 min-h-0 overflow-y-auto"
            >
              <WorkspaceFileTree
                tree={tree}
                gitStatus={gitStatus}
                selectedPath={selectedPath}
                onSelectFile={selectFile}
                deltaOnly={deltaOnly}
              />
            </div>
          </div>
        </ResizablePanel>

        <ResizableHandle withHandle />

        {/* File preview panel — real pixel minimum so it stays visible. */}
        <ResizablePanel id="ws-preview" defaultSize="60%" minSize={240}>
          <WorkspaceFilePreview
            path={selectedPath}
            content={fileContent}
            loading={deltaOnly ? diffLoading : contentLoading}
            diff={fileDiff}
            diffMode={deltaOnly}
          />
        </ResizablePanel>
      </ResizablePanelGroup>
    );

  return (
    <div className="flex h-full flex-col">
      {header}
      <AgentTabs
        worktrees={agentWorktrees}
        selectedAgentId={activeWorktree?.agent.id ?? null}
        onSelect={setSelectedAgentId}
      />
      {body}
    </div>
  );
}

// ── Agent tabs ────────────────────────────────────────────────────────────────

/**
 * Agent tabs: always visible when there is at least one worktree so users
 * know which agent's workdir they're browsing. When there's only one agent,
 * the single tab acts as a non-interactive label.
 */
function AgentTabs({
  worktrees,
  selectedAgentId,
  onSelect,
}: {
  worktrees: AgentWorktree[];
  selectedAgentId: string | null;
  onSelect: (agentId: string) => void;
}) {
  if (worktrees.length === 0) return null;
  const isInteractive = worktrees.length > 1;

  return (
    <div className="flex shrink-0 items-center gap-0.5 overflow-x-auto border-b px-2 py-1">
      {worktrees.map((w) => {
        const isSelected =
          w.agent.id === selectedAgentId ||
          (selectedAgentId === null && w === worktrees[0]);
        return (
          <button
            key={w.agent.id}
            type="button"
            disabled={!isInteractive}
            onClick={() => onSelect(w.agent.id)}
            className={cn(
              "flex shrink-0 items-center gap-1.5 rounded px-2 py-0.5 text-xs transition-colors",
              isSelected
                ? "bg-accent text-accent-foreground"
                : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
              !isInteractive && "cursor-default",
            )}
          >
            {w.agent.avatar_url ? (
              <img
                src={w.agent.avatar_url}
                alt={w.agent.name}
                className="h-3.5 w-3.5 rounded-full object-cover"
              />
            ) : (
              <Bot className="h-3 w-3 shrink-0" />
            )}
            <span className="max-w-[120px] truncate">{w.agent.name}</span>
            {w.isActive && (
              <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-green-500" />
            )}
          </button>
        );
      })}
    </div>
  );
}
