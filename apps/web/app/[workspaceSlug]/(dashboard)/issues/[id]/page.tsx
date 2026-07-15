import { redirect } from "next/navigation";

// Legacy issue-detail path. The canonical route moved to /issue/:id
// (identifier-first, Linear-style); old bookmarks and persisted tabs land
// here and get forwarded.
export default async function LegacyIssueDetailRedirect({
  params,
}: {
  params: Promise<{ workspaceSlug: string; id: string }>;
}) {
  const { workspaceSlug, id } = await params;
  redirect(`/${encodeURIComponent(workspaceSlug)}/issue/${encodeURIComponent(id)}`);
}
