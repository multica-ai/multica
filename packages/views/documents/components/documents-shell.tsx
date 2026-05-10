"use client";

import { useState, type ReactNode } from "react";
import { AlertCircle, FileText, Plus } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  documentListOptions,
  useUpsertDocument,
} from "@multica/core/documents";
import type { WorkspaceDocument } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { toast } from "sonner";
import { useNavigation } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { DocumentTreeSidebar } from "./document-tree-sidebar";
import { NewDocumentDialog } from "./new-document-dialog";
import { useT } from "../../i18n";

interface DocumentsShellProps {
  selectedId: string | null;
  children: ReactNode;
}

// Hosts the page header + tree sidebar that persist across /documents and
// /documents/[id]. Mounted via a Next.js layout so navigating between docs
// only swaps the right pane — sidebar state (expand/search) survives.
export function DocumentsShell({ selectedId, children }: DocumentsShellProps) {
  const { t } = useT("documents");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();

  const {
    data: documents = [],
    isLoading,
    error,
    refetch,
  } = useQuery(documentListOptions(wsId));

  const [createOpen, setCreateOpen] = useState(false);
  const upsertDoc = useUpsertDocument();

  const handleSelect = (doc: WorkspaceDocument) => {
    navigation.push(paths.documentDetail(doc.id));
  };

  const handleCreate = (path: string, title?: string, description?: string) => {
    upsertDoc.mutate(
      { path, content: "", title, description },
      {
        onSuccess: (doc) => {
          toast.success(t(($) => $.create_dialog.toast_created));
          setCreateOpen(false);
          navigation.push(paths.documentDetail(doc.id));
        },
        onError: (err) => {
          toast.error(
            err instanceof Error
              ? err.message
              : t(($) => $.create_dialog.toast_create_failed),
          );
        },
      },
    );
  };

  const totalCount = documents.length;

  if (isLoading) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <PageHeader className="justify-between px-5">
          <div className="flex items-center gap-2">
            <FileText className="h-4 w-4 text-muted-foreground" />
            <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
          </div>
        </PageHeader>
        <div className="flex flex-1 min-h-0">
          <div className="w-64 border-r p-3 space-y-2">
            <Skeleton className="h-7 w-full rounded-md" />
            <Skeleton className="h-5 w-3/4 rounded-md" />
            <Skeleton className="h-5 w-1/2 rounded-md" />
            <Skeleton className="h-5 w-2/3 rounded-md" />
          </div>
          <div className="flex-1 p-6">
            <Skeleton className="h-8 w-1/3 rounded-md" />
          </div>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <PageHeader className="justify-between px-5">
          <div className="flex items-center gap-2">
            <FileText className="h-4 w-4 text-muted-foreground" />
            <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
          </div>
        </PageHeader>
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-16 text-center">
          <AlertCircle className="h-8 w-8 text-destructive" />
          <p className="text-sm font-medium">
            {t(($) => $.page.list_error.title)}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {error instanceof Error
              ? error.message
              : t(($) => $.page.list_error.fallback)}
          </p>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => refetch()}
          >
            {t(($) => $.page.list_error.retry)}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <FileText className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
          {totalCount > 0 && (
            <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
              {totalCount}
            </span>
          )}
          <p className="ml-2 hidden text-xs text-muted-foreground md:block">
            {t(($) => $.page.tagline)}
          </p>
        </div>
        <Button type="button" size="sm" onClick={() => setCreateOpen(true)}>
          <Plus className="h-3 w-3" />
          {t(($) => $.page.new_document)}
        </Button>
      </PageHeader>

      {totalCount === 0 ? (
        <div className="flex flex-1 flex-col items-center justify-center px-6 py-16 text-center">
          <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted">
            <FileText className="h-6 w-6 text-muted-foreground" />
          </div>
          <h2 className="mt-4 text-base font-semibold">
            {t(($) => $.page.empty.title)}
          </h2>
          <p className="mt-1 max-w-md text-sm text-muted-foreground">
            {t(($) => $.page.empty.description)}
          </p>
          <Button
            type="button"
            onClick={() => setCreateOpen(true)}
            size="sm"
            className="mt-5"
          >
            <Plus className="h-3 w-3" />
            {t(($) => $.page.new_document)}
          </Button>
        </div>
      ) : (
        <div className="flex flex-1 min-h-0">
          <DocumentTreeSidebar
            documents={documents}
            selectedId={selectedId}
            onSelect={handleSelect}
            className="w-64 shrink-0"
          />
          <div className="flex flex-1 min-h-0 flex-col">{children}</div>
        </div>
      )}

      {createOpen && (
        <NewDocumentDialog
          onClose={() => setCreateOpen(false)}
          onCreated={handleCreate}
          isPending={upsertDoc.isPending}
        />
      )}
    </div>
  );
}
