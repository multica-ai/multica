import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import {
  useMemo,
  type CSSProperties,
  type HTMLAttributes,
  type MouseEvent,
  type ReactNode,
  type SVGProps
} from "react";
import { useTranslation } from "../../i18n/index";
import { cn } from "../../app/renderer/lib/utils";
import { NodeResizeHandles } from "./NodeResizeHandles";
import { WindowLayoutMenuButton } from "../workspaceDesktop/view/WindowLayoutMenuButton";
import {
  useNodeFrameResize,
  type ResizeEdges
} from "../../utils/nodeFrameResize";
import type { NodeFrame, Point, WorkspaceNodeKind } from "../../types";
import type { DesktopSize } from "../workspaceDesktop/types";
import { observer } from "mobx-react-lite";
export const WorkspaceNodeWindow = observer(WorkspaceNodeWindowRender);
const WorkspaceNodeTrafficLightButton = observer(
  WorkspaceNodeTrafficLightButtonRender
);
const WorkspaceNodeWindowTrafficLightIcon = observer(
  WorkspaceNodeWindowTrafficLightIconRender
);

interface WorkspaceNodeWindowInteractionOptions {
  normalizeViewport?: boolean;
  selectNode?: boolean;
  shiftKey?: boolean;
}

interface WorkspaceNodeWindowRenderFrame {
  position: Point;
  size: {
    width: number;
    height: number;
  };
}

type WorkspaceNodeWindowStyle = CSSProperties & {
  "--node-header-padding-x"?: string;
  "--node-header-traffic-light-title-offset"?: string;
};

export interface WorkspaceNodeWindowProps {
  nodeId: string;
  kind: WorkspaceNodeKind;
  title: string;
  position: Point;
  width: number;
  height: number;
  desktopSize: DesktopSize;
  minSize: { width: number; height: number };
  className?: string;
  bodyClassName?: string;
  rootProps?: Omit<
    HTMLAttributes<HTMLDivElement>,
    "className" | "children" | "style"
  > &
    Record<`data-${string}`, string | number | boolean | undefined>;
  sizeStyle?: CSSProperties;
  appearance?: "window" | "embedded";
  children: ReactNode | ((frame: WorkspaceNodeWindowRenderFrame) => ReactNode);
  customHeader?: ReactNode;
  titleIcon?: ReactNode;
  titleAccessory?: ReactNode;
  headerAccessory?: ReactNode;
  controlStartAccessory?: ReactNode;
  hideHeader?: boolean;
  onClose: () => void;
  onResize: (frame: NodeFrame) => void;
  onInteractionStart?: (
    options?: WorkspaceNodeWindowInteractionOptions
  ) => void;
  isMaximized?: boolean;
  isMuted?: boolean;
  hideMaximizeButton?: boolean;
  onMinimize?: () => void;
  onToggleMaximize?: () => void;
  resizeTestIdPrefix?: string;
  resizeHandlePointerDown?: (
    edges: ResizeEdges
  ) => (event: React.PointerEvent<HTMLElement>) => void;
}

function WorkspaceNodeWindowRender({
  nodeId,
  kind,
  title,
  position,
  width,
  height,
  desktopSize,
  minSize,
  className,
  bodyClassName,
  rootProps,
  sizeStyle,
  appearance = "window",
  children,
  customHeader,
  titleIcon,
  titleAccessory,
  headerAccessory,
  controlStartAccessory,
  hideHeader = false,
  onClose,
  onResize,
  onInteractionStart,
  isMaximized = false,
  isMuted = false,
  hideMaximizeButton = false,
  onMinimize,
  onToggleMaximize,
  resizeTestIdPrefix = `${kind}-node-resizer`,
  resizeHandlePointerDown
}: WorkspaceNodeWindowProps): React.JSX.Element {
  "use memo";
  const { t } = useTranslation();
  const { onClickCapture, ...restRootProps } = rootProps ?? {};
  const { draftFrame, handleResizePointerDown } = useNodeFrameResize({
    position,
    width,
    height,
    minSize,
    onResize
  });

  const renderedFrame = draftFrame ?? {
    position,
    size: { width, height }
  };

  const style = useMemo(
    () => ({
      width: renderedFrame.size.width,
      height: renderedFrame.size.height,
      transform:
        renderedFrame.position.x !== position.x ||
        renderedFrame.position.y !== position.y
          ? `translate(${renderedFrame.position.x - position.x}px, ${renderedFrame.position.y - position.y}px)`
          : undefined
    }),
    [
      position.x,
      position.y,
      renderedFrame.position.x,
      renderedFrame.position.y,
      renderedFrame.size.height,
      renderedFrame.size.width
    ]
  );
  const resolvedStyle = sizeStyle ?? style;
  const renderedChildren =
    typeof children === "function" ? children(renderedFrame) : children;
  const rootStyle: WorkspaceNodeWindowStyle = useMemo<WorkspaceNodeWindowStyle>(
    () =>
      appearance === "embedded"
        ? {
            "--node-header-padding-x": "16px",
            "--node-header-traffic-light-title-offset": "64px",
            ...resolvedStyle,
            width: "100%",
            height: "100%",
            transform: undefined,
            background: "transparent",
            border: "0",
            boxShadow: "none",
            backdropFilter: "none",
            WebkitBackdropFilter: "none"
          }
        : {
            "--node-header-padding-x": "16px",
            "--node-header-traffic-light-title-offset": "64px",
            ...resolvedStyle,
            background: "transparent",
            border: "1px solid var(--node-window-border)",
            boxShadow: "var(--window-drop-shadow)",
            backdropFilter: "var(--node-window-backdrop-filter)",
            WebkitBackdropFilter: "var(--node-window-backdrop-filter)"
          },
    [appearance, resolvedStyle]
  );
  const resolvedResizeHandlePointerDown =
    resizeHandlePointerDown ?? handleResizePointerDown;

  const onClickCaptureProp1Value1 = usePersistCallback<
    NonNullable<React.ComponentProps<"div">["onClickCapture"]>
  >((event) => {
    if (event.button !== 0 || !(event.target instanceof Element)) {
      return;
    }

    if (event.target.closest(".nodrag")) {
      return;
    }

    event.stopPropagation();
    onInteractionStart?.({ shiftKey: event.shiftKey });
  });
  const onClickCaptureProp1 = useMemo<
    NonNullable<React.ComponentProps<"div">["onClickCapture"]>
  >(() => onClickCaptureProp1Value1, [onClickCaptureProp1Value1]);
  return (
    <div
      {...restRootProps}
      className={cn(
        "workspace-node-window nowheel relative flex min-h-0 min-w-0 flex-col overflow-hidden rounded-[12px] border border-[var(--node-window-border)] bg-transparent text-foreground shadow-[var(--window-drop-shadow)]",
        appearance === "embedded" &&
          "h-full w-full rounded-none border-0 shadow-none",
        className
      )}
      style={rootStyle}
      data-workspace-node-window-root="true"
      data-workspace-node-window-appearance={appearance}
      data-workspace-node-window-kind={kind}
      data-workspace-node-window-maximized={isMaximized ? "true" : "false"}
      data-workspace-node-window-muted={isMuted ? "true" : "false"}
      onClickCapture={onClickCapture ?? onClickCaptureProp1}
    >
      {hideHeader ? null : customHeader ? (
        customHeader
      ) : (
        <HeaderBinding1
          onToggleMaximize={onToggleMaximize}
          t={t}
          onClose={onClose}
          onMinimize={onMinimize}
          kind={kind}
          hideMaximizeButton={hideMaximizeButton}
          isMaximized={isMaximized}
          title={title}
          titleIcon={titleIcon}
          titleAccessory={titleAccessory}
          headerAccessory={headerAccessory}
          controlStartAccessory={controlStartAccessory}
          nodeId={nodeId}
          desktopSize={desktopSize}
        />
      )}

      <div
        className={cn(
          "workspace-node-window__body flex min-h-0 min-w-0 flex-1 bg-[var(--node-surface)]",
          kind === "terminal" && "bg-[var(--terminal-node-surface)]",
          bodyClassName
        )}
        data-workspace-node-window-body="true"
      >
        {renderedChildren}
      </div>

      {!isMaximized ? (
        <NodeResizeHandles
          classNamePrefix="workspace-node-window"
          testIdPrefix={resizeTestIdPrefix}
          handleResizePointerDown={resolvedResizeHandlePointerDown}
        />
      ) : null}
    </div>
  );
}

function WorkspaceNodeTrafficLightButtonRender({
  ariaLabel,
  onClick,
  pressed,
  testId,
  tone
}: {
  ariaLabel: string;
  onClick: () => void;
  pressed?: boolean;
  testId?: string;
  tone: "close" | "maximize" | "minimize";
}): React.JSX.Element {
  const handleClick = (event: MouseEvent<HTMLButtonElement>): void => {
    event.stopPropagation();
    onClick();
  };
  const handleClickCallback = usePersistCallback(handleClick);

  const iconName =
    tone === "maximize" ? (pressed ? "unfullscreen" : "fullscreen") : tone;

  const onDoubleClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onDoubleClick"]>
  >((event) => event.stopPropagation());
  const onPointerDownProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onPointerDown"]>
  >((event) => event.stopPropagation());
  return (
    <button
      aria-label={ariaLabel}
      aria-pressed={pressed}
      className={cn(
        "relative -m-1 inline-flex size-5 shrink-0 cursor-pointer rounded-full border-0 bg-transparent p-0 opacity-[0.78] outline-none transition-opacity duration-150 before:absolute before:inset-1 before:rounded-full before:bg-[color-mix(in_srgb,var(--text-tertiary)_72%,transparent)] before:content-[''] group-hover/traffic-lights:opacity-100 group-focus-within/traffic-lights:opacity-100 focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-1 focus-visible:ring-offset-[var(--node-header-surface)]",
        tone === "close" &&
          "group-hover/traffic-lights:before:bg-[#ff5f57] group-focus-within/traffic-lights:before:bg-[#ff5f57]",
        tone === "minimize" &&
          "group-hover/traffic-lights:before:bg-[#ffbd2e] group-focus-within/traffic-lights:before:bg-[#ffbd2e]",
        tone === "maximize" &&
          "group-hover/traffic-lights:before:bg-[#28c840] group-focus-within/traffic-lights:before:bg-[#28c840]"
      )}
      data-window-header="top"
      data-workspace-node-window-traffic-light={tone}
      data-testid={testId}
      title={ariaLabel}
      type="button"
      onClick={handleClickCallback}
      onDoubleClick={onDoubleClickProp1}
      onPointerDown={onPointerDownProp2}
    >
      <WorkspaceNodeWindowTrafficLightIcon
        aria-hidden="true"
        className="pointer-events-none absolute inset-[5px] z-[1] size-[10px] text-[color-mix(in_srgb,#000_68%,transparent)] opacity-0 transition-opacity duration-150 group-hover/traffic-lights:opacity-100 group-focus-within/traffic-lights:opacity-100"
        data-workspace-node-window-traffic-light-icon={iconName}
        iconName={iconName}
      />
    </button>
  );
}

function WorkspaceNodeWindowTrafficLightIconRender({
  className,
  iconName,
  ...props
}: SVGProps<SVGSVGElement> & {
  iconName: "close" | "fullscreen" | "minimize" | "unfullscreen";
}): React.JSX.Element {
  return (
    <svg
      {...props}
      className={className}
      fill="currentColor"
      viewBox="0 0 24 24"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path d={trafficLightIconPathByName[iconName]} />
    </svg>
  );
}

const trafficLightIconPathByName = {
  close:
    "M16.9395 4.93953C17.5253 4.35374 18.4748 4.35374 19.0606 4.93953C19.6463 5.52532 19.6464 6.47486 19.0606 7.06062L14.1212 12.0001L19.0606 16.9395C19.6463 17.5253 19.6464 18.4749 19.0606 19.0606C18.4749 19.6464 17.5253 19.6463 16.9395 19.0606L12.0001 14.1212L7.06062 19.0606C6.47486 19.6464 5.52532 19.6463 4.93953 19.0606C4.35374 18.4748 4.35374 17.5253 4.93953 16.9395L9.87898 12.0001L4.93953 7.06062C4.35374 6.47484 4.35374 5.52532 4.93953 4.93953C5.52532 4.35374 6.47484 4.35374 7.06062 4.93953L12.0001 9.87898L16.9395 4.93953Z",
  fullscreen:
    "M18.1465 7.85352C18.4615 7.53861 18.9999 7.76165 19 8.20703V18.5C19 18.7761 18.7761 19 18.5 19H8.20703C7.76165 18.9999 7.53861 18.4615 7.85352 18.1465L18.1465 7.85352ZM15.793 5C16.2384 5.00006 16.4614 5.53855 16.1465 5.85352L5.85352 16.1465C5.53855 16.4614 5.00006 16.2384 5 15.793V5.5C5 5.22386 5.22386 5 5.5 5H15.793Z",
  minimize:
    "M5 10.5H19C19.8284 10.5 20.5 11.1716 20.5 12C20.5 12.8284 19.8284 13.5 19 13.5H5C4.17157 13.5 3.5 12.8284 3.5 12C3.5 11.1716 4.17157 10.5 5 10.5Z",
  unfullscreen:
    "M20.793 12C21.2384 12.0001 21.4614 12.5386 21.1465 12.8536L12.8536 21.1465C12.5386 21.4614 12.0001 21.2384 12.0001 20.793V12.5C12.0001 12.2239 12.2239 12 12.5001 12H20.793ZM11.1465 2.85356C11.4615 2.53864 12 2.76166 12.0001 3.20708V11.5C12 11.7761 11.7762 12 11.5001 12H3.20708C2.76166 12 2.53864 11.4615 2.85357 11.1465L11.1465 2.85356Z"
} as const;

const HeaderBinding1 = observer(function HeaderBinding1({
  onToggleMaximize,
  t,
  onClose,
  onMinimize,
  kind,
  hideMaximizeButton,
  isMaximized,
  title,
  titleIcon,
  titleAccessory,
  headerAccessory,
  controlStartAccessory,
  nodeId,
  desktopSize
}: HeaderBinding1Props) {
  const styleProp3 = useMemo<
    NonNullable<React.ComponentProps<"header">["style"]>
  >(
    () => ({
      borderBottomColor: "var(--node-header-border)",
      background: "var(--node-header-surface)"
    }),
    []
  );
  const onDoubleClickProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<"header">["onDoubleClick"]>
  >((event) => {
    if (event.target instanceof Element && event.target.closest(".nodrag")) {
      return;
    }
    event.stopPropagation();
    onToggleMaximize?.();
  });
  return (
    <header
      className="workspace-node-window__header relative flex h-[var(--node-header-height)] min-h-[var(--node-header-height)] cursor-grab items-center gap-2 border-b border-[var(--node-header-border)] bg-[var(--node-header-surface)] px-2 pl-[calc(var(--node-header-padding-x)+var(--node-header-traffic-light-title-offset))] active:cursor-grabbing"
      style={styleProp3}
      data-workspace-node-window-header="true"
      data-node-drag-handle
      data-window-header="top"
      onDoubleClick={onDoubleClickProp4}
    >
      <div
        className="workspace-node-window__controls nodrag group/traffic-lights absolute left-4 top-1/2 inline-flex -translate-y-1/2 items-center gap-2"
        data-workspace-node-window-controls="true"
      >
        <WorkspaceNodeTrafficLightButton
          ariaLabel={t("common.close")}
          onClick={onClose}
          tone="close"
        />
        {onMinimize ? (
          <WorkspaceNodeTrafficLightButton
            ariaLabel={t("common.minimize")}
            onClick={onMinimize}
            testId={`${kind}-node-minimize`}
            tone="minimize"
          />
        ) : null}
        {onToggleMaximize && !hideMaximizeButton ? (
          <WorkspaceNodeTrafficLightButton
            ariaLabel={isMaximized ? t("common.restore") : t("common.maximize")}
            onClick={onToggleMaximize}
            pressed={isMaximized}
            tone="maximize"
          />
        ) : null}
      </div>
      <div
        className="workspace-node-window__title flex min-w-0 max-w-[280px] flex-1 items-center gap-2 text-[15px] leading-5 font-semibold text-foreground"
        // i18n-check-ignore: Test selector marker, not a tooltip.
        data-workspace-node-window-title="true"
        title={title}
      >
        {titleIcon ? (
          <span
            className="workspace-node-window__title-icon inline-flex flex-none items-center"
            data-workspace-node-window-title-icon="true"
          >
            {titleIcon}
          </span>
        ) : null}
        <span className="block min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap">
          {title}
        </span>
        {titleAccessory ? (
          <span
            className="workspace-node-window__title-accessory nodrag inline-flex flex-none items-center"
            data-workspace-node-window-title-accessory="true"
          >
            {titleAccessory}
          </span>
        ) : null}
      </div>
      {headerAccessory ? (
        <div
          className="workspace-node-window__header-accessory nodrag inline-flex min-w-0 flex-none items-center gap-2"
          data-workspace-node-window-header-accessory="true"
        >
          {headerAccessory}
        </div>
      ) : null}
      <div
        className="workspace-node-window__header-tools nodrag inline-flex flex-none items-center gap-0.5"
        data-workspace-node-window-header-tools="true"
      >
        {controlStartAccessory}
        <WindowLayoutMenuButton windowId={nodeId} desktopSize={desktopSize} />
      </div>
    </header>
  );
});

type HeaderBinding1Props = {
  onToggleMaximize: NonNullable<WorkspaceNodeWindowProps>["onToggleMaximize"];
  t: import("../../i18n/runtime").TranslateFn;
  onClose: NonNullable<WorkspaceNodeWindowProps>["onClose"];
  onMinimize: NonNullable<WorkspaceNodeWindowProps>["onMinimize"];
  kind: NonNullable<WorkspaceNodeWindowProps>["kind"];
  hideMaximizeButton: boolean;
  isMaximized: boolean;
  title: NonNullable<WorkspaceNodeWindowProps>["title"];
  titleIcon: NonNullable<WorkspaceNodeWindowProps>["titleIcon"];
  titleAccessory: NonNullable<WorkspaceNodeWindowProps>["titleAccessory"];
  headerAccessory: NonNullable<WorkspaceNodeWindowProps>["headerAccessory"];
  controlStartAccessory: NonNullable<WorkspaceNodeWindowProps>["controlStartAccessory"];
  nodeId: NonNullable<WorkspaceNodeWindowProps>["nodeId"];
  desktopSize: NonNullable<WorkspaceNodeWindowProps>["desktopSize"];
};
