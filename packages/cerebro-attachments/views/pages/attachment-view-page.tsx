"use client";

import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Clipboard, Download, Link as LinkIcon, FileText } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { attachmentDetailOptions } from "@multica/cerebro-attachments/core/queries";
import { viewableKind } from "@multica/cerebro-attachments/core/viewable";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { api } from "@multica/core/api";
import { useNavigation } from "@multica/views/navigation";
import { MobileSidebarTrigger } from "@multica/views/layout/page-header";
import { ArtifactBody } from "@multica/cerebro-artifacts/views/components";

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

/**
 * Fetches viewable text bodies through the API content endpoint. This avoids
 * CDN CORS/content-disposition issues and lets PDFs return extracted text.
 */
function useFetchedText(attachmentId: string, enabled: boolean) {
  return useQuery({
    queryKey: ["attachment-body", attachmentId],
    enabled,
    queryFn: async () => (await api.getAttachmentTextContent(attachmentId)).text,
  });
}

export function AttachmentViewPage({ attachmentId }: { attachmentId: string }) {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const { data: attachment, isLoading, isError } = useQuery(
    attachmentDetailOptions(wsId, attachmentId),
  );

  const kind = attachment
    ? viewableKind(attachment.content_type, attachment.filename)
    : null;
  // For HTML we render the body via `srcDoc` rather than pointing the iframe
  // at the file URL — backends can serve attachments with `frame-ancestors
  // 'none'` (the dev server does this), which would block direct framing.
  const needsBody = kind === "html" || kind === "markdown" || kind === "text" || kind === "json" || kind === "pdf";
  const body = useFetchedText(attachment?.id ?? "", Boolean(attachment && needsBody));

  if (isLoading) {
    return (
      <div className="flex h-full flex-col">
        <div className="flex h-12 shrink-0 items-center border-b px-4">
          <MobileSidebarTrigger className="mr-0" />
        </div>
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          Loading…
        </div>
      </div>
    );
  }
  if (isError || !attachment) {
    return (
      <div className="h-full overflow-y-auto">
        <div className="mx-auto w-full max-w-3xl px-4 py-4 md:px-8 md:py-6">
          <div className="mb-3 flex items-center gap-2">
            <MobileSidebarTrigger className="mr-0" />
            <Button variant="ghost" size="sm" onClick={() => router.back()}>
              <ArrowLeft className="mr-1 size-4" /> Back
            </Button>
          </div>
          <p className="mt-4 text-sm text-muted-foreground">
            This attachment is not available.
          </p>
        </div>
      </div>
    );
  }

  const handleDownload = () => {
    if (attachment.download_url) {
      window.open(attachment.download_url, "_blank", "noopener,noreferrer");
    }
  };
  const handleCopyLink = async () => {
    const url = router.getShareableUrl
      ? router.getShareableUrl(wsPaths.attachmentView(attachment.id))
      : window.location.href;
    try {
      await navigator.clipboard.writeText(url);
      toast.success("Link copied");
    } catch {
      toast.error("Failed to copy link");
    }
  };
  const handleCopyText = async () => {
    if (!body.data) return;
    try {
      await navigator.clipboard.writeText(body.data);
      toast.success("Text copied");
    } catch {
      toast.error("Failed to copy text");
    }
  };

  // Issue context — when an attachment is bound to an issue, allow back-navigation
  // to it. Comment-bound attachments inherit the issue context via comment_id →
  // issue, but we don't load the comment here; the issue link is enough.
  const linkBackPath = attachment.issue_id
    ? wsPaths.issueDetail(attachment.issue_id)
    : null;

  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto w-full max-w-3xl px-4 py-4 md:px-8 md:py-6">
        <div className="mb-3 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <MobileSidebarTrigger className="mr-0" />
            <Button
              variant="ghost"
              size="sm"
              onClick={() => (linkBackPath ? router.push(linkBackPath) : router.back())}
            >
              <ArrowLeft className="mr-1 size-4" /> Back
            </Button>
          </div>
          <div className="flex items-center gap-1">
            <Button variant="ghost" size="sm" onClick={handleCopyLink}>
              <LinkIcon className="mr-1 size-4" /> Copy link
            </Button>
            {kind === "pdf" && body.data && (
              <Button variant="ghost" size="sm" onClick={handleCopyText}>
                <Clipboard className="mr-1 size-4" /> Copy text
              </Button>
            )}
            {attachment.download_url && (
              <Button variant="ghost" size="sm" onClick={handleDownload}>
                <Download className="mr-1 size-4" /> Download
              </Button>
            )}
          </div>
        </div>

        <div className="mb-4 flex items-center gap-2">
          <FileText className="size-4 text-muted-foreground" />
          <Badge variant="outline" className="text-xs uppercase">
            {kind ?? attachment.content_type.split("/")[1] ?? "file"}
          </Badge>
          <span className="text-xs text-muted-foreground">
            {formatBytes(attachment.size_bytes)}
          </span>
        </div>

        <h1 className="break-words text-2xl font-semibold leading-tight">
          {attachment.filename}
        </h1>

        <div className="mt-6">
          {kind === "html" ? (
            body.isLoading ? (
              <p className="text-sm text-muted-foreground">Loading…</p>
            ) : body.isError || !body.data ? (
              <FallbackUnavailable downloadUrl={attachment.download_url} />
            ) : (
              <iframe
                srcDoc={body.data}
                title={attachment.filename}
                sandbox="allow-scripts allow-popups"
                className="h-[80vh] w-full rounded border border-border bg-white"
              />
            )
          ) : kind === "markdown" ? (
            body.isLoading ? (
              <p className="text-sm text-muted-foreground">Loading…</p>
            ) : body.isError || !body.data ? (
              <FallbackUnavailable downloadUrl={attachment.download_url} />
            ) : (
              <div className="prose prose-sm max-w-none dark:prose-invert">
                <ArtifactBody body={body.data} />
              </div>
            )
          ) : kind === "json" || kind === "text" || kind === "pdf" ? (
            body.isLoading ? (
              <p className="text-sm text-muted-foreground">Loading…</p>
            ) : body.isError || !body.data ? (
              <FallbackUnavailable downloadUrl={attachment.download_url} isPdf={kind === "pdf"} />
            ) : (
              <pre className="overflow-x-auto rounded border border-border bg-muted/40 p-3 text-xs">
                <code>{body.data}</code>
              </pre>
            )
          ) : (
            // Non-viewable filetype — keep behavior consistent: nothing to render
            // inline, but still expose download.
            <FallbackUnavailable downloadUrl={attachment.download_url} />
          )}
        </div>
      </div>
    </div>
  );
}

function FallbackUnavailable({ downloadUrl, isPdf = false }: { downloadUrl: string; isPdf?: boolean }) {
  return (
    <div className="rounded border border-border bg-muted/40 p-4 text-sm text-muted-foreground">
      <p>
        {isPdf
          ? "This PDF has no extractable text. OCR is not supported in this version."
          : "This file can't be previewed inline."}
      </p>
      {downloadUrl && (
        <a
          href={downloadUrl}
          target="_blank"
          rel="noreferrer"
          className="mt-2 inline-flex items-center gap-1 underline hover:text-foreground"
        >
          <Download className="size-3.5" /> Download
        </a>
      )}
    </div>
  );
}
