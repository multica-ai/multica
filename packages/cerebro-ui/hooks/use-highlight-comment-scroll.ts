import { useEffect, useRef, useState } from "react"

/**
 * Inbox → issue auto-scroll for a specific comment.
 *
 * When the user taps an inbox item the host (IssueDetail) re-mounts and
 * receives a `highlightCommentId` prop pointing at the comment that triggered
 * the notification. This hook owns three concerns:
 *
 * 1. **Scroll**: once the corresponding `#comment-<id>` element is in the DOM,
 *    `scrollIntoView({ block: "start" })` anchors its top to the top of the
 *    nearest scroll container. We use `block: "start"` (not `center`) because
 *    on mobile, centering a comment leaves visible whitespace above and the
 *    landing position feels imprecise (JEH-1002).
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
      if (el) {
        didHighlightRef.current = highlightCommentId
        el.scrollIntoView({ behavior: "instant", block: "start" })
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
