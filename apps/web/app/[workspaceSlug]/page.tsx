import { WorkspaceRootRedirect } from "./workspace-root-redirect";

export default async function WorkspaceRootPage({
  params,
}: {
  params: Promise<{ workspaceSlug: string }>;
}) {
  const { workspaceSlug } = await params;
  return <WorkspaceRootRedirect workspaceSlug={workspaceSlug} />;
}
