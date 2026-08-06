import { useMemo, useState } from "react";
import { ChevronDown, ChevronRight, FolderKanban, ListTodo } from "lucide-react";
import { buildRockTree, type RockTreeNode } from "../core/rock-tree";
import type { Rock, Terminology } from "../core/types";

const collectExpandable = (nodes: RockTreeNode[], into: Set<string>) => {
  for (const node of nodes) {
    if (node.children.length > 0) into.add(node.key);
    collectExpandable(node.children, into);
  }
  return into;
};

const statusLabel = (status: string) => status.replaceAll("_", " ");

function TreeRow({ node, depth, expanded, onToggle }: {
  node: RockTreeNode;
  depth: number;
  expanded: Set<string>;
  onToggle: (key: string) => void;
}) {
  const hasChildren = node.children.length > 0;
  const isOpen = hasChildren && expanded.has(node.key);
  const isProject = node.kind === "project";
  const progress = node.issueCount ? (node.doneIssueCount / node.issueCount) * 100 : 0;

  return (
    <li role="treeitem" aria-level={depth + 1} aria-expanded={hasChildren ? isOpen : undefined} className="min-w-0">
      <div className="flex min-w-0 items-start gap-2 rounded-md py-1.5 pr-1 hover:bg-muted/50">
        {hasChildren ? (
          <button
            type="button"
            onClick={() => onToggle(node.key)}
            aria-label={`${isOpen ? "Collapse" : "Expand"} ${node.title}`}
            className="mt-0.5 flex size-5 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-muted"
          >
            {isOpen ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
          </button>
        ) : (
          <span className="mt-0.5 size-5 shrink-0" />
        )}
        {isProject ? <FolderKanban className="mt-1 size-4 shrink-0 text-muted-foreground" /> : <ListTodo className="mt-1 size-4 shrink-0 text-muted-foreground" />}
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            {node.identifier && <span className="shrink-0 font-mono text-xs text-muted-foreground">{node.identifier}</span>}
            <span className={`min-w-0 break-words text-sm ${isProject ? "font-medium" : ""}`}>{node.title}</span>
            {!isProject && node.status && <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-xs capitalize text-muted-foreground">{statusLabel(node.status)}</span>}
          </div>
          {node.issueCount > 0 && (
            <div className="mt-1 flex items-center gap-2">
              <div className="h-1 w-24 overflow-hidden rounded-full bg-muted"><div className="h-full bg-primary" style={{ width: `${progress}%` }} /></div>
              <span className="text-xs text-muted-foreground">{node.doneIssueCount}/{node.issueCount} done</span>
            </div>
          )}
        </div>
      </div>
      {isOpen && (
        <ul role="group" className="ml-2.5 border-l pl-3">
          {node.children.map((child) => <TreeRow key={child.key} node={child} depth={depth + 1} expanded={expanded} onToggle={onToggle} />)}
        </ul>
      )}
    </li>
  );
}

export function RockTree({ rock, terminology }: { rock: Rock; terminology: Terminology }) {
  const tree = useMemo(() => buildRockTree(rock), [rock]);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const expandable = useMemo(() => collectExpandable(tree, new Set<string>()), [tree]);
  const expanded = useMemo(() => new Set([...expandable].filter((key) => !collapsed.has(key))), [expandable, collapsed]);

  const toggle = (key: string) => setCollapsed((current) => {
    const next = new Set(current);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    return next;
  });

  if (tree.length === 0) {
    return <p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">This {terminology.rock.toLowerCase()} currently stands on its own. Add connections whenever execution is ready.</p>;
  }

  return (
    <ul role="tree" aria-label="Connected execution" className="min-w-0">
      {tree.map((node) => <TreeRow key={node.key} node={node} depth={0} expanded={expanded} onToggle={toggle} />)}
    </ul>
  );
}
