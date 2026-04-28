import { type RefObject, useEffect, useRef, useCallback, useState } from "react"

/**
 * Auto-scrolls a scroll container to the bottom when its inner content grows,
 * as long as the user hasn't scrolled up to read older content.
 *
 * Returns:
 *  - `suppressAutoScroll` — temporarily disable auto-scroll (e.g. during
 *    history prepend operations); call the returned fn to release.
 *  - `isAtBottom` — reactive flag indicating whether the container is
 *    currently within ~50px of the bottom. Useful for showing a
 *    "scroll to bottom" affordance.
 *  - `scrollToBottom` — smoothly scroll the container to the bottom and
 *    re-arm sticky auto-scroll behavior.
 */
export function useAutoScroll(ref: RefObject<HTMLElement | null>) {
  const stickRef = useRef(true)
  const lockRef = useRef(false)
  const isUserScrollingRef = useRef(false)
  const scrollTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  // Reactive mirror of stickRef so consumers can render UI based on it.
  // Default true: empty / non-scrollable lists are considered "at bottom",
  // which keeps the affordance hidden by default.
  const [isAtBottom, setIsAtBottom] = useState(true)

  useEffect(() => {
    const el = ref.current
    if (!el) return

    const computeAtBottom = () => {
      const { scrollTop, scrollHeight, clientHeight } = el
      return scrollHeight - scrollTop - clientHeight < 50
    }

    const updateStick = (next: boolean) => {
      stickRef.current = next
      setIsAtBottom((prev) => (prev === next ? prev : next))
    }

    const scrollToBottomInternal = () => {
      el.scrollTo({ top: el.scrollHeight })
    }

    const onScroll = () => {
      updateStick(computeAtBottom())

      // While the user is actively scrolling, suppress auto-scroll
      // so ResizeObserver callbacks don't fight the manual scroll.
      isUserScrollingRef.current = true
      if (scrollTimeoutRef.current) clearTimeout(scrollTimeoutRef.current)
      scrollTimeoutRef.current = setTimeout(() => {
        isUserScrollingRef.current = false
      }, 150)
    }

    const onContentChange = () => {
      if (lockRef.current) return
      if (isUserScrollingRef.current) return
      if (stickRef.current) {
        scrollToBottomInternal()
      }
      // Recompute after layout settles, in case content growth pushed
      // the viewport off-bottom without firing a user scroll event.
      updateStick(computeAtBottom())
    }

    // Watch child element resizes (content growth, image loads, streaming)
    const ro = new ResizeObserver(onContentChange)
    for (const child of el.children) {
      ro.observe(child)
    }

    // Watch for added/removed child nodes (new messages rendered)
    const mo = new MutationObserver((mutations) => {
      // Also observe newly added elements
      for (const mutation of mutations) {
        for (const node of mutation.addedNodes) {
          if (node instanceof Element) {
            ro.observe(node)
          }
        }
      }
      onContentChange()
    })
    mo.observe(el, { childList: true, subtree: true })

    el.addEventListener("scroll", onScroll, { passive: true })

    // Initial scroll to bottom
    scrollToBottomInternal()
    updateStick(computeAtBottom())

    return () => {
      el.removeEventListener("scroll", onScroll)
      ro.disconnect()
      mo.disconnect()
      if (scrollTimeoutRef.current) clearTimeout(scrollTimeoutRef.current)
    }
  }, [ref])

  /** Temporarily suppress auto-scroll during prepend operations */
  const suppressAutoScroll = useCallback(() => {
    lockRef.current = true
    return () => { lockRef.current = false }
  }, [])

  /**
   * Smoothly scroll to the bottom and re-arm sticky behavior so subsequent
   * content growth keeps the user pinned to the latest message.
   */
  const scrollToBottom = useCallback(() => {
    const el = ref.current
    if (!el) return
    stickRef.current = true
    setIsAtBottom(true)
    el.scrollTo({ top: el.scrollHeight, behavior: "smooth" })
  }, [ref])

  return { suppressAutoScroll, isAtBottom, scrollToBottom }
}
