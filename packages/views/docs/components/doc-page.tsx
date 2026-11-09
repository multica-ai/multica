"use client";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  History,
  MoreHorizontal,
  FolderInput,
  Trash2,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  docDetailOptions,
  docTreeOptions,
  folderAncestry,
} from "@multica/core/docs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import type { Doc } from "@multica/core/types";
import { ContentEditor } from "../../editor";
import { BreadcrumbHeader } from "../../layout/breadcrumb-header";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { useDocDraft } from "./use-doc-draft";
import { MoveDialog } from "./move-dialog";
import { DeleteDocDialog } from "./doc-tree";
export function DocPage({ docId }: { docId: string }) {
  const wsId = useWorkspaceId();
  const { t } = useT("docs");
  const query = useQuery(docDetailOptions(wsId, docId));
  if (!query.data)
    return (
      <div className="p-6">
        {query.isError ? (
          <Button onClick={() => void query.refetch()}>
            {t(($) => $.editor.retry)}
          </Button>
        ) : (
          t(($) => $.history.loading)
        )}
      </div>
    );
  return <DocEditor key={`${wsId}:${docId}`} doc={query.data} />;
}
function DocEditor({ doc }: { doc: Doc }) {
  const { t } = useT("docs");
  const p = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const { data: tree } = useQuery(docTreeOptions(wsId));
  const { getActorName } = useActorName();
  const { draft, status, dirty, change, retry, reload } = useDocDraft(doc);
  const [moving, setMoving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const ancestors = folderAncestry(tree?.folders ?? [], doc.folderId);
  const author = getActorName(doc.updatedByType, doc.updatedById);
  return (
    <div className="flex h-full min-h-0 flex-col">
      <BreadcrumbHeader
        segments={[
          { href: p.docs(), label: t(($) => $.kb) },
          ...ancestors.map((f) => ({
            href: p.docFolder(f.id),
            label: f.title,
          })),
        ]}
        leaf={
          <span className="truncate">{doc.title || t(($) => $.untitled)}</span>
        }
        actions={
          <>
            <AppLink
              href={p.docHistory(doc.id)}
              className="flex items-center gap-1 rounded-md px-2 py-1 text-caption hover:bg-surface-hover"
            >
              <History className="size-4" />
              {t(($) => $.actions.history)}
            </AppLink>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    aria-label={t(($) => $.actions.more)}
                  />
                }
              >
                <MoreHorizontal className="size-4" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => setMoving(true)}>
                  <FolderInput />
                  {t(($) => $.actions.move)}
                </DropdownMenuItem>
                <DropdownMenuItem
                  variant="destructive"
                  onClick={() => setDeleting(true)}
                >
                  <Trash2 />
                  {t(($) => $.actions.delete)}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        }
      />
      {status === "conflict" && (
        <div
          role="alert"
          className="flex flex-wrap items-center gap-2 border-b bg-warning/10 px-6 py-2 text-body"
        >
          <span className="flex-1">
            {t(($) => $.editor.conflict, { name: author })}
          </span>
          <Button size="sm" onClick={() => void reload()}>
            {t(($) => $.editor.reload)}
          </Button>
        </div>
      )}
      <div className="min-h-0 flex-1 overflow-auto">
        <div className="mx-auto max-w-3xl px-6 py-8">
          <div className="mb-5 flex flex-wrap items-center justify-between gap-2 text-caption text-muted-foreground">
            <span>
              {t(($) => $.updated, {
                name: author,
                time: new Date(doc.updatedAt).toLocaleString(),
              })}
            </span>
            <span role="status">
              {status === "error" ? (
                <Button size="xs" variant="ghost" onClick={() => void retry()}>
                  {t(($) => $.editor.save_failed)}
                </Button>
              ) : status === "saving" || (dirty && status === "saved") ? (
                t(($) => $.editor.saving)
              ) : status !== "conflict" ? (
                t(($) => $.editor.saved)
              ) : null}
            </span>
          </div>
          {draft.backup && (
            <details className="mb-5 rounded-md border p-3 text-body">
              <summary className="cursor-pointer">
                {t(($) => $.editor.draft)}
              </summary>
              <textarea
                aria-label={t(($) => $.editor.draft)}
                readOnly
                value={`${draft.backup.title}\n\n${draft.backup.content}`}
                className="mt-2 min-h-40 w-full resize-y rounded-md bg-muted p-2 font-mono text-caption"
              />
            </details>
          )}
          <input
            aria-label={t(($) => $.editor.title_label)}
            value={draft.title}
            maxLength={300}
            placeholder={t(($) => $.untitled)}
            onChange={(e) => change({ title: e.target.value })}
            className="mb-6 w-full bg-transparent text-display-sm font-semibold outline-none"
          />
          <ContentEditor
            value={draft.content}
            onUpdate={(content) => change({ content })}
            debounceMs={0}
            flushPendingOnUnmount
            placeholder={t(($) => $.editor.placeholder)}
            className="min-h-80"
          />
        </div>
      </div>
      <MoveDialog
        target={moving ? { kind: "doc", id: doc.id, title: doc.title } : null}
        onOpenChange={setMoving}
      />
      <DeleteDocDialog
        target={deleting ? { kind: "doc", item: doc } : null}
        onOpenChange={setDeleting}
      />
    </div>
  );
}
