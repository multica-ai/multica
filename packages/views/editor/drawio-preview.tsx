"use client";

/**
 * draw.io diagram preview (#6321), shared by both entry points:
 *   - DrawioBlock: a fenced ```drawio block in RichContent, inside the shared
 *     DynamicBlock frame (title bar, Preview | Source, copy, error panel);
 *   - DrawioStage: the stage of a .drawio / .dio attachment in the viewer.
 *
 * Both go through useDrawioViewerUrl and useDrawioEmbed, so the operator
 * switch, the same-origin refusal, the XML and size checks and the
 * reachability probe apply identically. When the preview is off the callers
 * show the XML source instead — never an empty frame.
 *
 * The viewer runs in a cross-origin iframe. It needs `allow-scripts
 * allow-same-origin` to run at all, which keeps it isolated from Multica only
 * because its origin differs from the app's (usableDrawioViewerUrl refuses a
 * same-origin viewer). No referrer is sent, and the diagram travels in the URL
 * fragment, which never leaves the browser.
 */

import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useConfigStore } from "@multica/core/config";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import {
  DYNAMIC_BLOCK_CHROME_PX,
  DynamicBlock,
  DynamicBlockSkeleton,
} from "./dynamic-block";
import {
  forgetDrawioViewerProbe,
  prepareDrawioEmbed,
  usableDrawioViewerUrl,
  type DrawioEmbedError,
} from "./utils/drawio";

/** Inline body height: tall enough for a typical diagram, below the collapse point. */
export const DRAWIO_BLOCK_BODY_PX = 400;
export const DRAWIO_BLOCK_RESERVED_PX = DYNAMIC_BLOCK_CHROME_PX + DRAWIO_BLOCK_BODY_PX;

/** The viewer base URL to embed, or null when the preview is off here. */
export function useDrawioViewerUrl(): string | null {
  const configured = useConfigStore((state) => state.drawioViewerUrl);
  return useMemo(
    () =>
      usableDrawioViewerUrl(
        configured,
        typeof window === "undefined" ? null : window.location.origin,
      ),
    [configured],
  );
}

type EmbedState =
  | { status: "loading" }
  | { status: "ready"; src: string }
  | { status: "error"; error: DrawioEmbedError };

/**
 * Prepares the iframe src for `xml`. `reloadKey` re-runs the checks; the
 * caller drops the cached probe first so a reload really asks the network.
 */
export function useDrawioEmbed(
  xml: string,
  viewerUrl: string,
  reloadKey: number,
): EmbedState {
  const [state, setState] = useState<EmbedState>({ status: "loading" });
  useEffect(() => {
    let cancelled = false;
    setState({ status: "loading" });
    prepareDrawioEmbed(xml, viewerUrl).then(
      (result) => {
        if (cancelled) return;
        setState(
          result.ok
            ? { status: "ready", src: result.src }
            : { status: "error", error: result.error },
        );
      },
      () => {
        // CompressionStream missing or the payload failed to encode: the
        // diagram cannot be shown, which is what "invalid" tells the reader.
        if (!cancelled) setState({ status: "error", error: "invalid" });
      },
    );
    return () => {
      cancelled = true;
    };
  }, [xml, viewerUrl, reloadKey]);
  return state;
}

export function useDrawioErrorMessage(): (error: DrawioEmbedError, viewerUrl: string) => string {
  const { t } = useT("editor");
  return (error, viewerUrl) => {
    switch (error) {
      case "invalid":
        return t(($) => $.drawio.invalid);
      case "too_large":
        return t(($) => $.drawio.too_large);
      case "unreachable":
        return t(($) => $.drawio.unreachable, { origin: new URL(viewerUrl).origin });
    }
  };
}

/** The viewer iframe, with a skeleton until the viewer page has loaded. */
export function DrawioFrame({
  src,
  title,
  className,
}: {
  src: string;
  title: string;
  className?: string;
}) {
  const [loaded, setLoaded] = useState(false);
  useEffect(() => setLoaded(false), [src]);
  return (
    <div className={cn("relative h-full w-full", className)}>
      <iframe
        src={src}
        title={title}
        sandbox="allow-scripts allow-same-origin"
        referrerPolicy="no-referrer"
        loading="lazy"
        className={cn("block h-full w-full border-0 bg-white", !loaded && "invisible")}
        onLoad={() => setLoaded(true)}
      />
      {!loaded && (
        <div className="absolute inset-0">
          <DynamicBlockSkeleton />
        </div>
      )}
    </div>
  );
}

/**
 * A fenced ```drawio block. Only mounted while the preview is on; RichFenceBlock
 * renders the XML as plain code otherwise.
 */
export function DrawioBlock({
  xml,
  title,
  viewerUrl,
}: {
  xml: string;
  title?: string | null;
  viewerUrl: string;
}) {
  const { t } = useT("editor");
  const [reloadKey, setReloadKey] = useState(0);
  const embed = useDrawioEmbed(xml, viewerUrl, reloadKey);
  const errorMessage = useDrawioErrorMessage();

  const reload = () => {
    forgetDrawioViewerProbe(viewerUrl);
    setReloadKey((key) => key + 1);
  };

  return (
    <DynamicBlock
      kind="drawio"
      title={title}
      source={xml}
      error={
        embed.status === "error"
          ? { message: errorMessage(embed.error, viewerUrl) }
          : null
      }
      onReload={reload}
      bodyHeight={DRAWIO_BLOCK_BODY_PX}
      preview={() => (
        <div style={{ height: DRAWIO_BLOCK_BODY_PX }}>
          {embed.status === "ready" ? (
            <DrawioFrame
              key={reloadKey}
              src={embed.src}
              title={title?.trim() || t(($) => $.drawio.title)}
            />
          ) : (
            <DynamicBlockSkeleton />
          )}
        </div>
      )}
    />
  );
}

/**
 * The stage of a .drawio / .dio attachment in the viewer. Only mounted while
 * the preview is on; the viewer shows the XML otherwise. Errors render
 * through `renderError` so the viewer keeps its own download fallback.
 */
export function DrawioStage({
  xml,
  title,
  viewerUrl,
  renderError,
}: {
  xml: string;
  title: string;
  viewerUrl: string;
  renderError: (message: string) => ReactNode;
}) {
  const embed = useDrawioEmbed(xml, viewerUrl, 0);
  const errorMessage = useDrawioErrorMessage();
  if (embed.status === "error") {
    return <>{renderError(errorMessage(embed.error, viewerUrl))}</>;
  }
  return (
    <div className="h-full pb-4">
      <div className="h-full overflow-hidden rounded-lg bg-background">
        {embed.status === "ready" ? (
          <DrawioFrame src={embed.src} title={title} />
        ) : (
          <DynamicBlockSkeleton />
        )}
      </div>
    </div>
  );
}

/**
 * The live preview above an editable ```drawio code block (Tiptap NodeView).
 * The editor keeps its own source toggle and chrome, so this is only the
 * diagram, or the reason it cannot be drawn.
 */
export function DrawioEditorPreview({
  xml,
  viewerUrl,
}: {
  xml: string;
  viewerUrl: string;
}) {
  const { t } = useT("editor");
  const embed = useDrawioEmbed(xml, viewerUrl, 0);
  const errorMessage = useDrawioErrorMessage();
  if (embed.status === "error") {
    return (
      <div
        role="alert"
        className="rounded-md border border-destructive/35 px-4 py-3 text-label"
      >
        <div className="font-semibold">{t(($) => $.drawio.error)}</div>
        <div className="mt-0.5 text-caption text-muted-foreground">
          {errorMessage(embed.error, viewerUrl)}
        </div>
      </div>
    );
  }
  return (
    <div
      className="overflow-hidden rounded-md border border-border"
      style={{ height: DRAWIO_BLOCK_BODY_PX }}
    >
      {embed.status === "ready" ? (
        <DrawioFrame src={embed.src} title={t(($) => $.drawio.title)} />
      ) : (
        <DynamicBlockSkeleton />
      )}
    </div>
  );
}
