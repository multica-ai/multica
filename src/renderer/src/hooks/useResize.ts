/**
 * Hook for drag-to-resize functionality
 */
import { useCallback, useEffect, useRef, useState } from 'react'

interface UseResizeOptions {
  /** Current width */
  width: number
  /** Minimum width */
  minWidth: number
  /** Maximum width */
  maxWidth: number
  /** Callback when width changes */
  onWidthChange: (width: number) => void
  /** Direction of resize: 'left' means drag from left edge, 'right' means drag from right edge */
  direction: 'left' | 'right'
}

interface UseResizeReturn {
  /** Whether currently resizing */
  isResizing: boolean
  /** Props to spread on the resize handle element */
  handleProps: {
    onMouseDown: (e: React.MouseEvent) => void
    style: React.CSSProperties
  }
}

export function useResize({
  width,
  minWidth,
  maxWidth,
  onWidthChange,
  direction
}: UseResizeOptions): UseResizeReturn {
  const [isResizing, setIsResizing] = useState(false)
  const startXRef = useRef(0)
  const startWidthRef = useRef(0)

  const handleMouseDown = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault()
      setIsResizing(true)
      startXRef.current = e.clientX
      startWidthRef.current = width
    },
    [width]
  )

  useEffect(() => {
    if (!isResizing) return

    const handleMouseMove = (e: MouseEvent) => {
      const delta = e.clientX - startXRef.current
      // For left sidebar, dragging right increases width
      // For right panel, dragging left increases width
      const newWidth =
        direction === 'right' ? startWidthRef.current + delta : startWidthRef.current - delta
      const clampedWidth = Math.max(minWidth, Math.min(maxWidth, newWidth))
      onWidthChange(clampedWidth)
    }

    const handleMouseUp = () => {
      setIsResizing(false)
    }

    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)

    // Add cursor style to body during resize
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'

    return () => {
      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }
  }, [isResizing, minWidth, maxWidth, onWidthChange, direction])

  return {
    isResizing,
    handleProps: {
      onMouseDown: handleMouseDown,
      style: { cursor: 'col-resize' }
    }
  }
}
