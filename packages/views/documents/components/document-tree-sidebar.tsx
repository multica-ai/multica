"use client";

import { useMemo, useState } from "react";
import {
  ChevronRight,
  File,
  Folder,
  FolderOpen,
  Pin,
  Search,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Input } from "@multica/ui/components/ui/input";
import { ScrollArea } from "@multica/ui/components/ui/scroll-area";
import type { WorkspaceDocument } from "@multica/core/types";
import { useT } from "../../i18n";

// ---------------------------------------------------------------------------
// Tree node builder — turns flat paths into a nested tree
// ---------------------------------------------------------------------------

interface TreeNode {
  name: string;
  path: string;
  isFolder: boolean;
  children: TreeNode[];
  doc?: WorkspaceDocument;
}

function buildTree(docs: WorkspaceDocument[]): TreeNode[] {
  const root: TreeNode[] = [];

  for (const doc of docs) {
    const parts = doc.path.split("/");
    let current = root;

    for (let i = 0; i < parts.length; i++) {
      const part = parts[i]!;
      const isLast = i === parts.length - 1;
      const partialPath = parts.slice(0, i + 1).join("/");

      let existing = current.find((n) => n.name === part && n.isFolder === !isLast);
      if (!existing) {
        existing = {
          name: part,
          path: partialPath,
          isFolder: !isLast,
          children: [],
          doc: isLast ? doc : undefined,
        };
        current.push(existing);
      }
      current = existing.children;
    }
  }

  // Sort: folders first, then alphabetically
  function sortNodes(nodes: TreeNode[]) {
    nodes.sort((a, b) => {
      if (a.isFolder !== b.isFolder) return a.isFolder ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
    for (const n of nodes) sortNodes(n.children);
  }
  sortNodes(root);

  return root;
}

// ---------------------------------------------------------------------------
// Tree node component
// ---------------------------------------------------------------------------

function TreeNodeItem({
  node,
  depth,
  selectedId,
  onSelect,
  expanded,
  onToggle,
}: {
  node: TreeNode;
  depth: number;
  selectedId: string | null;
  onSelect: (doc: WorkspaceDocument) => void;
  expanded: Set<string>;
  onToggle: (path: string) => void;
}) {
  const isExpanded = expanded.has(node.path);
  const isSelected = node.doc?.id === selectedId;

  if (node.isFolder) {
    return (
      <>
        <button
          type="button"
          onClick={() => onToggle(node.path)}
          className={cn(
            "flex w-full items-center gap-1.5 rounded-sm px-2 py-1 text-xs text-muted-foreground hover:bg-accent/50",
          )}
          style={{ paddingLeft: `${depth * 16 + 8}px` }}
        >
          <ChevronRight
            className={cn(
              "h-3 w-3 shrink-0 transition-transform",
              isExpanded && "rotate-90",
            )}
          />
          {isExpanded ? (
            <FolderOpen className="h-3.5 w-3.5 shrink-0 text-muted-foreground/70" />
          ) : (
            <Folder className="h-3.5 w-3.5 shrink-0 text-muted-foreground/70" />
          )}
          <span className="truncate font-medium">{node.name}</span>
        </button>
        {isExpanded &&
          node.children.map((child) => (
            <TreeNodeItem
              key={child.path}
              node={child}
              depth={depth + 1}
              selectedId={selectedId}
              onSelect={onSelect}
              expanded={expanded}
              onToggle={onToggle}
            />
          ))}
      </>
    );
  }

  return (
    <button
      type="button"
      onClick={() => node.doc && onSelect(node.doc)}
      className={cn(
        "flex w-full items-center gap-1.5 rounded-sm px-2 py-1 text-xs",
        isSelected
          ? "bg-accent text-accent-foreground"
          : "text-muted-foreground hover:bg-accent/50",
      )}
      style={{ paddingLeft: `${depth * 16 + 8}px` }}
    >
      <File className="h-3.5 w-3.5 shrink-0" />
      <span className="min-w-0 flex-1 truncate text-left">{node.name}</span>
      {node.doc?.pinned && (
        <Pin className="h-2.5 w-2.5 shrink-0 text-muted-foreground/50" />
      )}
    </button>
  );
}

// ---------------------------------------------------------------------------
// Sidebar component
// ---------------------------------------------------------------------------

interface DocumentTreeSidebarProps {
  documents: WorkspaceDocument[];
  selectedId: string | null;
  onSelect: (doc: WorkspaceDocument) => void;
  className?: string;
}

export function DocumentTreeSidebar({
  documents,
  selectedId,
  onSelect,
  className,
}: DocumentTreeSidebarProps) {
  const { t } = useT("documents");
  const [search, setSearch] = useState("");
  const [expanded, setExpanded] = useState<Set<string>>(() => {
    // Auto-expand all folders
    const paths = new Set<string>();
    for (const doc of documents) {
      const parts = doc.path.split("/");
      for (let i = 1; i < parts.length; i++) {
        paths.add(parts.slice(0, i).join("/"));
      }
    }
    return paths;
  });

  const filtered = useMemo(() => {
    if (!search.trim()) return documents;
    const q = search.trim().toLowerCase();
    return documents.filter(
      (d) =>
        d.path.toLowerCase().includes(q) ||
        d.title?.toLowerCase().includes(q) ||
        d.description?.toLowerCase().includes(q),
    );
  }, [documents, search]);

  const tree = useMemo(() => buildTree(filtered), [filtered]);

  const handleToggle = (path: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  };

  return (
    <div className={cn("flex flex-col border-r", className)}>
      <div className="shrink-0 border-b p-2">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2 top-1/2 h-3 w-3 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t(($) => $.page.search_placeholder)}
            className="h-7 pl-7 text-xs"
          />
        </div>
      </div>
      <ScrollArea className="flex-1">
        <div className="p-1">
          {tree.length === 0 ? (
            <p className="px-3 py-6 text-center text-xs text-muted-foreground">
              {search
                ? t(($) => $.page.no_matches.title)
                : t(($) => $.page.empty.title)}
            </p>
          ) : (
            tree.map((node) => (
              <TreeNodeItem
                key={node.path}
                node={node}
                depth={0}
                selectedId={selectedId}
                onSelect={onSelect}
                expanded={expanded}
                onToggle={handleToggle}
              />
            ))
          )}
        </div>
      </ScrollArea>
    </div>
  );
}
