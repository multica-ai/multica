"use client";

import { useRef, useState } from "react";
import {
  ChevronRight,
  ChevronDown,
  FileText,
  File,
  Folder,
  FolderOpen,
  MoreHorizontal,
  Pencil,
  Trash2,
} from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * What a row may offer beyond selection. Absent for read-only viewers and for
 * the reserved primary file, so the tree never shows an action it would then
 * have to refuse.
 */
export interface FileTreeActions {
  /** Returns an error message, or "" when the path is free to use. */
  validatePath: (path: string, existing: string[]) => string;
  onRename: (from: string, to: string) => void;
  onDelete: (path: string) => void;
  /** Path that owns the skill's primary content and cannot be moved or removed. */
  reservedPath: string;
}

// ---------------------------------------------------------------------------
// Tree data structures
// ---------------------------------------------------------------------------

interface FileTreeNode {
  name: string;
  path: string;
  isDirectory: boolean;
  children: FileTreeNode[];
}

function buildTree(filePaths: string[]): FileTreeNode[] {
  const root: FileTreeNode[] = [];

  for (const filePath of filePaths) {
    const parts = filePath.split("/");
    let current = root;

    for (let i = 0; i < parts.length; i++) {
      const name = parts[i]!;
      const isLast = i === parts.length - 1;
      const path = parts.slice(0, i + 1).join("/");

      let existing = current.find((n) => n.name === name);

      if (!existing) {
        existing = {
          name,
          path,
          isDirectory: !isLast,
          children: [],
        };
        current.push(existing);
      }

      if (!isLast) {
        current = existing.children;
      }
    }
  }

  function sortNodes(nodes: FileTreeNode[]): FileTreeNode[] {
    nodes.sort((a, b) => {
      if (a.path === "SKILL.md") return -1;
      if (b.path === "SKILL.md") return 1;
      if (a.isDirectory !== b.isDirectory) return a.isDirectory ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
    for (const node of nodes) {
      if (node.isDirectory) sortNodes(node.children);
    }
    return nodes;
  }

  return sortNodes(root);
}

function getFileIcon(name: string) {
  if (name.endsWith(".md") || name.endsWith(".mdx")) return FileText;
  return File;
}

// ---------------------------------------------------------------------------
// Tree node renderer
// ---------------------------------------------------------------------------

function TreeNodeItem({
  node,
  selectedPath,
  onSelect,
  allPaths,
  actions,
  depth = 0,
}: {
  node: FileTreeNode;
  selectedPath: string;
  onSelect: (path: string) => void;
  allPaths: string[];
  actions?: FileTreeActions;
  depth?: number;
}) {
  const { t } = useT("skills");
  const [renaming, setRenaming] = useState(false);
  const menuButtonRef = useRef<HTMLButtonElement | null>(null);
  const [expanded, setExpanded] = useState(true);
  const isSelected = node.path === selectedPath;

  if (node.isDirectory) {
    const FolderIcon = expanded ? FolderOpen : Folder;
    const ChevronIcon = expanded ? ChevronDown : ChevronRight;

    return (
      <div>
        <button
          type="button"
          onClick={() => setExpanded(!expanded)}
          className="flex h-8 w-full items-center gap-1.5 rounded-md pr-2.5 text-left text-caption text-muted-foreground transition-colors hover:bg-surface-hover hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          style={{ paddingLeft: `${depth * 12 + 10}px` }}
        >
          <ChevronIcon className="h-3 w-3 shrink-0" />
          <FolderIcon className="h-3.5 w-3.5 shrink-0" />
          <span className="truncate">{node.name}</span>
        </button>
        {expanded && (
          <div>
            {node.children.map((child) => (
              <TreeNodeItem
                key={child.path}
                node={child}
                selectedPath={selectedPath}
                onSelect={onSelect}
                allPaths={allPaths}
                actions={actions}
                depth={depth + 1}
              />
            ))}
          </div>
        )}
      </div>
    );
  }

  const Icon = getFileIcon(node.name);
  const editable = !!actions && node.path !== actions.reservedPath;

  if (renaming && actions) {
    return (
      <RenameRow
        node={node}
        depth={depth}
        allPaths={allPaths}
        actions={actions}
        onDone={() => setRenaming(false)}
      />
    );
  }

  return (
    <div
      className={cn(
        "group/row relative flex items-center rounded-md",
        isSelected
          ? "bg-surface-selected"
          : "hover:bg-surface-hover",
      )}
      // Right-click opens the same menu the trailing button does. The menu
      // anchors to that button rather than the cursor, which keeps one menu
      // per row instead of a context-menu root beside a dropdown root.
      onContextMenu={
        editable
          ? (event) => {
              event.preventDefault();
              menuButtonRef.current?.click();
            }
          : undefined
      }
    >
      <button
        type="button"
        role="tab"
        aria-selected={isSelected}
        onClick={() => onSelect(node.path)}
        className={cn(
          "flex h-8 min-w-0 flex-1 items-center gap-2 rounded-md pr-2.5 text-left text-caption transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
          isSelected
            ? "font-medium text-surface-selected-foreground"
            : "text-muted-foreground group-hover/row:text-foreground",
        )}
        style={{ paddingLeft: `${depth * 12 + 10}px` }}
      >
        <Icon className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate">{node.name}</span>
      </button>
      {editable && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <button
                ref={menuButtonRef}
                type="button"
                aria-label={t(($) => $.file_tree.actions.label, {
                  path: node.path,
                })}
                // Hidden until the row is hovered or something inside it holds
                // focus, so a rail of ten files is not a rail of ten buttons.
                // after:-inset-1 widens the hit area past the 20px glyph.
                className="mr-1 shrink-0 rounded p-0.5 text-faint-foreground opacity-0 transition-opacity after:absolute after:-inset-1 hover:text-foreground group-hover/row:opacity-100 focus-visible:opacity-100 aria-expanded:opacity-100"
                onClick={(event) => event.stopPropagation()}
              >
                <MoreHorizontal className="h-3.5 w-3.5" />
              </button>
            }
          />
          <DropdownMenuContent align="end" className="w-36">
            <DropdownMenuItem onClick={() => setRenaming(true)}>
              <Pencil />
              {t(($) => $.file_tree.actions.rename)}
            </DropdownMenuItem>
            <DropdownMenuItem
              variant="destructive"
              onClick={() => actions.onDelete(node.path)}
            >
              <Trash2 />
              {t(($) => $.file_tree.actions.delete)}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  );
}

/**
 * Rename happens in the row rather than a dialog: the name is being edited in
 * the place it is read, and the surrounding paths stay visible to compare
 * against. Validation is the caller's, so the tree does not carry a second
 * copy of what counts as a legal path.
 */
function RenameRow({
  node,
  depth,
  allPaths,
  actions,
  onDone,
}: {
  node: FileTreeNode;
  depth: number;
  allPaths: string[];
  actions: FileTreeActions;
  onDone: () => void;
}) {
  const [value, setValue] = useState(node.path);
  const [error, setError] = useState("");

  const submit = () => {
    const next = value.trim();
    if (next === node.path) {
      onDone();
      return;
    }
    const message = actions.validatePath(
      next,
      allPaths.filter((path) => path !== node.path),
    );
    if (message) {
      setError(message);
      return;
    }
    actions.onRename(node.path, next);
    onDone();
  };

  return (
    <div style={{ paddingLeft: `${depth * 12 + 10}px` }} className="pr-1">
      <Input
        autoFocus
        value={value}
        onChange={(event) => {
          setValue(event.target.value);
          setError("");
        }}
        onBlur={submit}
        onKeyDown={(event) => {
          if (event.key === "Enter") submit();
          if (event.key === "Escape") onDone();
        }}
        className="h-7 font-mono text-caption"
      />
      {error && (
        <p role="alert" className="mt-1 text-caption text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Public component
// ---------------------------------------------------------------------------

export function FileTree({
  filePaths,
  selectedPath,
  onSelect,
  actions,
}: {
  filePaths: string[];
  selectedPath: string;
  onSelect: (path: string) => void;
  /** Omit to render a read-only tree. */
  actions?: FileTreeActions;
}) {
  const { t } = useT("skills");
  const tree = buildTree(filePaths);

  if (tree.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
        <FolderOpen className="h-5 w-5 text-faint-foreground" />
        <p className="mt-2 text-caption">{t(($) => $.file_tree.no_files)}</p>
      </div>
    );
  }

  // No `role="tablist"` here: the caller owns the list semantics so a rail
  // split into "main" + "supporting" groups stays a single tab list.
  return (
    <div className="flex flex-col gap-0.5">
      {tree.map((node) => (
        <TreeNodeItem
          key={node.path}
          node={node}
          selectedPath={selectedPath}
          onSelect={onSelect}
          allPaths={filePaths}
          actions={actions}
        />
      ))}
    </div>
  );
}
