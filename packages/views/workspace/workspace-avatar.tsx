import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { cn } from "@multica/ui/lib/utils";

const sizeMap = {
  sm: "h-5 w-5 text-caption",
  md: "h-7 w-7 text-caption",
  lg: "h-9 w-9 text-body",
} as const;

/**
 * The shape when no theme sets `--avatar-radius`. A theme's value wins
 * everywhere, so every workspace avatar takes the theme's avatar shape.
 */
const radiusMap = {
  circle: "rounded-avatar",
  lg: "rounded-[var(--avatar-radius,var(--radius-lg))]",
  md: "rounded-[var(--avatar-radius,var(--radius-md))]",
  xs: "rounded-[var(--avatar-radius,var(--radius-xs))]",
} as const;

interface WorkspaceAvatarProps {
  name: string;
  avatarUrl?: string | null;
  size?: keyof typeof sizeMap;
  radius?: keyof typeof radiusMap;
  className?: string;
}

function WorkspaceAvatar({ name, avatarUrl, size = "sm", radius = "circle", className }: WorkspaceAvatarProps) {
  const resolvedUrl = resolvePublicFileUrl(avatarUrl);
  // The radius goes last so a call-site `className` can't override the shape;
  // pick a fallback with `radius` instead.
  if (resolvedUrl) {
    return (
      <img
        src={resolvedUrl}
        alt={name}
        className={cn("inline-block shrink-0 border object-cover", sizeMap[size], className, radiusMap[radius])}
      />
    );
  }
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center justify-center border bg-muted font-semibold text-muted-foreground",
        sizeMap[size],
        className,
        radiusMap[radius]
      )}
    >
      {name.charAt(0).toUpperCase()}
    </span>
  );
}

export { WorkspaceAvatar, type WorkspaceAvatarProps };
