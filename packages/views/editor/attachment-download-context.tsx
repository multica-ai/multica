"use client";

import { createContext, useContext, useMemo, type ReactNode } from "react";
import type { Attachment } from "@multica/core/types";
import { openExternal } from "../platform";
import { useDownloadAttachment } from "./use-download-attachment";

interface ResolvedDownload {
  // Returns the attachment id for a URL referenced in the markdown, or
  // `undefined` if it's an external link we don't manage.
  resolveAttachmentId: (url: string) => string | undefined;
  // Returns the full Attachment record (content_type, filename, download_url,
  // ...) for a URL referenced in the markdown. NodeView preview triggers use
  // this to decide whether the type is previewable and to feed the modal.
  resolveAttachment: (url: string) => Attachment | undefined;
  // Called by NodeView click handlers. Re-signs through `getAttachment` when
  // the URL maps to a known attachment; falls back to `openExternal` for
  // external URLs so Electron still routes through the IPC bridge instead of
  // letting `window.open` hit the `setWindowOpenHandler` deny path.
  openByUrl: (url: string) => void;
}

const AttachmentDownloadContext = createContext<ResolvedDownload | null>(null);

interface ProviderProps {
  attachments?: Attachment[];
  children: ReactNode;
}

/**
 * Provides a click-time download handler to Tiptap NodeViews mounted inside
 * `ContentEditor`. Without a provider the consumer falls back to opening the
 * raw URL via `openExternal` — same behaviour as before this hook existed.
 */
export function AttachmentDownloadProvider({ attachments, children }: ProviderProps) {
  const download = useDownloadAttachment();
  const value = useMemo<ResolvedDownload>(
    () => ({
      resolveAttachmentId: (url) => {
        if (!url || !attachments?.length) return undefined;
        return attachments.find((a) => a.url === url)?.id;
      },
      resolveAttachment: (url) => {
        if (!url || !attachments?.length) return undefined;
        return attachments.find((a) => a.url === url);
      },
      openByUrl: (url) => {
        const att = url && attachments?.length
          ? attachments.find((a) => a.url === url)
          : undefined;
        if (att) {
          download(att.id);
          return;
        }
        if (url) openExternal(url);
      },
    }),
    [attachments, download],
  );
  return (
    <AttachmentDownloadContext.Provider value={value}>
      {children}
    </AttachmentDownloadContext.Provider>
  );
}

/**
 * Returns the click-time download handler installed by a surrounding
 * `AttachmentDownloadProvider`, or a fallback that just opens the raw URL
 * externally. Used by file-card and image NodeViews so they can stay
 * usable in editor surfaces that haven't been wired up yet.
 */
export function useAttachmentDownloadResolver(): ResolvedDownload {
  const ctx = useContext(AttachmentDownloadContext);
  // Hooks-must-be-unconditional: always create the fallback object, but
  // memoization is unnecessary here because each NodeView render also
  // re-runs the click handler closure.
  if (ctx) return ctx;
  return {
    resolveAttachmentId: () => undefined,
    resolveAttachment: () => undefined,
    openByUrl: (url) => {
      if (url) openExternal(url);
    },
  };
}
