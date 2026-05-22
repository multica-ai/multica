"use client";

import { useEffect, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Download, ExternalLink, FileText, X } from "lucide-react";
import { useT } from "../i18n";
import { withFragmentNavShim } from "./utils/iframe-fragment-nav";

const STORAGE_PREFIX = "multica:inline-html-artifact:";
const STORAGE_TTL_MS = 60 * 60 * 1000;
const DEFAULT_FILENAME = "html-artifact.html";

interface StoredHtmlArtifact {
  html: string;
  filename: string;
  createdAt: number;
}

function safeFilename(filename: string | undefined): string {
  const base = (filename || DEFAULT_FILENAME)
    .trim()
    .replace(/[\\/:*?"<>|]+/g, "-");
  if (!base) return DEFAULT_FILENAME;
  return /\.html?$/i.test(base) ? base : `${base}.html`;
}

function storageKey(id: string): string {
  return `${STORAGE_PREFIX}${id}`;
}

function makeId(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function cleanupExpiredStoredArtifacts(now = Date.now()): void {
  if (typeof window === "undefined") return;
  try {
    for (let i = window.localStorage.length - 1; i >= 0; i -= 1) {
      const key = window.localStorage.key(i);
      if (!key?.startsWith(STORAGE_PREFIX)) continue;
      const raw = window.localStorage.getItem(key);
      if (!raw) {
        window.localStorage.removeItem(key);
        continue;
      }
      const parsed = JSON.parse(raw) as Partial<StoredHtmlArtifact>;
      if (!parsed.createdAt || now - parsed.createdAt > STORAGE_TTL_MS) {
        window.localStorage.removeItem(key);
      }
    }
  } catch {
    // localStorage can throw in restricted browser contexts. The caller will
    // fall back to a no-op if storing also fails.
  }
}

export function storeHtmlArtifactPreview(
  html: string,
  filename = DEFAULT_FILENAME,
): string | null {
  if (typeof window === "undefined") return null;
  cleanupExpiredStoredArtifacts();
  const id = makeId();
  const payload: StoredHtmlArtifact = {
    html,
    filename: safeFilename(filename),
    createdAt: Date.now(),
  };
  try {
    window.localStorage.setItem(storageKey(id), JSON.stringify(payload));
    return id;
  } catch {
    return null;
  }
}

export function readStoredHtmlArtifactPreview(
  id: string | null | undefined,
): StoredHtmlArtifact | null {
  if (!id || typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(storageKey(id));
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<StoredHtmlArtifact>;
    if (
      typeof parsed.html !== "string" ||
      typeof parsed.filename !== "string" ||
      typeof parsed.createdAt !== "number"
    ) {
      window.localStorage.removeItem(storageKey(id));
      return null;
    }
    if (Date.now() - parsed.createdAt > STORAGE_TTL_MS) {
      window.localStorage.removeItem(storageKey(id));
      return null;
    }
    return {
      html: parsed.html,
      filename: safeFilename(parsed.filename),
      createdAt: parsed.createdAt,
    };
  } catch {
    return null;
  }
}

export function downloadHtmlArtifact(
  html: string,
  filename = DEFAULT_FILENAME,
): void {
  if (typeof document === "undefined" || typeof URL === "undefined") return;
  const blob = new Blob([html], { type: "text/html;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = safeFilename(filename);
  anchor.rel = "noopener";
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 0);
}

export function HtmlArtifactPreviewModal({
  html,
  filename = DEFAULT_FILENAME,
  open,
  canOpenInNewTab = true,
  onClose,
  onOpenInNewTab,
  onDownload,
}: {
  html: string;
  filename?: string;
  open: boolean;
  canOpenInNewTab?: boolean;
  onClose: () => void;
  onOpenInNewTab: () => void;
  onDownload: () => void;
}) {
  const { t } = useT("editor");

  useEffect(() => {
    if (!open) return;
    const handler = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [open, onClose]);

  if (!open || typeof document === "undefined") return null;

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4"
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-label={filename}
    >
      <div
        className="flex h-[min(94vh,calc(100vh-2rem))] w-full max-w-[calc(100vw-2rem)] flex-col overflow-hidden rounded-lg bg-background shadow-xl"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="flex items-center gap-2 border-b border-border bg-muted/30 px-4 py-2">
          <FileText className="size-4 shrink-0 text-muted-foreground" />
          <p className="truncate text-sm font-medium">{safeFilename(filename)}</p>
          <div className="ml-auto flex items-center gap-1">
            {canOpenInNewTab && (
              <button
                type="button"
                className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
                title={t(($) => $.attachment.open_in_new_tab)}
                aria-label={t(($) => $.attachment.open_in_new_tab)}
                onClick={onOpenInNewTab}
              >
                <ExternalLink className="size-4" />
              </button>
            )}
            <button
              type="button"
              className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              title={t(($) => $.image.download)}
              aria-label={t(($) => $.image.download)}
              onClick={onDownload}
            >
              <Download className="size-4" />
            </button>
            <button
              type="button"
              className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              title={t(($) => $.attachment.close)}
              aria-label={t(($) => $.attachment.close)}
              onClick={onClose}
            >
              <X className="size-4" />
            </button>
          </div>
        </div>
        <iframe
          srcDoc={withFragmentNavShim(html)}
          sandbox="allow-scripts"
          title={safeFilename(filename)}
          className="min-h-0 flex-1 border-0 bg-background"
        />
      </div>
    </div>,
    document.body,
  );
}

export function HtmlArtifactPreviewPage({
  artifactKey,
}: {
  artifactKey?: string | null;
}): ReactNode {
  const { t } = useT("editor");
  const [payload, setPayload] = useState<StoredHtmlArtifact | null | undefined>(
    undefined,
  );

  useEffect(() => {
    const next = readStoredHtmlArtifactPreview(artifactKey);
    setPayload(next);
    if (next) {
      document.title = next.filename;
    }
  }, [artifactKey]);

  if (payload === undefined) {
    return (
      <div className="flex h-svh w-full items-center justify-center bg-background text-sm text-muted-foreground">
        {t(($) => $.attachment.preview_loading)}
      </div>
    );
  }

  if (!payload) {
    return (
      <div className="flex h-svh w-full items-center justify-center bg-background px-4 text-center text-sm text-muted-foreground">
        {t(($) => $.attachment.preview_failed)}
      </div>
    );
  }

  return (
    <iframe
      srcDoc={withFragmentNavShim(payload.html)}
      sandbox="allow-scripts"
      title={payload.filename}
      className="h-svh w-full border-0 bg-background"
    />
  );
}
