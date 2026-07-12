import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { cn } from "@multica/ui/lib/utils";

const sizeMap = {
  sm: "h-5 w-5 text-xs rounded-full",
  md: "h-7 w-7 text-xs rounded-full",
  lg: "h-9 w-9 text-sm rounded-full",
} as const;

interface WorkspaceAvatarProps {
  name: string;
  avatarUrl?: string | null;
  size?: keyof typeof sizeMap;
  className?: string;
}

function WorkspaceAvatar({ name, avatarUrl, size = "sm", className }: WorkspaceAvatarProps) {
  const resolvedUrl = resolvePublicFileUrl(avatarUrl);
  if (resolvedUrl) {
    return (
      <img
        src={resolvedUrl}
        alt={name}
        className={cn("inline-block shrink-0 border object-cover", sizeMap[size], className)}
      />
    );
  }
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center justify-center border bg-muted font-semibold text-muted-foreground",
        sizeMap[size],
        className
      )}
    >
      {name.charAt(0).toUpperCase()}
    </span>
  );
}

export { WorkspaceAvatar, type WorkspaceAvatarProps };
