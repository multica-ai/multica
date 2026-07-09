"use client";

import { useState, useMemo } from "react";
import { Webhook, ChevronDown, ChevronRight, Copy, Check } from "lucide-react";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { useT } from "../../i18n";

interface WebhookPayloadPreviewProps {
  payload: unknown;
  /** Default open vs collapsed. The dialog has limited vertical space, so
   *  we collapse by default and let the user expand. */
  defaultOpen?: boolean;
}

/**
 * Renders a webhook trigger payload (the WebhookEnvelope shape produced
 * server-side by normalizeWebhookPayload) inline with the autopilot run
 * detail. Falls back gracefully when the payload isn't an envelope —
 * showing whatever JSON is there with a generic header.
 *
 * This is intentionally read-only and decoupled from any specific dialog
 * — it gets dropped into AgentTranscriptDialog's headerSlot.
 */
export function WebhookPayloadPreview({
  payload,
  defaultOpen = false,
}: WebhookPayloadPreviewProps) {
  const { t } = useT("autopilots");
  const [open, setOpen] = useState(defaultOpen);
  const [copied, setCopied] = useState(false);

  const { event, receivedAt, contentType, fullJSON, displayJSON, isTruncated } = useMemo(() => {
    let event: string | null = null;
    let eventPayload: unknown = null;
    let receivedAt: string | null = null;
    let contentType: string | null = null;
    if (payload && typeof payload === "object" && !Array.isArray(payload)) {
      const obj = payload as Record<string, unknown>;
      if (typeof obj.event === "string") event = obj.event;
      if ("eventPayload" in obj) eventPayload = obj.eventPayload;
      const req = obj.request;
      if (req && typeof req === "object") {
        const r = req as Record<string, unknown>;
        if (typeof r.receivedAt === "string") receivedAt = r.receivedAt;
        if (typeof r.contentType === "string") contentType = r.contentType;
      }
    }
    // If the payload didn't match the envelope shape (caller wrote
    // directly to trigger_payload, malformed history row, etc.), show
    // the whole thing as the eventPayload so nothing is hidden.
    if (eventPayload === null && payload !== null && payload !== undefined) {
      eventPayload = payload;
    }
    const fullJSON = JSON.stringify(eventPayload, null, 2);
    // Truncate the in-DOM string so the dialog stays responsive even when a
    // provider sent a 256 KiB envelope. The Copy button still yields the
    // full string, so the user never loses the data. 4 KiB is large enough
    // to show the envelope header + first object-level fields of a typical
    // webhook payload.
    const TRUNCATE_AT = 4096;
    const isTruncated = fullJSON.length > TRUNCATE_AT;
    const displayJSON = isTruncated ? fullJSON.slice(0, TRUNCATE_AT) : fullJSON;
    return { event, receivedAt, contentType, fullJSON, displayJSON, isTruncated };
  }, [payload]);

  const handleCopy = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (await copyText(fullJSON)) {
      setCopied(true);
      toast.success(t(($) => $.webhook_payload.copied));
      setTimeout(() => setCopied(false), 1500);
    } else {
      toast.error(t(($) => $.webhook_payload.copy_failed));
    }
  };

  return (
    <div className="rounded-md border bg-background">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 px-3 py-2 text-left text-xs hover:bg-accent/30 transition-colors"
      >
        <Webhook className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <span className="font-medium">
          {t(($) => $.webhook_payload.label)}
        </span>
        <code className="truncate font-mono text-muted-foreground">
          {event ?? t(($) => $.webhook_payload.unknown_event)}
        </code>
        {receivedAt && (
          <span className="ml-auto shrink-0 text-muted-foreground/70">
            {receivedAt}
          </span>
        )}
        {open ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
      </button>
      {open && (
        <div className="border-t">
          <div className="flex items-center justify-between px-3 py-1.5 text-[11px] text-muted-foreground">
            <span>
              {contentType
                ? t(($) => $.webhook_payload.content_type, { type: contentType })
                : t(($) => $.webhook_payload.payload)}
            </span>
            <button
              type="button"
              onClick={handleCopy}
              className={cn(
                "flex items-center gap-1 rounded px-2 py-0.5 hover:bg-accent transition-colors",
              )}
            >
              {copied ? (
                <Check className="h-3 w-3 text-emerald-500" />
              ) : (
                <Copy className="h-3 w-3" />
              )}
              {copied
                ? t(($) => $.webhook_payload.copied_short)
                : t(($) => $.webhook_payload.copy)}
            </button>
          </div>
          <pre className="max-h-64 overflow-auto bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed">
            {displayJSON}
            {isTruncated && (
              <span className="block pt-2 text-muted-foreground/70">
                {t(($) => $.webhook_payload.truncated_marker)}
              </span>
            )}
          </pre>
        </div>
      )}
    </div>
  );
}
