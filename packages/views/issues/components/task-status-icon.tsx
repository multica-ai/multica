import { Ban, CheckCircle2, XCircle } from "lucide-react";
import type { AgentTask } from "@multica/core/types";

// Terminal-status glyph for one agent run. Shared by the execution log rows
// and the usage-detail dialog so a failed run carries the same mark wherever
// it is listed. Non-terminal statuses render nothing: those rows carry a live
// timer or a status word instead, and a glyph next to either reads as a
// second, contradictory status.
export function TaskStatusIcon({ status }: { status: AgentTask["status"] }) {
  switch (status) {
    case "completed":
      return <CheckCircle2 aria-hidden="true" className="h-3.5 w-3.5 shrink-0 text-success" />;
    case "failed":
      return <XCircle aria-hidden="true" className="h-3.5 w-3.5 shrink-0 text-destructive" />;
    case "cancelled":
      return <Ban aria-hidden="true" className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />;
    default:
      return null;
  }
}
