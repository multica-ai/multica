"use client";

import { useEffect, useState } from "react";
import { Download } from "lucide-react";
import { api } from "@multica/core/api";
import { cn } from "@multica/ui/lib/utils";

// The PDF file is served by the backend (same origin in production, a separate
// port in local dev) with `frame-ancestors 'none'`, so pointing an <iframe>
// straight at the file URL is blocked by CSP. Instead we fetch the bytes
// ourselves (the /uploads route is public + CORS-enabled, so credentials are
// harmless and cover signed-URL backends) and frame a client-side blob URL,
// which carries no server CSP headers. The browser's native PDF viewer renders
// the blob — scroll, zoom, search, and print all work without a JS PDF library.
function toAbsoluteUrl(url: string): string {
  if (/^(https?:|blob:|data:)/i.test(url)) return url;
  const base = api.getBaseUrl().replace(/\/$/, "");
  if (!base) return url;
  return `${base}${url.startsWith("/") ? "" : "/"}${url}`;
}

export function PdfViewer({
  fileUrl,
  title,
  className,
  downloadUrl,
}: {
  fileUrl: string;
  title: string;
  className?: string;
  // Optional separate URL for the download fallback. Defaults to fileUrl.
  downloadUrl?: string;
}) {
  const [blobUrl, setBlobUrl] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    let created: string | null = null;
    const controller = new AbortController();

    setBlobUrl(null);
    setFailed(false);

    void (async () => {
      try {
        const res = await fetch(toAbsoluteUrl(fileUrl), {
          credentials: "include",
          signal: controller.signal,
        });
        if (!res.ok) throw new Error(`pdf fetch failed: ${res.status}`);
        const raw = await res.blob();
        // Force the PDF MIME type so the native viewer kicks in even when the
        // storage backend labelled the object application/octet-stream.
        const pdf =
          raw.type === "application/pdf"
            ? raw
            : new Blob([raw], { type: "application/pdf" });
        created = URL.createObjectURL(pdf);
        if (cancelled) {
          URL.revokeObjectURL(created);
          return;
        }
        setBlobUrl(created);
      } catch {
        if (!controller.signal.aborted) setFailed(true);
      }
    })();

    return () => {
      cancelled = true;
      controller.abort();
      if (created) URL.revokeObjectURL(created);
    };
  }, [fileUrl]);

  if (failed) {
    return <PdfUnavailable downloadUrl={downloadUrl ?? fileUrl} />;
  }

  if (!blobUrl) {
    return (
      <div
        className={cn(
          "flex h-[80vh] w-full items-center justify-center rounded border border-border bg-card/40 text-sm text-muted-foreground",
          className,
        )}
      >
        Loading…
      </div>
    );
  }

  return (
    <iframe
      src={blobUrl}
      title={title}
      className={cn(
        "h-[80vh] w-full rounded border border-border bg-white",
        className,
      )}
    />
  );
}

function PdfUnavailable({ downloadUrl }: { downloadUrl: string }) {
  return (
    <div className="rounded border border-border bg-muted/40 p-4 text-sm text-muted-foreground">
      <p>This PDF could not be displayed inline.</p>
      {downloadUrl && (
        <a
          href={toAbsoluteUrl(downloadUrl)}
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
