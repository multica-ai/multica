import { type RefObject, useEffect, useRef, useCallback } from "react"

/**
 * Auto-scrolls a scroll container to the bottom when its inner content grows,
 * as long as the user hasn't scrolled up to read older content.
 *
 * Returns a `lockRef` that can be set to `true` to temporarily suppress
 * auto-scroll (e.g. during history prepend operations).
 */
export function useAutoScroll(ref: RefObject<HTMLElement | null>) {
  const stickRef = useRef(true)
  const lockRef = useRef(false)

  useEffect(() => {
    const el = ref.current
    if (!el) return

    const scrollToBottom = () => {
      el.scrollTo({ top: el.scrollHeight })
    }

    const onScroll = () => {
      const { scrollTop, scrollHeight, clientHeight } = el
      stickRef.current = scrollHeight - scrollTop - clientHeight < 50
    }

    const onContentChange = () => {
      if (lockRef.current) return
      if (stickRef.current) {
        scrollToBottom()
      }
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
    scrollToBottom()

    return () => {
      el.removeEventListener("scroll", onScroll)
      ro.disconnect()
      mo.disconnect()
    }
  }, [ref])

  /** Temporarily suppress auto-scroll during prepend operations */
  const suppressAutoScroll = useCallback(() => {
    lockRef.current = true
    return () => { lockRef.current = false }
  }, [])

  return { suppressAutoScroll }
}
