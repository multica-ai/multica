import { redirect } from "next/navigation";

export default async function NewAgentRoute({
  params,
}: {
  params: Promise<{ workspaceSlug: string }>;
}) {
  const { workspaceSlug } = await params;
  redirect(`/${encodeURIComponent(workspaceSlug)}/agents/new/manual`);
}
