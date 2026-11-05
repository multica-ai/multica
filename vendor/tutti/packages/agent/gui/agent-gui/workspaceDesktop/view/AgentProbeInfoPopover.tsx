import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { Info } from "lucide-react";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent,
  type ReactNode
} from "react";
import { createPortal } from "react-dom";
import type { DockAgentProbeTooltipLine } from "./desktopDockAgentProbeTooltipModel";
import { getPortalContainer } from "@tutti-os/ui-system/portal-container";
import { observer } from "mobx-react-lite";
export const AgentProbeInfoPopover = observer(AgentProbeInfoPopoverRender);

const POPOVER_MIN_VIEWPORT_INSET_PX = 16;
const POPOVER_DEFAULT_WIDTH_PX = 260;

function dockAgentProbeLineKey(line: DockAgentProbeTooltipLine): string {
  return typeof line === "string"
    ? line
    : `${line.label ?? ""}:${line.primary}:${line.secondary ?? ""}`;
}

function renderDockAgentProbeLine(line: DockAgentProbeTooltipLine): ReactNode {
  if (typeof line === "string") {
    return line;
  }
  return (
    <>
      {line.primary}
      {line.secondary ? (
        <>
          {" "}
          <span className="desktop-dock-popup__agent-status-secondary">
            {line.secondary}
          </span>
        </>
      ) : null}
    </>
  );
}

function formatDockAgentProbeLineText(line: DockAgentProbeTooltipLine): string {
  if (typeof line === "string") {
    return line;
  }
  return line.secondary ? `${line.primary} ${line.secondary}` : line.primary;
}

function getDockAgentProbeLineLabel(line: DockAgentProbeTooltipLine): string {
  return typeof line === "string" ? "" : (line.label ?? "");
}

function AgentProbeInfoPopoverRender({
  lines,
  testId = "agent-probe-info",
  className,
  onOpen,
  onClose
}: {
  lines: DockAgentProbeTooltipLine[];
  testId?: string;
  className?: string;
  onOpen?: () => void;
  onClose?: () => void;
}): React.JSX.Element | null {
  "use memo";
  const anchorRef = useRef<HTMLDivElement | null>(null);
  const popoverRef = useRef<HTMLDivElement | null>(null);
  const [isOpen, setIsOpen] = useState(false);
  const [popoverStyle, setPopoverStyle] = useState<CSSProperties | null>(null);
  const openPopover = useCallback((): void => {
    if (!isOpen) {
      onOpen?.();
    }
    setIsOpen(true);
  }, [isOpen, onOpen]);
  const openPopoverCallback = usePersistCallback(openPopover);
  const closePopover = useCallback((): void => {
    if (!isOpen) return;
    setIsOpen(false);
    onClose?.();
  }, [isOpen, onClose]);
  const closePopoverCallback = usePersistCallback(closePopover);
  const closeIfPointerLeavesPopover = useCallback(
    (event: MouseEvent): void => {
      const nextTarget = event.relatedTarget;
      if (
        nextTarget instanceof Node &&
        (anchorRef.current?.contains(nextTarget) ||
          popoverRef.current?.contains(nextTarget))
      ) {
        return;
      }
      closePopover();
    },
    [closePopover]
  );
  const closeIfPointerLeavesPopoverCallback = usePersistCallback(
    closeIfPointerLeavesPopover
  );
  const updatePopoverPosition = useCallback((): void => {
    const anchor = anchorRef.current;
    if (!anchor) {
      return;
    }
    const rect = anchor.getBoundingClientRect();
    const availableWidth = Math.max(
      0,
      window.innerWidth - POPOVER_MIN_VIEWPORT_INSET_PX * 2
    );
    const currentWidth = popoverRef.current?.offsetWidth;
    const measuredWidth = Math.min(
      currentWidth && currentWidth > 0
        ? currentWidth
        : POPOVER_DEFAULT_WIDTH_PX,
      availableWidth
    );
    const centeredLeft = rect.left + rect.width / 2 - measuredWidth / 2;
    const maxLeft =
      window.innerWidth - measuredWidth - POPOVER_MIN_VIEWPORT_INSET_PX;
    setPopoverStyle({
      top: rect.bottom + 8,
      left: Math.max(
        POPOVER_MIN_VIEWPORT_INSET_PX,
        Math.min(centeredLeft, maxLeft)
      ),
      maxWidth: availableWidth
    });
  }, []);
  const updatePopoverPositionCallback = usePersistCallback(
    updatePopoverPosition
  );

  useEffect(() => {
    if (!isOpen) {
      return;
    }
    updatePopoverPosition();
  }, [isOpen, lines, updatePopoverPosition]);

  useLayoutEffect(() => {
    if (!isOpen) {
      return;
    }

    updatePopoverPosition();
    window.addEventListener("resize", updatePopoverPosition);
    window.addEventListener("scroll", updatePopoverPosition, true);
    return () => {
      window.removeEventListener("resize", updatePopoverPosition);
      window.removeEventListener("scroll", updatePopoverPosition, true);
    };
  }, [isOpen, updatePopoverPosition]);

  if (lines.length === 0) {
    return null;
  }

  const popover = (
    <div
      ref={popoverRef}
      className="desktop-dock-popup__agent-info-popover desktop-dock-popup__agent-info-popover--portal"
      role="status"
      style={popoverStyle ?? undefined}
      onMouseEnter={openPopoverCallback}
      onMouseLeave={closeIfPointerLeavesPopoverCallback}
    >
      <ul className="desktop-dock-popup__agent-info-list">
        {lines.map((line) => {
          const label = getDockAgentProbeLineLabel(line);
          const valueText = formatDockAgentProbeLineText(line);
          return (
            <li
              key={`probe-info-${dockAgentProbeLineKey(line)}`}
              className="desktop-dock-popup__agent-info-item"
              data-has-label={label ? "true" : "false"}
            >
              {label ? (
                <span className="desktop-dock-popup__agent-info-label">
                  {label}
                </span>
              ) : null}
              <span
                className="desktop-dock-popup__agent-info-value"
                title={valueText}
              >
                {renderDockAgentProbeLine(line)}
              </span>
            </li>
          );
        })}
      </ul>
    </div>
  );

  return (
    <DivBinding1
      anchorRef={anchorRef}
      className={className}
      lines={lines}
      testId={testId}
      updatePopoverPosition={updatePopoverPositionCallback}
      openPopover={openPopoverCallback}
      closeIfPointerLeavesPopover={closeIfPointerLeavesPopoverCallback}
      closePopover={closePopoverCallback}
      isOpen={isOpen}
      popover={popover}
    />
  );
}

const DivBinding1 = observer(function DivBinding1({
  anchorRef,
  className,
  lines,
  testId,
  updatePopoverPosition,
  openPopover,
  closeIfPointerLeavesPopover,
  closePopover,
  isOpen,
  popover,
  ...slotProps
}: DivBinding1Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof DivBinding1Props>) {
  const onMouseEnterProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"div">["onMouseEnter"]>
  >(() => {
    updatePopoverPosition();
    openPopover();
  });
  const onFocusProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<"div">["onFocus"]>
  >(() => {
    updatePopoverPosition();
    openPopover();
  });
  return (
    <Slot.Root {...slotProps}>
      <div
        ref={anchorRef}
        className={`desktop-dock-popup__agent-info${className ? ` ${className}` : ""}`}
        tabIndex={0}
        role="button"
        aria-label={lines.map(formatDockAgentProbeLineText).join("，")}
        data-testid={testId}
        onMouseEnter={onMouseEnterProp1}
        onMouseLeave={closeIfPointerLeavesPopover}
        onFocus={onFocusProp2}
        onBlur={closePopover}
      >
        <Info size={15} strokeWidth={2.1} aria-hidden="true" />
        {isOpen ? createPortal(popover, getPortalContainer()) : null}
      </div>
    </Slot.Root>
  );
});

type DivBinding1Props = {
  anchorRef: import("react").RefObject<HTMLDivElement | null>;
  className: NonNullable<{
    lines: DockAgentProbeTooltipLine[];
    testId?: string;
    className?: string;
    onOpen?: () => void;
    onClose?: () => void;
  }>["className"];
  lines: NonNullable<{
    lines: DockAgentProbeTooltipLine[];
    testId?: string;
    className?: string;
    onOpen?: () => void;
    onClose?: () => void;
  }>["lines"];
  testId: string;
  updatePopoverPosition: () => void;
  openPopover: () => void;
  closeIfPointerLeavesPopover: (event: MouseEvent) => void;
  closePopover: () => void;
  isOpen: boolean;
  popover: import("react").JSX.Element;
};
