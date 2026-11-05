import { useSyncExternalStore } from "react";
import { useMemo } from "react";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import {
  Component,
  type CSSProperties,
  type JSX,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
  type RefObject,
  useCallback,
  useRef,
  useState
} from "react";
import { createPortal } from "react-dom";
import DOMPurify from "dompurify";
import mermaid from "mermaid";
import { Button, CopyIcon, RestoreIcon } from "@tutti-os/ui-system";
import { RotateCcwIcon, ZoomInIcon, ZoomOutIcon } from "lucide-react";
import { useTranslation } from "../i18n/index";
import { getPortalContainer } from "@tutti-os/ui-system/portal-container";
import { observer } from "mobx-react-lite";
import { observer as observeClass } from "mobx-react";
export const AgentMessageMermaid = observer(AgentMessageMermaidRender);
const MermaidFailure = observer(MermaidFailureRender);
const MermaidPlaceholder = observer(MermaidPlaceholderRender);
const MermaidSvg = observer(MermaidSvgRender);
const MermaidViewer = observer(MermaidViewerRender);

const MERMAID_MAX_TEXT_SIZE = 50_000;
const MERMAID_MAX_EDGES = 500;
const MERMAID_VIEWER_ZOOM_MIN = 0.5;
const MERMAID_VIEWER_ZOOM_MAX = 4;
const MERMAID_VIEWER_ZOOM_STEP = 0.25;

type MermaidTheme = "dark" | "default";

interface RenderedMermaidDiagram {
  source: string;
  svg: string;
  theme: MermaidTheme;
}

interface MermaidViewTransform {
  x: number;
  y: number;
  zoom: number;
}

interface MermaidDragState {
  originX: number;
  originY: number;
  pointerId: number;
  startX: number;
  startY: number;
}

const RESET_VIEW_TRANSFORM: MermaidViewTransform = {
  x: 0,
  y: 0,
  zoom: 1
};

function AgentMessageMermaidRender({
  source,
  streaming
}: {
  source: string;
  streaming: boolean;
}): JSX.Element {
  const { t } = useTranslation();
  const theme = useMermaidTheme();
  return (
    <MermaidRenderLifecycle
      copyLabel={t("agentHost.agentGui.copyCode")}
      expandLabel={t("agentHost.agentGui.mermaidExpand")}
      failureLabel={t("agentHost.agentGui.mermaidRenderFailed")}
      loadingLabel={t("agentHost.agentGui.mermaidLoading")}
      source={source}
      streaming={streaming}
      theme={theme}
    />
  );
}

interface MermaidRenderLifecycleProps {
  copyLabel: string;
  expandLabel: string;
  failureLabel: string;
  loadingLabel: string;
  source: string;
  streaming: boolean;
  theme: MermaidTheme;
}

interface MermaidRenderLifecycleState {
  failedRenderKey: string | null;
  isViewerOpen: boolean;
  rendered: RenderedMermaidDiagram | null;
}

class MermaidRenderLifecycle extends Component<
  MermaidRenderLifecycleProps,
  MermaidRenderLifecycleState
> {
  state: MermaidRenderLifecycleState = {
    failedRenderKey: null,
    isViewerOpen: false,
    rendered: null
  };

  private pendingRenderKey: string | null = null;
  private renderVersion = 0;

  componentDidMount(): void {
    this.ensureRendered();
  }

  componentDidUpdate(previousProps: MermaidRenderLifecycleProps): void {
    if (previousProps.source !== this.props.source && this.state.isViewerOpen) {
      this.setState({ isViewerOpen: false });
    }
    this.ensureRendered();
  }

  componentWillUnmount(): void {
    this.renderVersion += 1;
    this.pendingRenderKey = null;
  }

  private ensureRendered(): void {
    const { source, streaming, theme } = this.props;
    const { failedRenderKey, rendered } = this.state;
    const renderKey = `${theme}:${source}`;
    if (
      streaming ||
      source.length > MERMAID_MAX_TEXT_SIZE ||
      failedRenderKey === renderKey ||
      this.pendingRenderKey === renderKey ||
      (rendered?.source === source && rendered.theme === theme)
    ) {
      return;
    }

    const renderVersion = ++this.renderVersion;
    this.pendingRenderKey = renderKey;
    void renderMermaid(source, theme)
      .then((svg) => {
        if (this.renderVersion !== renderVersion) {
          return;
        }
        this.pendingRenderKey = null;
        this.setState({
          failedRenderKey: null,
          rendered: { source, svg, theme }
        });
      })
      .catch(() => {
        if (this.renderVersion !== renderVersion) {
          return;
        }
        this.pendingRenderKey = null;
        this.setState({ failedRenderKey: renderKey });
      });
  }

  private copySource = (): void => {
    if (!navigator.clipboard) {
      return;
    }
    void navigator.clipboard.writeText(this.props.source);
  };

  private closeViewer = (): void => {
    this.setState({ isViewerOpen: false });
  };

  private openViewer = (): void => {
    this.setState({ isViewerOpen: true });
  };

  private readonly handlePreviewKeyDown = (
    event: React.KeyboardEvent<HTMLDivElement>
  ): void => {
    if (event.key !== "Enter" && event.key !== " ") return;
    event.preventDefault();
    this.openViewer();
  };

  private readonly handleCopyClick = (
    event: React.MouseEvent<HTMLButtonElement>
  ): void => {
    event.stopPropagation();
    this.copySource();
  };

  render(): JSX.Element {
    const {
      copyLabel,
      expandLabel,
      failureLabel,
      loadingLabel,
      source,
      streaming,
      theme
    } = this.props;
    const { failedRenderKey, isViewerOpen, rendered } = this.state;
    const renderKey = `${theme}:${source}`;

    if (streaming) {
      return <MermaidPlaceholder label={loadingLabel} />;
    }

    if (
      source.length > MERMAID_MAX_TEXT_SIZE ||
      (failedRenderKey === renderKey && rendered?.source !== source)
    ) {
      return (
        <MermaidFailure
          copyLabel={copyLabel}
          label={failureLabel}
          onCopySource={this.copySource}
        />
      );
    }

    if (rendered?.source !== source || !rendered.svg) {
      return <MermaidPlaceholder label={loadingLabel} />;
    }

    const isRefreshingTheme = rendered.theme !== theme;
    return (
      <>
        <div
          className="agent-markdown-mermaid"
          data-agent-mermaid-state={isRefreshingTheme ? "refreshing" : "ready"}
        >
          {!isViewerOpen ? (
            <div
              aria-label={expandLabel}
              className="agent-markdown-mermaid__preview nodrag tsh-desktop-no-drag"
              data-testid="agent-mermaid-preview"
              role="button"
              tabIndex={0}
              onClick={this.openViewer}
              onKeyDown={this.handlePreviewKeyDown}
            >
              <MermaidSvg svg={rendered.svg} />
            </div>
          ) : (
            <div
              aria-hidden="true"
              className="agent-markdown-mermaid__preview-spacer"
            />
          )}
          <button
            type="button"
            aria-label={copyLabel}
            className="agent-markdown-mermaid__copy"
            title={copyLabel}
            onClick={this.handleCopyClick}
          >
            <CopyIcon aria-hidden="true" className="size-3.5" />
          </button>
        </div>
        {isViewerOpen
          ? createPortal(
              <MermaidViewer svg={rendered.svg} onClose={this.closeViewer} />,
              getPortalContainer()
            )
          : null}
      </>
    );
  }
}
observeClass(MermaidRenderLifecycle);

function MermaidFailureRender({
  copyLabel,
  label,
  onCopySource
}: {
  copyLabel: string;
  label: string;
  onCopySource: () => void;
}): JSX.Element {
  return (
    <div
      className="agent-markdown-mermaid agent-markdown-mermaid--failed"
      data-agent-mermaid-state="failed"
      data-testid="agent-mermaid-failure"
      role="status"
    >
      <span>{label}</span>
      <button
        type="button"
        aria-label={copyLabel}
        className="agent-markdown-mermaid__failure-copy"
        title={copyLabel}
        onClick={onCopySource}
      >
        <CopyIcon aria-hidden="true" className="size-3.5" />
        <span>{copyLabel}</span>
      </button>
    </div>
  );
}

function MermaidPlaceholderRender({ label }: { label: string }): JSX.Element {
  return (
    <div
      aria-label={label}
      aria-live="polite"
      aria-busy="true"
      className="agent-markdown-mermaid agent-markdown-mermaid--loading"
      data-agent-mermaid-state="loading"
      data-testid="agent-mermaid-placeholder"
      role="status"
    >
      <div className="agent-markdown-mermaid__placeholder" aria-hidden="true">
        <span className="agent-markdown-mermaid__placeholder-node agent-markdown-mermaid__placeholder-node--start" />
        <span className="agent-markdown-mermaid__placeholder-edge agent-markdown-mermaid__placeholder-edge--first" />
        <span className="agent-markdown-mermaid__placeholder-node agent-markdown-mermaid__placeholder-node--middle" />
        <span className="agent-markdown-mermaid__placeholder-edge agent-markdown-mermaid__placeholder-edge--second" />
        <span className="agent-markdown-mermaid__placeholder-node agent-markdown-mermaid__placeholder-node--end" />
      </div>
    </div>
  );
}

function MermaidSvgRender({ svg }: { svg: string }): JSX.Element {
  const dangerouslySetInnerHTMLProp1 = useMemo<
    NonNullable<React.ComponentProps<"div">["dangerouslySetInnerHTML"]>
  >(() => ({ __html: svg }), [svg]);
  return (
    <div
      className="agent-markdown-mermaid__svg"
      data-testid="agent-mermaid-svg"
      // Mermaid runs in strict mode and sanitizes its generated SVG.
      dangerouslySetInnerHTML={dangerouslySetInnerHTMLProp1}
    />
  );
}

class MermaidViewerLifecycle extends Component<{
  children: ReactNode;
  focusTargetRef: RefObject<HTMLElement | null>;
  onEscape: () => void;
  onSpaceDown: () => void;
  onSpaceUp: () => void;
  onUnmount: () => void;
  onWheel: (event: WheelEvent) => void;
  wheelTargetRef: RefObject<HTMLElement | null>;
}> {
  componentDidMount(): void {
    window.addEventListener("keydown", this.handleKeyDown, true);
    window.addEventListener("keyup", this.handleKeyUp, true);
    window.addEventListener("blur", this.handleWindowBlur);
    this.props.wheelTargetRef.current?.addEventListener(
      "wheel",
      this.handleWheel,
      { passive: false }
    );
    this.props.focusTargetRef.current?.focus();
  }

  componentWillUnmount(): void {
    window.removeEventListener("keydown", this.handleKeyDown, true);
    window.removeEventListener("keyup", this.handleKeyUp, true);
    window.removeEventListener("blur", this.handleWindowBlur);
    this.props.wheelTargetRef.current?.removeEventListener(
      "wheel",
      this.handleWheel
    );
    this.props.onUnmount();
  }

  private handleKeyDown = (event: KeyboardEvent): void => {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      this.props.onEscape();
      return;
    }
    if (!isSpaceKey(event) || isInteractiveEventTarget(event.target)) {
      return;
    }
    event.preventDefault();
    this.props.onSpaceDown();
  };

  private handleKeyUp = (event: KeyboardEvent): void => {
    if (!isSpaceKey(event)) {
      return;
    }
    event.preventDefault();
    this.props.onSpaceUp();
  };

  private handleWindowBlur = (): void => {
    this.props.onSpaceUp();
  };

  private handleWheel = (event: WheelEvent): void => {
    this.props.onWheel(event);
  };

  render(): ReactNode {
    return this.props.children;
  }
}
observeClass(MermaidViewerLifecycle);

function MermaidViewerRender({
  onClose,
  svg
}: {
  onClose: () => void;
  svg: string;
}): JSX.Element {
  const { t } = useTranslation();
  const rootRef = useRef<HTMLDivElement>(null);
  const stageRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<MermaidDragState | null>(null);
  const viewRef = useRef<MermaidViewTransform>(RESET_VIEW_TRANSFORM);
  const viewAnimationFrameRef = useRef<number | null>(null);
  const [isSpacePressed, setIsSpacePressed] = useState(false);
  const [isDragging, setIsDragging] = useState(false);
  const [view, setView] = useState<MermaidViewTransform>(RESET_VIEW_TRANSFORM);
  const zoomPercent = Math.round(view.zoom * 100);

  const cancelPendingViewCommit = useCallback((): void => {
    if (viewAnimationFrameRef.current === null) {
      return;
    }
    cancelAnimationFrame(viewAnimationFrameRef.current);
    viewAnimationFrameRef.current = null;
  }, []);
  const cancelPendingViewCommitCallback = usePersistCallback(
    cancelPendingViewCommit
  );

  const scheduleViewCommit = useCallback((): void => {
    if (viewAnimationFrameRef.current !== null) {
      return;
    }
    viewAnimationFrameRef.current = requestAnimationFrame(() => {
      viewAnimationFrameRef.current = null;
      setView(viewRef.current);
    });
  }, []);

  const commitViewImmediately = useCallback(
    (nextView: MermaidViewTransform): void => {
      cancelPendingViewCommit();
      viewRef.current = nextView;
      setView(nextView);
    },
    [cancelPendingViewCommit]
  );

  const stopDragging = (): void => {
    const drag = dragRef.current;
    if (drag && stageRef.current?.hasPointerCapture?.(drag.pointerId)) {
      stageRef.current.releasePointerCapture?.(drag.pointerId);
    }
    dragRef.current = null;
    setIsDragging(false);
  };
  const stopDraggingCallback = usePersistCallback(stopDragging);

  const releaseSpace = (): void => {
    setIsSpacePressed(false);
    stopDragging();
  };
  const releaseSpaceCallback = usePersistCallback(releaseSpace);

  const zoomAt = (factor: number, clientX?: number, clientY?: number): void => {
    const current = viewRef.current;
    const nextZoom = clampMermaidZoom(current.zoom * factor);
    if (nextZoom === current.zoom) {
      return;
    }
    const bounds = stageRef.current?.getBoundingClientRect();
    const anchorX =
      bounds && clientX !== undefined
        ? clientX - (bounds.left + bounds.width / 2)
        : 0;
    const anchorY =
      bounds && clientY !== undefined
        ? clientY - (bounds.top + bounds.height / 2)
        : 0;
    viewRef.current = zoomMermaidViewAt(current, nextZoom, anchorX, anchorY);
    scheduleViewCommit();
  };
  const zoomByStep = (step: number): void => {
    const current = viewRef.current;
    commitViewImmediately(
      zoomMermaidViewAt(current, clampMermaidZoom(current.zoom + step), 0, 0)
    );
  };

  const handleWheel = (event: WheelEvent): void => {
    event.preventDefault();
    event.stopPropagation();
    if (event.deltaY === 0) {
      return;
    }
    zoomAt(
      Math.pow(2, resolveWheelZoomDelta(event)),
      event.clientX,
      event.clientY
    );
  };
  const handleWheelCallback = usePersistCallback(handleWheel);

  const handlePointerDown = (
    event: ReactPointerEvent<HTMLDivElement>
  ): void => {
    if (!isSpacePressed || event.button !== 0) {
      return;
    }
    event.preventDefault();
    event.currentTarget.setPointerCapture?.(event.pointerId);
    const current = viewRef.current;
    dragRef.current = {
      originX: current.x,
      originY: current.y,
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY
    };
    setIsDragging(true);
  };
  const handlePointerDownCallback = usePersistCallback(handlePointerDown);

  const handlePointerMove = (
    event: ReactPointerEvent<HTMLDivElement>
  ): void => {
    const drag = dragRef.current;
    if (
      !drag ||
      (typeof drag.pointerId === "number" &&
        typeof event.pointerId === "number" &&
        drag.pointerId !== event.pointerId)
    ) {
      return;
    }
    event.preventDefault();
    viewRef.current = resolveDraggedMermaidView(
      viewRef.current,
      drag,
      event.clientX,
      event.clientY
    );
    scheduleViewCommit();
  };
  const handlePointerMoveCallback = usePersistCallback(handlePointerMove);

  const transformStyleTransformValue1 = `translate3d(${formatTransformNumber(view.x)}px, ${formatTransformNumber(view.y)}px, 0) scale(${formatTransformNumber(view.zoom)})`;
  const transformStyle: CSSProperties = useMemo<CSSProperties>(
    () => ({ transform: transformStyleTransformValue1 }),
    [transformStyleTransformValue1]
  );

  const onSpaceDownProp2 = usePersistCallback<
    NonNullable<
      React.ComponentProps<typeof MermaidViewerLifecycle>["onSpaceDown"]
    >
  >(() => setIsSpacePressed(true));
  const onBlurCaptureProp3 = usePersistCallback<
    NonNullable<React.ComponentProps<"div">["onBlurCapture"]>
  >((event) => {
    if (event.relatedTarget instanceof Node) {
      if (event.currentTarget.contains(event.relatedTarget)) {
        return;
      }
    }
    releaseSpace();
  });
  const onClickProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => zoomByStep(-MERMAID_VIEWER_ZOOM_STEP));
  const onClickProp5 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => commitViewImmediately(RESET_VIEW_TRANSFORM));
  const onClickProp6 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => zoomByStep(MERMAID_VIEWER_ZOOM_STEP));
  return (
    <MermaidViewerLifecycle
      focusTargetRef={rootRef}
      onEscape={onClose}
      onSpaceDown={onSpaceDownProp2}
      onSpaceUp={releaseSpaceCallback}
      onUnmount={cancelPendingViewCommitCallback}
      onWheel={handleWheelCallback}
      wheelTargetRef={rootRef}
    >
      <div
        ref={rootRef}
        aria-label={t("agentHost.agentGui.mermaidViewer")}
        aria-modal="true"
        className="agent-mermaid-viewer nodrag tsh-desktop-no-drag"
        data-agent-mermaid-dragging={isDragging ? "true" : undefined}
        data-agent-mermaid-space-pressed={isSpacePressed ? "true" : undefined}
        role="dialog"
        tabIndex={-1}
        onBlurCapture={onBlurCaptureProp3}
      >
        <div className="agent-mermaid-viewer__backdrop" />
        <div
          ref={stageRef}
          className="agent-mermaid-viewer__stage"
          data-testid="agent-mermaid-viewer-stage"
          tabIndex={-1}
          onPointerCancel={stopDraggingCallback}
          onPointerDown={handlePointerDownCallback}
          onPointerMove={handlePointerMoveCallback}
          onPointerUp={stopDraggingCallback}
        >
          <div
            className="agent-mermaid-viewer__canvas"
            data-testid="agent-mermaid-viewer-canvas"
            style={transformStyle}
          >
            <MermaidSvg svg={svg} />
          </div>
        </div>
        <div className="agent-mermaid-viewer__hint">
          {t("agentHost.agentGui.mermaidPanHint")}
        </div>
        <div className="tsh-zoom-dialog__zoom-controls agent-mermaid-viewer__zoom-controls">
          <button
            type="button"
            aria-label={t("agentHost.agentGui.mermaidZoomOut")}
            disabled={view.zoom <= MERMAID_VIEWER_ZOOM_MIN}
            title={t("agentHost.agentGui.mermaidZoomOut")}
            onClick={onClickProp4}
          >
            <ZoomOutIcon aria-hidden="true" className="size-4" />
          </button>
          <span
            aria-label={t("agentHost.agentGui.mermaidZoomPercent", {
              percent: zoomPercent
            })}
            role="status"
          >
            {zoomPercent}%
          </span>
          <button
            type="button"
            aria-label={t("agentHost.agentGui.mermaidResetZoom")}
            title={t("agentHost.agentGui.mermaidResetZoom")}
            onClick={onClickProp5}
          >
            <RotateCcwIcon aria-hidden="true" className="size-4" />
          </button>
          <button
            type="button"
            aria-label={t("agentHost.agentGui.mermaidZoomIn")}
            disabled={view.zoom >= MERMAID_VIEWER_ZOOM_MAX}
            title={t("agentHost.agentGui.mermaidZoomIn")}
            onClick={onClickProp6}
          >
            <ZoomInIcon aria-hidden="true" className="size-4" />
          </button>
        </div>
        <div className="tsh-zoom-dialog__toolbar-actions">
          <Button
            aria-label={t("agentHost.agentGui.mermaidCloseViewer")}
            className="tsh-zoom-dialog__icon-button"
            size="icon"
            title={t("agentHost.agentGui.mermaidCloseViewer")}
            variant="chrome"
            onClick={onClose}
          >
            <RestoreIcon aria-hidden="true" className="size-4" />
          </Button>
        </div>
      </div>
    </MermaidViewerLifecycle>
  );
}

function useMermaidTheme(): MermaidTheme {
  return useSyncExternalStore(
    subscribeMermaidTheme,
    resolveMermaidTheme,
    defaultMermaidTheme
  );
}

function defaultMermaidTheme(): MermaidTheme {
  return "default";
}

function subscribeMermaidTheme(changed: () => void): () => void {
  const observer = new MutationObserver(changed);
  observer.observe(getPortalContainer(), {
    attributes: true,
    attributeFilter: ["data-theme"]
  });
  const media =
    typeof window.matchMedia === "function"
      ? window.matchMedia("(prefers-color-scheme: dark)")
      : null;
  media?.addEventListener?.("change", changed);
  return () => {
    observer.disconnect();
    media?.removeEventListener?.("change", changed);
  };
}

function resolveMermaidTheme(): MermaidTheme {
  const documentTheme = getPortalContainer().dataset.theme;
  if (documentTheme === "dark") {
    return "dark";
  }
  if (documentTheme === "light") {
    return "default";
  }
  return typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "default";
}

function renderMermaid(source: string, theme: MermaidTheme): Promise<string> {
  mermaid.initialize({
    darkMode: theme === "dark",
    htmlLabels: false,
    maxEdges: MERMAID_MAX_EDGES,
    maxTextSize: MERMAID_MAX_TEXT_SIZE,
    secure: [
      "secure",
      "securityLevel",
      "startOnLoad",
      "maxTextSize",
      "suppressErrorRendering",
      "maxEdges",
      "htmlLabels"
    ],
    securityLevel: "strict",
    startOnLoad: false,
    suppressErrorRendering: true,
    theme
  });
  return mermaid
    .render(`agent-mermaid-${crypto.randomUUID()}`, source)
    .then((result) => sanitizeMermaidSvg(result.svg));
}

export function sanitizeMermaidSvg(svg: string): string {
  return String(
    DOMPurify.sanitize(svg, {
      FORBID_TAGS: ["embed", "iframe", "object", "script"],
      USE_PROFILES: {
        html: true,
        svg: true,
        svgFilters: true
      }
    })
  );
}

function resolveWheelZoomDelta(
  event: Pick<WheelEvent, "ctrlKey" | "deltaMode" | "deltaY">
): number {
  return (
    -event.deltaY *
    (event.deltaMode === 1 ? 0.05 : event.deltaMode ? 1 : 0.002) *
    (event.ctrlKey ? 10 : 1)
  );
}

function clampMermaidZoom(value: number): number {
  return Math.min(
    MERMAID_VIEWER_ZOOM_MAX,
    Math.max(MERMAID_VIEWER_ZOOM_MIN, value)
  );
}

function zoomMermaidViewAt(
  current: MermaidViewTransform,
  nextZoom: number,
  anchorX: number,
  anchorY: number
): MermaidViewTransform {
  if (nextZoom === current.zoom) {
    return current;
  }
  const ratio = nextZoom / current.zoom;
  return {
    zoom: nextZoom,
    x: anchorX - (anchorX - current.x) * ratio,
    y: anchorY - (anchorY - current.y) * ratio
  };
}

export function resolveDraggedMermaidView(
  current: MermaidViewTransform,
  drag: Pick<MermaidDragState, "originX" | "originY" | "startX" | "startY">,
  clientX: number,
  clientY: number
): MermaidViewTransform {
  return {
    ...current,
    x: drag.originX + clientX - drag.startX,
    y: drag.originY + clientY - drag.startY
  };
}

function formatTransformNumber(value: number): string {
  return Number(value.toFixed(3)).toString();
}

function isSpaceKey(event: { code: string; key: string }): boolean {
  return event.code === "Space" || event.key === " ";
}

function isInteractiveEventTarget(target: EventTarget | null): boolean {
  return (
    target instanceof Element &&
    Boolean(
      target.closest(
        "button, input, textarea, select, a, [contenteditable='true']"
      )
    )
  );
}

export const agentMessageMermaidLimits = {
  maxEdges: MERMAID_MAX_EDGES,
  maxTextSize: MERMAID_MAX_TEXT_SIZE
} as const;
