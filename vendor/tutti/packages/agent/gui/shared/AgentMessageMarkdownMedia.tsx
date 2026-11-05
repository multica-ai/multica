import { useContext, useEffect, useState, type JSX } from "react";
import { ZoomableImage } from "../app/renderer/components/ZoomableImage";
import { cn } from "../app/renderer/lib/utils";
import { useTranslation } from "../i18n/index";
import {
  getOptionalAgentHostApi,
  useOptionalAgentHostApi
} from "../agentActivityHost";
import { workspaceFileName as basenameWorkspacePath } from "@tutti-os/workspace-file-manager/services";
import type { MarkdownDomProps } from "./AgentMessageMarkdown";
import { MarkdownLinkContext } from "./agentMessageMarkdownContext";
import {
  cacheMarkdownMedia,
  canRenderMarkdownVideoFallback,
  peekCachedMarkdownMediaState,
  releaseCachedMarkdownMedia,
  resetCachedMarkdownMediaForTests,
  resolveMarkdownMediaKind,
  resolveMarkdownMediaType,
  resolveMarkdownWorkspaceMediaPath,
  resolveRenderableMarkdownMediaSrc,
  retainCachedMarkdownMedia,
  type MarkdownMediaState
} from "./agentMessageMarkdownLinks";
import { observer } from "mobx-react-lite";
export const MarkdownMedia = observer(MarkdownMediaRender);
const UnsupportedMarkdownMediaPreview = observer(
  UnsupportedMarkdownMediaPreviewRender
);

export function resetCachedMarkdownImagesForTests(): void {
  resetCachedMarkdownMediaForTests();
}

function MarkdownMediaRender({
  node: _node,
  src,
  alt,
  className,
  title,
  enableZoom = false,
  ...props
}: MarkdownDomProps<"img"> & {
  enableZoom?: boolean;
}): JSX.Element {
  "use memo";
  const { t } = useTranslation();
  const isInsideLink = useContext(MarkdownLinkContext);
  const agentHostApi = useOptionalAgentHostApi() ?? getOptionalAgentHostApi();
  const workspacePath =
    typeof src === "string" ? resolveMarkdownWorkspaceMediaPath(src) : null;
  const readWorkspaceImage = workspacePath
    ? agentHostApi?.workspace?.readFile
    : undefined;
  const canReadWorkspaceImage = Boolean(workspacePath && readWorkspaceImage);
  const shouldEnableZoom = enableZoom && !isInsideLink;
  const fallbackMediaKind =
    typeof src === "string" ? resolveMarkdownMediaKind(src) : null;
  const resolvedSrc =
    typeof src === "string" ? resolveRenderableMarkdownMediaSrc(src) : src;
  const [state, setState] = useState<MarkdownMediaState | null>(() =>
    canReadWorkspaceImage && workspacePath
      ? (peekCachedMarkdownMediaState(workspacePath) ?? { status: "loading" })
      : null
  );

  useEffect(() => {
    if (!workspacePath || !readWorkspaceImage) {
      setState(null);
      return;
    }

    const resolvedWorkspacePath = workspacePath;
    const resolvedReadWorkspaceImage = readWorkspaceImage;
    const cached = retainCachedMarkdownMedia(resolvedWorkspacePath);
    if (cached) {
      setState({ kind: cached.kind, status: "ready", src: cached.src });
      return () => {
        releaseCachedMarkdownMedia(resolvedWorkspacePath, cached.src);
      };
    }

    const mediaType = resolveMarkdownMediaType(resolvedWorkspacePath);
    if (!mediaType) {
      setState({
        status: "error",
        reason: "unsupported"
      });
      return;
    }
    const mediaKind = mediaType.kind;
    const mediaMimeType = mediaType.mimeType;

    let canceled = false;
    let objectUrl: string | null = null;
    setState({ status: "loading" });

    async function loadWorkspaceMedia(): Promise<void> {
      try {
        const result = await resolvedReadWorkspaceImage({
          path: resolvedWorkspacePath
        });
        if (canceled) {
          return;
        }
        const bytes =
          result.bytes instanceof Uint8Array
            ? result.bytes
            : new Uint8Array(result.bytes);
        const arrayBuffer = bytes.buffer.slice(
          bytes.byteOffset,
          bytes.byteOffset + bytes.byteLength
        ) as ArrayBuffer;
        objectUrl = cacheMarkdownMedia(
          resolvedWorkspacePath,
          mediaKind,
          new Blob([arrayBuffer], { type: mediaMimeType })
        );
        setState({ kind: mediaKind, status: "ready", src: objectUrl });
      } catch (error) {
        if (!canceled) {
          setState({
            status: "error",
            reason: "read-failed",
            detail: error instanceof Error ? error.message : String(error)
          });
        }
      }
    }

    void loadWorkspaceMedia();

    return () => {
      canceled = true;
      if (objectUrl) {
        releaseCachedMarkdownMedia(resolvedWorkspacePath, objectUrl);
      }
    };
  }, [canReadWorkspaceImage, workspacePath]);

  if (!workspacePath || !readWorkspaceImage) {
    if (fallbackMediaKind === "video") {
      if (!canRenderMarkdownVideoFallback(src)) {
        return <UnsupportedMarkdownMediaPreview />;
      }
      return (
        <video
          src={resolvedSrc}
          aria-label={alt || undefined}
          title={typeof title === "string" ? title : undefined}
          controls
          playsInline
          preload="metadata"
          className={cn(
            "mt-2 block max-h-[360px] max-w-full rounded-[8px] bg-[var(--transparency-block)]",
            className
          )}
        />
      );
    }

    if (!shouldEnableZoom) {
      return (
        <img
          {...props}
          src={resolvedSrc}
          alt={alt}
          title={title}
          className={className}
        />
      );
    }

    return (
      <ZoomableImage
        {...props}
        src={resolvedSrc}
        alt={alt}
        title={title}
        downloadName={resolveMarkdownImageDownloadName(src, alt)}
        className={className}
        wrapElement="span"
      />
    );
  }

  if (state?.status === "ready") {
    if (state.kind === "video") {
      return (
        <video
          src={state.src}
          aria-label={alt || undefined}
          title={typeof title === "string" ? title : undefined}
          controls
          playsInline
          preload="metadata"
          className={cn(
            "mt-2 block max-h-[360px] max-w-full rounded-[8px] bg-[var(--transparency-block)]",
            className
          )}
        />
      );
    }

    if (!shouldEnableZoom) {
      return (
        <img
          {...props}
          src={state.src}
          alt={alt}
          title={title}
          className={cn(
            "mt-2 block max-h-[360px] max-w-full rounded-[8px] bg-[var(--transparency-block)] object-contain",
            className
          )}
        />
      );
    }

    return (
      <ZoomableImage
        {...props}
        src={state.src}
        alt={alt}
        title={title}
        downloadName={resolveMarkdownImageDownloadName(workspacePath, alt)}
        className={cn(
          "mt-2 block max-h-[360px] max-w-full rounded-[8px] bg-[var(--transparency-block)] object-contain",
          className
        )}
        wrapElement="span"
      />
    );
  }

  return (
    <span className="mt-2 flex min-h-[160px] w-full items-center justify-center rounded-[8px] bg-[var(--transparency-block)] px-5 py-5 text-center text-[13px] leading-5 text-[var(--text-tertiary)]">
      {state?.status === "error"
        ? state.reason === "unsupported"
          ? t("agentHost.workspaceFileManager.previewUnsupported")
          : t("agentHost.workspaceFileManager.previewReadFailed", {
              message: state.detail ?? ""
            })
        : t("agentHost.workspaceFileManager.previewLoading")}
    </span>
  );
}

function UnsupportedMarkdownMediaPreviewRender(): JSX.Element {
  const { t } = useTranslation();
  return (
    <span className="mt-2 flex min-h-[160px] w-full items-center justify-center rounded-[8px] bg-[var(--transparency-block)] px-5 py-5 text-center text-[13px] leading-5 text-[var(--text-tertiary)]">
      {t("agentHost.workspaceFileManager.previewUnsupported")}
    </span>
  );
}

function resolveMarkdownImageDownloadName(
  src: unknown,
  alt: unknown
): string | undefined {
  if (typeof src === "string") {
    const pathName = basenameWorkspacePath(src.trim());
    if (pathName) {
      return pathName;
    }
  }
  return typeof alt === "string" ? alt.trim() || undefined : undefined;
}
