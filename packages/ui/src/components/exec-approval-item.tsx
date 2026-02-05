"use client"

import { memo, useState, useEffect, useCallback } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  Tick01Icon,
  TickDouble01Icon,
  Cancel01Icon,
  CommandLineIcon,
} from "@hugeicons/core-free-icons"
import { cn } from "@multica/ui/lib/utils"
import { Button } from "@multica/ui/components/ui/button"

export interface ExecApprovalItemProps {
  command: string
  cwd?: string
  riskLevel: "safe" | "needs-review" | "dangerous"
  riskReasons: string[]
  expiresAtMs: number
  onDecision: (decision: "allow-once" | "allow-always" | "deny") => void
}

function useCountdown(expiresAtMs: number): number {
  const [remaining, setRemaining] = useState(() =>
    Math.max(0, Math.ceil((expiresAtMs - Date.now()) / 1000)),
  )

  useEffect(() => {
    const id = setInterval(() => {
      const next = Math.max(0, Math.ceil((expiresAtMs - Date.now()) / 1000))
      setRemaining(next)
      if (next <= 0) clearInterval(id)
    }, 1000)
    return () => clearInterval(id)
  }, [expiresAtMs])

  return remaining
}

export const ExecApprovalItem = memo(function ExecApprovalItem({
  command,
  cwd,
  riskLevel,
  riskReasons,
  expiresAtMs,
  onDecision,
}: ExecApprovalItemProps) {
  const remaining = useCountdown(expiresAtMs)
  const [decided, setDecided] = useState(false)

  const handleDecision = useCallback(
    (decision: "allow-once" | "allow-always" | "deny") => {
      if (decided) return
      setDecided(true)
      onDecision(decision)
    },
    [decided, onDecision],
  )

  const riskLabel =
    riskLevel === "dangerous"
      ? "Dangerous command"
      : riskLevel === "needs-review"
        ? "Needs review"
        : "Command approval"

  return (
    <div className="py-0.5 px-2.5 text-sm text-muted-foreground">
      <div className="rounded bg-muted/30 px-3 py-2.5 space-y-2.5">
        {/* Header: icon + risk label + countdown */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5">
            <HugeiconsIcon icon={CommandLineIcon} strokeWidth={2} className="size-3.5 shrink-0" />
            <span className="font-medium text-foreground">{riskLabel}</span>
          </div>
          {remaining > 0 && !decided && (
            <span className="text-xs text-muted-foreground/60 font-[tabular-nums]">
              {remaining}s
            </span>
          )}
        </div>

        {/* Command */}
        <div className="rounded bg-background/80 border border-border/50 px-2.5 py-1.5 font-mono text-xs text-foreground break-words">
          {command}
          {cwd && (
            <span className="block mt-1 text-muted-foreground/60 font-sans">
              in {cwd}
            </span>
          )}
        </div>

        {/* Risk reasons */}
        {riskReasons.length > 0 && (
          <div className="text-xs text-muted-foreground/60 space-y-0.5">
            {riskReasons.map((reason, i) => (
              <p key={i}>{reason}</p>
            ))}
          </div>
        )}

        {/* Actions */}
        {!decided && remaining > 0 ? (
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              className="h-7 text-xs gap-1.5 px-2.5"
              onClick={() => handleDecision("allow-once")}
            >
              <HugeiconsIcon icon={Tick01Icon} strokeWidth={2} className="size-3.5" />
              Allow
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="h-7 text-xs gap-1.5 px-2.5"
              onClick={() => handleDecision("allow-always")}
            >
              <HugeiconsIcon icon={TickDouble01Icon} strokeWidth={2} className="size-3.5" />
              Always
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="h-7 text-xs gap-1.5 px-2.5"
              onClick={() => handleDecision("deny")}
            >
              <HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} className="size-3.5" />
              Deny
            </Button>
          </div>
        ) : (
          <p className={cn(
            "text-xs",
            decided ? "text-muted-foreground" : "text-muted-foreground/60",
          )}>
            {decided ? "Decision sent" : "Expired"}
          </p>
        )}
      </div>
    </div>
  )
})
