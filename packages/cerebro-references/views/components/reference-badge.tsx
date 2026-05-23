import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
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
  clickable = true,
}: {
  reference: IssueReference;
  className?: string;
  clickable?: boolean;
}) {
  const router = useNavigation();
  const paths = useWorkspacePaths();
  const renderer = getObjectRenderer(reference.object);
  const label = renderer.formatBadge(reference);
  if (!label) return null;

  const rawState =
    typeof reference.metadata?.state === "string"
      ? (reference.metadata.state as string).trim().toLowerCase()
      : "";
  const variant = STATE_VARIANT[rawState] ?? "outline";

  const badge = (
    <Badge variant={variant} className={cn("shrink-0 text-[10px]", className)}>
      {label}
    </Badge>
  );

  if (!clickable) return badge;

  return (
    <button
      type="button"
      title="Show issues with this reference"
      aria-label="Show issues with this reference"
      className="shrink-0 rounded-md outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      onClick={(event) => {
        event.preventDefault();
        event.stopPropagation();
        router.push(paths.referenceObject(reference.object, reference.ref_id));
      }}
    >
      {badge}
    </button>
  );
}
