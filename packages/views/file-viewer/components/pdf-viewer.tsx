"use client";

import { FileText } from "lucide-react";

/**
 * PDF viewer component.
 *
 * Uses the browser's built-in PDF renderer via an <object> element
 * with an <iframe> fallback. No external dependencies needed.
 *
 * Accepts either:
 * - A URL string pointing to the PDF
 * - A base64 data URI (data:application/pdf;base64,...)
 */
export function PdfViewer({ url }: { url: string }) {
  return (
    <div className="flex h-full flex-col">
      <object
        data={url}
        type="application/pdf"
        className="flex-1 min-h-0 w-full"
      >
        {/* Fallback if <object> is unsupported */}
        <iframe
          src={url}
          title="PDF viewer"
          className="h-full w-full border-0"
        />
      </object>

      {/* Download fallback for environments where embed fails */}
      <div className="flex items-center justify-center gap-2 border-t px-4 py-2 bg-muted/20">
        <FileText className="size-4 text-muted-foreground" />
        <a
          href={url}
          target="_blank"
          rel="noopener noreferrer"
          className="text-xs text-primary hover:underline"
        >
          Open PDF in new tab
        </a>
      </div>
    </div>
  );
}
