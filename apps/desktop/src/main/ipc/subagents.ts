/**
 * Subagent IPC handlers for Electron main process.
 *
 * Exposes subagent registry data to the renderer process
 * for the Subagent Dashboard UI.
 */
import { ipcMain } from 'electron'
import { listSubagentRuns, getSubagentGroup } from '@multica/core'
import type { SubagentRunRecord } from '@multica/core'

/** Serializable DTO for renderer consumption */
export interface SubagentRunInfo {
  runId: string
  label: string | undefined
  task: string
  status: 'queued' | 'running' | 'ok' | 'error' | 'timeout' | 'unknown'
  groupId: string | undefined
  groupLabel: string | undefined
  startedAt: number | undefined
  endedAt: number | undefined
  createdAt: number
  findings: string | undefined
  error: string | undefined
}

function deriveStatus(record: SubagentRunRecord): SubagentRunInfo['status'] {
  if (!record.startedAt) return 'queued'
  if (!record.endedAt) return 'running'
  return record.outcome?.status ?? 'unknown'
}

function toDTO(record: SubagentRunRecord): SubagentRunInfo {
  const group = record.groupId ? getSubagentGroup(record.groupId) : undefined
  return {
    runId: record.runId,
    label: record.label,
    task: record.task,
    status: deriveStatus(record),
    groupId: record.groupId,
    groupLabel: group?.label,
    startedAt: record.startedAt,
    endedAt: record.endedAt,
    createdAt: record.createdAt,
    findings: record.findings ? record.findings.slice(0, 500) : undefined,
    error: record.outcome?.error,
  }
}

/** Hide completed runs after 5 minutes */
const COMPLETED_RETENTION_MS = 5 * 60 * 1000

/**
 * Register all Subagent-related IPC handlers.
 */
export function registerSubagentsIpcHandlers(): void {
  ipcMain.handle('subagents:list', async (_event, requesterSessionId: string) => {
    const now = Date.now()
    const runs = listSubagentRuns(requesterSessionId)
    return runs
      .filter((r) => !r.endedAt || now - r.endedAt < COMPLETED_RETENTION_MS)
      .map(toDTO)
  })
}
