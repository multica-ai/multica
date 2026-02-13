import { useState, useEffect, useRef } from 'react'

/** Auto-dismiss delay after all runs complete (ms) */
const DISMISS_DELAY_MS = 30_000

interface SubagentStatusBarProps {
  runs: SubagentRunInfo[]
  onViewClick: () => void
}

export function SubagentStatusBar({ runs, onViewClick }: SubagentStatusBarProps) {
  const [dismissed, setDismissed] = useState(false)
  const prevHadActiveRef = useRef(false)

  const running = runs.filter((r) => r.status === 'running' || r.status === 'queued').length
  const completed = runs.filter((r) => r.status !== 'running' && r.status !== 'queued').length
  const hasActive = running > 0

  // Auto-dismiss after all runs complete
  useEffect(() => {
    if (hasActive) {
      // Reset dismissed state when new active runs appear
      prevHadActiveRef.current = true
      setDismissed(false)
      return
    }

    // Only auto-dismiss if we previously had active runs (transition to all-complete)
    if (!prevHadActiveRef.current || runs.length === 0) return

    const timer = setTimeout(() => setDismissed(true), DISMISS_DELAY_MS)
    return () => clearTimeout(timer)
  }, [hasActive, runs.length])

  if (runs.length === 0 || dismissed) return null

  let statusText: string
  if (running > 0 && completed > 0) {
    statusText = `${running} running, ${completed} completed`
  } else if (running > 0) {
    statusText = `${running} subagent${running > 1 ? 's' : ''} running`
  } else {
    statusText = `${completed} completed`
  }

  return (
    <div className="container px-4">
      <div className="flex items-center justify-between h-8 px-3 rounded-lg bg-muted/50 border text-xs text-muted-foreground">
        <div className="flex items-center gap-2">
          {running > 0 && (
            <span className="relative flex h-2 w-2">
              <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-blue-400 opacity-75" />
              <span className="relative inline-flex rounded-full h-2 w-2 bg-blue-500" />
            </span>
          )}
          <span>{statusText}</span>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={onViewClick}
            className="text-xs font-medium text-foreground/70 hover:text-foreground transition-colors"
          >
            View
          </button>
          {!hasActive && (
            <button
              onClick={() => setDismissed(true)}
              className="text-xs text-muted-foreground/50 hover:text-muted-foreground transition-colors"
            >
              Dismiss
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
