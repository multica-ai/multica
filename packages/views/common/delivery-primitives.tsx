"use client";

import { useState } from "react";
import { Copy, Check } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { toast } from "sonner";

// Shared primitives for webhook delivery detail views (inbound autopilot
// deliveries and outbound subscription deliveries). Kept i18n-namespace-
// agnostic: callers pass already-translated label strings so the same
// component serves both the "autopilots" and "settings" translation trees.

// formatDeliveryDate renders a compact local timestamp, or an em dash when
// empty. Shared so both delivery views format timestamps identically.
export function formatDeliveryDate(value: string): string {
  if (!value) return "—";
  return new Date(value).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

// MetaRow is a label/value pair in a delivery detail meta grid.
export function MetaRow({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="flex flex-col">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={cn("truncate text-foreground", mono && "font-mono")} title={value}>
        {value}
      </dd>
    </div>
  );
}

// CodeBlockLabels are the translated strings the CodeBlock needs. Callers
// supply them from whichever i18n namespace they live in.
export interface CodeBlockLabels {
  // Button text in its default state.
  copy: string;
  // Button text right after a successful copy.
  copied: string;
  // Toast on successful copy.
  copiedToast: string;
  // Toast when the clipboard write fails.
  copyFailedToast: string;
  // Marker appended when the displayed body is truncated.
  truncated: string;
}

// CodeBlock renders a labeled, copyable, scrollable code box. The DOM display
// is truncated for very large bodies (the Copy button still yields the full
// string) to keep the dialog responsive.
export function CodeBlock({
  label,
  value,
  labels,
}: {
  label: string;
  value: string;
  labels: CodeBlockLabels;
}) {
  const [copied, setCopied] = useState(false);
  const TRUNCATE_AT = 4096;
  const isTruncated = value.length > TRUNCATE_AT;
  const display = isTruncated ? value.slice(0, TRUNCATE_AT) : value;

  const handleCopy = async () => {
    if (await copyText(value)) {
      setCopied(true);
      toast.success(labels.copiedToast);
      setTimeout(() => setCopied(false), 1500);
    } else {
      toast.error(labels.copyFailedToast);
    }
  };

  return (
    // min-w-0 lets this card shrink below the <pre>'s intrinsic min-content
    // width — without it a minified single-line body pushes the dialog past
    // the viewport edge.
    <div className="min-w-0 rounded-md border bg-background">
      <div className="flex items-center justify-between border-b px-3 py-1.5 text-[11px]">
        <span className="font-medium text-muted-foreground">{label}</span>
        <button
          type="button"
          onClick={handleCopy}
          className="flex items-center gap-1 rounded px-2 py-0.5 hover:bg-accent transition-colors"
        >
          {copied ? (
            <Check className="h-3 w-3 text-emerald-500" />
          ) : (
            <Copy className="h-3 w-3" />
          )}
          {copied ? labels.copied : labels.copy}
        </button>
      </div>
      {/* whitespace-pre-wrap keeps indentation but wraps long lines; break-all
          is the only thing that breaks mid-token (minified JSON has no
          whitespace to break at). */}
      <pre className="max-h-48 overflow-auto bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed whitespace-pre-wrap break-all">
        {display}
        {isTruncated && (
          <span className="block pt-2 text-muted-foreground/70">{labels.truncated}</span>
        )}
      </pre>
    </div>
  );
}
