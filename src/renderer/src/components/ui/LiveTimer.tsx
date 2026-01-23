/**
 * LiveTimer - Real-time timer with smooth updates during processing
 *
 * Key features:
 * - Event-driven updates: when events are arriving, shows exact value from props
 * - Timeout projection: when no events for 500ms, projects time forward with variable interval
 * - Variable interval: 80-180ms using sin wave for natural feel (not mechanical tick)
 * - Immediate sync: snaps to accurate value when new events arrive
 */
import { useState, useEffect, useRef } from 'react'
import { formatDuration } from '@/lib/utils'

export interface LiveTimerProps {
  /** Base elapsed time in milliseconds (from events) */
  baseElapsedMs: number
  /** Whether the message is still processing */
  isProcessing: boolean
  /** Timestamp when the last event was received (Date.now() value) */
  lastEventTimestamp?: number
  /** Additional className */
  className?: string
}

// Timeout threshold: start projecting after 500ms of no events
const PROJECTION_THRESHOLD_MS = 500
// Base update interval for variable timing
const BASE_INTERVAL_MS = 130
// Amplitude for sin wave variation (±50ms)
const INTERVAL_AMPLITUDE_MS = 50
// Period for sin wave oscillation
const SIN_PERIOD_MS = 400

export function LiveTimer({
  baseElapsedMs,
  isProcessing,
  lastEventTimestamp,
  className
}: LiveTimerProps): React.JSX.Element {
  // Store the projection offset (time added on top of baseElapsedMs when projecting)
  const [projectionOffset, setProjectionOffset] = useState(0)

  // Track when we last updated the display (for variable interval)
  const lastUpdateRef = useRef<number>(0)
  // Track when projection started
  const projectionStartRef = useRef<number>(0)
  // Track the baseElapsedMs that was active when projection started
  const projectionBaseRef = useRef<number>(baseElapsedMs)

  // Calculate displayed value:
  // - When processing: base + projection offset (for smooth animation during pauses)
  // - When not processing: just base (projection offset is irrelevant)
  // This eliminates the need for a separate effect to reset projectionOffset
  const displayedMs = isProcessing ? baseElapsedMs + projectionOffset : baseElapsedMs

  // Animation loop for smooth projection during long pauses
  useEffect(() => {
    if (!isProcessing) {
      // Not processing, no animation needed
      // Note: projectionOffset may be stale, but displayedMs ignores it when !isProcessing
      return
    }

    // Reset refs when effect starts (new processing session)
    projectionStartRef.current = 0
    lastUpdateRef.current = 0
    projectionBaseRef.current = baseElapsedMs

    let animationFrameId: number

    const animate = (): void => {
      const now = performance.now()

      // Calculate time since last event
      const timeSinceLastEvent = lastEventTimestamp ? Date.now() - lastEventTimestamp : Infinity

      // Only project if we've exceeded the threshold
      if (timeSinceLastEvent > PROJECTION_THRESHOLD_MS) {
        // Initialize projection start time if this is the first projection frame
        if (projectionStartRef.current === 0) {
          projectionStartRef.current = now
          lastUpdateRef.current = now
        }

        // Calculate variable interval using sin wave
        // This creates a natural, non-mechanical feel: 80-180ms
        const variableInterval =
          BASE_INTERVAL_MS + Math.sin(now / SIN_PERIOD_MS) * INTERVAL_AMPLITUDE_MS

        // Check if enough time has passed since last update
        if (now - lastUpdateRef.current >= variableInterval) {
          // Calculate projected offset since projection started
          const newOffset = now - projectionStartRef.current

          setProjectionOffset(newOffset)
          lastUpdateRef.current = now
        }
      } else {
        // Events are arriving, reset projection (called from callback, so OK)
        if (projectionStartRef.current !== 0) {
          projectionStartRef.current = 0
          setProjectionOffset(0)
        }
      }

      animationFrameId = requestAnimationFrame(animate)
    }

    // Start animation loop
    animationFrameId = requestAnimationFrame(animate)

    return () => {
      cancelAnimationFrame(animationFrameId)
    }
  }, [isProcessing, lastEventTimestamp, baseElapsedMs])

  return <span className={className}>{displayedMs > 0 ? formatDuration(displayedMs) : null}</span>
}
