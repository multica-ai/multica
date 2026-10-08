import {
  Clock,
  CheckCircle2,
  XCircle,
  Loader2,
  Play,
} from "lucide-react";

// Visual-only mapping for a task status: icon + tone. Deliberately carries no
// `label` — the human-readable status word comes from `taskStatusLabel(status, t)`
// against the `agents` locale bundle. A `label` here would be a second,
// untranslatable source of the same string sitting inside a translated package,
// which is exactly the drift #7411 reported.
export const taskStatusConfig: Record<string, { icon: typeof CheckCircle2; color: string }> = {
  queued: { icon: Clock, color: "text-muted-foreground" },
  dispatched: { icon: Play, color: "text-info" },
  running: { icon: Loader2, color: "text-brand" },
  completed: { icon: CheckCircle2, color: "text-success" },
  failed: { icon: XCircle, color: "text-destructive" },
  cancelled: { icon: XCircle, color: "text-muted-foreground" },
};
