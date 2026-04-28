import {
  Clock,
  CheckCircle2,
  XCircle,
  Loader2,
  Play,
} from "lucide-react";

export const taskStatusConfig: Record<string, { label: string; icon: typeof CheckCircle2; color: string }> = {
  queued: { label: "Queued", icon: Clock, color: "text-muted-foreground" },
  dispatched: { label: "Dispatched", icon: Play, color: "text-info" },
  running: { label: "Running", icon: Loader2, color: "text-success" },
  completed: { label: "Completed", icon: CheckCircle2, color: "text-success" },
  failed: { label: "Failed", icon: XCircle, color: "text-destructive" },
  cancelled: { label: "Cancelled", icon: XCircle, color: "text-muted-foreground" },
};
