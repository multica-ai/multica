import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useEffect, useRef, useState } from "react";
import type { JSX, SVGProps } from "react";
import { GridBottomLinedIcon } from "../../../app/renderer/components/icons/GridBottomLinedIcon";
import { GridLeftLinedIcon } from "../../../app/renderer/components/icons/GridLeftLinedIcon";
import { GridRightLinedIcon } from "../../../app/renderer/components/icons/GridRightLinedIcon";
import { GridTopLinedIcon } from "../../../app/renderer/components/icons/GridTopLinedIcon";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger
} from "@tutti-os/ui-system";
import { Button } from "../../../app/renderer/components/ui/button";
import { useTranslation } from "../../../i18n/index";
import { CanvasNodeGhostIconButton } from "../../shared/CanvasNodeGhostIconButton";
import {
  CanvasNodeLayoutLinedIcon,
  CanvasNodeMaximizeLinedIcon,
  CanvasNodeMinimizeLinedIcon
} from "../../shared/canvasNodeChromeIcons";
import { useDesktopStore } from "../store";
import type { DesktopSize, WindowQuickLayoutTarget } from "../types";
import { observer } from "mobx-react-lite";
export const WindowLayoutMenuButton = observer(WindowLayoutMenuButtonRender);

const QUICK_LAYOUT_OPTIONS: Array<{
  target: WindowQuickLayoutTarget;
  icon: (props: SVGProps<SVGSVGElement>) => JSX.Element;
  labelKey: string;
}> = [
  {
    target: "left",
    icon: GridLeftLinedIcon,
    labelKey: "workspaceWindowLayout.left"
  },
  {
    target: "right",
    icon: GridRightLinedIcon,
    labelKey: "workspaceWindowLayout.right"
  },
  {
    target: "top",
    icon: GridTopLinedIcon,
    labelKey: "workspaceWindowLayout.top"
  },
  {
    target: "bottom",
    icon: GridBottomLinedIcon,
    labelKey: "workspaceWindowLayout.bottom"
  }
];

const WINDOW_LAYOUT_MENU_HOVER_OPEN_DELAY_MS = 300;

interface WindowLayoutMenuButtonProps {
  windowId: string;
  desktopSize: DesktopSize;
  className?: string;
  "data-window-header"?: string;
}

function WindowLayoutMenuButtonRender({
  windowId,
  desktopSize,
  className,
  "data-window-header": windowHeader
}: WindowLayoutMenuButtonProps): JSX.Element {
  "use memo";
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const hoverOpenTimeoutRef = useRef<number | null>(null);
  const applyQuickLayout = useDesktopStore((state) => state.applyQuickLayout);
  const enterFullscreenWindow = useDesktopStore(
    (state) => state.enterFullscreenWindow
  );
  const exitFullscreenWindow = useDesktopStore(
    (state) => state.exitFullscreenWindow
  );
  const isFullscreen = useDesktopStore(
    (state) =>
      state.windows.find((window) => window.id === windowId)?.displayMode ===
      "fullscreen"
  );
  const fullscreenLabelKey = isFullscreen
    ? "workspaceWindowLayout.restore"
    : "workspaceWindowLayout.fullscreen";
  const fullscreenIconState = isFullscreen ? "restore" : "fullscreen";
  const FullscreenIcon = isFullscreen
    ? CanvasNodeMinimizeLinedIcon
    : CanvasNodeMaximizeLinedIcon;

  const applyLayout = (target: WindowQuickLayoutTarget) => {
    applyQuickLayout(windowId, target, desktopSize);
    setOpen(false);
  };
  const applyLayoutCallback = usePersistCallback(applyLayout);

  const clearHoverOpenTimeout = () => {
    if (hoverOpenTimeoutRef.current !== null) {
      window.clearTimeout(hoverOpenTimeoutRef.current);
      hoverOpenTimeoutRef.current = null;
    }
  };
  const clearHoverOpenTimeoutCallback = usePersistCallback(
    clearHoverOpenTimeout
  );

  const scheduleHoverOpen = () => {
    if (open || hoverOpenTimeoutRef.current !== null) {
      return;
    }
    hoverOpenTimeoutRef.current = window.setTimeout(() => {
      hoverOpenTimeoutRef.current = null;
      setOpen(true);
    }, WINDOW_LAYOUT_MENU_HOVER_OPEN_DELAY_MS);
  };
  const scheduleHoverOpenCallback = usePersistCallback(scheduleHoverOpen);

  const toggleFullscreen = () => {
    if (isFullscreen) {
      exitFullscreenWindow(windowId, desktopSize);
    } else {
      enterFullscreenWindow(windowId, desktopSize);
    }
    setOpen(false);
  };
  const toggleFullscreenCallback = usePersistCallback(toggleFullscreen);

  useEffect(() => clearHoverOpenTimeout, []);

  const onOpenChangeProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof DropdownMenu>["onOpenChange"]>
  >((nextOpen) => {
    if (!nextOpen) {
      clearHoverOpenTimeout();
    }
    setOpen(nextOpen);
  });
  return (
    <DropdownMenu open={open} onOpenChange={onOpenChangeProp1}>
      <DropdownMenuTrigger asChild>
        <CanvasNodeGhostIconButton
          className={className}
          data-window-header={windowHeader}
          data-window-layout-menu-trigger="true"
          data-menu-open={open ? "true" : undefined}
          data-testid="window-layout-menu-trigger"
          aria-label={t("workspaceWindowLayout.trigger")}
          title={t("workspaceWindowLayout.trigger")}
          onPointerEnter={scheduleHoverOpenCallback}
          onPointerLeave={clearHoverOpenTimeoutCallback}
        >
          <CanvasNodeLayoutLinedIcon
            width={18}
            height={18}
            aria-hidden="true"
            data-testid="window-layout-menu-trigger-icon"
          />
        </CanvasNodeGhostIconButton>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        sideOffset={10}
        className="t-dropdown is-open relative min-w-[160px] rounded-[var(--tutti-radius-xl)] border border-hairline bg-background-fronted p-0 text-foreground shadow-[var(--tsh-shell-shadow)]"
        data-origin="top-right"
        data-testid="window-layout-menu"
      >
        <div className="p-2">
          <div className="mx-1 mt-0.5 mb-1.5 px-1 text-[13px] leading-[20px] font-semibold text-[var(--text-primary)]">
            {t("workspaceWindowLayout.moveAndResize")}
          </div>
          <div className="grid grid-cols-4 items-center gap-1">
            {QUICK_LAYOUT_OPTIONS.map((option) => {
              const Icon = option.icon;
              return (
                <ButtonBinding1
                  key={option.target}
                  option={option}
                  t={t}
                  applyLayout={applyLayoutCallback}
                  Icon={Icon}
                />
              );
            })}
          </div>
        </div>
        <div className="mx-1.5 my-1 border-t border-[var(--line-2)]" />
        <div className="px-1">
          <button
            className="inline-flex min-h-8 w-full items-center gap-2 whitespace-nowrap rounded-[6px] border border-transparent bg-transparent px-0 text-left text-[13px] leading-[20px] font-semibold text-[var(--text-primary)] transition-[background-color,color,border-color] duration-150 ease-in-out hover:bg-block focus-visible:bg-block focus-visible:outline-none"
            type="button"
            aria-label={t(fullscreenLabelKey)}
            title={t(fullscreenLabelKey)}
            onClick={toggleFullscreenCallback}
          >
            <FullscreenIcon
              width={16}
              height={16}
              aria-hidden="true"
              data-testid="window-layout-menu-fullscreen-icon"
              data-window-layout-fullscreen-icon={fullscreenIconState}
              className="shrink-0"
            />
            <span className="whitespace-nowrap">{t(fullscreenLabelKey)}</span>
          </button>
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

const ButtonBinding1 = observer(function ButtonBinding1({
  option,
  t,
  applyLayout,
  Icon,
  ...slotProps
}: ButtonBinding1Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding1Props>) {
  const onClickProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => applyLayout(option.target));
  return (
    <Slot.Root {...slotProps}>
      <Button
        key={option.target}
        type="button"
        variant="ghost"
        size="iconClose"
        aria-label={t(option.labelKey)}
        title={t(option.labelKey)}
        data-testid={`window-layout-menu-quick-layout-${option.target}`}
        onClick={onClickProp2}
      >
        <Icon width={16} height={16} aria-hidden="true" />
      </Button>
    </Slot.Root>
  );
});

type ButtonBinding1Props = {
  option: {
    target: WindowQuickLayoutTarget;
    icon: (props: SVGProps<SVGSVGElement>) => JSX.Element;
    labelKey: string;
  };
  t: import("../../../i18n/runtime").TranslateFn;
  applyLayout: (target: WindowQuickLayoutTarget) => void;
  Icon: (props: SVGProps<SVGSVGElement>) => JSX.Element;
};
