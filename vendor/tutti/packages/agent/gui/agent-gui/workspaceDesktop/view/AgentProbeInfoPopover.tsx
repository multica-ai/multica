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

export function AgentProbeInfoPopover({
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
  const closePopover = useCallback((): void => {
    if (!isOpen) return;
    setIsOpen(false);
    onClose?.();
  }, [isOpen, onClose]);
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
      onMouseEnter={openPopover}
      onMouseLeave={closeIfPointerLeavesPopover}
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
    <div
      ref={anchorRef}
      className={`desktop-dock-popup__agent-info${className ? ` ${className}` : ""}`}
      tabIndex={0}
      role="button"
      aria-label={lines.map(formatDockAgentProbeLineText).join("，")}
      data-testid={testId}
      onMouseEnter={() => {
        updatePopoverPosition();
        openPopover();
      }}
      onMouseLeave={closeIfPointerLeavesPopover}
      onFocus={() => {
        updatePopoverPosition();
        openPopover();
      }}
      onBlur={closePopover}
    >
      <Info size={15} strokeWidth={2.1} aria-hidden="true" />
      {isOpen ? createPortal(popover, getPortalContainer()) : null}
    </div>
  );
}
