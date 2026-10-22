"use client";

import { Info } from "lucide-react";
import { useT } from "../../i18n";

/** Why a message with files starts after the run instead of joining it. */
export function SteerAttachmentNotice() {
  const { t } = useT("issues");
  return (
    <p role="status" className="mb-2 flex items-start gap-1.5 rounded-md bg-muted px-2 py-1.5 text-caption text-muted-foreground">
      <Info aria-hidden className="mt-px size-3.5 shrink-0" />
      <span>{t(($) => $.comment.steer_attachment_notice)}</span>
    </p>
  );
}
