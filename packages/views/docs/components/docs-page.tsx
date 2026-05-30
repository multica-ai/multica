"use client";

import { useState } from "react";
import { FileText, Plus, ChevronRight } from "lucide-react";
import type { Doc } from "@multica/core/types";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { docListOptions } from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { PageHeader } from "../../layout/page-header";
import { AppLink } from "../../navigation";

function DocTreeItem({ doc, paths, depth = 0 }: { doc: Doc; paths: ReturnType<typeof useWorkspacePaths>; depth?: number }) {
  return (
    <AppLink
      href={paths.docDetail(doc.id)}
      className="group flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent"
      style={{ paddingLeft: `${8 + depth * 16}px` }}
    >
      <ChevronRight className="h-3 w-3 text-muted-foreground opacity-0 group-hover:opacity-100" />
      <FileText className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      <span className="flex-1 truncate">{doc.title || "Untitled"}</span>
    </AppLink>
  );
}

function buildTree(docs: Doc[]): { root: Doc[]; childrenOf: Map<string, Doc[]> } {
  const childrenOf = new Map<string, Doc[]>();
  const root: Doc[] = [];
  for (const doc of docs) {
    if (doc.parent_id) {
      const arr = childrenOf.get(doc.parent_id) ?? [];
      arr.push(doc);
      childrenOf.set(doc.parent_id, arr);
    } else {
      root.push(doc);
    }
  }
  return { root, childrenOf };
}

function DocTree({ docs, paths }: { docs: Doc[]; paths: ReturnType<typeof useWorkspacePaths> }) {
  const { root, childrenOf } = buildTree(docs);

  function renderNodes(nodes: Doc[], depth: number): React.ReactNode {
    return nodes.map((doc) => (
      <div key={doc.id}>
        <DocTreeItem doc={doc} paths={paths} depth={depth} />
        {childrenOf.has(doc.id) && renderNodes(childrenOf.get(doc.id)!, depth + 1)}
      </div>
    ));
  }

  if (root.length === 0) {
    return (
      <div className="flex flex-col items-center gap-2 py-12 text-center text-muted-foreground">
        <FileText className="h-8 w-8 opacity-40" />
        <p className="text-sm">No docs yet. Create your first doc.</p>
      </div>
    );
  }

  return <div className="flex flex-col gap-0.5 py-1">{renderNodes(root, 0)}</div>;
}

export function DocsPage() {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const qc = useQueryClient();
  const [creating, setCreating] = useState(false);

  const { data: docs = [], isLoading } = useQuery(docListOptions(wsId));

  const createMutation = useMutation({
    mutationFn: () => api.createDoc({ title: "", content: "", position: (docs.length + 1) * 1000 }),
    onSuccess: (doc) => {
      qc.invalidateQueries({ queryKey: ["workspaces", wsId, "docs"] });
      // Navigate to new doc
      window.location.href = paths.docDetail(doc.id);
    },
    onSettled: () => setCreating(false),
  });

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <FileText className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">Docs</h1>
          {docs.length > 0 && (
            <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
              {docs.length}
            </span>
          )}
        </div>
        <Button
          type="button"
          size="sm"
          disabled={creating}
          onClick={() => {
            setCreating(true);
            createMutation.mutate();
          }}
        >
          <Plus className="h-3 w-3" />
          New Doc
        </Button>
      </PageHeader>

      <div className="flex-1 overflow-auto p-2">
        {isLoading ? (
          <div className="flex flex-col gap-1 px-2 py-1">
            {[1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-7 w-full rounded-md" />
            ))}
          </div>
        ) : (
          <DocTree docs={docs} paths={paths} />
        )}
      </div>
    </div>
  );
}
