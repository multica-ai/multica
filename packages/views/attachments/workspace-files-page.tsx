"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import { workspaceFilesOptions } from "@multica/core/attachments";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Download,
  Eye,
  FileText,
  ListTodo,
  MessageSquare,
} from "lucide-react";
import { isPreviewable, useAttachmentPreview, useDownloadAttachment } from "../editor";
import { useT } from "../i18n";
import { AppLink } from "../navigation";

function formatFileSize(size: number): string {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

export function WorkspaceFilesPage() {
  const { t } = useT("chat");
  const workspaceId = useWorkspaceId();
  const workspacePaths = useWorkspacePaths();
  const preview = useAttachmentPreview();
  const download = useDownloadAttachment();
  const query = useInfiniteQuery(workspaceFilesOptions(workspaceId));
  const files = query.data?.pages.flatMap((page) => page.attachments) ?? [];

  return (
    <section className="mx-auto flex h-full w-full max-w-6xl flex-col px-4 py-5 sm:px-6 sm:py-7">
      <header className="mb-5">
        <h1 className="text-title font-semibold text-foreground">
          {t(($) => $.files.title)}
        </h1>
        <p className="mt-1 text-body text-muted-foreground">
          {t(($) => $.files.description)}
        </p>
      </header>

      {query.isPending ? (
        <div role="status" aria-label={t(($) => $.files.loading)} className="grid gap-2">
          {[0, 1, 2].map((key) => (
            <Skeleton key={key} className="h-20 w-full rounded-lg" />
          ))}
        </div>
      ) : query.isError ? (
        <div className="flex min-h-64 flex-col items-center justify-center rounded-xl border border-border px-6 text-center">
          <FileText className="mb-3 size-8 text-muted-foreground" aria-hidden />
          <p className="text-body font-medium text-foreground">
            {t(($) => $.files.load_failed)}
          </p>
          <Button className="mt-4 min-h-11" variant="outline" onClick={() => void query.refetch()}>
            {t(($) => $.files.retry)}
          </Button>
        </div>
      ) : files.length === 0 ? (
        <div className="flex min-h-64 flex-col items-center justify-center rounded-xl border border-dashed border-border px-6 text-center">
          <FileText className="mb-3 size-8 text-muted-foreground" aria-hidden />
          <p className="text-body font-medium text-foreground">
            {t(($) => $.files.empty_title)}
          </p>
          <p className="mt-1 max-w-sm text-caption text-muted-foreground">
            {t(($) => $.files.empty_description)}
          </p>
        </div>
      ) : (
        <>
          <ul className="grid gap-2" aria-label={t(($) => $.files.list_label)}>
            {files.map((file) => {
              const canPreview = isPreviewable(file.contentType, file.filename);
              const source = file.sourceType === "issue"
                ? {
                    href: workspacePaths.issueDetail(file.sourceId),
                    label: t(($) => $.files.source_issue),
                    title: file.sourceTitle || t(($) => $.files.source_issue),
                    Icon: ListTodo,
                  }
                : {
                    href: workspacePaths.chatSession(file.sourceId),
                    label: t(($) => $.files.source_chat),
                    title: file.sourceTitle || t(($) => $.files.untitled_chat),
                    Icon: MessageSquare,
                  };
              return (
                <li
                  key={file.id}
                  className="flex min-w-0 flex-col gap-3 rounded-lg border border-border bg-card p-3 sm:flex-row sm:items-center sm:px-4"
                >
                  <div className="flex min-w-0 flex-1 items-center gap-3">
                    <div className="flex size-10 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
                      <FileText className="size-5" aria-hidden />
                    </div>
                    <div className="min-w-0">
                      <p className="truncate text-body font-medium text-foreground" title={file.filename}>
                        {file.filename}
                      </p>
                      <div className="mt-1 flex min-w-0 items-center gap-2 text-caption text-muted-foreground">
                        <span className="shrink-0">{formatFileSize(file.sizeBytes)}</span>
                        <span aria-hidden>·</span>
                        <AppLink
                          href={source.href}
                          aria-label={`${source.label}: ${source.title}`}
                          className="inline-flex min-h-11 min-w-0 items-center gap-1 hover:text-foreground hover:underline sm:min-h-0"
                        >
                          <source.Icon className="size-3.5 shrink-0" aria-hidden />
                          <span className="truncate">{source.label}: {source.title}</span>
                        </AppLink>
                      </div>
                    </div>
                  </div>
                  <div className="flex shrink-0 gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      className="min-h-11 flex-1 sm:min-h-9 sm:flex-none"
                      disabled={!canPreview}
                      aria-label={`${t(($) => $.files.preview)} ${file.filename}`}
                      onClick={() => preview.tryOpen({
                        kind: "full",
                        attachment: file,
                      })}
                    >
                      <Eye className="size-4" aria-hidden />
                      {t(($) => $.files.preview)}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      className="min-h-11 flex-1 sm:min-h-9 sm:flex-none"
                      aria-label={`${t(($) => $.files.download)} ${file.filename}`}
                      onClick={() => void download(file.id)}
                    >
                      <Download className="size-4" aria-hidden />
                      {t(($) => $.files.download)}
                    </Button>
                  </div>
                </li>
              );
            })}
          </ul>
          {query.hasNextPage ? (
            <Button
              className="mx-auto mt-5 min-h-11"
              variant="outline"
              disabled={query.isFetchingNextPage}
              onClick={() => void query.fetchNextPage()}
            >
              {query.isFetchingNextPage
                ? t(($) => $.files.loading_more)
                : t(($) => $.files.load_more)}
            </Button>
          ) : null}
        </>
      )}
      {preview.modal}
    </section>
  );
}
