import { create } from 'zustand'

interface SubagentsStore {
  runs: SubagentRunInfo[]
  fetch: (requesterSessionId: string) => Promise<void>
}

export const useSubagentsStore = create<SubagentsStore>()((set) => ({
  runs: [],

  fetch: async (requesterSessionId: string) => {
    try {
      const result = await window.electronAPI.subagents.list(requesterSessionId)
      if (Array.isArray(result)) {
        set({ runs: result })
      }
    } catch (err) {
      console.error('[SubagentsStore] Failed to fetch:', err)
    }
  },
}))

export function selectRunningCount(runs: SubagentRunInfo[]): number {
  return runs.filter((r) => r.status === 'running' || r.status === 'queued').length
}

export function selectHasActiveRuns(runs: SubagentRunInfo[]): boolean {
  return runs.some((r) => r.status === 'running' || r.status === 'queued')
}
