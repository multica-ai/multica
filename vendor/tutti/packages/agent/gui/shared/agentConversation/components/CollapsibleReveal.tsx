import { useMemo } from "react";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type JSX,
  type ReactNode,
  type TransitionEvent
} from "react";
import { observer } from "mobx-react-lite";
export const CollapsibleReveal = observer(CollapsibleRevealRender);

interface CollapsibleRevealProps {
  expanded: boolean;
  children: ReactNode;
  className?: string;
  innerClassName?: string;
  onHeightTransitionEnd?: () => void;
  preMountOnIdle?: boolean;
}

function CollapsibleRevealRender({
  expanded,
  children,
  className,
  innerClassName,
  onHeightTransitionEnd,
  preMountOnIdle = false
}: CollapsibleRevealProps): JSX.Element | null {
  const setRootRefCallback = usePersistCallback<typeof setRootRef>((...args) =>
    setRootRef(...args)
  );

  const handleTransitionEndCallback = usePersistCallback<
    typeof handleTransitionEnd
  >((...args) => handleTransitionEnd(...args));

  ("use memo");
  const [mounted, setMounted] = useState(expanded);
  const [visible, setVisible] = useState(expanded);
  const [height, setHeight] = useState<string>(expanded ? "auto" : "0px");
  const rootRef = useRef<HTMLDivElement | null>(null);
  const innerRef = useRef<HTMLDivElement | null>(null);
  const heightRef = useRef(height);
  const measuredHeightRef = useRef<number | null>(null);
  const previousExpandedRef = useRef(expanded);
  const heightTransitionPendingRef = useRef(false);

  const setRootRef = useCallback(
    (node: HTMLDivElement | null) => {
      if (
        rootRef.current !== null &&
        node === null &&
        heightTransitionPendingRef.current
      ) {
        heightTransitionPendingRef.current = false;
        onHeightTransitionEnd?.();
      }
      rootRef.current = node;
    },
    [onHeightTransitionEnd]
  );

  const setRevealHeight = useCallback((nextHeight: string) => {
    heightRef.current = nextHeight;
    setHeight(nextHeight);
  }, []);

  useLayoutEffect(() => {
    if (!expanded || mounted) {
      return undefined;
    }
    if (preMountOnIdle) {
      setMounted(true);
      return undefined;
    }
    let mountFrameStarted = false;
    const animationFrame = requestAnimationFrame(() => {
      mountFrameStarted = true;
      setMounted(true);
    });
    return () => {
      cancelAnimationFrame(animationFrame);
      if (!mountFrameStarted) {
        onHeightTransitionEnd?.();
      }
    };
  }, [expanded, mounted, onHeightTransitionEnd, preMountOnIdle]);

  useEffect(() => {
    if (!preMountOnIdle || mounted || expanded) {
      return undefined;
    }

    const mountCollapsedContent = () => setMounted(true);
    if ("requestIdleCallback" in window) {
      const idleCallbackId = window.requestIdleCallback(mountCollapsedContent, {
        timeout: 600
      });
      return () => window.cancelIdleCallback(idleCallbackId);
    }

    const timeoutId = globalThis.setTimeout(mountCollapsedContent, 120);
    return () => globalThis.clearTimeout(timeoutId);
  }, [expanded, mounted, preMountOnIdle]);

  useLayoutEffect(() => {
    if (!mounted) {
      return undefined;
    }

    const root = rootRef.current;
    if (!root) {
      return undefined;
    }

    const wasExpanded = previousExpandedRef.current;
    previousExpandedRef.current = expanded;

    if (expanded) {
      if (wasExpanded && visible && height === "auto") {
        return undefined;
      }
      heightTransitionPendingRef.current = true;
      setVisible(false);
      setRevealHeight("0px");
      const animationFrame = requestAnimationFrame(() => {
        const measured = Math.max(
          root.scrollHeight,
          innerRef.current?.scrollHeight ?? 0
        );
        measuredHeightRef.current = measured;
        // Make the reveal accessible on the first measure frame even when
        // children have not reported a measurable height yet.
        setVisible(true);
        setRevealHeight(measured > 0 ? `${measured}px` : "auto");
      });
      return () => {
        cancelAnimationFrame(animationFrame);
      };
    }

    if (!wasExpanded) {
      heightTransitionPendingRef.current = false;
      setVisible(false);
      setRevealHeight("0px");
      return undefined;
    }

    const renderedHeight = root.getBoundingClientRect().height;
    const cachedHeight = measuredHeightRef.current;
    const measuredHeight =
      renderedHeight > 0 ? renderedHeight : (cachedHeight ?? root.scrollHeight);
    measuredHeightRef.current = measuredHeight;
    heightTransitionPendingRef.current = true;
    setRevealHeight(`${measuredHeight}px`);
    setVisible(false);
    const animationFrame = requestAnimationFrame(() => {
      setRevealHeight("0px");
    });
    return () => cancelAnimationFrame(animationFrame);
  }, [expanded, mounted]);

  useLayoutEffect(() => {
    if (!mounted || !expanded || !visible) {
      return undefined;
    }

    const root = rootRef.current;
    const inner = innerRef.current;
    if (!root || !inner || typeof ResizeObserver === "undefined") {
      return undefined;
    }

    let animationFrame: number | null = null;
    const resizeObserver = new ResizeObserver((entries) => {
      const innerScrollHeight = inner.scrollHeight;
      const nextHeight = Math.ceil(
        innerScrollHeight > 0
          ? innerScrollHeight
          : (entries[0]?.contentRect.height ?? 0)
      );
      const previousHeight = measuredHeightRef.current;
      if (!nextHeight || previousHeight === nextHeight) {
        return;
      }

      measuredHeightRef.current = nextHeight;
      if (heightRef.current === "auto") {
        return;
      }
      setRevealHeight(
        `${previousHeight ?? root.getBoundingClientRect().height}px`
      );
      if (animationFrame !== null) {
        cancelAnimationFrame(animationFrame);
      }
      animationFrame = requestAnimationFrame(() => {
        setRevealHeight(`${nextHeight}px`);
      });
    });

    resizeObserver.observe(inner);
    return () => {
      if (animationFrame !== null) {
        cancelAnimationFrame(animationFrame);
      }
      resizeObserver.disconnect();
    };
  }, [expanded, mounted, setRevealHeight, visible]);

  const rootStyle: CSSProperties = useMemo<CSSProperties>(
    () => ({ height }),
    [height]
  );

  if (!mounted) {
    return null;
  }

  const handleTransitionEnd = (event: TransitionEvent<HTMLDivElement>) => {
    if (
      event.target !== event.currentTarget ||
      (event.propertyName ? event.propertyName !== "height" : false)
    ) {
      return;
    }
    if (visible) {
      setRevealHeight("auto");
    } else if (!expanded && !preMountOnIdle) {
      setMounted(false);
    }
    if (heightTransitionPendingRef.current) {
      heightTransitionPendingRef.current = false;
      onHeightTransitionEnd?.();
    }
  };

  return (
    <div
      ref={setRootRefCallback}
      className={["agent-collapsible-reveal", className ?? ""]
        .filter(Boolean)
        .join(" ")}
      data-expanded={
        preMountOnIdle && expanded ? "true" : visible ? "true" : "false"
      }
      aria-hidden={visible ? undefined : true}
      style={rootStyle}
      onTransitionEnd={handleTransitionEndCallback}
    >
      <div
        ref={innerRef}
        className={["agent-collapsible-reveal__inner", innerClassName ?? ""]
          .filter(Boolean)
          .join(" ")}
      >
        {children}
      </div>
    </div>
  );
}
