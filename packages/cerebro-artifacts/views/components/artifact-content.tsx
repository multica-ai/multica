"use client";

import type { Artifact } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { ArtifactBody } from "./artifact-body";
import { sanitizeHtml } from "../utils/sanitize-html";

function normalizeTitle(s: string): string {
  return s.replace(/\s+/g, " ").trim().toLowerCase();
}

// Agents sometimes save HTML artifacts as a full <!DOCTYPE html> document
// with their own CSS. Keep that isolated from the app shell.
function isFullHtmlDocument(body: string): boolean {
  const head = body.slice(0, 256).toLowerCase();
  return head.includes("<!doctype html") || /<html[\s>]/.test(head);
}

// Authors often repeat the artifact title as a leading H1 inside the body.
// Strip it so the surrounding document/sheet heading is not duplicated.
export function stripLeadingTitleHeading(
  body: string,
  title: string,
  format: string,
): string {
  if (!body || !title) return body;
  const want = normalizeTitle(title);
  if (format === "html") {
    const m = body.match(/^\s*<h1[^>]*>([\s\S]*?)<\/h1>\s*/i);
    if (m && normalizeTitle((m[1] ?? "").replace(/<[^>]+>/g, "")) === want) {
      return body.slice(m[0].length);
    }
    return body;
  }

  const m = body.match(/^\s*#\s+([^\n]+)\n+/);
  if (m && normalizeTitle(m[1] ?? "") === want) {
    return body.slice(m[0].length);
  }
  return body;
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

export function ArtifactContent({
  artifact,
  className,
  frameClassName,
}: {
  artifact: Artifact;
  className?: string;
  frameClassName?: string;
}) {
  const renderedBody = stripLeadingTitleHeading(
    artifact.body ?? "",
    artifact.title,
    artifact.format,
  );

  if (artifact.format === "pdf" && artifact.file_url) {
    return (
      <div className={cn("flex flex-col gap-2", className)}>
        <div className="flex items-center justify-between rounded border border-border bg-card/40 px-3 py-2 text-xs text-muted-foreground">
          <span>
            PDF
            {artifact.file_size_bytes
              ? ` - ${formatBytes(artifact.file_size_bytes)}`
              : ""}
          </span>
        </div>
        <iframe
          src={artifact.file_url}
          title={artifact.title}
          className={cn(
            "h-[80vh] w-full rounded border border-border bg-white",
            frameClassName,
          )}
        />
      </div>
    );
  }

  if (artifact.format === "html") {
    if (isFullHtmlDocument(renderedBody)) {
      return (
        <iframe
          srcDoc={renderedBody}
          sandbox=""
          title={artifact.title}
          className={cn(
            "h-[80vh] w-full rounded border border-border bg-white",
            frameClassName,
          )}
        />
      );
    }

    return (
      <div
        className={cn("prose prose-sm max-w-none dark:prose-invert", className)}
        dangerouslySetInnerHTML={{ __html: sanitizeHtml(renderedBody) }}
      />
    );
  }

  if (renderedBody) {
    return (
      <div
        className={cn("prose prose-sm max-w-none dark:prose-invert", className)}
      >
        <ArtifactBody body={renderedBody} />
      </div>
    );
  }

  return <p className="text-sm text-muted-foreground">No content.</p>;
}
