import { redirect } from "next/navigation";

export default async function NewAgentAiSessionRoute({
  params,
}: {
  params: Promise<{ workspaceSlug: string; sessionId: string }>;
}) {
  const { workspaceSlug } = await params;
  redirect(`/${encodeURIComponent(workspaceSlug)}/agents`);
}
