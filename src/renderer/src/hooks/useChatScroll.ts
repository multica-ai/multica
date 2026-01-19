/**
 * Chat scroll behavior hook
 *
 * Handles:
 * 1. Session switching - reset to "at bottom" state, scroll handled by onContentUpdate
 * 2. Auto-scroll during streaming - only if at bottom, using rAF to prevent jitter
 * 3. User scroll detection - respect user intent with a lock period
 * 4. IntersectionObserver for precise bottom detection
 *
 * Design principle: Keep it simple. No scroll position storage per session.
 * Switching sessions always scrolls to bottom (better UX, simpler code).
 */
import { useRef, useEffect, useCallback, useState } from 'react'

interface UseChatScrollOptions {
  /** Current session ID - scroll resets when this changes */
  sessionId: string | null
  /** Whether new content is being generated */
  isStreaming?: boolean
}

interface UseChatScrollReturn {
  /** Ref for the scroll container */
  containerRef: React.RefObject<HTMLDivElement | null>
  /** Ref for the bottom anchor element */
  bottomRef: React.RefObject<HTMLDivElement | null>
  /** Whether the user is currently at the bottom */
  isAtBottom: boolean
  /** Scroll event handler - attach to container's onScroll */
  handleScroll: () => void
  /** Manually scroll to bottom (e.g., for a "scroll to bottom" button) */
  scrollToBottom: (smooth?: boolean) => void
  /** Call this when new content arrives to auto-scroll if appropriate */
  onContentUpdate: () => void
}

// How long to "lock" after user scrolls (ms)
const USER_SCROLL_LOCK_DURATION = 150

// Threshold for "at bottom" detection (percentage of container height)
// 20% provides good coverage for large content like headers, code blocks, dividers
const AT_BOTTOM_THRESHOLD_PERCENT = 0.2

export function useChatScroll({
  sessionId,
  isStreaming: _isStreaming = false
}: UseChatScrollOptions): UseChatScrollReturn {
  // Note: _isStreaming is available for future use (e.g., different scroll behavior during streaming)
  void _isStreaming
  const containerRef = useRef<HTMLDivElement>(null)
  const bottomRef = useRef<HTMLDivElement>(null)

  // Track if user is at bottom (for UI, e.g., showing "scroll to bottom" button)
  const [isAtBottom, setIsAtBottom] = useState(true)

  // Internal refs for scroll logic (refs don't trigger re-renders)
  const isAtBottomRef = useRef(true)
  const prevSessionIdRef = useRef<string | null>(null)
  const userScrollingRef = useRef(false)
  const scrollLockTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const rafRef = useRef<number | null>(null)
  const observerRef = useRef<IntersectionObserver | null>(null)

  // Cleanup function for timeouts and rafs
  const cleanup = useCallback(() => {
    if (scrollLockTimeoutRef.current) {
      clearTimeout(scrollLockTimeoutRef.current)
      scrollLockTimeoutRef.current = null
    }
    if (rafRef.current) {
      cancelAnimationFrame(rafRef.current)
      rafRef.current = null
    }
  }, [])

  // Scroll to bottom with rAF debouncing
  const scrollToBottom = useCallback((smooth = false) => {
    // Cancel any pending scroll
    if (rafRef.current) {
      cancelAnimationFrame(rafRef.current)
    }

    rafRef.current = requestAnimationFrame(() => {
      bottomRef.current?.scrollIntoView({
        behavior: smooth ? 'smooth' : 'instant'
      })
      rafRef.current = null
    })
  }, [])

  // Session change detection - reset state and let onContentUpdate handle scrolling
  useEffect(() => {
    if (sessionId !== prevSessionIdRef.current) {
      prevSessionIdRef.current = sessionId

      // Reset scroll state
      userScrollingRef.current = false
      cleanup()

      // Reset to "at bottom" state - onContentUpdate will scroll when content arrives
      isAtBottomRef.current = true
      setIsAtBottom(true)
    }
  }, [sessionId, cleanup])

  // Set up IntersectionObserver for precise bottom detection
  useEffect(() => {
    // Clean up previous observer
    if (observerRef.current) {
      observerRef.current.disconnect()
    }

    const container = containerRef.current
    const bottom = bottomRef.current

    if (!container || !bottom) return

    // Calculate rootMargin based on container height (20%)
    const rootMarginBottom = Math.round(container.clientHeight * AT_BOTTOM_THRESHOLD_PERCENT)

    observerRef.current = new IntersectionObserver(
      ([entry]) => {
        // Only update if user is not actively scrolling
        if (!userScrollingRef.current) {
          const isVisible = entry.isIntersecting
          isAtBottomRef.current = isVisible
          setIsAtBottom(isVisible)
        }
      },
      {
        root: container,
        // Use percentage-based margin for better coverage of large content
        rootMargin: `0px 0px ${rootMarginBottom}px 0px`,
        threshold: 0
      }
    )

    observerRef.current.observe(bottom)

    return () => {
      if (observerRef.current) {
        observerRef.current.disconnect()
        observerRef.current = null
      }
    }
  }, [sessionId]) // Re-create observer when session changes

  // Handle user scroll - detect scrolling and set lock period
  const handleScroll = useCallback(() => {
    // Mark user as scrolling
    userScrollingRef.current = true

    // Clear previous timeout
    if (scrollLockTimeoutRef.current) {
      clearTimeout(scrollLockTimeoutRef.current)
    }

    // After lock period (debounced): update isAtBottom state
    scrollLockTimeoutRef.current = setTimeout(() => {
      userScrollingRef.current = false

      const container = containerRef.current
      if (container) {
        // Update isAtBottom based on current scroll position
        const threshold = container.clientHeight * AT_BOTTOM_THRESHOLD_PERCENT
        const distanceFromBottom =
          container.scrollHeight - container.scrollTop - container.clientHeight
        const atBottom = distanceFromBottom < threshold
        isAtBottomRef.current = atBottom
        setIsAtBottom(atBottom)
      }

      scrollLockTimeoutRef.current = null
    }, USER_SCROLL_LOCK_DURATION)
  }, []) // No dependencies - stable callback

  // Called when new content arrives (streaming chunks, new messages)
  const onContentUpdate = useCallback(() => {
    // Don't auto-scroll if:
    // 1. User is actively scrolling
    // 2. User has scrolled away from bottom
    if (userScrollingRef.current || !isAtBottomRef.current) {
      return
    }

    // Use instant scroll during streaming to avoid animation stacking
    scrollToBottom(false)
  }, [scrollToBottom])

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      cleanup()
      if (observerRef.current) {
        observerRef.current.disconnect()
      }
    }
  }, [cleanup])

  return {
    containerRef,
    bottomRef,
    isAtBottom,
    handleScroll,
    scrollToBottom,
    onContentUpdate
  }
}
