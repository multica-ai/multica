"use client";

import { Badge } from "@multica/ui/components/ui/badge";

type BadgeVariant = "default" | "secondary" | "destructive" | "outline";

const LABELS: Record<string, string> = {
  pending: "Pending",
  approved: "Approved",
  rejected: "Rejected",
  delegated: "Delegated",
  expired: "Expired",
  cancelled: "Cancelled",
};

const VARIANTS: Record<string, BadgeVariant> = {
  pending: "default",
  approved: "secondary",
  rejected: "destructive",
  delegated: "outline",
  expired: "outline",
  cancelled: "outline",
};

// Renders an approval status. Unknown server-side values downgrade to a generic
// outline badge instead of crashing (enum-drift rule).
export function StatusBadge({ status }: { status: string }) {
  const variant = VARIANTS[status] ?? "outline";
  const label = LABELS[status] ?? status;
  return <Badge variant={variant}>{label}</Badge>;
}
