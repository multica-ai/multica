import { useEffect, useRef } from 'react'
import { useSubagentsStore, selectHasActiveRuns } from '../stores/subagents'

const ACTIVE_INTERVAL_MS = 2_000
const IDLE_INTERVAL_MS = 10_000

/**
 * Polls for subagent runs at an adaptive interval.
 * 2s when there are active (running/queued) runs, 10s otherwise.
 */
export function useSubagentPolling(agentId: string | null): void {
  const fetch = useSubagentsStore((s) => s.fetch)
  const runs = useSubagentsStore((s) => s.runs)
  const hasActive = selectHasActiveRuns(runs)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    if (!agentId) return

    // Fetch immediately
    fetch(agentId)

    const ms = hasActive ? ACTIVE_INTERVAL_MS : IDLE_INTERVAL_MS
    intervalRef.current = setInterval(() => fetch(agentId), ms)

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current)
        intervalRef.current = null
      }
    }
  }, [agentId, hasActive, fetch])
}
