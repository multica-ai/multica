import {
  AlertTriangle,
  Ban,
  CheckCircle2,
  Clock,
  HelpCircle,
  Loader2,
  ShieldOff,
  XCircle,
} from "@multica/ui/icons";
import { messageDeliveryStatusKey, type MessageDeliveryStatusKey } from "./copy";

export type DeliveryStatusVisual = {
  color: string;
  icon: typeof CheckCircle2;
  spin?: boolean;
};

// Semantic status tokens only (CLAUDE.md UI rules): the icon carries the
// state color; 12px label text stays text-foreground for contrast.
const STATUS_VISUAL: Record<MessageDeliveryStatusKey, DeliveryStatusVisual> = {
  queued: { color: "text-info", icon: Clock },
  sending: { color: "text-info", icon: Loader2, spin: true },
  sent: { color: "text-success", icon: CheckCircle2 },
  failed: { color: "text-destructive", icon: XCircle },
  // The send may have landed; only a manual verify-and-retry resolves it.
  uncertain: { color: "text-warning", icon: AlertTriangle },
  cancelled: { color: "text-muted-foreground", icon: Ban },
  // Condition mismatch / unresolved historical source — deliberately muted,
  // it is a recorded non-send, not a bug.
  suppressed: { color: "text-muted-foreground", icon: ShieldOff },
  unknown: { color: "text-muted-foreground", icon: HelpCircle },
};

/** Unknown/future server statuses degrade to the generic "unknown" visual. */
export function deliveryStatusVisual(status: string): DeliveryStatusVisual {
  return STATUS_VISUAL[messageDeliveryStatusKey(status)];
}
