import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { Button } from "@tutti-os/ui-system";
import {
  AlertCircle,
  LoaderCircle,
  RotateCcw,
  type LucideIcon
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState, type JSX } from "react";
import styles from "../../../agent-gui/agentGuiNode/AgentGUIConversation.styles";
import { ZoomableImage } from "../../../app/renderer/components/ZoomableImage";
import {
  useOptionalConversationServices,
  type ConversationServices
} from "../../../conversationServices";
import { useTranslation } from "../../../i18n/index";
import type {
  AgentMessageContentVM,
  AgentMessageImageVM
} from "../contracts/agentMessageRowVM";
import { observer } from "mobx-react-lite";
export const AgentUserImageGrid = observer(AgentUserImageGridRender);
const AgentUserImageTile = observer(AgentUserImageTileRender);
const ImageLoadingPlaceholder = observer(ImageLoadingPlaceholderRender);
const ImageLoadFailurePlaceholder = observer(ImageLoadFailurePlaceholderRender);

type ImageFailureKind = "load" | "read" | "unavailable";
type ImageFailureUnavailableReason = "source_vm";
type ImageFailureSource =
  | {
      kind: "resolved";
      src: string;
    }
  | {
      agentSessionId: string;
      attachmentId: string;
      kind: "locator";
      path: string;
      readerAvailable: boolean;
      unavailableReason?: ImageFailureUnavailableReason;
      workspaceId: string;
    };
type ImageFailureReporter = (
  image: AgentMessageImageVM,
  kind: ImageFailureKind,
  attempt: number
) => void;

function AgentUserImageGridRender({
  message
}: {
  message: AgentMessageContentVM;
}): JSX.Element {
  "use memo";
  const images = message.images ?? [];
  const runtime = useOptionalConversationServices();
  const reportFailure = useCallback(
    (image: AgentMessageImageVM, kind: ImageFailureKind, attempt: number) => {
      reportImageLoadFailure(runtime, image, kind, attempt);
    },
    [runtime]
  );
  const reportFailureCallback = usePersistCallback(reportFailure);
  const { failedSources, loadingIds, retryCounts, sources, markFailed, retry } =
    useAgentMessageImageSources(images, reportFailure);
  const { t } = useTranslation();
  const columnCount = Math.min(Math.max(images.length, 1), 4);
  const thumbnailWidth = images.length === 1 ? "160px" : "80px";
  const styleProp1Value1 = `repeat(${columnCount}, ${thumbnailWidth})`;
  const styleProp1 = useMemo<NonNullable<React.ComponentProps<"div">["style"]>>(
    () => ({ gridTemplateColumns: styleProp1Value1 }),
    [styleProp1Value1]
  );
  return (
    <div className={styles.userImageGrid} style={styleProp1}>
      {images.map((image) => {
        const src = sources.get(image.id) ?? imageSourceUrl(image);
        const failureSource = imageFailureSource(image, src, runtime);
        const failedSource = failedSources.get(image.id);
        const displayedFailureSource = failedSource ?? failureSource;
        const failed = isMatchingImageFailure(failedSource, failureSource);
        const canRetry = canRetryImageFailure(displayedFailureSource);
        const attempt = retryCounts.get(image.id) ?? 0;
        return (
          <AgentUserImageTileBinding1
            key={image.id}
            image={image}
            canRetry={canRetry}
            src={src}
            failed={failed}
            loadingIds={loadingIds}
            attempt={attempt}
            t={t}
            displayedFailureSource={displayedFailureSource}
            markFailed={markFailed}
            failureSource={failureSource}
            reportFailure={reportFailureCallback}
            retry={retry}
          />
        );
      })}
    </div>
  );
}

function AgentUserImageTileRender({
  attempt,
  canRetry,
  failed,
  failureLabel,
  image,
  onFailed,
  onRetry,
  retryLabel,
  sourceLoading,
  src
}: AgentUserImageTileProps): JSX.Element {
  const [loadedImage, setLoadedImage] = useState<{
    attempt: number;
    src: string;
  } | null>(null);
  const browserLoaded = Boolean(
    src && loadedImage?.attempt === attempt && loadedImage.src === src
  );
  const showingFailure = failed || (!src && !sourceLoading);
  const loading = !failed && (src ? !browserLoaded : sourceLoading);
  const state = showingFailure ? "failed" : loading ? "loading" : "loaded";

  return (
    <div
      aria-busy={loading || undefined}
      className={styles.userImageThumbnail}
      data-image-state={state}
    >
      {showingFailure ? (
        <ImageLoadFailurePlaceholder
          canRetry={canRetry}
          label={failureLabel}
          retryLabel={retryLabel}
          icon={AlertCircle}
          onRetry={onRetry}
        />
      ) : src ? (
        <>
          <ZoomableImageBinding2
            key={`${image.id}:${attempt}:${src}`}
            image={image}
            attempt={attempt}
            src={src}
            setLoadedImage={setLoadedImage}
            onFailed={onFailed}
          />
          {loading ? <ImageLoadingPlaceholder /> : null}
        </>
      ) : (
        <ImageLoadingPlaceholder />
      )}
    </div>
  );
}

function useAgentMessageImageSources(
  images: readonly AgentMessageImageVM[],
  reportFailure: ImageFailureReporter
): {
  failedSources: ReadonlyMap<string, ImageFailureSource>;
  loadingIds: ReadonlySet<string>;
  retryCounts: ReadonlyMap<string, number>;
  sources: ReadonlyMap<string, string>;
  markFailed: (imageId: string, source: ImageFailureSource) => void;
  retry: (imageId: string) => void;
} {
  const runtime = useOptionalConversationServices();
  const [failedSources, setFailedSources] = useState<
    Map<string, ImageFailureSource>
  >(() => new Map());
  const [sources, setSources] = useState<Map<string, string>>(() => new Map());
  const [loadingIds, setLoadingIds] = useState<Set<string>>(() => new Set());
  const [retryCounts, setRetryCounts] = useState<Map<string, number>>(
    () => new Map()
  );
  const markFailed = useCallback(
    (imageId: string, source: ImageFailureSource): void => {
      setFailedSources((current) => {
        if (isMatchingImageFailure(current.get(imageId), source)) {
          return current;
        }
        const next = new Map(current);
        next.set(imageId, source);
        return next;
      });
      setLoadingIds((current) => {
        if (!current.has(imageId)) return current;
        const next = new Set(current);
        next.delete(imageId);
        return next;
      });
    },
    []
  );
  const retry = useCallback((imageId: string): void => {
    setFailedSources((current) => {
      if (!current.has(imageId)) return current;
      const next = new Map(current);
      next.delete(imageId);
      return next;
    });
    setRetryCounts((current) => {
      const next = new Map(current);
      next.set(imageId, (next.get(imageId) ?? 0) + 1);
      return next;
    });
  }, []);
  const missingImages = useMemo(
    () =>
      images.filter(
        (image) =>
          !imageSourceUrl(image) &&
          !sources.has(image.id) &&
          !isMatchingImageFailure(
            failedSources.get(image.id),
            imageFailureSource(image, null, runtime)
          ) &&
          image.workspaceId &&
          image.agentSessionId &&
          (image.attachmentId
            ? runtime?.attachments?.readSessionAttachment
            : runtime?.attachments?.readPromptAsset) &&
          (image.attachmentId || image.path)
      ),
    [failedSources, images, runtime, sources]
  );
  const unavailableImages = useMemo(
    () =>
      images.filter(
        (image) =>
          !imageSourceUrl(image) &&
          !sources.has(image.id) &&
          !isMatchingImageFailure(
            failedSources.get(image.id),
            imageFailureSource(image, null, runtime)
          ) &&
          (!image.workspaceId ||
            !image.agentSessionId ||
            !(image.attachmentId || image.path) ||
            (image.attachmentId
              ? !runtime?.attachments?.readSessionAttachment
              : !runtime?.attachments?.readPromptAsset))
      ),
    [failedSources, images, runtime, sources]
  );
  const visibleLoadingIds = new Set(loadingIds);
  for (const image of missingImages) visibleLoadingIds.add(image.id);

  useEffect(() => {
    let canceled = false;
    if (
      runtime &&
      (runtime.attachments!.readSessionAttachment ||
        runtime.attachments?.readPromptAsset) &&
      missingImages.length > 0
    ) {
      for (const image of missingImages) {
        const readImage = image.attachmentId
          ? (
              runtime.attachments!.readSessionAttachmentSource ??
              runtime.attachments?.readSessionAttachment
            )?.({
              workspaceId: image.workspaceId ?? "",
              agentSessionId: image.agentSessionId,
              attachmentId: image.attachmentId ?? ""
            })
          : runtime.attachments?.readPromptAsset?.({
              workspaceId: image.workspaceId ?? "",
              agentSessionId: image.agentSessionId,
              mimeType: image.mimeType,
              name: image.name,
              path: image.path
            });
        if (!readImage) continue;
        setLoadingIds((current) => new Set(current).add(image.id));
        const attempt = retryCounts.get(image.id) ?? 0;
        void readImage
          .then((attachment) => {
            if (canceled) return;
            setSources((current) => {
              const next = new Map(current);
              next.set(
                image.id,
                "url" in attachment
                  ? attachment.url
                  : `data:${attachment.mimeType};base64,${attachment.data}`
              );
              return next;
            });
          })
          .catch((error: unknown) => {
            if (canceled) return;
            const unavailableReason = isSourceVMUnavailableError(error)
              ? "source_vm"
              : undefined;
            markFailed(
              image.id,
              imageFailureSource(image, null, runtime, unavailableReason)
            );
            reportFailure(
              image,
              unavailableReason ? "unavailable" : "read",
              attempt
            );
          })
          .finally(() => {
            if (canceled) return;
            setLoadingIds((current) => {
              const next = new Set(current);
              next.delete(image.id);
              return next;
            });
          });
      }
    }
    for (const image of unavailableImages) {
      const attempt = retryCounts.get(image.id) ?? 0;
      markFailed(image.id, imageFailureSource(image, null, runtime));
      reportFailure(image, "unavailable", attempt);
    }
    return () => {
      canceled = true;
    };
  }, [
    markFailed,
    missingImages,
    reportFailure,
    retryCounts,
    runtime,
    unavailableImages
  ]);

  return {
    failedSources,
    loadingIds: visibleLoadingIds,
    retryCounts,
    sources,
    markFailed,
    retry
  };
}

function ImageLoadingPlaceholderRender(): JSX.Element {
  return (
    <div
      className="absolute inset-0 flex items-center justify-center bg-[color-mix(in_srgb,var(--text-primary)_6%,transparent)]"
      data-testid="agent-gui-message-image-loading"
    >
      <LoaderCircle
        aria-hidden="true"
        className="size-5 animate-spin text-[color-mix(in_srgb,var(--text-primary)_45%,transparent)]"
        strokeWidth={2}
      />
    </div>
  );
}

function ImageLoadFailurePlaceholderRender({
  canRetry,
  icon: Icon,
  label,
  onRetry,
  retryLabel
}: {
  canRetry: boolean;
  icon: LucideIcon;
  label: string;
  onRetry: () => void;
  retryLabel: string;
}): JSX.Element {
  return (
    <div
      className="flex h-full w-full flex-col items-center justify-center gap-1 bg-[var(--transparency-block)] px-1 text-[var(--text-secondary)]"
      data-testid="agent-gui-message-image-failed"
      role="status"
      aria-label={label}
    >
      <Icon aria-hidden="true" className="size-4" />
      <span className="max-w-full truncate text-xs" title={label}>
        {label}
      </span>
      {canRetry ? (
        <Button type="button" variant="ghost" size="xs" onClick={onRetry}>
          <RotateCcw aria-hidden="true" />
          {retryLabel}
        </Button>
      ) : null}
    </div>
  );
}

function reportImageLoadFailure(
  runtime: ConversationServices | null,
  image: AgentMessageImageVM,
  kind: ImageFailureKind,
  attempt: number
): void {
  const reportDiagnostic = runtime?.diagnostics?.reportDiagnostic;
  if (!reportDiagnostic) return;
  try {
    void Promise.resolve(
      reportDiagnostic.call(runtime, {
        event: "agent.gui.conversation.image_load_failed",
        level: "warn",
        source: "agent-gui",
        workspaceId: image.workspaceId ?? null,
        details: {
          imageId: image.id,
          attempt,
          failureKind: kind,
          hasUrl: Boolean(image.url?.trim()),
          hasData: Boolean(image.data?.trim()),
          hasAttachmentId: Boolean(image.attachmentId?.trim()),
          hasPath: Boolean(image.path?.trim())
        }
      })
    ).catch((error: unknown) => {
      reportImageLoadDiagnosticFailure(image, error);
    });
  } catch (error) {
    reportImageLoadDiagnosticFailure(image, error);
  }
}

function reportImageLoadDiagnosticFailure(
  image: AgentMessageImageVM,
  error: unknown
): void {
  console.warn(
    "[agent-gui]",
    JSON.stringify({
      event: "agent.gui.conversation.image_diagnostic_failed",
      level: "warn",
      source: "agent-gui",
      workspaceId: image.workspaceId ?? null,
      details: {
        diagnosticEvent: "agent.gui.conversation.image_load_failed",
        error: error instanceof Error ? error.message : String(error)
      }
    })
  );
}

function imageDataUrl(image: AgentMessageImageVM): string | null {
  const data = image.data?.trim() ?? "";
  const mimeType = image.mimeType.trim();
  if (!data || !mimeType) return null;
  return data.startsWith("data:") ? data : `data:${mimeType};base64,${data}`;
}

function imageSourceUrl(image: AgentMessageImageVM): string | null {
  const url = image.url?.trim() ?? "";
  return url || imageDataUrl(image);
}

function imageFailureSource(
  image: AgentMessageImageVM,
  src: string | null,
  runtime: ConversationServices | null,
  unavailableReason?: ImageFailureUnavailableReason
): ImageFailureSource {
  if (src) {
    return { kind: "resolved", src };
  }
  const attachmentId = image.attachmentId?.trim() ?? "";
  return {
    kind: "locator",
    workspaceId: image.workspaceId?.trim() ?? "",
    agentSessionId: image.agentSessionId.trim(),
    attachmentId,
    path: image.path?.trim() ?? "",
    readerAvailable: Boolean(
      attachmentId
        ? runtime?.attachments?.readSessionAttachment
        : runtime?.attachments?.readPromptAsset
    ),
    ...(unavailableReason ? { unavailableReason } : {})
  };
}

function canRetryImageFailure(source: ImageFailureSource): boolean {
  return (
    source.kind === "resolved" ||
    (source.readerAvailable && !source.unavailableReason)
  );
}

function imageFailureLabelKey(
  source: ImageFailureSource
):
  | "agentHost.agentGui.imageLoadFailed"
  | "agentHost.agentGui.imageTemporarilyUnavailable" {
  return source.kind === "locator" &&
    (source.unavailableReason === "source_vm" || !source.readerAvailable)
    ? "agentHost.agentGui.imageTemporarilyUnavailable"
    : "agentHost.agentGui.imageLoadFailed";
}

function isSourceVMUnavailableError(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    "code" in error &&
    (error as { code?: unknown }).code ===
      "agent_session_attachment_vm_unavailable"
  );
}

function isMatchingImageFailure(
  failed: ImageFailureSource | undefined,
  current: ImageFailureSource
): boolean {
  if (!failed || failed.kind !== current.kind) return false;
  if (failed.kind === "resolved" && current.kind === "resolved") {
    return failed.src === current.src;
  }
  if (failed.kind !== "locator" || current.kind !== "locator") return false;
  return (
    failed.workspaceId === current.workspaceId &&
    failed.agentSessionId === current.agentSessionId &&
    failed.attachmentId === current.attachmentId &&
    failed.path === current.path &&
    failed.readerAvailable === current.readerAvailable
  );
}

const AgentUserImageTileBinding1 = observer(
  function AgentUserImageTileBinding1({
    image,
    canRetry,
    src,
    failed,
    loadingIds,
    attempt,
    t,
    displayedFailureSource,
    markFailed,
    failureSource,
    reportFailure,
    retry
  }: AgentUserImageTileBinding1Props) {
    const onFailedProp2 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof AgentUserImageTile>["onFailed"]>
    >(() => {
      markFailed(image.id, failureSource);
      reportFailure(image, "load", attempt);
    });
    const onRetryProp3 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof AgentUserImageTile>["onRetry"]>
    >(() => retry(image.id));
    return (
      <AgentUserImageTile
        key={image.id}
        canRetry={canRetry}
        image={image}
        src={src}
        failed={failed}
        sourceLoading={!src && loadingIds.has(image.id)}
        attempt={attempt}
        failureLabel={t(imageFailureLabelKey(displayedFailureSource))}
        retryLabel={t("agentHost.agentGui.retryImage")}
        onFailed={onFailedProp2}
        onRetry={onRetryProp3}
      />
    );
  }
);

const ZoomableImageBinding2 = observer(function ZoomableImageBinding2({
  image,
  attempt,
  src,
  setLoadedImage,
  onFailed
}: {
  image: NonNullable<AgentUserImageTileProps>["image"];
  attempt: NonNullable<AgentUserImageTileProps>["attempt"];
  src: string;
  setLoadedImage: import("react").Dispatch<
    import("react").SetStateAction<{ attempt: number; src: string } | null>
  >;
  onFailed: NonNullable<AgentUserImageTileProps>["onFailed"];
}) {
  const onLoadProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof ZoomableImage>["onLoad"]>
  >(() => {
    setLoadedImage((current) =>
      current?.attempt === attempt && current.src === src
        ? current
        : { attempt, src }
    );
  });
  return (
    <ZoomableImage
      key={`${image.id}:${attempt}:${src}`}
      src={src}
      alt={image.name?.trim() || "image"}
      className="block h-full w-full rounded-[7px] object-contain"
      draggable={false}
      downloadName={image.name?.trim() || "image.png"}
      onLoad={onLoadProp4}
      onError={onFailed}
    />
  );
});

type AgentUserImageTileProps = {
  attempt: number;
  canRetry: boolean;
  failed: boolean;
  failureLabel: string;
  image: AgentMessageImageVM;
  onFailed: () => void;
  onRetry: () => void;
  retryLabel: string;
  sourceLoading: boolean;
  src: string | null;
};

type AgentUserImageTileBinding1Props = {
  image: AgentMessageImageVM;
  canRetry: boolean;
  src: string | null;
  failed: boolean;
  loadingIds: ReadonlySet<string>;
  attempt: number;
  t: import("../../../i18n/runtime").TranslateFn;
  displayedFailureSource: ImageFailureSource;
  markFailed: (imageId: string, source: ImageFailureSource) => void;
  failureSource: ImageFailureSource;
  reportFailure: (
    image: AgentMessageImageVM,
    kind: ImageFailureKind,
    attempt: number
  ) => void;
  retry: (imageId: string) => void;
};

type ZoomableImageBinding2Props = {
  image: AgentMessageImageVM;
  attempt: number;
  src: string;
  setLoadedImage: import("react").Dispatch<
    import("react").SetStateAction<{ attempt: number; src: string } | null>
  >;
  onFailed: () => void;
};
