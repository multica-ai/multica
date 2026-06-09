// CEREBRO-PATCH(workspace-avatar-logo): FIR-2580 — render the uploaded workspace
// logo when avatarUrl is set, falling back to the letter tile otherwise.
import { useEffect, useState, type ReactNode } from "react";
import { cn } from "@multica/ui/lib/utils";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";

const sizeMap = {
  sm: "h-5 w-5 text-xs rounded",
  md: "h-7 w-7 text-xs rounded-md",
  lg: "h-9 w-9 text-sm rounded-md",
} as const;

interface WorkspaceAvatarProps {
  name: string;
  avatarUrl?: string | null;
  size?: keyof typeof sizeMap;
  className?: string;
}

function WorkspaceAvatar({ name, size = "sm", className, avatarUrl }: WorkspaceAvatarProps) {
  // Only resolve when a logo is actually set — skips the api.getBaseUrl() call
  // for the common letter-fallback case.
  const resolved = avatarUrl ? resolvePublicFileUrl(avatarUrl) : null;
  const [imgError, setImgError] = useState(false);
  useEffect(() => {
    setImgError(false);
  }, [resolved]);

  const showImg = !!resolved && !imgError;

  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center justify-center overflow-hidden border bg-muted font-semibold text-muted-foreground",
        sizeMap[size],
        className
      )}
    >
      {showImg ? (
        <img
          src={resolved}
          alt={name}
          className="h-full w-full object-cover"
          onError={() => setImgError(true)}
        />
      ) : (
        name.charAt(0).toUpperCase()
      )}
    </span>
  );
}

// CEREBRO-PATCH(workspace-avatar-logo): FIR-2580 — for breadcrumbs whose default
// is a static icon (issues, inbox), swap in the active workspace's logo only
// when the feature flag is on AND a logo is set; otherwise render the existing
// icon untouched so the flag-off experience is identical to upstream.
function WorkspaceBreadcrumbLogo({ fallback }: { fallback: ReactNode }) {
  const enabled = useFeatureFlag("cerebro_workspace_logo");
  const workspace = useCurrentWorkspace();
  if (!enabled || !workspace?.avatar_url) return <>{fallback}</>;
  return <WorkspaceAvatar name={workspace.name} size="sm" avatarUrl={workspace.avatar_url} />;
}

export { WorkspaceAvatar, WorkspaceBreadcrumbLogo, type WorkspaceAvatarProps };
