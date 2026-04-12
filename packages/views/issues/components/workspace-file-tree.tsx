"use client";

import { useState, useCallback, useEffect, useMemo } from "react";
import {
  ChevronRight,
  ChevronDown,
  FileText,
  File,
  Folder,
  FolderOpen,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import type { FileNode, GitFileStatus } from "@multica/core/types";

// ---------------------------------------------------------------------------
// Git status helpers
// ---------------------------------------------------------------------------

function gitStatusColor(status: GitFileStatus): string {
  switch (status) {
    case "A":
    case "?":
      return "text-green-500";
    case "M":
      return "text-yellow-500";
    case "D":
      return "text-red-500";
    case "R":
      return "text-blue-500";
    default:
      return "";
  }
}

function gitStatusLabel(status: GitFileStatus): string {
  switch (status) {
    case "A":
      return "A";
    case "M":
      return "M";
    case "D":
      return "D";
    case "?":
      return "U";
    case "R":
      return "R";
    default:
      return "";
  }
}

/** Check if any descendant of a directory has git changes. */
function dirHasChanges(
  node: FileNode,
  gitStatus: Record<string, GitFileStatus>,
): GitFileStatus | null {
  if (node.type !== "directory" || !node.children) return null;
  let hasModified = false;
  let hasAdded = false;
  for (const child of node.children) {
    const s = gitStatus[child.path];
    if (s === "M") hasModified = true;
    if (s === "A" || s === "?") hasAdded = true;
    if (child.type === "directory") {
      const childStatus = dirHasChanges(child, gitStatus);
      if (childStatus === "M") hasModified = true;
      if (childStatus === "A" || childStatus === "?") hasAdded = true;
    }
  }
  if (hasModified) return "M";
  if (hasAdded) return "A";
  return null;
}

/**
 * Return a new tree containing only files that have a git status, plus
 * their ancestor directories. Directories whose entire subtree has no
 * changes are removed.
 */
function filterTreeToChanged(
  nodes: FileNode[],
  gitStatus: Record<string, GitFileStatus>,
): FileNode[] {
  const out: FileNode[] = [];
  for (const node of nodes) {
    if (node.type === "file") {
      if (gitStatus[node.path]) out.push(node);
      continue;
    }
    // Directory — keep it only if it contains at least one changed file.
    const filteredChildren = filterTreeToChanged(node.children ?? [], gitStatus);
    if (filteredChildren.length > 0) {
      out.push({ ...node, children: filteredChildren });
    }
  }
  return out;
}

function getFileIcon(name: string) {
  if (name.endsWith(".md") || name.endsWith(".mdx")) return FileText;
  return File;
}

// ---------------------------------------------------------------------------
// Tree node
// ---------------------------------------------------------------------------

function TreeNode({
  node,
  selectedPath,
  gitStatus,
  depth,
  expandedFolders,
  onToggleFolder,
  onSelectFile,
}: {
  node: FileNode;
  selectedPath: string | null;
  gitStatus: Record<string, GitFileStatus>;
  depth: number;
  expandedFolders: Set<string>;
  onToggleFolder: (path: string) => void;
  onSelectFile: (path: string) => void;
}) {
  if (node.type === "directory") {
    const isExpanded = expandedFolders.has(node.path);
    const FolderIcon = isExpanded ? FolderOpen : Folder;
    const ChevronIcon = isExpanded ? ChevronDown : ChevronRight;
    const dirStatus = dirHasChanges(node, gitStatus);

    return (
      <div>
        <button
          onClick={() => onToggleFolder(node.path)}
          className="flex w-full items-center gap-1.5 py-1 text-left text-xs hover:bg-accent/50 rounded-sm"
          style={{ paddingLeft: `${depth * 12 + 8}px` }}
        >
          <ChevronIcon className="h-3 w-3 shrink-0 text-muted-foreground" />
          <FolderIcon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span className={cn("truncate", dirStatus && gitStatusColor(dirStatus))}>
            {node.name}
          </span>
        </button>
        {isExpanded && node.children && (
          <div>
            {node.children.map((child) => (
              <TreeNode
                key={child.path}
                node={child}
                selectedPath={selectedPath}
                gitStatus={gitStatus}
                depth={depth + 1}
                expandedFolders={expandedFolders}
                onToggleFolder={onToggleFolder}
                onSelectFile={onSelectFile}
              />
            ))}
          </div>
        )}
      </div>
    );
  }

  // File node
  const Icon = getFileIcon(node.name);
  const status = gitStatus[node.path];
  const isSelected = node.path === selectedPath;

  return (
    <button
      onClick={() => onSelectFile(node.path)}
      className={cn(
        "flex w-full items-center gap-1.5 py-1 text-left text-xs rounded-sm",
        isSelected ? "bg-accent text-accent-foreground" : "hover:bg-accent/50",
      )}
      style={{ paddingLeft: `${depth * 12 + 8 + 16}px` }}
    >
      <Icon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      <span className={cn("truncate flex-1", status && gitStatusColor(status))}>
        {node.name}
      </span>
      {status && (
        <span className={cn("text-[10px] font-mono shrink-0 mr-2", gitStatusColor(status))}>
          {gitStatusLabel(status)}
        </span>
      )}
    </button>
  );
}

// ---------------------------------------------------------------------------
// Public component
// ---------------------------------------------------------------------------

export function WorkspaceFileTree({
  tree,
  gitStatus,
  selectedPath,
  onSelectFile,
  deltaOnly = false,
}: {
  tree: FileNode[];
  gitStatus: Record<string, GitFileStatus>;
  selectedPath: string | null;
  onSelectFile: (path: string) => void;
  /** When true, only show files with git changes. */
  deltaOnly?: boolean;
}) {
  const [expandedFolders, setExpandedFolders] = useState<Set<string>>(new Set());

  // Apply delta filter if enabled.
  const visibleTree = useMemo(
    () => (deltaOnly ? filterTreeToChanged(tree, gitStatus) : tree),
    [tree, gitStatus, deltaOnly],
  );

  // When delta mode is on, expand all directories in the filtered tree so
  // users see all changed files at a glance without clicking through.
  useEffect(() => {
    if (!deltaOnly) return;
    const allDirs: string[] = [];
    const walk = (nodes: FileNode[]) => {
      for (const n of nodes) {
        if (n.type === "directory") {
          allDirs.push(n.path);
          if (n.children) walk(n.children);
        }
      }
    };
    walk(visibleTree);
    setExpandedFolders((prev) => {
      const next = new Set(prev);
      let changed = false;
      for (const p of allDirs) {
        if (!next.has(p)) {
          next.add(p);
          changed = true;
        }
      }
      return changed ? next : prev;
    });
  }, [deltaOnly, visibleTree]);

  // Auto-expand parents of selected file
  useEffect(() => {
    if (!selectedPath) return;
    const parts = selectedPath.split("/");
    const parents: string[] = [];
    for (let i = 0; i < parts.length - 1; i++) {
      parents.push(parts.slice(0, i + 1).join("/"));
    }
    if (parents.length > 0) {
      setExpandedFolders((prev) => {
        const next = new Set(prev);
        let changed = false;
        for (const p of parents) {
          if (!next.has(p)) {
            next.add(p);
            changed = true;
          }
        }
        return changed ? next : prev;
      });
    }
  }, [selectedPath]);

  const handleToggleFolder = useCallback((path: string) => {
    setExpandedFolders((prev) => {
      const next = new Set(prev);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
      }
      return next;
    });
  }, []);

  if (tree.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
        <FolderOpen className="h-5 w-5 text-muted-foreground/40" />
        <p className="mt-2 text-xs">No files yet</p>
      </div>
    );
  }

  if (visibleTree.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
        <p className="text-xs">No changes</p>
      </div>
    );
  }

  return (
    <div className="py-1 px-1">
      {visibleTree.map((node) => (
        <TreeNode
          key={node.path}
          node={node}
          selectedPath={selectedPath}
          gitStatus={gitStatus}
          depth={0}
          expandedFolders={expandedFolders}
          onToggleFolder={handleToggleFolder}
          onSelectFile={onSelectFile}
        />
      ))}
    </div>
  );
}
