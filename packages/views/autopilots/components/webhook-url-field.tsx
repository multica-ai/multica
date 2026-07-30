"use client";

import { useState, type ReactNode } from "react";
import { Check, Copy, Eye, EyeOff } from "lucide-react";
import { toast } from "sonner";
import { maskAutopilotWebhookUrl } from "@multica/core/autopilots";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { useT } from "../../i18n";

const SIZES = {
  // Trigger row on the detail page — compact, sits inline with the row actions.
  sm: {
    row: "items-center",
    value: "rounded bg-muted px-2 py-1 text-xs",
    button: "h-7 w-7",
    buttonVariant: "ghost",
    icon: "h-3.5 w-3.5",
  },
  // Post-create panel — larger standalone field.
  md: {
    row: "items-stretch",
    value: "rounded-md border bg-muted px-3 py-2 text-xs",
    button: "h-9 w-9",
    buttonVariant: "outline",
    icon: "size-4",
  },
} as const;

interface WebhookUrlFieldProps {
  url: string;
  size?: keyof typeof SIZES;
  /** Extra controls rendered after Copy (rotate, delete). */
  actions?: ReactNode;
}

/**
 * Webhook URL display that hides the token by default.
 *
 * The URL is a bearer credential: anyone who reads it off a screen share or a
 * screenshot can fire the autopilot. So the masked form is what renders — the
 * plaintext token is never in the DOM until the user asks for it, which a
 * CSS-only blur would not give us. Copy still works while hidden, so the common
 * case never needs a reveal at all.
 */
export function WebhookUrlField({ url, size = "sm", actions }: WebhookUrlFieldProps) {
  const { t } = useT("autopilots");
  // A reveal authorizes one specific URL, not the field. Rotating the token
  // swaps `url` under a mounted row, and the new credential has to be revealed
  // again on its own — a bare boolean would hand the old grant to the new
  // secret, exposing it exactly when the user rotated to contain a leak.
  // Deriving this during render (rather than resetting in an effect) means the
  // new token never reaches the DOM, not even for the pre-effect frame.
  const [revealedUrl, setRevealedUrl] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const revealed = revealedUrl === url;
  const s = SIZES[size];

  const handleCopy = async () => {
    if (!url) return;
    if (await copyText(url)) {
      setCopied(true);
      toast.success(t(($) => $.trigger_row.url_copied));
      setTimeout(() => setCopied(false), 1500);
    } else {
      toast.error(t(($) => $.trigger_row.url_copy_failed));
    }
  };

  const valueClassName = cn(
    "flex-1 min-w-0 truncate font-mono text-foreground",
    s.value,
  );

  return (
    <div className={cn("flex gap-1.5", s.row)}>
      {revealed ? (
        <code className={valueClassName}>{url}</code>
      ) : (
        // Click-to-reveal lives on the value itself. Once revealed it goes back
        // to plain text so the URL stays selectable — a toggle button there
        // would swallow the drag-select.
        <button
          type="button"
          onClick={() => setRevealedUrl(url)}
          className={cn(valueClassName, "text-left cursor-pointer transition-colors hover:bg-muted/70")}
          title={t(($) => $.trigger_row.show_url)}
          aria-label={t(($) => $.trigger_row.hidden_url_aria)}
        >
          {maskAutopilotWebhookUrl(url)}
        </button>
      )}
      <Button
        size="icon"
        variant={s.buttonVariant}
        className={cn("shrink-0", s.button)}
        onClick={() => setRevealedUrl(revealed ? null : url)}
        title={revealed ? t(($) => $.trigger_row.hide_url) : t(($) => $.trigger_row.show_url)}
        aria-label={revealed ? t(($) => $.trigger_row.hide_url) : t(($) => $.trigger_row.show_url)}
      >
        {revealed ? (
          <EyeOff className={cn(s.icon, "text-muted-foreground")} />
        ) : (
          <Eye className={cn(s.icon, "text-muted-foreground")} />
        )}
      </Button>
      <Button
        size="icon"
        variant={s.buttonVariant}
        className={cn("shrink-0", s.button)}
        onClick={handleCopy}
        title={t(($) => $.trigger_row.copy_url)}
        aria-label={t(($) => $.trigger_row.copy_url)}
      >
        {copied ? (
          <Check className={cn(s.icon, "text-emerald-500")} />
        ) : (
          <Copy className={cn(s.icon, "text-muted-foreground")} />
        )}
      </Button>
      {actions}
    </div>
  );
}
