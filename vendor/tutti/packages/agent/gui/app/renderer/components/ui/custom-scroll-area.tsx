import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useMemo } from "react";
import {
  forwardRef,
  useCallback,
  useEffect,
  useRef,
  useState,
  type HTMLAttributes,
  type ForwardedRef,
  type MouseEvent as ReactMouseEvent,
  type MutableRefObject,
  type ReactNode
} from "react";
import { cn } from "../../lib/utils";
import { observer } from "mobx-react-lite";
export const CustomScrollbar = observer(CustomScrollbarRender);

const MIN_THUMB_HEIGHT = 24;

interface ScrollbarState {
  scrollable: boolean;
  thumbHeight: number;
  thumbTop: number;
}

interface ScrollbarDragState {
  maxScrollTop: number;
  maxThumbTop: number;
  startClientY: number;
  startScrollTop: number;
}

export interface CustomScrollbarProps {
  getViewport: () => HTMLElement | null;
  className?: string;
  thumbClassName?: string;
  testId?: string;
  thumbTestId?: string;
  syncKey?: unknown;
}

function CustomScrollbarRender({
  getViewport,
  className,
  thumbClassName,
  testId,
  thumbTestId,
  syncKey
}: CustomScrollbarProps): React.JSX.Element {
  "use memo";
  const trackRef = useRef<HTMLDivElement | null>(null);
  const dragStateRef = useRef<ScrollbarDragState | null>(null);
  const [scrollbarState, setScrollbarState] = useState<ScrollbarState>({
    scrollable: false,
    thumbHeight: 0,
    thumbTop: 0
  });
  const [dragging, setDragging] = useState(false);

  const syncScrollbarState = useCallback(() => {
    const viewport = getViewport();
    if (!viewport) {
      setScrollbarState({ scrollable: false, thumbHeight: 0, thumbTop: 0 });
      return;
    }

    const { clientHeight, scrollHeight, scrollTop } = viewport;
    const trackHeight = trackRef.current?.clientHeight ?? clientHeight;
    const maxScrollTop = Math.max(0, scrollHeight - clientHeight);
    if (clientHeight <= 0 || trackHeight <= 0 || maxScrollTop <= 0) {
      setScrollbarState({ scrollable: false, thumbHeight: 0, thumbTop: 0 });
      return;
    }

    const thumbHeight = Math.max(
      MIN_THUMB_HEIGHT,
      Math.round((clientHeight / scrollHeight) * trackHeight)
    );
    const maxThumbTop = Math.max(0, trackHeight - thumbHeight);
    const thumbTop = Math.round((scrollTop / maxScrollTop) * maxThumbTop);
    setScrollbarState((previous) =>
      previous.scrollable &&
      previous.thumbHeight === thumbHeight &&
      previous.thumbTop === thumbTop
        ? previous
        : { scrollable: true, thumbHeight, thumbTop }
    );
  }, [getViewport]);

  const scrollViewportToThumbTop = useCallback(
    (thumbTop: number) => {
      const viewport = getViewport();
      const track = trackRef.current;
      if (!viewport || !track) {
        return;
      }

      const maxScrollTop = Math.max(
        0,
        viewport.scrollHeight - viewport.clientHeight
      );
      const maxThumbTop = Math.max(
        0,
        track.clientHeight - scrollbarState.thumbHeight
      );
      if (maxScrollTop <= 0 || maxThumbTop <= 0) {
        return;
      }

      viewport.scrollTop =
        (clamp(thumbTop, 0, maxThumbTop) / maxThumbTop) * maxScrollTop;
      syncScrollbarState();
    },
    [getViewport, scrollbarState.thumbHeight, syncScrollbarState]
  );

  const handleTrackMouseDown = useCallback(
    (event: ReactMouseEvent<HTMLDivElement>) => {
      if (event.button !== 0 || !scrollbarState.scrollable) {
        return;
      }

      const track = trackRef.current;
      if (!track) {
        return;
      }

      event.preventDefault();
      event.stopPropagation();
      const trackRect = track.getBoundingClientRect();
      scrollViewportToThumbTop(
        event.clientY - trackRect.top - scrollbarState.thumbHeight / 2
      );
    },
    [
      scrollViewportToThumbTop,
      scrollbarState.scrollable,
      scrollbarState.thumbHeight
    ]
  );
  const handleTrackMouseDownCallback = usePersistCallback(handleTrackMouseDown);

  const handleThumbMouseDown = useCallback(
    (event: ReactMouseEvent<HTMLDivElement>) => {
      if (event.button !== 0 || !scrollbarState.scrollable) {
        return;
      }

      const viewport = getViewport();
      const track = trackRef.current;
      if (!viewport || !track) {
        return;
      }

      const maxScrollTop = Math.max(
        0,
        viewport.scrollHeight - viewport.clientHeight
      );
      const maxThumbTop = Math.max(
        0,
        track.clientHeight - scrollbarState.thumbHeight
      );
      if (maxScrollTop <= 0 || maxThumbTop <= 0) {
        return;
      }

      event.preventDefault();
      event.stopPropagation();
      dragStateRef.current = {
        maxScrollTop,
        maxThumbTop,
        startClientY: event.clientY,
        startScrollTop: viewport.scrollTop
      };
      setDragging(true);
    },
    [getViewport, scrollbarState.scrollable, scrollbarState.thumbHeight]
  );
  const handleThumbMouseDownCallback = usePersistCallback(handleThumbMouseDown);

  useEffect(() => {
    if (!dragging) {
      return;
    }

    const handleMouseMove = (event: MouseEvent): void => {
      const dragState = dragStateRef.current;
      const viewport = getViewport();
      if (!dragState || !viewport) {
        return;
      }

      const nextThumbTop =
        (dragState.startScrollTop / dragState.maxScrollTop) *
          dragState.maxThumbTop +
        (event.clientY - dragState.startClientY);
      viewport.scrollTop =
        (clamp(nextThumbTop, 0, dragState.maxThumbTop) /
          dragState.maxThumbTop) *
        dragState.maxScrollTop;
      syncScrollbarState();
    };

    const handleMouseUp = (): void => {
      dragStateRef.current = null;
      setDragging(false);
    };

    window.addEventListener("mousemove", handleMouseMove);
    window.addEventListener("mouseup", handleMouseUp);
    return () => {
      window.removeEventListener("mousemove", handleMouseMove);
      window.removeEventListener("mouseup", handleMouseUp);
    };
  }, [dragging, getViewport, syncScrollbarState]);

  useEffect(() => {
    const viewport = getViewport();
    if (!viewport) {
      setScrollbarState({ scrollable: false, thumbHeight: 0, thumbTop: 0 });
      return;
    }

    syncScrollbarState();
    viewport.addEventListener("scroll", syncScrollbarState, { passive: true });
    const resizeObserver =
      typeof ResizeObserver !== "undefined"
        ? new ResizeObserver(syncScrollbarState)
        : null;
    resizeObserver?.observe(viewport);
    const animationFrameId = window.requestAnimationFrame(syncScrollbarState);

    return () => {
      window.cancelAnimationFrame(animationFrameId);
      viewport.removeEventListener("scroll", syncScrollbarState);
      resizeObserver?.disconnect();
    };
  }, [getViewport, syncKey, syncScrollbarState]);

  const styleProp1Value1 = `${scrollbarState.thumbHeight}px`;
  const styleProp1Value2 = `translateY(${scrollbarState.thumbTop}px)`;
  const styleProp1 = useMemo<NonNullable<React.ComponentProps<"div">["style"]>>(
    () => ({ height: styleProp1Value1, transform: styleProp1Value2 }),
    [styleProp1Value1, styleProp1Value2]
  );
  return (
    <div
      ref={trackRef}
      className={cn("tsh-custom-scrollbar", className)}
      data-scrollable={scrollbarState.scrollable ? "true" : "false"}
      data-dragging={dragging ? "true" : "false"}
      data-testid={testId}
      aria-hidden="true"
      onMouseDown={handleTrackMouseDownCallback}
    >
      <div
        className={cn("tsh-custom-scrollbar__thumb", thumbClassName)}
        data-testid={thumbTestId}
        onMouseDown={handleThumbMouseDownCallback}
        style={styleProp1}
      />
    </div>
  );
}

export interface CustomScrollAreaProps extends HTMLAttributes<HTMLDivElement> {
  children: ReactNode;
  viewportClassName?: string;
  scrollbarClassName?: string;
  scrollbarThumbClassName?: string;
  scrollbarTestId?: string;
  scrollbarThumbTestId?: string;
  syncKey?: unknown;
}

export const CustomScrollArea = observer(
  forwardRef<HTMLDivElement, CustomScrollAreaProps>(function CustomScrollArea(
    {
      children,
      className,
      viewportClassName,
      scrollbarClassName,
      scrollbarThumbClassName,
      scrollbarTestId,
      scrollbarThumbTestId,
      syncKey,
      ...viewportProps
    },
    forwardedRef
  ): React.JSX.Element {
    "use memo";
    const viewportRef = useRef<HTMLDivElement | null>(null);
    const getViewport = useCallback(() => viewportRef.current, []);
    const getViewportCallback = usePersistCallback(getViewport);

    return (
      <div
        className={cn(
          "tsh-custom-scroll-area relative min-h-0 min-w-0",
          className
        )}
      >
        <div
          ref={setRefs(viewportRef, forwardedRef)}
          className={cn(
            "overflow-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden",
            viewportClassName
          )}
          {...viewportProps}
        >
          {children}
        </div>
        <CustomScrollbar
          getViewport={getViewportCallback}
          className={scrollbarClassName}
          thumbClassName={scrollbarThumbClassName}
          testId={scrollbarTestId}
          thumbTestId={scrollbarThumbTestId}
          syncKey={syncKey ?? children}
        />
      </div>
    );
  })
);

function setRefs<T>(
  localRef: MutableRefObject<T | null>,
  forwardedRef: ForwardedRef<T>
): (node: T | null) => void {
  return (node) => {
    localRef.current = node;
    if (typeof forwardedRef === "function") {
      forwardedRef(node);
    } else if (forwardedRef) {
      forwardedRef.current = node;
    }
  };
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}
