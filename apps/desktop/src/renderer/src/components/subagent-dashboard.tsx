import { useState, useEffect } from 'react'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from '@multica/ui/components/ui/sheet'
import { Badge } from '@multica/ui/components/ui/badge'

interface SubagentDashboardProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  runs: SubagentRunInfo[]
}

const STATUS_CONFIG: Record<SubagentRunInfo['status'], { label: string; variant: 'default' | 'secondary' | 'destructive' | 'outline' }> = {
  running: { label: 'Running', variant: 'default' },
  queued: { label: 'Queued', variant: 'secondary' },
  ok: { label: 'Completed', variant: 'outline' },
  error: { label: 'Error', variant: 'destructive' },
  timeout: { label: 'Timeout', variant: 'destructive' },
  unknown: { label: 'Unknown', variant: 'secondary' },
}

function formatElapsed(startMs: number, endMs?: number): string {
  const elapsed = (endMs ?? Date.now()) - startMs
  const seconds = Math.floor(elapsed / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  const remainSec = seconds % 60
  if (minutes < 60) return `${minutes}m ${remainSec}s`
  const hours = Math.floor(minutes / 60)
  const remainMin = minutes % 60
  return `${hours}h ${remainMin}m`
}

function RunCard({ run }: { run: SubagentRunInfo }) {
  const config = STATUS_CONFIG[run.status]
  const isActive = run.status === 'running' || run.status === 'queued'
  const [, setTick] = useState(0)

  // Tick every 1s for running agents to update elapsed time
  useEffect(() => {
    if (!isActive) return
    const timer = setInterval(() => setTick((t) => t + 1), 1000)
    return () => clearInterval(timer)
  }, [isActive])

  return (
    <div className="rounded-lg border bg-card p-3 space-y-2">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium truncate">
            {run.label || run.task.slice(0, 80)}
          </p>
          {run.label && (
            <p className="text-xs text-muted-foreground truncate mt-0.5">
              {run.task.slice(0, 120)}
            </p>
          )}
        </div>
        <Badge variant={config.variant} className="shrink-0">
          {config.label}
        </Badge>
      </div>

      <div className="flex items-center gap-3 text-xs text-muted-foreground">
        {run.startedAt && (
          <span>{formatElapsed(run.startedAt, run.endedAt)}</span>
        )}
        {run.groupLabel && (
          <span className="bg-muted px-1.5 py-0.5 rounded text-[10px]">
            {run.groupLabel}
          </span>
        )}
      </div>

      {run.error && (
        <p className="text-xs text-destructive bg-destructive/5 rounded px-2 py-1 font-mono">
          {run.error}
        </p>
      )}

      {run.findings && !run.error && (
        <p className="text-xs text-muted-foreground bg-muted/50 rounded px-2 py-1.5 font-mono whitespace-pre-wrap line-clamp-4">
          {run.findings.slice(0, 200)}
        </p>
      )}
    </div>
  )
}

export function SubagentDashboard({ open, onOpenChange, runs }: SubagentDashboardProps) {
  // Sort: active first (running, queued), then by createdAt desc
  const sorted = [...runs].sort((a, b) => {
    const aActive = a.status === 'running' || a.status === 'queued' ? 0 : 1
    const bActive = b.status === 'running' || b.status === 'queued' ? 0 : 1
    if (aActive !== bActive) return aActive - bActive
    return b.createdAt - a.createdAt
  })

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="sm:max-w-md">
        <SheetHeader>
          <SheetTitle>Subagents ({runs.length})</SheetTitle>
          <SheetDescription>Child agents spawned by the current session</SheetDescription>
        </SheetHeader>
        <div className="flex-1 min-h-0 overflow-y-auto px-4 pb-4 space-y-2">
          {sorted.length === 0 ? (
            <p className="text-sm text-muted-foreground text-center py-8">
              No subagents yet
            </p>
          ) : (
            sorted.map((run) => <RunCard key={run.runId} run={run} />)
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
