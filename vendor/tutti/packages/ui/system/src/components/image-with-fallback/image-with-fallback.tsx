import {
  useEffect,
  useRef,
  useState,
  type ImgHTMLAttributes,
  type ReactNode
} from "react";

const DEFAULT_RETRY_COUNT = 1;
const DEFAULT_RETRY_DELAY_MS = 150;
const MAX_RETRY_COUNT = 3;

interface ImageDeliveryState {
  attempt: number;
  phase: "ready" | "retrying" | "failed";
  src: string;
}

export interface ImageWithFallbackProps extends Omit<
  ImgHTMLAttributes<HTMLImageElement>,
  "src"
> {
  /** Content shown while retrying and after the retry budget is exhausted. */
  fallback?: ReactNode;
  /** Number of additional requests allowed after the first load failure. */
  retryCount?: number;
  /** Delay between bounded image requests. */
  retryDelayMs?: number;
  src?: string | null;
}

function normalizeRetryCount(value: number): number {
  if (!Number.isFinite(value)) {
    return DEFAULT_RETRY_COUNT;
  }
  return Math.min(MAX_RETRY_COUNT, Math.max(0, Math.floor(value)));
}

function normalizeRetryDelay(value: number): number {
  if (!Number.isFinite(value)) {
    return DEFAULT_RETRY_DELAY_MS;
  }
  return Math.max(0, Math.floor(value));
}

/**
 * Decorative remote image with a bounded same-URL retry and caller-owned
 * fallback. Retrying remounts the image without mutating the URL, so signed
 * URLs remain valid and content-addressed URLs keep their cache identity.
 */
export function ImageWithFallback({
  fallback = null,
  onError,
  retryCount = DEFAULT_RETRY_COUNT,
  retryDelayMs = DEFAULT_RETRY_DELAY_MS,
  src,
  ...imageProps
}: ImageWithFallbackProps): React.JSX.Element | null {
  const normalizedSrc = src?.trim() ?? "";
  const maxRetries = normalizeRetryCount(retryCount);
  const delayMs = normalizeRetryDelay(retryDelayMs);
  const retryTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [deliveryState, setDeliveryState] = useState<ImageDeliveryState>({
    attempt: 0,
    phase: "ready",
    src: normalizedSrc
  });

  useEffect(() => {
    if (deliveryState.src !== normalizedSrc) {
      setDeliveryState({ attempt: 0, phase: "ready", src: normalizedSrc });
    }
    if (retryTimerRef.current !== null) {
      clearTimeout(retryTimerRef.current);
      retryTimerRef.current = null;
    }
  }, [deliveryState.src, normalizedSrc]);

  useEffect(
    () => () => {
      if (retryTimerRef.current !== null) {
        clearTimeout(retryTimerRef.current);
      }
    },
    []
  );

  const currentState =
    deliveryState.src === normalizedSrc
      ? deliveryState
      : { attempt: 0, phase: "ready" as const, src: normalizedSrc };

  if (
    normalizedSrc.length === 0 ||
    currentState.phase === "failed" ||
    currentState.phase === "retrying"
  ) {
    return <>{fallback}</>;
  }

  return (
    <img
      {...imageProps}
      draggable={false}
      key={`${normalizedSrc}:${currentState.attempt}`}
      src={normalizedSrc}
      onError={(event) => {
        onError?.(event);
        if (currentState.attempt >= maxRetries) {
          setDeliveryState((previous) =>
            previous.src === normalizedSrc
              ? { ...previous, phase: "failed" }
              : previous
          );
          return;
        }

        const nextAttempt = currentState.attempt + 1;
        setDeliveryState((previous) =>
          previous.src === normalizedSrc
            ? { attempt: nextAttempt, phase: "retrying", src: normalizedSrc }
            : previous
        );
        retryTimerRef.current = setTimeout(() => {
          retryTimerRef.current = null;
          setDeliveryState((previous) =>
            previous.src === normalizedSrc &&
            previous.attempt === nextAttempt &&
            previous.phase === "retrying"
              ? { ...previous, phase: "ready" }
              : previous
          );
        }, delayMs);
      }}
    />
  );
}
