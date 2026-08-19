import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { authorityKeys, tagAuthorityClient } from "@multica/core/tag-authority";
import { Button } from "@multica/ui/components/ui/button";
import { AuthorityMembersPage } from "@multica/views/tag-authority";
import { toTagShareUrl } from "@/platform/paths";
import { TagWorkspaceRoute } from "./tag-workspace-route";

export function AuthorityMembersRoute({
  workspaceSlug,
}: {
  workspaceSlug: string;
}) {
  return (
    <TagWorkspaceRoute
      workspaceSlug={workspaceSlug}
      resetKeys={[workspaceSlug, "members"]}
    >
      <AuthorityMembersContent workspaceSlug={workspaceSlug} />
    </TagWorkspaceRoute>
  );
}

function AuthorityMembersContent({ workspaceSlug }: { workspaceSlug: string }) {
  const user = useAuthStore((state) => state.user);
  const workspaces = useQuery({
    queryKey: authorityKeys.workspaces(),
    queryFn: () => tagAuthorityClient.listWorkspaces(),
  });
  const workspace = workspaces.data?.find(
    (candidate) => candidate.slug === workspaceSlug,
  );

  if (workspaces.isLoading) {
    return <RouteStatus label="Loading VIBES Workspace authority" />;
  }
  if (workspaces.isError || !workspace || !user) {
    return (
      <RouteStatus label="Workspace authority is unavailable or access was removed">
        <Button className="min-h-11" onClick={() => void workspaces.refetch()}>
          Retry
        </Button>
      </RouteStatus>
    );
  }

  return (
    <AuthorityMembersPage
      workspace={workspace}
      currentUserId={user.id}
      buildJoinLinkUrl={(token) =>
        toTagShareUrl(
          window.location.origin,
          `/join?token=${encodeURIComponent(token)}`,
        )
      }
    />
  );
}

function RouteStatus({
  label,
  children,
}: {
  label: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="grid min-h-0 flex-1 place-items-center bg-background px-4 text-center">
      <div className="flex flex-col items-center gap-4">
        <p role="status" className="text-body text-muted-foreground">
          {label}
        </p>
        {children}
      </div>
    </div>
  );
}
