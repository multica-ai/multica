import { useEffect, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import Image from "next/image";
import { api, ApiError, type SharedIssueDeliverable } from "@multica/core/api";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import { Download, File, FileImage, FileText } from "@multica/ui/icons";
import { formatBytes } from "@multica/views/common/format-bytes";
import { useT } from "@multica/views/i18n";

const MAX_PREVIEW_BYTES = 10 * 1024 * 1024;
const RASTER_TYPES = new Set(["image/png", "image/jpeg", "image/gif", "image/webp", "image/avif", "image/bmp"]);

function FileImagePreview({ blob, name }: { blob: Blob; name: string }) {
  const [url, setUrl] = useState<string>();
  useEffect(() => {
    const objectUrl = URL.createObjectURL(blob);
    setUrl(objectUrl);
    return () => URL.revokeObjectURL(objectUrl);
  }, [blob]);
  return url && <Image src={url} alt={name} unoptimized width={1024} height={768} className="max-h-[65vh] h-auto w-auto max-w-full" />;
}

function SharedDeliverable({ file, userId, token }: {
  file: SharedIssueDeliverable;
  userId: string;
  token: string;
}) {
  const [open, setOpen] = useState(false);
  const mimeType = file.content_type.split(";", 1)[0]?.trim().toLowerCase() ?? "";
  const raster = RASTER_TYPES.has(mimeType);
  const text = mimeType.startsWith("text/") || mimeType === "application/json";
  const Icon = raster ? FileImage : text ? FileText : File;
  const size = formatBytes(file.size);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger
        render={<Button type="button" variant="ghost" className="h-auto w-full min-w-0 justify-start gap-2 px-2 py-1.5" />}
      >
        <Icon className="shrink-0 text-muted-foreground" aria-hidden />
        <span className="min-w-0 flex-1 truncate text-left text-caption" title={file.filename}>{file.filename}</span>
        <span className="shrink-0 text-micro text-muted-foreground">{size}</span>
      </DialogTrigger>
      <DialogContent className="sm:max-w-2xl">
        {open && (
          <DeliverableContent
            file={file} userId={userId} token={token} size={size}
            previewType={raster ? mimeType : text ? "text/plain" : null}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function DeliverableContent({ file, userId, token, size, previewType }: {
  file: SharedIssueDeliverable;
  userId: string;
  token: string;
  size: string;
  previewType: string | null;
}) {
  const { t } = useT("issues");
  const { t: editorT } = useT("editor");
  const previewable = file.size <= MAX_PREVIEW_BYTES && previewType !== null;
  const preview = useQuery({
    queryKey: ["shared-issue", userId, token, "deliverable", file.id],
    queryFn: async ({ signal }) => {
      const blob = await api.getSharedIssueDeliverable(token, file.id, signal);
      if (blob.size > MAX_PREVIEW_BYTES) return null;
      if (previewType && RASTER_TYPES.has(previewType)) return { image: blob.slice(0, blob.size, previewType) };
      return { text: await blob.text() };
    },
    enabled: previewable,
    gcTime: 0,
    retry: false,
  });
  const download = useMutation({
    mutationFn: () => api.getSharedIssueDeliverable(token, file.id),
    gcTime: 0,
    onSuccess: (blob) => {
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = file.filename;
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 0);
    },
  });
  const unavailable = [preview.error, download.error].some(
    (error) => error instanceof ApiError && [403, 404].includes(error.status),
  );
  const previewTooLarge = file.size > MAX_PREVIEW_BYTES || preview.data === null;

  return (
    <>
      <DialogHeader>
        <DialogTitle className="break-all pr-6">{file.filename}</DialogTitle>
        <DialogDescription>{size}</DialogDescription>
      </DialogHeader>
      {unavailable ? (
        <p role="alert">{t(($) => $.sharing.unavailable)}</p>
      ) : (
        <>
          {previewable && preview.isPending && <p role="status">{editorT(($) => $.attachment.preview_loading)}</p>}
          {preview.isError && (
            <div className="space-y-2">
              <p role="alert">{editorT(($) => $.attachment.preview_failed)}</p>
              <Button variant="outline" size="sm" onClick={() => void preview.refetch()}>{t(($) => $.sharing.retry)}</Button>
            </div>
          )}
          {previewTooLarge && <p>{editorT(($) => $.attachment.preview_too_large)}</p>}
          {!previewTooLarge && !previewable && <p>{editorT(($) => $.attachment.preview_unsupported)}</p>}
          {preview.data?.image && <FileImagePreview blob={preview.data.image} name={file.filename} />}
          {preview.data?.text !== undefined && (
            <pre className="whitespace-pre-wrap break-words rounded-md bg-muted p-3 text-caption">{preview.data.text}</pre>
          )}
          {download.isError && <p role="alert">{editorT(($) => $.attachment.download_failed)}</p>}
          <DialogFooter>
            <Button disabled={download.isPending} aria-busy={download.isPending} onClick={() => download.mutate()}>
              <Download aria-hidden />
              {editorT(($) => $.image.download)}
            </Button>
          </DialogFooter>
        </>
      )}
    </>
  );
}

export function SharedIssueDeliverables({ files, userId, token }: {
  files: SharedIssueDeliverable[];
  userId: string;
  token: string;
}) {
  const { t } = useT("issues");
  return (
    <section className="space-y-3 border-t pt-5" aria-labelledby="shared-issue-deliverables">
      <h2 id="shared-issue-deliverables" className="text-label font-semibold">{t(($) => $.deliverables.section_title)}</h2>
      {files.length ? (
        <ul className="space-y-1">
          {files.map((file) => (
            <li key={file.id}><SharedDeliverable file={file} userId={userId} token={token} /></li>
          ))}
        </ul>
      ) : (
        <p className="text-caption text-muted-foreground">{t(($) => $.sharing.no_deliverables)}</p>
      )}
    </section>
  );
}
