import { Slot } from "radix-ui";
import { useMemo } from "react";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import {
  type CSSProperties,
  type ComponentPropsWithoutRef,
  type JSX,
  type MouseEvent,
  type WheelEvent,
  useCallback,
  useEffect,
  useState
} from "react";
import { createPortal } from "react-dom";
import {
  Button,
  ToastProvider,
  ToastRoot,
  ToastTitle,
  ToastViewport,
  CloseIcon,
  CopyIcon,
  DownloadIcon
} from "@tutti-os/ui-system";
import { RotateCcwIcon, ZoomInIcon, ZoomOutIcon } from "lucide-react";
import { useTranslation } from "../../../i18n/index";
import { cn } from "../lib/utils";
import { ConversationImageContextMenu } from "../../../shared/agentConversation/components/ConversationImageContextMenu";
import { copyImageToClipboard } from "../../../shared/agentConversation/lib/copyImageToClipboard";
import { useOptionalAgentHostApi } from "../../../agentActivityHost";
import {
  downloadImage,
  resolveImageDownloadName
} from "./zoomableImageDownload";
import { getPortalContainer } from "@tutti-os/ui-system/portal-container";
import { observer } from "mobx-react-lite";
export const ZoomableImage = observer(ZoomableImageRender);
const ImageCopyStatusToast = observer(ImageCopyStatusToastRender);
const ImagePreviewZoomControls = observer(ImagePreviewZoomControlsRender);
const ImageActionButtons = observer(ImageActionButtonsRender);

interface ZoomableImageProps extends ComponentPropsWithoutRef<"img"> {
  downloadName?: string;
  wrapElement?: "div" | "span";
}

type ImageCopyStatus = {
  busy: boolean;
  message: string;
  variant: "destructive" | "success";
};

const IMAGE_PREVIEW_ZOOM_MIN = 0.5;
const IMAGE_PREVIEW_ZOOM_MAX = 3;
const IMAGE_PREVIEW_ZOOM_STEP = 0.25;

function ZoomableImageRender({
  alt,
  className,
  downloadName,
  onContextMenu,
  src,
  wrapElement = "div",
  ...props
}: ZoomableImageProps): JSX.Element {
  const { t } = useTranslation();
  const agentHostApi = useOptionalAgentHostApi();
  const actionSource =
    typeof src === "string" && src.trim() ? src.trim() : null;
  const hasImageActions = Boolean(actionSource && downloadName !== undefined);
  const [contextMenuPosition, setContextMenuPosition] = useState<{
    x: number;
    y: number;
    inZoomDialog: boolean;
  } | null>(null);
  const [copyStatus, setCopyStatus] = useState<ImageCopyStatus | null>(null);
  const [imagePreviewZoom, setImagePreviewZoom] = useState(1);
  const [isWheelZooming, setIsWheelZooming] = useState(false);
  const [isImagePreviewOpen, setIsImagePreviewOpen] = useState(false);
  const [isImagePreviewClosing, setIsImagePreviewClosing] = useState(false);
  const imagePreviewZoomPercent = Math.round(imagePreviewZoom * 100);
  const canZoomOut = imagePreviewZoom > IMAGE_PREVIEW_ZOOM_MIN;
  const canZoomIn = imagePreviewZoom < IMAGE_PREVIEW_ZOOM_MAX;

  const closeContextMenu = useCallback(() => {
    setContextMenuPosition(null);
  }, []);

  const finishClosePreviewImage = (): void => {
    setIsImagePreviewOpen(false);
    setIsImagePreviewClosing(false);
    setIsWheelZooming(false);
    setImagePreviewZoom(1);
  };
  const finishClosePreviewImageCallback = usePersistCallback(
    finishClosePreviewImage
  );

  const closePreviewImage = (): void => {
    if (!isImagePreviewOpen) {
      return;
    }
    setIsImagePreviewClosing(true);
    setIsWheelZooming(false);
    setImagePreviewZoom(1);
    closeContextMenu();
  };
  const closePreviewImageCallback = usePersistCallback(closePreviewImage);

  const openPreviewImage = (): void => {
    if (!actionSource) {
      return;
    }
    closeContextMenu();
    setIsImagePreviewClosing(false);
    setIsImagePreviewOpen(true);
  };
  const openPreviewImageCallback = usePersistCallback(openPreviewImage);

  const handlePreviewSurfaceClick = (
    event: MouseEvent<HTMLDivElement>
  ): void => {
    const target = event.target;
    if (target === event.currentTarget) {
      closePreviewImage();
      return;
    }
    if (!(target instanceof HTMLElement)) {
      return;
    }
    if (
      target.hasAttribute("data-rmiz-modal-overlay") ||
      target.hasAttribute("data-rmiz-modal-content")
    ) {
      closePreviewImage();
    }
  };
  const handlePreviewSurfaceClickCallback = usePersistCallback(
    handlePreviewSurfaceClick
  );

  useEffect(() => {
    const handleWindowKeyDown = (event: globalThis.KeyboardEvent): void => {
      if (!isImagePreviewOpen || event.key !== "Escape") {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      setIsImagePreviewClosing(true);
      setIsWheelZooming(false);
      setImagePreviewZoom(1);
      closeContextMenu();
    };

    if (contextMenuPosition) {
      document.addEventListener("click", closeContextMenu);
      document.addEventListener("scroll", closeContextMenu, true);
    }
    if (isImagePreviewOpen) {
      window.addEventListener("keydown", handleWindowKeyDown, true);
    }
    return () => {
      document.removeEventListener("click", closeContextMenu);
      document.removeEventListener("scroll", closeContextMenu, true);
      window.removeEventListener("keydown", handleWindowKeyDown, true);
    };
  }, [closeContextMenu, contextMenuPosition, isImagePreviewOpen]);

  const handleContextMenu = useCallback(
    (event: MouseEvent<HTMLElement>): void => {
      onContextMenu?.(event as MouseEvent<HTMLImageElement>);
      if (event.defaultPrevented || !actionSource || !hasImageActions) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      setContextMenuPosition({
        x: event.clientX,
        y: event.clientY,
        inZoomDialog: Boolean(event.currentTarget.closest(".tsh-zoom-dialog"))
      });
    },
    [actionSource, hasImageActions, onContextMenu]
  );
  const handleContextMenuCallback = usePersistCallback(handleContextMenu);

  const handleCopyImage = useCallback(async (): Promise<void> => {
    if (!actionSource) {
      return;
    }
    const copyingMessage = t("common.copying");
    setCopyStatus({
      busy: true,
      message: copyingMessage,
      variant: "success"
    });
    closeContextMenu();
    const copied = await Promise.race([
      copyImageToClipboard(actionSource, agentHostApi?.clipboard),
      new Promise<boolean>((resolve) => {
        window.setTimeout(() => resolve(false), 5000);
      })
    ]);
    const message = t(
      copied ? "agentHost.agentGui.messageCopied" : "common.copyFailed"
    );
    setCopyStatus({
      busy: false,
      message,
      variant: copied ? "success" : "destructive"
    });
  }, [actionSource, agentHostApi?.clipboard, closeContextMenu, t]);

  const handleCopyImageAction = useCallback((): void => {
    void handleCopyImage().catch(() => undefined);
  }, [handleCopyImage]);
  const handleCopyImageActionCallback = usePersistCallback(
    handleCopyImageAction
  );

  const handleDownloadImage = useCallback((): void => {
    if (!actionSource) {
      return;
    }
    closeContextMenu();
    downloadImage(
      actionSource,
      resolveImageDownloadName(downloadName, actionSource, alt)
    );
    setCopyStatus({
      busy: false,
      message: t("agentHost.agentGui.imageDownloaded"),
      variant: "success"
    });
  }, [actionSource, alt, closeContextMenu, downloadName, t]);
  const handleDownloadImageCallback = usePersistCallback(handleDownloadImage);
  const zoomOutPreviewImage = useCallback((): void => {
    setIsWheelZooming(false);
    setImagePreviewZoom((value) =>
      clampSteppedImagePreviewZoom(value - IMAGE_PREVIEW_ZOOM_STEP)
    );
  }, []);
  const zoomOutPreviewImageCallback = usePersistCallback(zoomOutPreviewImage);
  const zoomInPreviewImage = useCallback((): void => {
    setIsWheelZooming(false);
    setImagePreviewZoom((value) =>
      clampSteppedImagePreviewZoom(value + IMAGE_PREVIEW_ZOOM_STEP)
    );
  }, []);
  const zoomInPreviewImageCallback = usePersistCallback(zoomInPreviewImage);
  const resetPreviewImageZoom = useCallback((): void => {
    setIsWheelZooming(false);
    setImagePreviewZoom(1);
  }, []);
  const resetPreviewImageZoomCallback = usePersistCallback(
    resetPreviewImageZoom
  );
  const handlePreviewImageWheel = useCallback(
    (event: WheelEvent<HTMLElement>): void => {
      event.preventDefault();
      event.stopPropagation();
      if (event.deltaY === 0) {
        return;
      }
      setIsWheelZooming(true);
      setImagePreviewZoom((value) =>
        clampImagePreviewZoom(value * Math.pow(2, resolveWheelZoomDelta(event)))
      );
    },
    []
  );
  const handlePreviewImageWheelCallback = usePersistCallback(
    handlePreviewImageWheel
  );

  const actionButtons = hasImageActions ? (
    <ImageActionButtons
      copyLabel={t("common.copyImage")}
      downloadLabel={t("common.downloadImage")}
      onCopy={handleCopyImageActionCallback}
      onDownload={handleDownloadImageCallback}
    />
  ) : null;

  const previewImage =
    (isImagePreviewOpen || isImagePreviewClosing) && actionSource ? (
      <ImgBinding1
        alt={alt}
        imagePreviewZoom={imagePreviewZoom}
        actionSource={actionSource}
        props={props}
        isWheelZooming={isWheelZooming}
        closePreviewImage={closePreviewImageCallback}
        hasImageActions={hasImageActions}
        handleContextMenu={handleContextMenuCallback}
        onContextMenu={onContextMenu}
        isImagePreviewClosing={isImagePreviewClosing}
        finishClosePreviewImage={finishClosePreviewImageCallback}
        handlePreviewImageWheel={handlePreviewImageWheelCallback}
      />
    ) : null;

  const previewContent =
    previewImage && !actionButtons && actionSource ? (
      <ConversationImageContextMenuBinding2
        actionSource={actionSource}
        previewImage={previewImage}
      />
    ) : (
      previewImage
    );

  const Wrapper = wrapElement;

  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"img">["onClick"]>
  >((event) => {
    props.onClick?.(event);
    if (!event.defaultPrevented) {
      openPreviewImage();
    }
  });
  return (
    <>
      <Wrapper
        className={cn(
          "tsh-zoomable-image nodrag tsh-desktop-no-drag",
          wrapElement === "div" && "tsh-zoomable-image--block"
        )}
      >
        <img
          {...props}
          alt={alt}
          src={src}
          onClick={onClickProp1}
          onContextMenu={hasImageActions ? handleContextMenu : onContextMenu}
          className={cn("nodrag tsh-desktop-no-drag cursor-zoom-in", className)}
        />
        <button
          type="button"
          aria-label={t("common.expandImage")}
          className="tsh-zoomable-image__trigger nodrag tsh-desktop-no-drag"
          onClick={openPreviewImageCallback}
          onContextMenu={handleContextMenuCallback}
        />
      </Wrapper>
      {contextMenuPosition && !contextMenuPosition.inZoomDialog && actionButtons
        ? createPortal(
            <DivBinding3
              contextMenuPosition={contextMenuPosition}
              t={t}
              handleCopyImageAction={handleCopyImageActionCallback}
              handleDownloadImage={handleDownloadImageCallback}
            />,
            getPortalContainer()
          )
        : null}
      {(isImagePreviewOpen || isImagePreviewClosing) && actionSource
        ? createPortal(
            <DivBinding1
              isImagePreviewClosing={isImagePreviewClosing}
              hasImageActions={hasImageActions}
              handlePreviewSurfaceClick={handlePreviewSurfaceClickCallback}
              finishClosePreviewImage={finishClosePreviewImageCallback}
              previewContent={previewContent}
              canZoomIn={canZoomIn}
              canZoomOut={canZoomOut}
              imagePreviewZoomPercent={imagePreviewZoomPercent}
              t={t}
              copyStatus={copyStatus}
              resetPreviewImageZoom={resetPreviewImageZoomCallback}
              zoomInPreviewImage={zoomInPreviewImageCallback}
              zoomOutPreviewImage={zoomOutPreviewImageCallback}
              handlePreviewImageWheel={handlePreviewImageWheelCallback}
              actionButtons={actionButtons}
              closePreviewImage={closePreviewImageCallback}
              contextMenuPosition={contextMenuPosition}
              handleCopyImageAction={handleCopyImageActionCallback}
              handleDownloadImage={handleDownloadImageCallback}
              setCopyStatus={setCopyStatus}
            />,
            getPortalContainer()
          )
        : null}
      {copyStatus && !isImagePreviewOpen
        ? createPortal(
            <ImageCopyStatusToastBinding7
              copyStatus={copyStatus}
              setCopyStatus={setCopyStatus}
            />,
            getPortalContainer()
          )
        : null}
    </>
  );
}

function ImageCopyStatusToastRender({
  busy,
  message,
  onOpenChange,
  variant
}: {
  busy: boolean;
  message: string;
  onOpenChange: (open: boolean) => void;
  variant: ImageCopyStatus["variant"];
}): JSX.Element {
  const styleProp2 = useMemo<
    NonNullable<React.ComponentProps<typeof ToastRoot>["style"]>
  >(() => ({ zIndex: 100303 }), []);

  const styleProp3 = useMemo<
    NonNullable<React.ComponentProps<typeof ToastViewport>["style"]>
  >(
    () => ({
      top: "max(20px, calc(var(--cove-titlebar-reserve, 0px) + 10px))",
      zIndex: 100303
    }),
    []
  );
  return (
    <ToastProvider duration={1600} swipeDirection="right">
      <ToastRoot
        open
        anchor="viewport"
        busy={busy}
        variant={variant}
        data-tsh-image-copy-status="true"
        style={styleProp2}
        onOpenChange={onOpenChange}
      >
        <ToastTitle>{message}</ToastTitle>
      </ToastRoot>
      <ToastViewport
        className="nodrag tsh-desktop-no-drag"
        style={styleProp3}
      />
    </ToastProvider>
  );
}

function ImagePreviewZoomControlsRender({
  canZoomIn,
  canZoomOut,
  percent,
  percentLabel,
  reportPercentStatus,
  resetLabel,
  zoomInLabel,
  zoomOutLabel,
  onReset,
  onZoomIn,
  onZoomOut,
  onWheel
}: ImagePreviewZoomControlsProps): JSX.Element {
  return (
    <div
      className="tsh-zoom-dialog__zoom-controls nodrag tsh-desktop-no-drag"
      onWheel={onWheel}
    >
      <button
        type="button"
        title={zoomOutLabel}
        aria-label={zoomOutLabel}
        disabled={!canZoomOut}
        onClick={onZoomOut}
      >
        <ZoomOutIcon aria-hidden="true" className="size-4" />
      </button>
      <span
        aria-label={percentLabel}
        role={reportPercentStatus ? "status" : undefined}
      >
        {percent}%
      </span>
      <button
        type="button"
        title={resetLabel}
        aria-label={resetLabel}
        onClick={onReset}
      >
        <RotateCcwIcon aria-hidden="true" className="size-4" />
      </button>
      <button
        type="button"
        title={zoomInLabel}
        aria-label={zoomInLabel}
        disabled={!canZoomIn}
        onClick={onZoomIn}
      >
        <ZoomInIcon aria-hidden="true" className="size-4" />
      </button>
    </div>
  );
}

function ImageActionButtonsRender({
  copyLabel,
  downloadLabel,
  itemRole,
  onCopy,
  onDownload
}: {
  copyLabel: string;
  downloadLabel: string;
  itemRole?: "menuitem";
  onCopy: () => void;
  onDownload: () => void;
}): JSX.Element {
  const onPointerDownProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onPointerDown"]>
  >((event) => event.stopPropagation());
  const onClickProp5 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >((event) => {
    event.preventDefault();
    event.stopPropagation();
    onCopy();
  });
  const onPointerDownProp6 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onPointerDown"]>
  >((event) => event.stopPropagation());
  const onClickProp7 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >((event) => {
    event.preventDefault();
    event.stopPropagation();
    onDownload();
  });
  if (!itemRole) {
    return (
      <>
        <Button
          aria-label={copyLabel}
          className="tsh-zoom-dialog__icon-button"
          size="icon"
          title={copyLabel}
          onPointerDown={onPointerDownProp4}
          variant="chrome"
          onClick={onClickProp5}
        >
          <CopyIcon aria-hidden="true" className="size-4" />
        </Button>
        <Button
          aria-label={downloadLabel}
          className="tsh-zoom-dialog__icon-button"
          size="icon"
          title={downloadLabel}
          onPointerDown={onPointerDownProp6}
          variant="chrome"
          onClick={onClickProp7}
        >
          <DownloadIcon aria-hidden="true" className="size-4" />
        </Button>
      </>
    );
  }

  return (
    <>
      <ButtonBinding8
        itemRole={itemRole}
        copyLabel={copyLabel}
        onCopy={onCopy}
      />
      <ButtonBinding9
        itemRole={itemRole}
        downloadLabel={downloadLabel}
        onDownload={onDownload}
      />
    </>
  );
}

function resolveWheelZoomDelta(event: WheelEvent<HTMLElement>): number {
  return (
    -event.deltaY *
    (event.deltaMode === 1 ? 0.05 : event.deltaMode ? 1 : 0.002) *
    (event.ctrlKey ? 10 : 1)
  );
}

function resolveZoomedImageTransform(
  transform: CSSProperties["transform"],
  zoom: number,
  width: number | null,
  height: number | null
): CSSProperties["transform"] {
  const baseTransform = typeof transform === "string" ? transform.trim() : "";
  const zoomTransform = resolveImagePreviewZoomTransform(zoom, width, height);
  if (!baseTransform) {
    return zoomTransform || undefined;
  }
  if (!zoomTransform) {
    return baseTransform;
  }
  return `${baseTransform} ${zoomTransform}`;
}

function resolveImagePreviewZoomTransform(
  zoom: number,
  width: number | null,
  height: number | null
): string {
  if (zoom === 1) {
    return "";
  }

  const scale = `scale(${formatImagePreviewZoom(zoom)})`;
  if (width === null || height === null) {
    return scale;
  }

  const halfWidth = formatCssNumber(width / 2);
  const halfHeight = formatCssNumber(height / 2);
  return `translate(${halfWidth}px,${halfHeight}px) ${scale} translate(-${halfWidth}px,-${halfHeight}px)`;
}

function formatCssNumber(value: number): string {
  return Number(value.toFixed(3)).toString();
}

function mergeImagePreviewTransition(
  transition: CSSProperties["transition"]
): CSSProperties["transition"] {
  const zoomTransition = "transform 120ms ease-out";
  if (typeof transition !== "string" || !transition.trim()) {
    return zoomTransition;
  }
  return transition.includes("transform")
    ? transition
    : `${transition}, ${zoomTransition}`;
}

function clampSteppedImagePreviewZoom(value: number): number {
  const stepped =
    Math.round(value / IMAGE_PREVIEW_ZOOM_STEP) * IMAGE_PREVIEW_ZOOM_STEP;
  return clampImagePreviewZoom(stepped);
}

function clampImagePreviewZoom(value: number): number {
  return Math.min(
    IMAGE_PREVIEW_ZOOM_MAX,
    Math.max(IMAGE_PREVIEW_ZOOM_MIN, value)
  );
}

function formatImagePreviewZoom(value: number): string {
  return Number(value.toFixed(2)).toString();
}

const ImgBinding1 = observer(function ImgBinding1({
  alt,
  imagePreviewZoom,
  actionSource,
  props,
  isWheelZooming,
  closePreviewImage,
  hasImageActions,
  handleContextMenu,
  onContextMenu,
  isImagePreviewClosing,
  finishClosePreviewImage,
  handlePreviewImageWheel
}: ImgBinding1Props) {
  const styleProp8Value1 = resolveZoomedImageTransform(
    undefined,
    imagePreviewZoom,
    null,
    null
  );
  const styleProp8Value2 = isWheelZooming
    ? "none"
    : mergeImagePreviewTransition(undefined);
  const styleProp8 = useMemo<NonNullable<React.ComponentProps<"img">["style"]>>(
    () => ({ transform: styleProp8Value1, transition: styleProp8Value2 }),
    [styleProp8Value1, styleProp8Value2]
  );
  const onClickProp9 = usePersistCallback<
    NonNullable<React.ComponentProps<"img">["onClick"]>
  >((event) => {
    event.stopPropagation();
    closePreviewImage();
  });
  return (
    <img
      alt={alt}
      data-rmiz-modal-img=""
      data-tsh-image-zoom={formatImagePreviewZoom(imagePreviewZoom)}
      draggable={false}
      src={actionSource}
      title={typeof props.title === "string" ? props.title : undefined}
      className="tsh-zoom-dialog__image nodrag tsh-desktop-no-drag"
      style={styleProp8}
      onClick={onClickProp9}
      onContextMenu={hasImageActions ? handleContextMenu : onContextMenu}
      onTransitionEnd={
        isImagePreviewClosing ? finishClosePreviewImage : undefined
      }
      onWheel={handlePreviewImageWheel}
    />
  );
});

const ConversationImageContextMenuBinding2 = observer(
  function ConversationImageContextMenuBinding2({
    actionSource,
    previewImage
  }: {
    actionSource: string;
    previewImage: import("react").JSX.Element;
  }) {
    const contentStyleProp10 = useMemo<
      NonNullable<
        React.ComponentProps<
          typeof ConversationImageContextMenu
        >["contentStyle"]
      >
    >(() => ({ zIndex: "var(--z-dialog-popover)" }), []);
    return (
      <ConversationImageContextMenu
        src={actionSource}
        asChild
        contentStyle={contentStyleProp10}
      >
        {previewImage}
      </ConversationImageContextMenu>
    );
  }
);

const DivBinding3 = observer(function DivBinding3({
  contextMenuPosition,
  t,
  handleCopyImageAction,
  handleDownloadImage,
  ...slotProps
}: DivBinding3Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof DivBinding3Props>) {
  const styleProp11Value1 = contextMenuPosition.x;
  const styleProp11Value2 = contextMenuPosition.y;
  const styleProp11 = useMemo<
    NonNullable<React.ComponentProps<"div">["style"]>
  >(
    () => ({ left: styleProp11Value1, top: styleProp11Value2 }),
    [styleProp11Value1, styleProp11Value2]
  );
  const onClickProp12 = usePersistCallback<
    NonNullable<React.ComponentProps<"div">["onClick"]>
  >((event) => event.stopPropagation());
  return (
    <Slot.Root {...slotProps}>
      <div
        className="tsh-image-context-menu nodrag tsh-desktop-no-drag"
        style={styleProp11}
        role="menu"
        onClick={onClickProp12}
      >
        <ImageActionButtons
          copyLabel={t("common.copyImage")}
          downloadLabel={t("common.downloadImage")}
          itemRole="menuitem"
          onCopy={handleCopyImageAction}
          onDownload={handleDownloadImage}
        />
      </div>
    </Slot.Root>
  );
});

const ButtonBinding4 = observer(function ButtonBinding4({
  t,
  closePreviewImage,
  ...slotProps
}: ButtonBinding4Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding4Props>) {
  const onPointerDownProp13 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onPointerDown"]>
  >((event) => event.stopPropagation());
  const onClickProp14 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >((event) => {
    event.preventDefault();
    event.stopPropagation();
    closePreviewImage();
  });
  return (
    <Slot.Root {...slotProps}>
      <Button
        aria-label={t("common.close")}
        className="tsh-zoom-dialog__icon-button nodrag tsh-desktop-no-drag"
        size="icon"
        title={t("common.close")}
        onPointerDown={onPointerDownProp13}
        variant="chrome"
        onClick={onClickProp14}
      >
        <CloseIcon aria-hidden="true" className="size-4" />
      </Button>
    </Slot.Root>
  );
});

const DivBinding5 = observer(function DivBinding5({
  contextMenuPosition,
  t,
  handleCopyImageAction,
  handleDownloadImage,
  ...slotProps
}: DivBinding5Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof DivBinding5Props>) {
  const styleProp15Value1 = contextMenuPosition.x;
  const styleProp15Value2 = contextMenuPosition.y;
  const styleProp15 = useMemo<
    NonNullable<React.ComponentProps<"div">["style"]>
  >(
    () => ({ left: styleProp15Value1, top: styleProp15Value2 }),
    [styleProp15Value1, styleProp15Value2]
  );
  const onClickProp16 = usePersistCallback<
    NonNullable<React.ComponentProps<"div">["onClick"]>
  >((event) => event.stopPropagation());
  return (
    <Slot.Root {...slotProps}>
      <div
        className="tsh-image-context-menu nodrag tsh-desktop-no-drag"
        style={styleProp15}
        role="menu"
        onClick={onClickProp16}
      >
        <ImageActionButtons
          copyLabel={t("common.copyImage")}
          downloadLabel={t("common.downloadImage")}
          itemRole="menuitem"
          onCopy={handleCopyImageAction}
          onDownload={handleDownloadImage}
        />
      </div>
    </Slot.Root>
  );
});

const ImageCopyStatusToastBinding6 = observer(
  function ImageCopyStatusToastBinding6({
    copyStatus,
    setCopyStatus
  }: ImageCopyStatusToastBinding6Props) {
    const onOpenChangeProp17 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof ImageCopyStatusToast>["onOpenChange"]
      >
    >((open) => {
      if (!open) {
        setCopyStatus(null);
      }
    });
    return (
      <ImageCopyStatusToast
        busy={copyStatus.busy}
        message={copyStatus.message}
        variant={copyStatus.variant}
        onOpenChange={onOpenChangeProp17}
      />
    );
  }
);

const ImageCopyStatusToastBinding7 = observer(
  function ImageCopyStatusToastBinding7({
    copyStatus,
    setCopyStatus
  }: {
    copyStatus: ImageCopyStatus;
    setCopyStatus: import("react").Dispatch<
      import("react").SetStateAction<ImageCopyStatus | null>
    >;
  }) {
    const onOpenChangeProp18 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof ImageCopyStatusToast>["onOpenChange"]
      >
    >((open) => {
      if (!open) {
        setCopyStatus(null);
      }
    });
    return (
      <ImageCopyStatusToast
        busy={copyStatus.busy}
        message={copyStatus.message}
        variant={copyStatus.variant}
        onOpenChange={onOpenChangeProp18}
      />
    );
  }
);

const ButtonBinding8 = observer(function ButtonBinding8({
  itemRole,
  copyLabel,
  onCopy,
  ...slotProps
}: ButtonBinding8Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding8Props>) {
  const onPointerDownProp19 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onPointerDown"]>
  >((event) => event.stopPropagation());
  const onClickProp20 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >((event) => {
    event.preventDefault();
    event.stopPropagation();
    onCopy();
  });
  return (
    <Slot.Root {...slotProps}>
      <button
        type="button"
        role={itemRole}
        title={copyLabel}
        onPointerDown={onPointerDownProp19}
        onClick={onClickProp20}
      >
        <CopyIcon aria-hidden="true" className="size-4" />
        <span>{copyLabel}</span>
      </button>
    </Slot.Root>
  );
});

const ButtonBinding9 = observer(function ButtonBinding9({
  itemRole,
  downloadLabel,
  onDownload,
  ...slotProps
}: ButtonBinding9Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding9Props>) {
  const onPointerDownProp21 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onPointerDown"]>
  >((event) => event.stopPropagation());
  const onClickProp22 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >((event) => {
    event.preventDefault();
    event.stopPropagation();
    onDownload();
  });
  return (
    <Slot.Root {...slotProps}>
      <button
        type="button"
        role={itemRole}
        title={downloadLabel}
        onPointerDown={onPointerDownProp21}
        onClick={onClickProp22}
      >
        <DownloadIcon aria-hidden="true" className="size-4" />
        <span>{downloadLabel}</span>
      </button>
    </Slot.Root>
  );
});

const DivBinding1 = observer(function DivBinding1({
  isImagePreviewClosing,
  hasImageActions,
  handlePreviewSurfaceClick,
  finishClosePreviewImage,
  previewContent,
  canZoomIn,
  canZoomOut,
  imagePreviewZoomPercent,
  t,
  copyStatus,
  resetPreviewImageZoom,
  zoomInPreviewImage,
  zoomOutPreviewImage,
  handlePreviewImageWheel,
  actionButtons,
  closePreviewImage,
  contextMenuPosition,
  handleCopyImageAction,
  handleDownloadImage,
  setCopyStatus,
  ...slotProps
}: DivBinding1Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof DivBinding1Props>) {
  const onAnimationEndProp23 = usePersistCallback<
    NonNullable<React.ComponentProps<"div">["onAnimationEnd"]>
  >((event) => {
    if (isImagePreviewClosing && event.currentTarget === event.target) {
      finishClosePreviewImage();
    }
  });
  return (
    <Slot.Root {...slotProps}>
      <div
        aria-modal="true"
        autoFocus
        className="tsh-zoom-dialog nodrag tsh-desktop-no-drag"
        data-closing={isImagePreviewClosing ? "true" : undefined}
        data-tsh-image-actions={hasImageActions ? "true" : undefined}
        data-rmiz-modal=""
        role="dialog"
        tabIndex={-1}
        onClick={handlePreviewSurfaceClick}
        onAnimationEnd={onAnimationEndProp23}
      >
        <div data-rmiz-modal-overlay="visible" />
        <div data-rmiz-modal-content="true">{previewContent}</div>
        <ImagePreviewZoomControls
          canZoomIn={canZoomIn}
          canZoomOut={canZoomOut}
          percent={imagePreviewZoomPercent}
          percentLabel={t("common.imageZoomPercent", {
            percent: imagePreviewZoomPercent
          })}
          reportPercentStatus={!copyStatus}
          resetLabel={t("common.resetImageZoom")}
          zoomInLabel={t("common.zoomInImage")}
          zoomOutLabel={t("common.zoomOutImage")}
          onReset={resetPreviewImageZoom}
          onZoomIn={zoomInPreviewImage}
          onZoomOut={zoomOutPreviewImage}
          onWheel={handlePreviewImageWheel}
        />
        <div className="tsh-zoom-dialog__toolbar-actions nodrag tsh-desktop-no-drag">
          {actionButtons}
          <ButtonBinding4 t={t} closePreviewImage={closePreviewImage} />
        </div>
        {contextMenuPosition?.inZoomDialog && actionButtons ? (
          <DivBinding5
            contextMenuPosition={contextMenuPosition}
            t={t}
            handleCopyImageAction={handleCopyImageAction}
            handleDownloadImage={handleDownloadImage}
          />
        ) : null}
        {copyStatus ? (
          <ImageCopyStatusToastBinding6
            copyStatus={copyStatus}
            setCopyStatus={setCopyStatus}
          />
        ) : null}
      </div>
    </Slot.Root>
  );
});

type ImagePreviewZoomControlsProps = {
  canZoomIn: boolean;
  canZoomOut: boolean;
  percent: number;
  percentLabel: string;
  reportPercentStatus: boolean;
  resetLabel: string;
  zoomInLabel: string;
  zoomOutLabel: string;
  onReset: () => void;
  onZoomIn: () => void;
  onZoomOut: () => void;
  onWheel: (event: WheelEvent<HTMLElement>) => void;
};

type ImgBinding1Props = {
  alt: NonNullable<ZoomableImageProps>["alt"];
  imagePreviewZoom: number;
  actionSource: string;
  props: Omit<
    ZoomableImageProps,
    | "alt"
    | "className"
    | "downloadName"
    | "onContextMenu"
    | "src"
    | "wrapElement"
  >;
  isWheelZooming: boolean;
  closePreviewImage: () => void;
  hasImageActions: boolean;
  handleContextMenu: (event: MouseEvent<HTMLElement>) => void;
  onContextMenu: NonNullable<ZoomableImageProps>["onContextMenu"];
  isImagePreviewClosing: boolean;
  finishClosePreviewImage: () => void;
  handlePreviewImageWheel: (event: WheelEvent<HTMLElement>) => void;
};

type DivBinding3Props = {
  contextMenuPosition: { x: number; y: number; inZoomDialog: boolean };
  t: import("../../../i18n/runtime").TranslateFn;
  handleCopyImageAction: () => void;
  handleDownloadImage: () => void;
};

type ButtonBinding4Props = {
  t: import("../../../i18n/runtime").TranslateFn;
  closePreviewImage: () => void;
};

type DivBinding5Props = {
  contextMenuPosition: { x: number; y: number; inZoomDialog: boolean };
  t: import("../../../i18n/runtime").TranslateFn;
  handleCopyImageAction: () => void;
  handleDownloadImage: () => void;
};

type ImageCopyStatusToastBinding6Props = {
  copyStatus: ImageCopyStatus;
  setCopyStatus: NonNullable<{
    isImagePreviewClosing: boolean;
    hasImageActions: boolean;
    handlePreviewSurfaceClick: (event: MouseEvent<HTMLDivElement>) => void;
    finishClosePreviewImage: () => void;
    previewContent: import("react").JSX.Element | null;
    canZoomIn: boolean;
    canZoomOut: boolean;
    imagePreviewZoomPercent: number;
    t: import("../../../i18n/runtime").TranslateFn;
    copyStatus: ImageCopyStatus | null;
    resetPreviewImageZoom: () => void;
    zoomInPreviewImage: () => void;
    zoomOutPreviewImage: () => void;
    handlePreviewImageWheel: (event: WheelEvent<HTMLElement>) => void;
    actionButtons: import("react").JSX.Element | null;
    closePreviewImage: () => void;
    contextMenuPosition: { x: number; y: number; inZoomDialog: boolean } | null;
    handleCopyImageAction: () => void;
    handleDownloadImage: () => void;
    setCopyStatus: import("react").Dispatch<
      import("react").SetStateAction<ImageCopyStatus | null>
    >;
  }>["setCopyStatus"];
};

type ButtonBinding8Props = {
  itemRole: "menuitem";
  copyLabel: NonNullable<{
    copyLabel: string;
    downloadLabel: string;
    itemRole?: "menuitem";
    onCopy: () => void;
    onDownload: () => void;
  }>["copyLabel"];
  onCopy: NonNullable<{
    copyLabel: string;
    downloadLabel: string;
    itemRole?: "menuitem";
    onCopy: () => void;
    onDownload: () => void;
  }>["onCopy"];
};

type ButtonBinding9Props = {
  itemRole: "menuitem";
  downloadLabel: NonNullable<{
    copyLabel: string;
    downloadLabel: string;
    itemRole?: "menuitem";
    onCopy: () => void;
    onDownload: () => void;
  }>["downloadLabel"];
  onDownload: NonNullable<{
    copyLabel: string;
    downloadLabel: string;
    itemRole?: "menuitem";
    onCopy: () => void;
    onDownload: () => void;
  }>["onDownload"];
};

type DivBinding1Props = {
  isImagePreviewClosing: boolean;
  hasImageActions: boolean;
  handlePreviewSurfaceClick: (event: MouseEvent<HTMLDivElement>) => void;
  finishClosePreviewImage: () => void;
  previewContent: import("react").JSX.Element | null;
  canZoomIn: boolean;
  canZoomOut: boolean;
  imagePreviewZoomPercent: number;
  t: import("../../../i18n/runtime").TranslateFn;
  copyStatus: ImageCopyStatus | null;
  resetPreviewImageZoom: () => void;
  zoomInPreviewImage: () => void;
  zoomOutPreviewImage: () => void;
  handlePreviewImageWheel: (event: WheelEvent<HTMLElement>) => void;
  actionButtons: import("react").JSX.Element | null;
  closePreviewImage: () => void;
  contextMenuPosition: { x: number; y: number; inZoomDialog: boolean } | null;
  handleCopyImageAction: () => void;
  handleDownloadImage: () => void;
  setCopyStatus: import("react").Dispatch<
    import("react").SetStateAction<ImageCopyStatus | null>
  >;
};
