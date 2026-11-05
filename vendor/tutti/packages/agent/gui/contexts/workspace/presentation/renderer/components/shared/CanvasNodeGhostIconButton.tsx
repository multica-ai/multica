import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from "react";
import { cn } from "../../../../../../app/renderer/lib/utils";
import { observer } from "mobx-react-lite";

export type CanvasNodeGhostIconButtonProps = Omit<
  ButtonHTMLAttributes<HTMLButtonElement>,
  "type"
> & {
  children: ReactNode;
  /**
   * When true (default), pointer/click stopPropagation — keeps React Flow from stealing node chrome
   * interactions. Set false for Radix `DropdownMenuTrigger asChild` and other portaled triggers.
   */
  stopsEventPropagation?: boolean;
};

/**
 * Ghost icon control for canvas node chrome.
 */
export const CanvasNodeGhostIconButton = observer(
  forwardRef<HTMLButtonElement, CanvasNodeGhostIconButtonProps>(
    function CanvasNodeGhostIconButton(
      {
        className,
        children,
        onPointerDown,
        onMouseDown,
        onClick,
        stopsEventPropagation = true,
        ...rest
      },
      ref
    ) {
      "use memo";
      const onPointerDownProp1 = usePersistCallback<
        NonNullable<React.ComponentProps<"button">["onPointerDown"]>
      >((event) => {
        if (stopsEventPropagation) {
          event.stopPropagation();
        }
        onPointerDown?.(event);
      });
      const onMouseDownProp2 = usePersistCallback<
        NonNullable<React.ComponentProps<"button">["onMouseDown"]>
      >((event) => {
        if (stopsEventPropagation) {
          event.stopPropagation();
        }
        onMouseDown?.(event);
      });
      const onClickProp3 = usePersistCallback<
        NonNullable<React.ComponentProps<"button">["onClick"]>
      >((event) => {
        if (stopsEventPropagation) {
          event.stopPropagation();
        }
        onClick?.(event);
      });
      return (
        <button
          ref={ref}
          {...rest}
          type="button"
          className={cn(
            "nodrag inline-flex h-7 min-h-7 w-7 min-w-7 shrink-0 items-center justify-center rounded-[4px] border border-transparent bg-transparent p-0 text-[var(--text-secondary)] transition-[background-color,color,border-color,opacity] duration-200 ease-in-out [-webkit-app-region:no-drag] hover:border-transparent hover:bg-[var(--transparency-block)] hover:text-[var(--text-primary)] active:bg-[var(--transparency-block-active)] active:text-[var(--text-primary)] disabled:cursor-not-allowed disabled:opacity-45 focus-visible:outline-2 focus-visible:outline-[color:color-mix(in_srgb,var(--tsh-shell-accent)_80%,white)] focus-visible:outline-offset-2 [&_svg]:block [&_svg]:h-3.5 [&_svg]:w-3.5 [&_svg]:shrink-0 [&_svg:not([data-nexight-chrome-glyph=fill]):not([data-tutti-chrome-glyph=fill])]:fill-none [&_svg:not([data-nexight-chrome-glyph=fill]):not([data-tutti-chrome-glyph=fill])]:stroke-current data-[menu-open=true]:border-transparent data-[menu-open=true]:bg-[var(--transparency-block)] data-[menu-open=true]:text-[var(--text-primary)] data-[menu-open=true]:hover:bg-[var(--transparency-block-active)]",
            className
          )}
          onPointerDown={onPointerDownProp1}
          onMouseDown={onMouseDownProp2}
          onClick={onClickProp3}
        >
          {children}
        </button>
      );
    }
  )
);
