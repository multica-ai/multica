"use client"

import * as React from "react"

export type ResizeAxis = "x" | "y" | "xy"

export type ResizeDelta = {
  // Pointer displacement from where the gesture started, in CSS pixels.
  // Sign, clamping and committing are the caller's job: every call site
  // maps these to a different target (sidebar width, chat width/height)
  // with a different sign convention.
  dx: number
  dy: number
}

export type ResizeEndMode = "commit" | "cancel"

const RESIZE_AXIS_ATTRIBUTE = "data-resize-axis"
const RESIZING_ATTRIBUTE = "data-resizing"

export interface UseResizeGestureOptions {
  axis: ResizeAxis
  // Return false to abort the gesture before it starts (for example when the
  // layout elements the caller needs are not mounted).
  onStart?: (event: React.PointerEvent<HTMLElement>) => boolean | void
  // Called only after `threshold` has been exceeded, so the first call always
  // means "the user is really dragging", not "the user clicked".
  onMove: (delta: ResizeDelta, event: PointerEvent) => void
  onEnd?: (mode: ResizeEndMode) => void
  // Pointer travel required before the gesture counts as a drag.
  threshold?: number
  disabled?: boolean
}

function exceedsThreshold(axis: ResizeAxis, delta: ResizeDelta, threshold: number) {
  if (threshold <= 0) return true
  switch (axis) {
    case "x":
      return Math.abs(delta.dx) >= threshold
    case "y":
      return Math.abs(delta.dy) >= threshold
    case "xy":
      return Math.max(Math.abs(delta.dx), Math.abs(delta.dy)) >= threshold
  }
}

// Shared pointer-drag gesture for every hand-written resize handle.
//
// It owns exactly three things, which are the three contracts that used to be
// re-implemented (and got out of sync) at each call site:
//
//   1. The global cursor lock. `data-resize-axis` on <html> drives the
//      `!important` rules in base.css, so the resize cursor survives leaving
//      the handle and beats descendants that declare their own cursor.
//   2. `data-resizing` on the handle, for the active indicator. Set
//      imperatively so a live drag never goes through React state.
//   3. Teardown. A single `finishDrag` covers pointerup, pointercancel,
//      lostpointercapture, window blur, `disabled` flipping mid-drag, unmount
//      and a throwing callback. Leaking `data-resize-axis` would lock the
//      cursor for the whole document until reload, so every path runs it.
//
// State stays with the caller: this hook holds no React state and never
// re-renders on pointer move.
export function useResizeGesture({
  axis,
  onStart,
  onMove,
  onEnd,
  threshold = 0,
  disabled = false,
}: UseResizeGestureOptions) {
  const cancelActiveDragRef = React.useRef<(() => void) | null>(null)

  // Keep callbacks in refs so an in-flight drag always calls the latest
  // version without re-subscribing listeners mid-gesture.
  const onStartRef = React.useRef(onStart)
  const onMoveRef = React.useRef(onMove)
  const onEndRef = React.useRef(onEnd)
  onStartRef.current = onStart
  onMoveRef.current = onMove
  onEndRef.current = onEnd

  React.useEffect(() => () => cancelActiveDragRef.current?.(), [])

  React.useEffect(() => {
    if (disabled) cancelActiveDragRef.current?.()
  }, [disabled])

  const onPointerDown = React.useCallback(
    (event: React.PointerEvent<HTMLElement>) => {
      if (disabled) return
      if (event.button !== 0 || event.isPrimary === false) return

      const handleEl = event.currentTarget
      const { pointerId } = event

      event.preventDefault()
      cancelActiveDragRef.current?.()

      if (onStartRef.current?.(event) === false) return

      const startX = event.clientX
      const startY = event.clientY
      let moved = false
      let finished = false

      const finishDrag = (mode: ResizeEndMode) => {
        if (finished) return
        finished = true

        document.removeEventListener("pointermove", handlePointerMove)
        document.removeEventListener("pointerup", handlePointerUp)
        document.removeEventListener("pointercancel", handlePointerCancel)
        window.removeEventListener("blur", handleWindowBlur)
        handleEl.removeEventListener("lostpointercapture", handleLostPointerCapture)

        try {
          onEndRef.current?.(mode)
        } finally {
          document.documentElement.removeAttribute(RESIZE_AXIS_ATTRIBUTE)
          handleEl.removeAttribute(RESIZING_ATTRIBUTE)
          cancelActiveDragRef.current = null
          if (handleEl.hasPointerCapture?.(pointerId)) {
            handleEl.releasePointerCapture?.(pointerId)
          }
        }
      }

      const handlePointerMove = (moveEvent: PointerEvent) => {
        if (moveEvent.pointerId !== pointerId) return

        const delta: ResizeDelta = {
          dx: moveEvent.clientX - startX,
          dy: moveEvent.clientY - startY,
        }
        if (!moved) {
          if (!exceedsThreshold(axis, delta, threshold)) return
          moved = true
        }

        try {
          onMoveRef.current(delta, moveEvent)
        } catch (error) {
          // Never leave the document cursor locked because a caller threw.
          finishDrag("cancel")
          throw error
        }
      }
      const handlePointerUp = (upEvent: PointerEvent) => {
        if (upEvent.pointerId === pointerId) finishDrag("commit")
      }
      const handlePointerCancel = (cancelEvent: PointerEvent) => {
        if (cancelEvent.pointerId === pointerId) finishDrag("cancel")
      }
      const handleLostPointerCapture = (lostEvent: Event) => {
        if ((lostEvent as PointerEvent).pointerId === pointerId) finishDrag("cancel")
      }
      const handleWindowBlur = () => finishDrag("cancel")

      document.documentElement.setAttribute(RESIZE_AXIS_ATTRIBUTE, axis)
      handleEl.setAttribute(RESIZING_ATTRIBUTE, "true")

      document.addEventListener("pointermove", handlePointerMove)
      document.addEventListener("pointerup", handlePointerUp)
      document.addEventListener("pointercancel", handlePointerCancel)
      window.addEventListener("blur", handleWindowBlur)
      handleEl.addEventListener("lostpointercapture", handleLostPointerCapture)
      cancelActiveDragRef.current = () => finishDrag("cancel")
      handleEl.setPointerCapture?.(pointerId)
    },
    [axis, disabled, threshold]
  )

  return { onPointerDown }
}
