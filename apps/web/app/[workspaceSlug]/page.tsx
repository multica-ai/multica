import { redirect } from "next/navigation";
import { cookies, headers } from "next/headers";
import { startPagePath } from "@/lib/start-page-redirect";

export default async function WorkspaceRootPage({
  params,
}: {
  params: Promise<{ workspaceSlug: string }>;
}) {
  const { workspaceSlug } = await params;
  redirect(startPagePath(workspaceSlug, await cookies(), await headers()));
}
