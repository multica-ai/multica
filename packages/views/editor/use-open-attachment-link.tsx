"use client";

/**
 * Open a link that points at one of this deployment's attachments inside the
 * app — the surface's viewer, the standalone preview, or a download — instead
 * of handing the raw storage URL to the browser (MUL-9566).
 *
 * A plain link is how agents and pasted content most often reference a file:
 * `[demo.mp4](https://static.multica.ai/workspaces/<ws>/<id>.mp4)` written
 * inline, a bare CDN URL that linkify turned into an anchor, or the stable
 * `/api/attachments/<id>/download` URL. Opened as an ordinary link, a video
 * lands on the CDN host's native player in a fresh tab with no history, and a
 * signed CDN rejects the bare URL outright. Every link entry point (readonly
 * content, the editor, the hover card's Open button) goes through here first
 * and only falls back to `openLink` when the href is not an attachment.
 */

import { useCallback, type ReactNode } from "react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { matchAttachmentByURL } from "@multica/core/attachments/image-sequence";
import { useConfigStore } from "@multica/core/config";
import type { Attachment } from "@multica/core/types";
import {
  attachmentIdFromDownloadURL,
  attachmentIdFromLocalUploadURL,
  attachmentIdFromStorageURL,
} from "@multica/core/types/attachment-url";
import { useAppOrigin } from "../navigation";
import { useT } from "../i18n";
import { useAttachmentDownloadResolver } from "./attachment-download-context";
import { useAttachmentPreview } from "./attachment-preview-modal";
import { usePreviewSequence } from "./preview-sequence-context";
import { useDownloadAttachment } from "./use-download-attachment";

export interface AttachmentLinkContext {
  /** The app's public origin (`useAppOrigin`). */
  appOrigin: string | null;
  /** Origin the server persists attachment URLs under (`/api/config`). */
  attachmentPublicUrl: string;
  /** Storage CDN hostname (`/api/config`). */
  cdnDomain: string;
  /** Records of the surrounding surface, for legacy exact-URL references. */
  attachments?: ReadonlyArray<Attachment>;
}

/**
 * Id of the attachment `href` points at on THIS deployment; `undefined` for
 * anything else.
 *
 * The `/api/attachments/<id>/download` and `/uploads/workspaces/...` path
 * shapes alone do not establish which deployment owns a link: another Multica
 * instance uses the same shapes with unrelated ids. They count only when
 * site-relative or on one of this deployment's own origins. A storage URL is
 * recognized by the configured CDN hostname. A record of the surrounding
 * surface matches on exact URL, which covers legacy bodies that persisted a
 * storage URL on another host (direct S3).
 */
export function resolveAttachmentLinkId(
  href: string,
  ctx: AttachmentLinkContext,
): string | undefined {
  if (!href) return undefined;
  const pathId = attachmentIdFromDownloadURL(href) ?? attachmentIdFromLocalUploadURL(href);
  if (pathId && isOwnOrigin(href, ctx)) return pathId;
  const storageId = attachmentIdFromStorageURL(href, ctx.cdnDomain);
  if (storageId) return storageId;
  const record = matchAttachmentByURL(href, ctx.attachments);
  return record?.id;
}

function isOwnOrigin(href: string, ctx: AttachmentLinkContext): boolean {
  if (href.startsWith("/") && !href.startsWith("//")) return true;
  // PublicURL can differ from both the web app origin and the browser's
  // same-origin API proxy, so all three count as this deployment.
  try {
    const target = new URL(href);
    if (target.username || target.password) return false;
    const ownOrigins = [ctx.appOrigin, api.getBaseUrl?.(), ctx.attachmentPublicUrl];
    return ownOrigins.some((base) => {
      if (!base) return false;
      try {
        return new URL(base).origin === target.origin;
      } catch {
        return false;
      }
    });
  } catch {
    return false;
  }
}

export interface OpenAttachmentLinkHandle {
  /** The attachment id `href` points at, or `undefined` when it is a regular link. */
  resolve: (href: string) => string | undefined;
  /**
   * Open the attachment `href` points at inside the app. Returns false — and
   * does nothing — when `href` is not an attachment of this deployment, so the
   * caller falls back to its regular link handling.
   */
  open: (href: string) => boolean;
  /** Render this for the standalone preview. */
  modal: ReactNode;
}

/**
 * `attachments` are records a caller holds outside any surrounding
 * `AttachmentDownloadProvider` — a component that renders the provider itself
 * runs its own hooks above it. They match alongside the provider's records.
 */
export function useOpenAttachmentLink(
  attachments?: ReadonlyArray<Attachment>,
): OpenAttachmentLinkHandle {
  const { t } = useT("editor");
  const appOrigin = useAppOrigin();
  const attachmentPublicUrl = useConfigStore((s) => s.attachmentPublicUrl);
  const cdnDomain = useConfigStore((s) => s.cdnDomain);
  const { resolveAttachment } = useAttachmentDownloadResolver();
  const { openAt } = usePreviewSequence();
  const preview = useAttachmentPreview();
  const { tryOpen } = preview;
  const download = useDownloadAttachment("markdown-link");

  const resolve = useCallback(
    (href: string) => {
      const surfaceRecord = resolveAttachment(href);
      return resolveAttachmentLinkId(href, {
        appOrigin,
        attachmentPublicUrl,
        cdnDomain,
        attachments: surfaceRecord ? [surfaceRecord, ...(attachments ?? [])] : attachments,
      });
    },
    [resolveAttachment, attachments, appOrigin, attachmentPublicUrl, cdnDomain],
  );

  const openById = useCallback(
    async (attachmentId: string) => {
      // The link names this file: if it fails to load, stay on it and say so
      // rather than showing the reader a different one.
      if (openAt(attachmentId, { stayOnError: true })) return;
      let record: Attachment;
      try {
        // Fresh metadata: the record carries the content type the viewer
        // dispatches on and a newly signed URL to play from. Resolved by the
        // attachment, not the current workspace, as a pasted link may come
        // from another one.
        record = await api.getAttachmentForDownload(attachmentId);
      } catch {
        toast.error(t(($) => $.attachment.download_failed));
        return;
      }
      if (record.id && tryOpen({ kind: "full", attachment: record })) return;
      await download(attachmentId);
    },
    [openAt, tryOpen, download, t],
  );

  const open = useCallback(
    (href: string) => {
      const attachmentId = resolve(href);
      if (!attachmentId) return false;
      void openById(attachmentId);
      return true;
    },
    [resolve, openById],
  );

  return { resolve, open, modal: preview.modal };
}
