import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";
import {
  getObjectRenderer,
  type IssueReference,
} from "@multica/cerebro-references/core";

// Compact badge for list/inline contexts. Resolves the object renderer's
// state badge (e.g. github_pr → "Open"/"Merged") and tints it accordingly.
// Renders nothing when the renderer has no badge for this reference — the
// fallback renderer returns null, so unknown object kinds simply omit the
// badge instead of breaking.

const STATE_VARIANT: Record<string, "default" | "secondary" | "outline" | "destructive"> = {
  open: "default",
  merged: "secondary",
  draft: "outline",
  closed: "destructive",
};

export function ReferenceBadge({
  reference,
  className,
}: {
  reference: IssueReference;
  className?: string;
}) {
  const renderer = getObjectRenderer(reference.object);
  const label = renderer.formatBadge(reference);
  if (!label) return null;

  const rawState =
    typeof reference.metadata?.state === "string"
      ? (reference.metadata.state as string).trim().toLowerCase()
      : "";
  const variant = STATE_VARIANT[rawState] ?? "outline";

  return (
    <Badge variant={variant} className={cn("shrink-0 text-[10px]", className)}>
      {label}
    </Badge>
  );
}
