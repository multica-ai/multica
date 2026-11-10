"use client";

import type { ComponentProps, MouseEvent } from "react";
import { useRouter } from "next/navigation";
import { localizeHref } from "@/lib/landing-locale-path";
import { useOptionalLocale } from "../../i18n/context";
import { markPendingDownload } from "../../utils/download-target";
import type { DownloadKey } from "../../utils/parse-release-assets";

function isModifiedClick(event: MouseEvent) {
  return event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey;
}

/**
 * A link to a desktop installer. On the download page the browser fetches
 * the file natively and `onStart` switches the page to its post-download
 * state. With `handoff` (the homepage), a plain click goes to the download
 * page instead, which starts the file and shows what to do next. Modified
 * clicks always keep the browser's own behaviour.
 */
export function DownloadLink({
  downloadKey,
  href,
  handoff,
  onStart,
  onClick,
  ...props
}: Omit<ComponentProps<"a">, "href"> & {
  downloadKey: DownloadKey;
  href: string;
  handoff?: boolean;
  onStart?: (key: DownloadKey) => void;
}) {
  const router = useRouter();
  const locale = useOptionalLocale() ?? "en";
  return (
    <a
      href={href}
      onClick={(event) => {
        onClick?.(event);
        if (event.defaultPrevented || isModifiedClick(event)) return;
        if (handoff) {
          event.preventDefault();
          markPendingDownload(downloadKey);
          router.push(localizeHref(locale, `/download?started=${downloadKey}`));
          return;
        }
        // After the click's default action, so the link is still in place when the download begins.
        if (onStart) setTimeout(() => onStart(downloadKey), 0);
      }}
      {...props}
    />
  );
}
