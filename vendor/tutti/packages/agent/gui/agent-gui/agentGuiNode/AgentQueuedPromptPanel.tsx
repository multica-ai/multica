import {
  Component,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type PointerEvent
} from "react";
import { ChevronRight } from "lucide-react";
import { SideQueueAction } from "@multica/tutti-agent-gui-host/side-entry";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
  useTextOverflow
} from "@tutti-os/ui-system";
import { extractPlainTextFromContent } from "@tutti-os/ui-rich-text";
import {
  AgentMessageMarkdown,
  type AgentMessageMarkdownWorkspaceAppIcon
} from "../../shared/AgentMessageMarkdown";
import type { AgentPromptContentBlock } from "../../shared/contracts/dto/agentSession";
import type {
  AgentGUIQueueStatus,
  AgentGUIQueuedPromptVM
} from "./model/agentGuiNodeTypes";
import {
  agentPromptContentDisplayText,
  agentPromptContentImageBlocks
} from "./model/agentComposerDraft";
import { ZoomableImage } from "../../app/renderer/components/ZoomableImage";
import { CanvasNodeGhostIconButton } from "../shared/CanvasNodeGhostIconButton";
import {
  CanvasNodeGuideLinedIcon,
  CanvasNodeMoreLinedIcon,
  CanvasNodeTrashLinedIcon
} from "../shared/canvasNodeChromeIcons";
import styles from "./AgentGUINode.styles";
import {
  QueuedPromptImageLoadOwner,
  queuedPromptImageHasSafeRemoteUrl,
  queuedPromptImageLoadRequestIdentity
} from "./queuedPromptImageLoadOwner";
import {
  useOptionalAgentGUIRuntime,
  type AgentGUIRuntime
} from "../../agentActivityRuntime";

const EMPTY_WORKSPACE_APP_ICONS: readonly AgentMessageMarkdownWorkspaceAppIcon[] =
  [];
const QUEUED_PROMPT_OVERFLOW_DESCENDANTS =
  '[data-workspace-agent-markdown="true"], .tsh-agent-object-token__main';

type QueuedPromptImageBlock = AgentPromptContentBlock & {
  type: "image";
  mimeType: "image/png" | "image/jpeg" | "image/webp";
  attachmentId?: string;
  data?: string;
  url?: string;
  path?: string;
};

interface AgentQueuedPromptPanelProps {
  queueStatus?: AgentGUIQueueStatus;
  queuedPrompts: readonly AgentGUIQueuedPromptVM[];
  drainingQueuedPromptId: string | null;
  labels: {
    queuedLabel: string;
    queuePausedByUserLabel: string;
    sendQueuedPromptNext: string;
    editQueuedPrompt: string;
    deleteQueuedPrompt: string;
    queuedPromptMoreActions: string;
  };
  onSendQueuedPromptNext: (queuedPromptId: string) => void;
  onRemoveQueuedPrompt: (queuedPromptId: string) => void;
  onEditQueuedPrompt: (queuedPromptId: string) => void;
  agentSessionId?: string | null;
  onLinkClick?: (href: string) => void;
  workspaceId?: string | null;
  workspaceAppIcons?: readonly AgentMessageMarkdownWorkspaceAppIcon[];
}

function queuedPromptImages(
  queuedPrompt: AgentGUIQueuedPromptVM
): QueuedPromptImageBlock[] {
  return agentPromptContentImageBlocks(queuedPrompt.content);
}

function queuedPromptImageDataUrl(
  image: QueuedPromptImageBlock
): string | null {
  const data = image.data?.trim() ?? "";
  const mimeType = image.mimeType.trim();
  if (!data || !mimeType) {
    return null;
  }
  return data.startsWith("data:") ? data : `data:${mimeType};base64,${data}`;
}

function queuedPromptImageImmediateSource(
  image: QueuedPromptImageBlock
): string | null {
  return queuedPromptImageDataUrl(image);
}

function queuedPromptImageKey(
  queuedPrompt: AgentGUIQueuedPromptVM,
  image: QueuedPromptImageBlock,
  index: number
): string {
  return [
    queuedPrompt.id,
    index,
    image.attachmentId?.trim() ?? "",
    image.path?.trim() ?? "",
    image.url?.trim() ?? "",
    image.data?.trim() ?? "",
    image.name?.trim() ?? "",
    image.mimeType
  ].join(":");
}

interface AgentQueuedPromptImageProps {
  agentSessionId?: string | null;
  image: QueuedPromptImageBlock;
  imageKey: string;
  runtime: AgentGUIRuntime | null;
  workspaceId?: string | null;
}

interface AgentQueuedPromptImageState {
  requestIdentity: string;
  source: string | null;
}

class AgentQueuedPromptImage extends Component<
  AgentQueuedPromptImageProps,
  AgentQueuedPromptImageState
> {
  private loadOwner: QueuedPromptImageLoadOwner | null = null;

  state: AgentQueuedPromptImageState = {
    requestIdentity: "",
    source: null
  };

  componentDidMount(): void {
    this.syncLoadOwner();
  }

  componentDidUpdate(previousProps: AgentQueuedPromptImageProps): void {
    if (
      previousProps.runtime !== this.props.runtime ||
      this.requestIdentity(previousProps) !== this.requestIdentity(this.props)
    ) {
      this.syncLoadOwner();
    }
  }

  componentWillUnmount(): void {
    this.loadOwner?.dispose();
    this.loadOwner = null;
  }

  render(): React.JSX.Element | null {
    const source =
      queuedPromptImageImmediateSource(this.props.image) ??
      (this.state.requestIdentity === this.requestIdentity(this.props)
        ? this.state.source
        : null);

    return source ? (
      <ZoomableImage
        alt={this.props.image.name?.trim() || ""}
        className={styles.composerQueuedPromptImage}
        draggable={false}
        src={source}
        wrapElement="span"
      />
    ) : null;
  }

  private requestIdentity(props: AgentQueuedPromptImageProps): string {
    return queuedPromptImageLoadRequestIdentity({
      agentSessionId: props.agentSessionId?.trim() ?? "",
      attachmentId: props.image.attachmentId?.trim() ?? "",
      imageKey: props.imageKey,
      mimeType: props.image.mimeType,
      name: props.image.name?.trim() ?? "",
      path: props.image.path?.trim() ?? "",
      remoteUrl: props.image.url?.trim() ?? "",
      workspaceId: props.workspaceId?.trim() ?? ""
    });
  }

  private syncLoadOwner(): void {
    this.loadOwner?.dispose();
    this.loadOwner = null;
    const { image, runtime } = this.props;
    const workspaceId = this.props.workspaceId?.trim() ?? "";
    const agentSessionId = this.props.agentSessionId?.trim() ?? "";
    const attachmentId = image.attachmentId?.trim() ?? "";
    const path = image.path?.trim() ?? "";
    const remoteUrl = image.url?.trim() ?? "";
    const requestIdentity = this.requestIdentity(this.props);
    if (
      queuedPromptImageImmediateSource(image) ||
      !runtime ||
      !workspaceId ||
      (!attachmentId && !path) ||
      queuedPromptImageHasSafeRemoteUrl(remoteUrl) ||
      (!runtime.readSessionAttachment && !runtime.readPromptAsset)
    ) {
      this.setState({ requestIdentity, source: null });
      return;
    }
    this.setState({ requestIdentity, source: null });
    this.loadOwner = new QueuedPromptImageLoadOwner(
      {
        agentSessionId,
        attachmentId,
        imageKey: this.props.imageKey,
        mimeType: image.mimeType,
        name: image.name?.trim() ?? "",
        path,
        remoteUrl,
        runtime,
        workspaceId
      },
      (source) => this.setState({ requestIdentity, source })
    );
    this.loadOwner.start();
  }
}

/**
 * Text shown for a queued prompt. Falls back to the display prompt so a
 * pasted-text-only queue entry (whose content is a structured pasted-text file
 * block with no text) still renders its reference instead of appearing blank.
 */
function queuedPromptDisplayText(queuedPrompt: AgentGUIQueuedPromptVM): string {
  const displayPrompt = queuedPrompt.displayPrompt?.trim();
  if (displayPrompt) {
    return displayPrompt;
  }
  const prompt = agentPromptContentDisplayText(queuedPrompt.content);
  if (prompt) {
    return prompt;
  }
  return "";
}

function queuedPromptTitle(queuedPrompt: AgentGUIQueuedPromptVM): string {
  const prompt = queuedPromptDisplayText(queuedPrompt);
  if (prompt) {
    return extractPlainTextFromContent(prompt);
  }
  return queuedPromptImages(queuedPrompt)
    .map((image) => image.name?.trim() ?? "")
    .filter(Boolean)
    .join(", ");
}

interface AgentQueuedPromptTextProps {
  displayText: string;
  measurementRef?: React.RefObject<HTMLDivElement | null>;
  onLinkClick?: (href: string) => void;
  title: string;
  workspaceAppIcons: readonly AgentMessageMarkdownWorkspaceAppIcon[];
}

function AgentQueuedPromptText({
  displayText,
  measurementRef,
  onLinkClick,
  title,
  workspaceAppIcons
}: AgentQueuedPromptTextProps): React.JSX.Element {
  const { ref: overflowRef, overflowing } = useTextOverflow<HTMLDivElement>(
    displayText,
    QUEUED_PROMPT_OVERFLOW_DESCENDANTS
  );
  const content = (
    <div
      ref={(element) => {
        overflowRef.current = element;
        if (measurementRef) measurementRef.current = element;
      }}
      className={styles.composerQueuedPromptText}
      data-overflowing={overflowing ? "true" : "false"}
      onClick={(event) => {
        if (event.target instanceof Element && event.target.closest("a")) {
          event.stopPropagation();
        }
      }}
    >
      <AgentMessageMarkdown
        content={displayText}
        className="agent-gui-node__composer-queued-prompt-markdown"
        inline
        onLinkClick={onLinkClick}
        workspaceAppIcons={workspaceAppIcons}
      />
    </div>
  );
  return (
    <TooltipProvider delayDuration={200}>
      <Tooltip>
        <TooltipTrigger asChild>{content}</TooltipTrigger>
        {overflowing && title ? (
          <TooltipContent className="max-w-[min(520px,calc(100vw-32px))] whitespace-pre-wrap text-left [overflow-wrap:anywhere]">
            {title}
          </TooltipContent>
        ) : null}
      </Tooltip>
    </TooltipProvider>
  );
}

export function AgentQueuedPromptPanel({
  queueStatus = "active",
  queuedPrompts,
  drainingQueuedPromptId,
  labels,
  onSendQueuedPromptNext,
  onRemoveQueuedPrompt,
  onEditQueuedPrompt,
  agentSessionId = null,
  onLinkClick,
  workspaceId = null,
  workspaceAppIcons = EMPTY_WORKSPACE_APP_ICONS
}: AgentQueuedPromptPanelProps): React.JSX.Element {
  "use memo";
  const runtime = useOptionalAgentGUIRuntime();
  const [isExpanded, setIsExpanded] = useState(false);
  const singlePromptTextRef = useRef<HTMLDivElement | null>(null);
  const queuedPromptListRef = useRef<HTMLDivElement | null>(null);
  const pointerHandledEditPromptIdRef = useRef<string | null>(null);
  const [isSinglePromptOverflowing, setIsSinglePromptOverflowing] =
    useState(false);
  const [expandedListMaxHeightPx, setExpandedListMaxHeightPx] = useState(280);
  const singlePromptHasImages =
    queuedPrompts.length === 1 &&
    queuedPromptImages(queuedPrompts[0]!).length > 0;
  const canExpand =
    queuedPrompts.length > 1 ||
    singlePromptHasImages ||
    isSinglePromptOverflowing;
  const panelStyle = {
    "--agent-gui-queued-prompt-expanded-height": `${expandedListMaxHeightPx}px`
  } as CSSProperties &
    Record<"--agent-gui-queued-prompt-expanded-height", string>;
  const toggleExpanded = (): void => {
    if (!canExpand) {
      return;
    }
    setIsExpanded((current) => !current);
  };
  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>): void => {
    if (!canExpand) {
      return;
    }
    if (event.key !== "Enter" && event.key !== " ") {
      return;
    }
    event.preventDefault();
    toggleExpanded();
  };
  const editQueuedPrompt = (queuedPromptId: string): void => {
    pointerHandledEditPromptIdRef.current = null;
    onEditQueuedPrompt(queuedPromptId);
  };
  const handleEditQueuedPromptPointerDown = (
    event: PointerEvent,
    queuedPromptId: string
  ): void => {
    if (event.button !== 0 || event.ctrlKey) {
      return;
    }
    pointerHandledEditPromptIdRef.current = queuedPromptId;
    onEditQueuedPrompt(queuedPromptId);
  };
  const handleEditQueuedPromptSelect = (queuedPromptId: string): void => {
    if (pointerHandledEditPromptIdRef.current === queuedPromptId) {
      pointerHandledEditPromptIdRef.current = null;
      return;
    }
    editQueuedPrompt(queuedPromptId);
  };

  useLayoutEffect(() => {
    if (queuedPrompts.length !== 1) {
      setIsSinglePromptOverflowing(false);
      return;
    }

    const element = singlePromptTextRef.current;
    if (!element) {
      setIsSinglePromptOverflowing(false);
      return;
    }

    const measure = (): void => {
      setIsSinglePromptOverflowing(
        element.scrollWidth > element.clientWidth + 1
      );
    };

    measure();
    const resizeObserver =
      typeof ResizeObserver === "undefined"
        ? null
        : new ResizeObserver(measure);
    resizeObserver?.observe(element);
    return () => {
      resizeObserver?.disconnect();
    };
  }, [queuedPrompts]);

  useLayoutEffect(() => {
    if (!canExpand && isExpanded) {
      setIsExpanded(false);
    }
  }, [canExpand, isExpanded]);

  useLayoutEffect(() => {
    const element = queuedPromptListRef.current;
    if (!element) {
      return;
    }

    const measure = (): void => {
      const viewportCap =
        typeof window === "undefined"
          ? 280
          : Math.round(window.innerHeight * 0.38);
      setExpandedListMaxHeightPx(Math.max(32, Math.min(280, viewportCap)));
    };

    measure();
    const resizeObserver =
      typeof ResizeObserver === "undefined"
        ? null
        : new ResizeObserver(measure);
    resizeObserver?.observe(element);
    window.addEventListener("resize", measure);
    return () => {
      resizeObserver?.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [queuedPrompts]);

  return (
    <div
      className={styles.composerQueuedPromptPanel}
      data-expanded={isExpanded ? "true" : "false"}
      data-expandable={canExpand ? "true" : "false"}
      data-queue-status={queueStatus}
      style={panelStyle}
      tabIndex={canExpand ? 0 : -1}
      onClick={toggleExpanded}
      onKeyDown={handleKeyDown}
    >
      <div className={styles.composerQueuedPromptHeader}>
        <span className={styles.composerQueuedPromptLabel}>
          {queueStatus === "paused_by_user"
            ? labels.queuePausedByUserLabel
            : labels.queuedLabel}
        </span>
        <span className={styles.composerQueuedPromptCount}>
          {queuedPrompts.length}
        </span>
        {canExpand ? (
          <ChevronRight
            aria-hidden="true"
            className={styles.composerQueuedPromptExpandCue}
            data-testid="agent-gui-composer-queued-prompt-expand-cue"
            size={16}
            strokeWidth={2}
          />
        ) : null}
      </div>
      <div
        ref={queuedPromptListRef}
        className={styles.composerQueuedPromptList}
      >
        {queuedPrompts.map((queuedPrompt) => {
          const isDraining = queuedPrompt.id === drainingQueuedPromptId;
          const images = queuedPromptImages(queuedPrompt);
          const displayText = queuedPromptDisplayText(queuedPrompt);
          const title = queuedPromptTitle(queuedPrompt);
          return (
            <div
              key={queuedPrompt.id}
              className={styles.composerQueuedPromptRow}
              data-testid={`agent-gui-composer-queued-prompt-${queuedPrompt.id}`}
              data-draining={isDraining ? "true" : "false"}
            >
              <div className={styles.composerQueuedPromptMain}>
                <div className={styles.composerQueuedPromptBody}>
                  {displayText ? (
                    <AgentQueuedPromptText
                      displayText={displayText}
                      measurementRef={
                        queuedPrompts.length === 1
                          ? singlePromptTextRef
                          : undefined
                      }
                      onLinkClick={onLinkClick}
                      title={title}
                      workspaceAppIcons={workspaceAppIcons}
                    />
                  ) : null}
                  {images.length > 0 ? (
                    <div className={styles.composerQueuedPromptImages}>
                      {images.slice(0, 3).map((image, index) => {
                        const imageKey = queuedPromptImageKey(
                          queuedPrompt,
                          image,
                          index
                        );
                        return (
                          <AgentQueuedPromptImage
                            key={imageKey}
                            agentSessionId={agentSessionId}
                            image={image}
                            imageKey={imageKey}
                            runtime={runtime}
                            workspaceId={workspaceId}
                          />
                        );
                      })}
                    </div>
                  ) : null}
                </div>
              </div>
              <div className={styles.composerQueuedPromptActions}>
                <TooltipProvider delayDuration={200}>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <CanvasNodeGhostIconButton
                        aria-label={labels.sendQueuedPromptNext}
                        disabled={isDraining}
                        onClick={() => onSendQueuedPromptNext(queuedPrompt.id)}
                      >
                        <CanvasNodeGuideLinedIcon aria-hidden="true" />
                      </CanvasNodeGhostIconButton>
                    </TooltipTrigger>
                    <TooltipContent>
                      {labels.sendQueuedPromptNext}
                    </TooltipContent>
                  </Tooltip>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <CanvasNodeGhostIconButton
                        aria-label={labels.deleteQueuedPrompt}
                        disabled={isDraining}
                        onClick={() => onRemoveQueuedPrompt(queuedPrompt.id)}
                      >
                        <CanvasNodeTrashLinedIcon aria-hidden="true" />
                      </CanvasNodeGhostIconButton>
                    </TooltipTrigger>
                    <TooltipContent>{labels.deleteQueuedPrompt}</TooltipContent>
                  </Tooltip>
                  <DropdownMenu>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <DropdownMenuTrigger asChild>
                          <CanvasNodeGhostIconButton
                            aria-label={labels.queuedPromptMoreActions}
                            disabled={isDraining}
                            stopsEventPropagation={false}
                            onClick={(event) => event.stopPropagation()}
                          >
                            <CanvasNodeMoreLinedIcon aria-hidden="true" />
                          </CanvasNodeGhostIconButton>
                        </DropdownMenuTrigger>
                      </TooltipTrigger>
                      <TooltipContent>
                        {labels.queuedPromptMoreActions}
                      </TooltipContent>
                    </Tooltip>
                    <DropdownMenuContent
                      align="end"
                      className={`${styles.composerMenuContent} multica-queue-menu`}
                      sideOffset={8}
                    >
                      <SideQueueAction agentSessionId={agentSessionId} promptId={queuedPrompt.id} disabled={isDraining} />
                      <DropdownMenuItem
                        className={styles.composerMenuItem}
                        disabled={isDraining}
                        onPointerDown={(event) => {
                          handleEditQueuedPromptPointerDown(
                            event,
                            queuedPrompt.id
                          );
                        }}
                        onSelect={() => {
                          handleEditQueuedPromptSelect(queuedPrompt.id);
                        }}
                      >
                        <span>{labels.editQueuedPrompt}</span>
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </TooltipProvider>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
