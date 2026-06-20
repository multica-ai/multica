import { type RefObject, useEffect, useRef, useState } from "react"

/**
 * Inbox → issue auto-scroll for a specific comment.
 *
 * When the user taps an inbox item the host (IssueDetail) re-mounts and
 * receives a `highlightCommentId` prop pointing at the comment that triggered
 * the notification. This hook owns three concerns:
 *
 * 1. **Scroll**: once the corresponding `#comment-<id>` element is in the DOM,
 *    we scroll the passed `scrollContainerRef` (the issue body's own scroll
 *    region) so the comment's top aligns with the container's top. We must NOT
 *    use `el.scrollIntoView()` here: it bubbles to every scrollable ancestor,
 *    including the dashboard's `overflow:hidden` SidebarInset, which it scrolls
 *    too — dragging the whole detail pane (title bar included) up under the top
 *    edge with no way to scroll it back (FIR-1652). Scrolling only the known
 *    container keeps the header in place. We anchor to the top (not center)
 *    because on mobile, centering a comment leaves visible whitespace above and
 *    the landing position feels imprecise (JEH-1002).
 *
 * 2. **Retry**: re-tapping the same inbox item is the failure mode that broke
 *    the original implementation. Issue + timeline data are cached from the
 *    first visit, so on remount `timeline.length` never transitions 0→N and a
 *    single-shot `useEffect` keyed on that value misses the moment the comment
 *    node appears in the DOM. We poll on `requestAnimationFrame` (capped at
 *    3s) so a cached remount lands on the highlighted comment exactly like a
 *    cold mount does.
 *
 * 3. **Pulse**: returns the highlighted comment id for ~2s so the caller can
 *    render a brief visual pulse on the comment. The pulse is the entire
 *    "you're here" affordance — we avoid smooth scrolling because animated
 *    inbox-yank behaviour was the original UX complaint that prompted this
 *    work.
 *
 * `didHighlightRef` is the per-mount guard against re-firing within the same
 * IssueDetail lifecycle; it's a ref (not state) because we want it set
 * synchronously inside the rAF callback before any subsequent frame retries.
 */
export function useHighlightCommentScroll(
  highlightCommentId: string | undefined,
  scrollContainerRef: RefObject<HTMLElement | null>,
  pulseMs = 2000,
  retryBudgetMs = 3000,
): string | null {
  const [highlightedId, setHighlightedId] = useState<string | null>(null)
  const didHighlightRef = useRef<string | null>(null)

  useEffect(() => {
    if (!highlightCommentId) return
    if (didHighlightRef.current === highlightCommentId) return
    let cancelled = false
    let pulseTimer: ReturnType<typeof setTimeout> | null = null
    const deadline = performance.now() + retryBudgetMs

    const tryScroll = () => {
      if (cancelled) return
      if (didHighlightRef.current === highlightCommentId) return
      const el = document.getElementById(`comment-${highlightCommentId}`)
      const container = scrollContainerRef.current
      if (el && container) {
        didHighlightRef.current = highlightCommentId
        // Scroll ONLY this container — never el.scrollIntoView(), which also
        // scrolls overflow:hidden ancestors and hides the pane header (FIR-1652).
        const delta =
          el.getBoundingClientRect().top - container.getBoundingClientRect().top
        container.scrollTop += delta
        setHighlightedId(highlightCommentId)
        pulseTimer = setTimeout(() => setHighlightedId(null), pulseMs)
        return
      }
      if (performance.now() < deadline) requestAnimationFrame(tryScroll)
    }

    requestAnimationFrame(tryScroll)

    return () => {
      cancelled = true
      if (pulseTimer) clearTimeout(pulseTimer)
    }
  }, [highlightCommentId, pulseMs, retryBudgetMs])

  return highlightedId
}
