// CEREBRO-PATCH(ui-hook-use-sticky-bottom): cerebro modification of upstream file
import {
  type RefObject,
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react"

interface Options {
  /** Distance from the bottom (px) below which the user is considered "at bottom". */
  threshold?: number
  /** Sync-jump to bottom on first mount (no animation). */
  initialScroll?: boolean
}

interface StickyBottomState {
  /** True when the user is within `threshold` px of the bottom. */
  isAtBottom: boolean
  /** True when content was appended below the viewport while the user wasn't at bottom. */
  hasNewBelow: boolean
  /** Smooth-scroll to bottom (clears `hasNewBelow`). */
  scrollToBottom: (smooth?: boolean) => void
  /** Temporarily suppress auto-scroll during prepend operations (e.g. infinite scroll up). */
  suppressAutoScroll: () => () => void
}

/**
 * Pins a scroll container to the bottom while the user is already at the bottom,
 * but never yanks them down once they've scrolled up to read older content.
 *
 * - Initial mount jumps instantly to the bottom (sync, no animation flash).
 * - New content while at bottom: stays glued.
 * - New content while scrolled up: surfaces `hasNewBelow` so callers can render
 *   a "jump to latest" affordance.
 */
export function useStickyBottom(
  ref: RefObject<HTMLElement | null>,
  options: Options = {},
): StickyBottomState {
  const { threshold = 50, initialScroll = true } = options
  // Default to `initialScroll` for both refs/state: chat-style consumers
  // (`initialScroll: true`) want the auto-snap to fire on the first content
  // burst, but consumers that pass `initialScroll: false` (issue pages with a
  // scroll target) must NOT snap to bottom before they've had a chance to
  // run their own `scrollIntoView`. JEH-1002: without this, the first
  // MutationObserver callback after a click-through inbox navigation saw
  // `stickRef.current = true` (the old default) and yanked the page to the
  // bottom, beating the highlight scroll.
  const stickRef = useRef(initialScroll)
  const lockRef = useRef(false)
  const [isAtBottom, setIsAtBottom] = useState(initialScroll)
  const [hasNewBelow, setHasNewBelow] = useState(false)

  // Sync jump on first mount — runs before paint so the user never sees the
  // top of the list animate down.
  useLayoutEffect(() => {
    if (!initialScroll) return
    const el = ref.current
    if (!el) return
    el.scrollTop = el.scrollHeight
  }, [ref, initialScroll])

  useEffect(() => {
    const el = ref.current
    if (!el) return

    const measure = () => {
      const distance = el.scrollHeight - el.scrollTop - el.clientHeight
      return distance < threshold
    }

    const onScroll = () => {
      const at = measure()
      stickRef.current = at
      setIsAtBottom(at)
      if (at) setHasNewBelow(false)
    }

    const onContentChange = () => {
      if (lockRef.current) return
      if (stickRef.current) {
        el.scrollTop = el.scrollHeight
      } else {
        // Content grew below the viewport while we're not stuck → notify caller.
        setHasNewBelow(true)
      }
    }

    const ro = new ResizeObserver(onContentChange)
    for (const child of el.children) {
      ro.observe(child)
    }

    const mo = new MutationObserver((mutations) => {
      for (const mutation of mutations) {
        for (const node of mutation.addedNodes) {
          if (node instanceof Element) ro.observe(node)
        }
      }
      onContentChange()
    })
    mo.observe(el, { childList: true, subtree: true })

    el.addEventListener("scroll", onScroll, { passive: true })
    // Measure once so callers see the real state (e.g. issue pages that
    // render with content below the fold but never scrolled).
    // For non-initialScroll consumers, only believe we're at-bottom if
    // `scrollTop > 0` — at `scrollTop === 0` we're indistinguishable from
    // "just mounted, content hasn't loaded yet", and snapping to bottom on
    // the next mutation would steal a pending highlight scroll. Chat-style
    // consumers always run `useLayoutEffect` to jump to bottom first, so
    // `scrollTop` is already positive by the time we measure here.
    const initiallyAt = initialScroll
      ? measure()
      : el.scrollTop > 0 && measure()
    stickRef.current = initiallyAt
    setIsAtBottom(initiallyAt)

    return () => {
      el.removeEventListener("scroll", onScroll)
      ro.disconnect()
      mo.disconnect()
    }
  }, [ref, threshold, initialScroll])

  const scrollToBottom = useCallback(
    (smooth = true) => {
      const el = ref.current
      if (!el) return
      el.scrollTo({
        top: el.scrollHeight,
        behavior: smooth ? "smooth" : "auto",
      })
      stickRef.current = true
      setHasNewBelow(false)
    },
    [ref],
  )

  const suppressAutoScroll = useCallback(() => {
    lockRef.current = true
    return () => {
      lockRef.current = false
    }
  }, [])

  return { isAtBottom, hasNewBelow, scrollToBottom, suppressAutoScroll }
}
